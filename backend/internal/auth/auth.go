package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	db        *pgxpool.Pool
	jwtSecret []byte
	limiter   *loginLimiter
}

// loginLimiter slows password guessing: after maxLoginFailures failed
// sign-ins for one email within loginWindow, further attempts get 429 until
// the window passes. It is in-memory (per API process); a shared store would
// be needed behind several instances.
type loginLimiter struct {
	mu        sync.Mutex
	failures  map[string]*loginAttempt
	lastPrune time.Time
}

type loginAttempt struct {
	count int
	first time.Time
}

const (
	maxLoginFailures = 8
	loginWindow      = 15 * time.Minute
	maxTrackedLogins = 10000
	maxEmailLength   = 254 // RFC 5321 path limit
	maxPasswordBytes = 72  // bcrypt ignores everything beyond this
)

// limiterKey bounds what an attacker can make the limiter remember: however
// long the submitted email is, an entry is one fixed-size digest.
func limiterKey(email string) string {
	sum := sha256.Sum256([]byte(email))
	return hex.EncodeToString(sum[:])
}

// dummyPasswordHash is compared against when the email is unknown so that
// unknown and known accounts cost the same time to reject.
var dummyPasswordHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	return h
}()

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: map[string]*loginAttempt{}}
}

func (l *loginLimiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	a := l.failures[key]
	if a == nil {
		return false
	}

	if now.Sub(a.first) > loginWindow {
		delete(l.failures, key)
		return false
	}

	return a.count >= maxLoginFailures
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Keep the map bounded: drop expired entries, and if it is still full,
	// make room by evicting some other entry.
	if len(l.failures) >= maxTrackedLogins/2 && now.Sub(l.lastPrune) > time.Minute {
		l.lastPrune = now

		for k, a := range l.failures {
			if now.Sub(a.first) > loginWindow {
				delete(l.failures, k)
			}
		}
	}

	if len(l.failures) >= maxTrackedLogins {
		for k := range l.failures {
			if len(l.failures) < maxTrackedLogins {
				break
			}
			if k != key {
				delete(l.failures, k)
			}
		}
	}

	a := l.failures[key]
	if a == nil || now.Sub(a.first) > loginWindow {
		l.failures[key] = &loginAttempt{count: 1, first: now}
		return
	}

	a.count++
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

type contextKey string

const userIDKey contextKey = "user_id"

// UserIDFromContext returns the authenticated user's ID set by RequireAuth.
func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(userIDKey).(string)
	return userID
}

func NewHandler(db *pgxpool.Pool, jwtSecret string) *Handler {
	return &Handler{
		db:        db,
		jwtSecret: []byte(jwtSecret),
		limiter:   newLoginLimiter(),
	}
}

func (h *Handler) RequireAnyRole(
	next http.HandlerFunc,
	roleKeys ...string,
) http.HandlerFunc {
	return h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(userIDKey).(string)
		if !ok || userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var allowed bool

		err := h.db.QueryRow(
			r.Context(),
			`
			SELECT EXISTS (
				SELECT 1
				FROM user_roles ur
				JOIN roles ro ON ro.id = ur.role_id
				WHERE ur.user_id = $1
				  AND ro.key = ANY($2)
			)
			`,
			userID,
			roleKeys,
		).Scan(&allowed)

		if err != nil {
			writeError(
				w,
				http.StatusInternalServerError,
				"could not verify user role",
			)
			return
		}

		if !allowed {
			writeError(
				w,
				http.StatusForbidden,
				"you do not have permission to perform this action",
			)
			return
		}

		next(w, r)
	})
}

type SignupRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Email     string `json:"email"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if req.FirstName == "" ||
		req.LastName == "" ||
		req.Username == "" ||
		req.Email == "" ||
		req.Password == "" {
		writeError(w, http.StatusBadRequest, "all fields are required")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	var exists bool

	err := h.db.QueryRow(
		r.Context(),
		`SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE LOWER(email) = LOWER($1)
			   OR LOWER(username) = LOWER($2)
		)`,
		req.Email,
		req.Username,
	).Scan(&exists)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if exists {
		writeError(w, http.StatusConflict, "email or username already exists")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	var user UserResponse

	err = h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO users (
			first_name,
			last_name,
			username,
			email,
			password_hash
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING
			id,
			first_name,
			last_name,
			username,
			email
		`,
		req.FirstName,
		req.LastName,
		req.Username,
		req.Email,
		string(passwordHash),
	).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Username,
		&user.Email,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	token, err := h.createToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{
		Token: token,
		User:  user,
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if len(req.Email) > maxEmailLength || len(req.Password) > maxPasswordBytes {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	key := limiterKey(req.Email)

	if h.limiter.blocked(key, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "too many failed sign-in attempts; try again in a few minutes")
		return
	}

	var user UserResponse
	var passwordHash string
	var isActive bool

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			first_name,
			last_name,
			username,
			email,
			password_hash,
			is_active
		FROM users
		WHERE LOWER(email) = LOWER($1)
		`,
		req.Email,
	).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Username,
		&user.Email,
		&passwordHash,
		&isActive,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
		h.limiter.fail(key, time.Now())
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(req.Password),
	); err != nil {
		h.limiter.fail(key, time.Now())
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// Only someone who proved the password learns the account is switched
	// off; a wrong password cannot be used to probe which accounts exist.
	if !isActive {
		writeError(w, http.StatusUnauthorized, "account is inactive")
		return
	}

	h.limiter.reset(key)

	token, err := h.createToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(userIDKey).(string)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var user UserResponse

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			first_name,
			last_name,
			username,
			email
		FROM users
		WHERE id = $1
		  AND is_active = TRUE
		`,
		userID,
	).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Username,
		&user.Email,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) createToken(userID string) (string, error) {
	now := time.Now()

	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(h.jwtSecret)
}

// RequireAuth accepts a valid, unexpired token that belongs to an existing,
// active user. A deactivated or deleted account loses access immediately
// even though its token has not expired.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "authorization token required")
			return
		}

		tokenString := strings.TrimSpace(
			strings.TrimPrefix(authHeader, "Bearer "),
		)

		token, err := jwt.Parse(
			tokenString,
			func(token *jwt.Token) (any, error) {
				return h.jwtSecret, nil
			},
			jwt.WithValidMethods([]string{"HS256"}),
			jwt.WithExpirationRequired(),
		)

		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		userID, err := claims.GetSubject()
		if err != nil || userID == "" {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		var active bool

		err = h.db.QueryRow(
			r.Context(),
			`SELECT is_active FROM users WHERE id::text = $1`,
			userID,
		).Scan(&active)

		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
			writeError(w, http.StatusUnauthorized, "account is inactive or no longer exists")
			return
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not verify session")
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)

		next(w, r.WithContext(ctx))
	}
}

// RequireStaff requires an authenticated user who has at least one role.
// Anyone can sign up, but an account sees no practice data until an
// administrator assigns it a role.
func (h *Handler) RequireStaff(next http.HandlerFunc) http.HandlerFunc {
	return h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(userIDKey).(string)

		var hasRole bool

		err := h.db.QueryRow(
			r.Context(),
			`SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id::text = $1)`,
			userID,
		).Scan(&hasRole)

		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not verify user role")
			return
		}

		if !hasRole {
			writeError(w, http.StatusForbidden, "your account has no role yet; ask a practice administrator to assign one")
			return
		}

		next(w, r)
	})
}

// RequireSelfOrAnyRole lets a user act on their own record (the {param} path
// value is their id) and otherwise requires one of the roles.
func (h *Handler) RequireSelfOrAnyRole(next http.HandlerFunc, param string, roleKeys ...string) http.HandlerFunc {
	withRole := h.RequireAnyRole(next, roleKeys...)

	return h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(userIDKey).(string)

		if userID != "" && strings.EqualFold(userID, r.PathValue(param)) {
			next(w, r)
			return
		}

		withRole(w, r)
	})
}
