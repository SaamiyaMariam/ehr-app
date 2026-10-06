package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ICD-10-CM format: letter, digit, alphanumeric, then optionally a dot and
// 1-4 alphanumerics. Format only — no catalog lookup.
var icd10Pattern = regexp.MustCompile(`^[A-Z][0-9][0-9A-Z](\.[0-9A-Z]{1,4})?$`)

type Diagnosis struct {
	ID          string `json:"id"`
	PatientID   string `json:"patient_id"`
	ICD10Code   string `json:"icd10_code"`
	Description string `json:"description"`
	IsPrimary   bool   `json:"is_primary"`
	IsActive    bool   `json:"is_active"`
	InUse       bool   `json:"in_use"`
}

// normalizeICD10 uppercases, strips spaces and inserts the dot after the
// third character when omitted ("f411" → "F41.1").
func normalizeICD10(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))

	if !strings.Contains(code, ".") && len(code) > 3 {
		code = code[:3] + "." + code[3:]
	}

	return code
}

func validateDiagnosis(d *Diagnosis) string {
	d.ICD10Code = normalizeICD10(d.ICD10Code)
	d.Description = strings.TrimSpace(d.Description)

	if !icd10Pattern.MatchString(d.ICD10Code) {
		return "diagnosis code must be a valid ICD-10 code (e.g. F41.1)"
	}

	if d.Description == "" {
		return "diagnosis description is required"
	}

	if len(d.Description) > 255 {
		return "diagnosis description must be 255 characters or fewer"
	}

	return ""
}

const selectDiagnosis = `
	SELECT
		d.id, d.patient_id, d.icd10_code, d.description, d.is_primary, d.is_active,
		EXISTS (SELECT 1 FROM billing_charge_diagnoses cd WHERE cd.diagnosis_id = d.id)
	FROM patient_diagnoses d
`

func scanDiagnosis(row pgx.Row, d *Diagnosis) error {
	return row.Scan(&d.ID, &d.PatientID, &d.ICD10Code, &d.Description, &d.IsPrimary, &d.IsActive, &d.InUse)
}

func (h *Handler) ListDiagnoses(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load diagnoses")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectDiagnosis+`
		WHERE d.patient_id = $1
		ORDER BY d.is_active DESC, d.is_primary DESC, d.icd10_code
		`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load diagnoses")
		return
	}
	defer rows.Close()

	result := make([]Diagnosis, 0)

	for rows.Next() {
		var d Diagnosis

		if err := scanDiagnosis(rows, &d); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load diagnoses")
			return
		}

		result = append(result, d)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getDiagnosis(ctx context.Context, q queryRower, id string) (Diagnosis, error) {
	var d Diagnosis
	err := scanDiagnosis(q.QueryRow(ctx, selectDiagnosis+`WHERE d.id = $1`, id), &d)
	return d, err
}

// clearOtherPrimary keeps at most one primary diagnosis per patient.
func clearOtherPrimary(ctx context.Context, tx pgx.Tx, patientID, keepID string) error {
	_, err := tx.Exec(
		ctx,
		`
		UPDATE patient_diagnoses
		SET is_primary = FALSE, updated_at = NOW()
		WHERE patient_id = $1 AND is_primary AND id <> $2
		`,
		patientID,
		keepID,
	)

	return err
}

func (h *Handler) CreateDiagnosis(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var d Diagnosis

	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if message := validateDiagnosis(&d); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add diagnosis")
		return
	}
	defer tx.Rollback(r.Context())

	var exists, duplicate bool

	if err := tx.QueryRow(
		r.Context(),
		`
		SELECT
			EXISTS (SELECT 1 FROM patients WHERE id = $1),
			EXISTS (SELECT 1 FROM patient_diagnoses WHERE patient_id = $1 AND icd10_code = $2 AND is_active)
		`,
		patientID,
		d.ICD10Code,
	).Scan(&exists, &duplicate); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add diagnosis")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	if duplicate {
		writeError(w, http.StatusConflict, "this diagnosis is already active for the patient")
		return
	}

	var id string

	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO patient_diagnoses (patient_id, icd10_code, description)
		VALUES ($1, $2, $3)
		RETURNING id
		`,
		patientID,
		d.ICD10Code,
		d.Description,
	).Scan(&id); err != nil {
		writeError(w, http.StatusBadRequest, "could not add diagnosis")
		return
	}

	if d.IsPrimary {
		if err := clearOtherPrimary(r.Context(), tx, patientID, id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not add diagnosis")
			return
		}

		if _, err := tx.Exec(r.Context(), `UPDATE patient_diagnoses SET is_primary = TRUE WHERE id = $1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not add diagnosis")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add diagnosis")
		return
	}

	created, err := h.getDiagnosis(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load diagnosis")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) UpdateDiagnosis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	var d Diagnosis

	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if message := validateDiagnosis(&d); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update diagnosis")
		return
	}
	defer tx.Rollback(r.Context())

	existing, err := h.getDiagnosis(r.Context(), tx, id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update diagnosis")
		return
	}

	// Charges point at diagnoses by ID, so the code itself is frozen once
	// used; add a new diagnosis instead.
	if existing.InUse && existing.ICD10Code != d.ICD10Code {
		writeError(w, http.StatusConflict, "this diagnosis is used on billing charges; its code cannot be changed")
		return
	}

	if d.IsPrimary && !existing.IsActive {
		writeError(w, http.StatusBadRequest, "a disabled diagnosis cannot be primary")
		return
	}

	if d.IsPrimary {
		if err := clearOtherPrimary(r.Context(), tx, existing.PatientID, id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update diagnosis")
			return
		}
	}

	if _, err := tx.Exec(
		r.Context(),
		`
		UPDATE patient_diagnoses
		SET icd10_code = $1, description = $2, is_primary = $3, updated_at = NOW()
		WHERE id = $4
		`,
		d.ICD10Code,
		d.Description,
		d.IsPrimary,
		id,
	); err != nil {
		writeError(w, http.StatusConflict, "could not update diagnosis (is the code already active for this patient?)")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update diagnosis")
		return
	}

	updated, err := h.getDiagnosis(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load diagnosis")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) SetDiagnosisActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeError(w, http.StatusBadRequest, "is_active is required")
		return
	}

	// Disabling also clears primary. Re-enabling fails (unique index) if the
	// same code is already active again.
	tag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE patient_diagnoses
		SET is_active = $1,
			is_primary = CASE WHEN $1 THEN is_primary ELSE FALSE END,
			updated_at = NOW()
		WHERE id = $2
		`,
		*req.IsActive,
		id,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "could not update diagnosis status (is the same code already active?)")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"is_active": *req.IsActive})
}
