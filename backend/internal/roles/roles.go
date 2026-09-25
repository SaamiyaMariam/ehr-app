package roles

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type Role struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type UpdateUserRolesRequest struct {
	Roles []string `json:"roles"`
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT key, name
		FROM roles
		ORDER BY id
		`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load roles")
		return
	}
	defer rows.Close()

	result := make([]Role, 0)

	for rows.Next() {
		var role Role

		if err := rows.Scan(&role.Key, &role.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load roles")
			return
		}

		result = append(result, role)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT r.key, r.name
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY r.id
		`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user roles")
		return
	}
	defer rows.Close()

	result := make([]Role, 0)

	for rows.Next() {
		var role Role

		if err := rows.Scan(&role.Key, &role.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load user roles")
			return
		}

		result = append(result, role)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	var req UpdateUserRolesRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update roles")
		return
	}
	defer tx.Rollback(r.Context())

	var userExists bool

	if err := tx.QueryRow(
		r.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`,
		userID,
	).Scan(&userExists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update roles")
		return
	}

	if !userExists {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`DELETE FROM user_roles WHERE user_id = $1`,
		userID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update roles")
		return
	}

	for _, roleKey := range req.Roles {
		commandTag, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1, id
			FROM roles
			WHERE key = $2
			ON CONFLICT DO NOTHING
			`,
			userID,
			roleKey,
		)

		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not update roles")
			return
		}

		if commandTag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest, "invalid role: "+roleKey)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update roles")
		return
	}

	h.GetUserRoles(w, r)
}
