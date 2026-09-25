package payers

import (
	"encoding/json"
	"errors"
	"net/http"
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

type Payer struct {
	ID string `json:"id"`

	PayerName string `json:"payer_name"`
	PayerID   string `json:"payer_id"`

	InNetwork bool `json:"in_network"`

	Address1 string `json:"address_1"`
	Address2 string `json:"address_2"`
	Zip      string `json:"zip"`
	City     string `json:"city"`
	State    string `json:"state"`

	Phone string `json:"phone"`
	Fax   string `json:"fax"`

	IsActive bool `json:"is_active"`
}

type PayerListItem struct {
	ID        string `json:"id"`
	PayerName string `json:"payer_name"`
	PayerID   string `json:"payer_id"`
	InNetwork bool   `json:"in_network"`
	Phone     string `json:"phone"`
	IsActive  bool   `json:"is_active"`
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

func cleanPayer(payer *Payer) {
	payer.PayerName = strings.TrimSpace(payer.PayerName)
	payer.PayerID = strings.TrimSpace(payer.PayerID)

	payer.Address1 = strings.TrimSpace(payer.Address1)
	payer.Address2 = strings.TrimSpace(payer.Address2)
	payer.Zip = strings.TrimSpace(payer.Zip)
	payer.City = strings.TrimSpace(payer.City)
	payer.State = strings.TrimSpace(payer.State)

	payer.Phone = strings.TrimSpace(payer.Phone)
	payer.Fax = strings.TrimSpace(payer.Fax)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var payer Payer

	if err := json.NewDecoder(r.Body).Decode(&payer); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPayer(&payer)

	if payer.PayerName == "" {
		writeError(w, http.StatusBadRequest, "payer name is required")
		return
	}

	// Newly created payers should be enabled.
	payer.IsActive = true

	err := h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO payers (
			payer_name,
			payer_id,
			in_network,
			address_1,
			address_2,
			zip,
			city,
			state,
			phone,
			fax,
			is_active
		)
		VALUES (
			$1,
			NULLIF($2, ''),
			$3,
			NULLIF($4, ''),
			NULLIF($5, ''),
			NULLIF($6, ''),
			NULLIF($7, ''),
			NULLIF($8, ''),
			NULLIF($9, ''),
			NULLIF($10, ''),
			TRUE
		)
		RETURNING id, is_active
		`,
		payer.PayerName,
		payer.PayerID,
		payer.InNetwork,
		payer.Address1,
		payer.Address2,
		payer.Zip,
		payer.City,
		payer.State,
		payer.Phone,
		payer.Fax,
	).Scan(
		&payer.ID,
		&payer.IsActive,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create payer")
		return
	}

	writeJSON(w, http.StatusCreated, payer)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			id,
			payer_name,
			COALESCE(payer_id, ''),
			in_network,
			COALESCE(phone, ''),
			is_active
		FROM payers
		ORDER BY payer_name
		`,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payers")
		return
	}
	defer rows.Close()

	result := make([]PayerListItem, 0)

	for rows.Next() {
		var payer PayerListItem

		if err := rows.Scan(
			&payer.ID,
			&payer.PayerName,
			&payer.PayerID,
			&payer.InNetwork,
			&payer.Phone,
			&payer.IsActive,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load payers")
			return
		}

		result = append(result, payer)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var payer Payer

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			payer_name,
			COALESCE(payer_id, ''),
			in_network,
			COALESCE(address_1, ''),
			COALESCE(address_2, ''),
			COALESCE(zip, ''),
			COALESCE(city, ''),
			COALESCE(state, ''),
			COALESCE(phone, ''),
			COALESCE(fax, ''),
			is_active
		FROM payers
		WHERE id = $1
		`,
		id,
	).Scan(
		&payer.ID,
		&payer.PayerName,
		&payer.PayerID,
		&payer.InNetwork,
		&payer.Address1,
		&payer.Address2,
		&payer.Zip,
		&payer.City,
		&payer.State,
		&payer.Phone,
		&payer.Fax,
		&payer.IsActive,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payer")
		return
	}

	writeJSON(w, http.StatusOK, payer)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var payer Payer

	if err := json.NewDecoder(r.Body).Decode(&payer); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPayer(&payer)

	if payer.PayerName == "" {
		writeError(w, http.StatusBadRequest, "payer name is required")
		return
	}

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE payers
		SET
			payer_name = $1,
			payer_id = NULLIF($2, ''),
			in_network = $3,
			address_1 = NULLIF($4, ''),
			address_2 = NULLIF($5, ''),
			zip = NULLIF($6, ''),
			city = NULLIF($7, ''),
			state = NULLIF($8, ''),
			phone = NULLIF($9, ''),
			fax = NULLIF($10, ''),
			updated_at = NOW()
		WHERE id = $11
		`,
		payer.PayerName,
		payer.PayerID,
		payer.InNetwork,
		payer.Address1,
		payer.Address2,
		payer.Zip,
		payer.City,
		payer.State,
		payer.Phone,
		payer.Fax,
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update payer")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	payer.ID = id

	err = h.db.QueryRow(
		r.Context(),
		`SELECT is_active FROM payers WHERE id = $1`,
		id,
	).Scan(&payer.IsActive)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payer status")
		return
	}

	writeJSON(w, http.StatusOK, payer)
}

func (h *Handler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		IsActive bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE payers
		SET
			is_active = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		req.IsActive,
		id,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update payer status")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"is_active": req.IsActive,
	})
}
