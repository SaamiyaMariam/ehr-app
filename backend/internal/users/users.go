package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

func (h *Handler) ListClinicians(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			u.id,
			u.first_name,
			u.last_name,
			COALESCE(u.preferred_name, ''),
			u.username,
			u.email
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id
		WHERE r.key = 'clinician'
		  AND u.is_active = TRUE
		ORDER BY u.last_name, u.first_name
		`,
	)

	if err != nil {
		writeError(
			w,
			http.StatusInternalServerError,
			"could not load clinicians",
		)
		return
	}
	defer rows.Close()

	result := make([]UserListItem, 0)

	for rows.Next() {
		var user UserListItem

		if err := rows.Scan(
			&user.ID,
			&user.FirstName,
			&user.LastName,
			&user.PreferredName,
			&user.Username,
			&user.Email,
		); err != nil {
			writeError(
				w,
				http.StatusInternalServerError,
				"could not load clinicians",
			)
			return
		}

		result = append(result, user)
	}

	writeJSON(w, http.StatusOK, result)
}

type User struct {
	ID string `json:"id"`

	UserComments string `json:"user_comments"`

	FirstName  string `json:"first_name"`
	MiddleName string `json:"middle_name"`
	LastName   string `json:"last_name"`
	Suffix     string `json:"suffix"`

	PreferredName string `json:"preferred_name"`
	Pronouns      string `json:"pronouns"`

	Username    string   `json:"username"`
	DateOfBirth string   `json:"date_of_birth"`
	Languages   []string `json:"languages"`

	Email                  string `json:"email"`
	MobilePhone            string `json:"mobile_phone"`
	CanReceiveTextMessages bool   `json:"can_receive_text_messages"`
	WorkPhone              string `json:"work_phone"`
	HomePhone              string `json:"home_phone"`

	Address1 string `json:"address_1"`
	Address2 string `json:"address_2"`
	Zip      string `json:"zip"`
	City     string `json:"city"`
	State    string `json:"state"`
}

type CreateUserRequest struct {
	User
	Password string `json:"password"`
}

type UserListItem struct {
	ID            string `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	PreferredName string `json:"preferred_name"`
	Username      string `json:"username"`
	Email         string `json:"email"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func cleanUser(user *User) {
	user.UserComments = strings.TrimSpace(user.UserComments)

	user.FirstName = strings.TrimSpace(user.FirstName)
	user.MiddleName = strings.TrimSpace(user.MiddleName)
	user.LastName = strings.TrimSpace(user.LastName)
	user.Suffix = strings.TrimSpace(user.Suffix)

	user.PreferredName = strings.TrimSpace(user.PreferredName)
	user.Pronouns = strings.TrimSpace(user.Pronouns)

	user.Username = strings.TrimSpace(user.Username)
	user.DateOfBirth = strings.TrimSpace(user.DateOfBirth)

	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.MobilePhone = strings.TrimSpace(user.MobilePhone)
	user.WorkPhone = strings.TrimSpace(user.WorkPhone)
	user.HomePhone = strings.TrimSpace(user.HomePhone)

	user.Address1 = strings.TrimSpace(user.Address1)
	user.Address2 = strings.TrimSpace(user.Address2)
	user.Zip = strings.TrimSpace(user.Zip)
	user.City = strings.TrimSpace(user.City)
	user.State = strings.TrimSpace(user.State)

	// A missing list must be stored as empty, not NULL.
	if user.Languages == nil {
		user.Languages = []string{}
	}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanUser(&req.User)

	if req.FirstName == "" ||
		req.LastName == "" ||
		req.Username == "" ||
		req.Email == "" ||
		req.Password == "" {
		writeError(
			w,
			http.StatusBadRequest,
			"first name, last name, username, email, and password are required",
		)
		return
	}

	if len(req.Password) < 8 {
		writeError(
			w,
			http.StatusBadRequest,
			"password must be at least 8 characters",
		)
		return
	}

	var exists bool

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE LOWER(email) = LOWER($1)
			   OR LOWER(username) = LOWER($2)
		)
		`,
		req.Email,
		req.Username,
	).Scan(&exists)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if exists {
		writeError(
			w,
			http.StatusConflict,
			"email or username already exists",
		)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}

	err = h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO users (
			user_comments,
			first_name,
			middle_name,
			last_name,
			suffix,
			preferred_name,
			pronouns,
			username,
			date_of_birth,
			languages,
			email,
			mobile_phone,
			can_receive_text_messages,
			work_phone,
			home_phone,
			address_1,
			address_2,
			zip,
			city,
			state,
			password_hash
		)
		VALUES (
			NULLIF($1, ''),
			$2,
			NULLIF($3, ''),
			$4,
			NULLIF($5, ''),
			NULLIF($6, ''),
			NULLIF($7, ''),
			$8,
			NULLIF($9, '')::date,
			$10,
			$11,
			NULLIF($12, ''),
			$13,
			NULLIF($14, ''),
			NULLIF($15, ''),
			NULLIF($16, ''),
			NULLIF($17, ''),
			NULLIF($18, ''),
			NULLIF($19, ''),
			NULLIF($20, ''),
			$21
		)
		RETURNING id
		`,
		req.UserComments,
		req.FirstName,
		req.MiddleName,
		req.LastName,
		req.Suffix,
		req.PreferredName,
		req.Pronouns,
		req.Username,
		req.DateOfBirth,
		req.Languages,
		req.Email,
		req.MobilePhone,
		req.CanReceiveTextMessages,
		req.WorkPhone,
		req.HomePhone,
		req.Address1,
		req.Address2,
		req.Zip,
		req.City,
		req.State,
		string(passwordHash),
	).Scan(&req.ID)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create user")
		return
	}

	writeJSON(w, http.StatusCreated, req.User)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			id,
			first_name,
			last_name,
			COALESCE(preferred_name, ''),
			username,
			email
		FROM users
		ORDER BY last_name, first_name
		`,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load users")
		return
	}
	defer rows.Close()

	result := make([]UserListItem, 0)

	for rows.Next() {
		var user UserListItem

		if err := rows.Scan(
			&user.ID,
			&user.FirstName,
			&user.LastName,
			&user.PreferredName,
			&user.Username,
			&user.Email,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load users")
			return
		}

		result = append(result, user)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var user User

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			COALESCE(user_comments, ''),
			first_name,
			COALESCE(middle_name, ''),
			last_name,
			COALESCE(suffix, ''),
			COALESCE(preferred_name, ''),
			COALESCE(pronouns, ''),
			username,
			COALESCE(TO_CHAR(date_of_birth, 'YYYY-MM-DD'), ''),
			languages,
			email,
			COALESCE(mobile_phone, ''),
			can_receive_text_messages,
			COALESCE(work_phone, ''),
			COALESCE(home_phone, ''),
			COALESCE(address_1, ''),
			COALESCE(address_2, ''),
			COALESCE(zip, ''),
			COALESCE(city, ''),
			COALESCE(state, '')
		FROM users
		WHERE id = $1
		`,
		id,
	).Scan(
		&user.ID,
		&user.UserComments,
		&user.FirstName,
		&user.MiddleName,
		&user.LastName,
		&user.Suffix,
		&user.PreferredName,
		&user.Pronouns,
		&user.Username,
		&user.DateOfBirth,
		&user.Languages,
		&user.Email,
		&user.MobilePhone,
		&user.CanReceiveTextMessages,
		&user.WorkPhone,
		&user.HomePhone,
		&user.Address1,
		&user.Address2,
		&user.Zip,
		&user.City,
		&user.State,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var user User

	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanUser(&user)

	if user.FirstName == "" ||
		user.LastName == "" ||
		user.Username == "" ||
		user.Email == "" {
		writeError(
			w,
			http.StatusBadRequest,
			"first name, last name, username, and email are required",
		)
		return
	}

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE users
		SET
			user_comments = NULLIF($1, ''),
			first_name = $2,
			middle_name = NULLIF($3, ''),
			last_name = $4,
			suffix = NULLIF($5, ''),
			preferred_name = NULLIF($6, ''),
			pronouns = NULLIF($7, ''),
			username = $8,
			date_of_birth = NULLIF($9, '')::date,
			languages = $10,
			email = $11,
			mobile_phone = NULLIF($12, ''),
			can_receive_text_messages = $13,
			work_phone = NULLIF($14, ''),
			home_phone = NULLIF($15, ''),
			address_1 = NULLIF($16, ''),
			address_2 = NULLIF($17, ''),
			zip = NULLIF($18, ''),
			city = NULLIF($19, ''),
			state = NULLIF($20, ''),
			updated_at = NOW()
		WHERE id = $21
		`,
		user.UserComments,
		user.FirstName,
		user.MiddleName,
		user.LastName,
		user.Suffix,
		user.PreferredName,
		user.Pronouns,
		user.Username,
		user.DateOfBirth,
		user.Languages,
		user.Email,
		user.MobilePhone,
		user.CanReceiveTextMessages,
		user.WorkPhone,
		user.HomePhone,
		user.Address1,
		user.Address2,
		user.Zip,
		user.City,
		user.State,
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update user")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	user.ID = id

	writeJSON(w, http.StatusOK, user)
}
