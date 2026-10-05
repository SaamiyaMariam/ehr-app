package servicecodes

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type ServiceCode struct {
	ID string `json:"id"`

	Code        string `json:"code"`
	Description string `json:"description"`

	IsAddOn            bool `json:"is_add_on"`
	AllowMultipleUnits bool `json:"allow_multiple_units"`

	// Decimal amount as a string to avoid float rounding, e.g. "150.00".
	StandardRate string `json:"standard_rate"`

	IsActive bool `json:"is_active"`
}

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
	)

	// Matches NUMERIC(10, 2): non-negative, up to 8 integer digits and 2 decimals.
	moneyPattern = regexp.MustCompile(`^\d{1,8}(\.\d{1,2})?$`)
)

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

func cleanServiceCode(s *ServiceCode) {
	s.Code = strings.TrimSpace(s.Code)
	s.Description = strings.TrimSpace(s.Description)
	s.StandardRate = strings.TrimSpace(s.StandardRate)
}

// validateServiceCode returns a user-facing message, or "" when valid.
func validateServiceCode(s *ServiceCode) string {
	if s.Code == "" {
		return "service code is required"
	}

	if len(s.Code) > 50 {
		return "service code must be 50 characters or fewer"
	}

	if s.Description == "" {
		return "description is required"
	}

	if s.StandardRate != "" && !moneyPattern.MatchString(s.StandardRate) {
		return "standard rate must be a non-negative amount with at most 2 decimal places"
	}

	// Multiple units only apply to add-on codes.
	if !s.IsAddOn {
		s.AllowMultipleUnits = false
	}

	return ""
}

const selectServiceCode = `
	SELECT
		id,
		code,
		description,
		is_add_on,
		allow_multiple_units,
		COALESCE(standard_rate::text, ''),
		is_active
	FROM service_codes
`

func scanServiceCode(row pgx.Row, s *ServiceCode) error {
	return row.Scan(
		&s.ID,
		&s.Code,
		&s.Description,
		&s.IsAddOn,
		&s.AllowMultipleUnits,
		&s.StandardRate,
		&s.IsActive,
	)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		selectServiceCode+`ORDER BY is_active DESC, code, description`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load service codes")
		return
	}
	defer rows.Close()

	result := make([]ServiceCode, 0)

	for rows.Next() {
		var s ServiceCode

		if err := scanServiceCode(rows, &s); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load service codes")
			return
		}

		result = append(result, s)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load service codes")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var s ServiceCode

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanServiceCode(&s)

	if message := validateServiceCode(&s); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	err := scanServiceCode(
		h.db.QueryRow(
			r.Context(),
			`
			INSERT INTO service_codes (
				code,
				description,
				is_add_on,
				allow_multiple_units,
				standard_rate
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::numeric)
			RETURNING
				id,
				code,
				description,
				is_add_on,
				allow_multiple_units,
				COALESCE(standard_rate::text, ''),
				is_active
			`,
			s.Code,
			s.Description,
			s.IsAddOn,
			s.AllowMultipleUnits,
			s.StandardRate,
		),
		&s,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create service code")
		return
	}

	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	var s ServiceCode

	err := scanServiceCode(
		h.db.QueryRow(r.Context(), selectServiceCode+`WHERE id = $1`, id),
		&s,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load service code")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	var s ServiceCode

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanServiceCode(&s)

	if message := validateServiceCode(&s); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	// is_active is changed only through the status endpoint.
	err := scanServiceCode(
		h.db.QueryRow(
			r.Context(),
			`
			UPDATE service_codes
			SET
				code = $1,
				description = $2,
				is_add_on = $3,
				allow_multiple_units = $4,
				standard_rate = NULLIF($5, '')::numeric,
				updated_at = NOW()
			WHERE id = $6
			RETURNING
				id,
				code,
				description,
				is_add_on,
				allow_multiple_units,
				COALESCE(standard_rate::text, ''),
				is_active
			`,
			s.Code,
			s.Description,
			s.IsAddOn,
			s.AllowMultipleUnits,
			s.StandardRate,
			id,
		),
		&s,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update service code")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeError(w, http.StatusBadRequest, "is_active is required")
		return
	}

	// Disabling only blocks new selections; existing prior authorization
	// mappings are kept as history.
	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE service_codes
		SET
			is_active = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		*req.IsActive,
		id,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update service code status")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "service code not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"is_active": *req.IsActive,
	})
}
