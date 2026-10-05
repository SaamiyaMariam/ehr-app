package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	db        *pgxpool.Pool
	jwtSecret []byte
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
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !isActive {
		writeError(w, http.StatusUnauthorized, "account is inactive")
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(req.Password),
	); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

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
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}

				return h.jwtSecret, nil
			},
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

		ctx := context.WithValue(r.Context(), userIDKey, userID)

		next(w, r.WithContext(ctx))
	}
}
