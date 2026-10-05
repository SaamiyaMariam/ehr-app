package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type BillingSettings struct {
	PatientID       string `json:"patient_id"`
	BillingComments string `json:"billing_comments"`
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing settings")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	settings := BillingSettings{PatientID: patientID}

	err = h.db.QueryRow(
		r.Context(),
		`
		SELECT COALESCE(billing_comments, '')
		FROM patient_billing_settings
		WHERE patient_id = $1
		`,
		patientID,
	).Scan(&settings.BillingComments)

	// No settings row yet is a normal state: return the empty defaults.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not load billing settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var settings BillingSettings

	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The patient always comes from the URL, never from the body.
	settings.PatientID = patientID
	settings.BillingComments = strings.TrimSpace(settings.BillingComments)

	// Selecting from patients makes the upsert a no-op (no rows returned)
	// when the patient does not exist, so we can answer 404.
	err := h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO patient_billing_settings (patient_id, billing_comments)
		SELECT id, NULLIF($2, '')
		FROM patients
		WHERE id = $1
		ON CONFLICT (patient_id) DO UPDATE
		SET
			billing_comments = EXCLUDED.billing_comments,
			updated_at = NOW()
		RETURNING COALESCE(billing_comments, '')
		`,
		patientID,
		settings.BillingComments,
	).Scan(&settings.BillingComments)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save billing settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}
