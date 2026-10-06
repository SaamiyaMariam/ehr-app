package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ClaimDocument struct {
	ID            string `json:"id"`
	Version       int    `json:"version"`
	FrequencyCode string `json:"frequency_code"`
	PageCount     int    `json:"page_count"`
	GeneratedBy   string `json:"generated_by"`
	CreatedAt     string `json:"created_at"`
}

func (h *Handler) listClaimDocuments(ctx context.Context, q queryRower, claimID string) ([]ClaimDocument, error) {
	rows, err := q.Query(
		ctx,
		`
		SELECT d.id, d.version, d.frequency_code, d.page_count,
			COALESCE(u.first_name || ' ' || u.last_name, ''),
			TO_CHAR(d.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM claim_documents d
		LEFT JOIN users u ON u.id = d.generated_by
		WHERE d.claim_id = $1
		ORDER BY d.version DESC
		`,
		claimID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ClaimDocument{}

	for rows.Next() {
		var d ClaimDocument
		if err := rows.Scan(&d.ID, &d.Version, &d.FrequencyCode, &d.PageCount, &d.GeneratedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}

	return result, rows.Err()
}

// priorPayerPaid sums what earlier payers paid, as recorded in the claim
// snapshot (CMS-1500 item 29 on secondary claims).
func priorPayerPaid(s ClaimSnapshot) int64 {
	var total int64

	for _, o := range s.OtherInsurance {
		if cents, ok := parseSignedMoney(o.AmountPaid); ok {
			total += cents
		}
	}

	return total
}

// generateClaimDocument renders and stores a new CMS-1500 version for a
// claim inside tx, using only the claim's snapshot data.
func (h *Handler) generateClaimDocument(ctx context.Context, tx pgx.Tx, claimID, userID string) (ClaimDocument, error) {
	claim, err := h.getClaim(ctx, tx, claimID)
	if err != nil {
		return ClaimDocument{}, err
	}

	lines := make([]ClaimLine, 0, len(claim.Lines))
	for _, l := range claim.Lines {
		if l.IsCurrent || claim.Status == "voided" {
			lines = append(lines, l)
		}
	}

	data := cms1500Data{
		ClaimNumber:   claim.ClaimNumber,
		Snapshot:      *claim.Snapshot,
		Lines:         lines,
		Diagnoses:     claim.Diagnoses,
		FrequencyCode: frequencyCodeFor(claim.ResubmissionType),
		OriginalRef:   claim.PayerClaimControlNumber,
		AmountPaid:    priorPayerPaid(*claim.Snapshot),
		GeneratedAt:   time.Now(),
	}

	pdf, sum, pages, err := renderCMS1500(data)
	if err != nil {
		return ClaimDocument{}, err
	}

	var doc ClaimDocument

	err = tx.QueryRow(
		ctx,
		`
		INSERT INTO claim_documents (claim_id, version, frequency_code, pdf, sha256, page_count, generated_by)
		VALUES (
			$1,
			COALESCE((SELECT MAX(version) FROM claim_documents WHERE claim_id = $1), 0) + 1,
			$2, $3, $4, $5, $6
		)
		RETURNING id, version, frequency_code, page_count, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		`,
		claimID, data.FrequencyCode, pdf, sum, pages, nullIfEmpty(userID),
	).Scan(&doc.ID, &doc.Version, &doc.FrequencyCode, &doc.PageCount, &doc.CreatedAt)

	return doc, err
}

type claimActionError struct {
	status     int
	message    string
	validation *ClaimValidation
}

func (e *claimActionError) Error() string { return e.message }

func writeClaimActionError(w http.ResponseWriter, err error, fallback string) {
	var actionErr *claimActionError

	if errors.As(err, &actionErr) {
		body := map[string]any{"error": actionErr.message}
		if actionErr.validation != nil {
			body["validation"] = actionErr.validation
		}
		writeJSON(w, actionErr.status, body)
		return
	}

	if errors.Is(err, errInsufficientAuthorizationUses) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeError(w, http.StatusInternalServerError, fallback)
}

// ensureSubmittable re-validates an editable claim and returns an error
// carrying the validation result if it cannot be submitted. The validation
// outcome is persisted by the caller's transaction either way.
func (h *Handler) ensureSubmittable(ctx context.Context, tx pgx.Tx, claimID, status, userID string) (string, error) {
	if !claimEditable(status) {
		return status, nil
	}

	v, err := h.revalidateClaim(ctx, tx, claimID, status, userID)
	if err != nil {
		return status, err
	}

	if !v.Valid() {
		return "validation_error", &claimActionError{
			status:     http.StatusUnprocessableEntity,
			message:    "the claim has validation errors; fix them before submitting",
			validation: &v,
		}
	}

	return "ready", nil
}

// GenerateCMS1500 produces the paper claim. The first generation from a
// ready claim moves it to paper_generated and consumes prior authorization
// uses (exactly once); later calls only produce a new copy.
func (h *Handler) GenerateCMS1500(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
		return
	}
	defer tx.Rollback(r.Context())

	status, err := lockClaim(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
		return
	}

	var method string
	if err := tx.QueryRow(r.Context(), `SELECT submission_method FROM claims WHERE id = $1`, id).Scan(&method); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
		return
	}

	if method != "paper" {
		writeError(w, http.StatusConflict, "CMS-1500 generation is only for paper claims")
		return
	}

	if status == "voided" {
		writeError(w, http.StatusConflict, "voided claims cannot be printed")
		return
	}

	firstGeneration := claimEditable(status)

	newStatus, err := h.ensureSubmittable(r.Context(), tx, id, status, userID)
	if err != nil {
		var actionErr *claimActionError
		if errors.As(err, &actionErr) {
			// Keep the recorded validation result.
			if commitErr := tx.Commit(r.Context()); commitErr != nil {
				writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
				return
			}
		}
		writeClaimActionError(w, err, "could not generate CMS-1500")
		return
	}

	if firstGeneration {
		if err := consumeClaimAuthorizations(r.Context(), tx, id, "cms1500_generated", userID); err != nil {
			writeClaimActionError(w, err, "could not record prior authorization usage")
			return
		}

		if err := transitionClaim(r.Context(), tx, id, newStatus, "paper_generated", "cms1500_generated", "user",
			"CMS-1500 generated for mailing.", userID); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}

	doc, err := h.generateClaimDocument(r.Context(), tx, id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
		return
	}

	if !firstGeneration {
		if err := recordClaimHistory(r.Context(), tx, id, "cms1500_regenerated", status, status, "user",
			fmt.Sprintf("CMS-1500 copy generated (version %d). No status or authorization change.", doc.Version), userID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate CMS-1500")
		return
	}

	claim, err := h.getClaim(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"document": doc, "claim": claim})
}

func writePDF(w http.ResponseWriter, filename string, pdf []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// DownloadCMS1500 returns the latest (or ?version=N) stored PDF. Read-only:
// it never changes claim state or authorization usage.
func (h *Handler) DownloadCMS1500(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	version := 0
	if v := r.URL.Query().Get("version"); v != "" {
		if _, err := fmt.Sscan(v, &version); err != nil || version < 1 {
			writeError(w, http.StatusBadRequest, "version must be a positive number")
			return
		}
	}

	var pdf []byte
	var claimNumber string
	var docVersion int

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT d.pdf, c.claim_number, d.version
		FROM claim_documents d
		JOIN claims c ON c.id = d.claim_id
		WHERE d.claim_id = $1 AND ($2 = 0 OR d.version = $2)
		ORDER BY d.version DESC
		LIMIT 1
		`,
		id, version,
	).Scan(&pdf, &claimNumber, &docVersion)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no CMS-1500 has been generated for this claim")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load CMS-1500")
		return
	}

	writePDF(w, fmt.Sprintf("%s-cms1500-v%d.pdf", claimNumber, docVersion), pdf)
}

// =========================================================
// SUPERBILLS
// =========================================================

// superbillEligibleSQL limits superbills to services the patient pays and
// may submit themselves: direct or out-of-network billing.
const superbillEligibleSQL = `
	c.status = 'active'
	AND (c.billing_method = 'direct' OR c.billing_method LIKE 'insurance_out_of_network_%')
`

func (h *Handler) ListSuperbillCharges(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectCharge+`WHERE c.patient_id = $1 AND `+superbillEligibleSQL+`
		ORDER BY c.date_of_service DESC LIMIT 500`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load services")
		return
	}
	defer rows.Close()

	result := []Charge{}

	for rows.Next() {
		var c Charge
		if err := scanCharge(rows, &c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load services")
			return
		}
		result = append(result, c)
	}

	writeJSON(w, http.StatusOK, result)
}

type Superbill struct {
	ID           string `json:"id"`
	PatientID    string `json:"patient_id"`
	TotalCharges string `json:"total_charges"`
	TotalPaid    string `json:"total_paid"`
	ServiceCount int    `json:"service_count"`
	FirstDate    string `json:"first_date_of_service"`
	LastDate     string `json:"last_date_of_service"`
	GeneratedBy  string `json:"generated_by"`
	CreatedAt    string `json:"created_at"`
}

const selectSuperbill = `
	SELECT s.id, s.patient_id, s.total_charges::text, s.total_paid::text,
		(SELECT COUNT(*) FROM superbill_charges x WHERE x.superbill_id = s.id),
		COALESCE((SELECT TO_CHAR(MIN(c.date_of_service), 'YYYY-MM-DD') FROM superbill_charges x JOIN billing_charges c ON c.id = x.charge_id WHERE x.superbill_id = s.id), ''),
		COALESCE((SELECT TO_CHAR(MAX(c.date_of_service), 'YYYY-MM-DD') FROM superbill_charges x JOIN billing_charges c ON c.id = x.charge_id WHERE x.superbill_id = s.id), ''),
		COALESCE(u.first_name || ' ' || u.last_name, ''),
		TO_CHAR(s.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
	FROM superbills s
	LEFT JOIN users u ON u.id = s.generated_by
`

func scanSuperbill(row pgx.Row, s *Superbill) error {
	return row.Scan(&s.ID, &s.PatientID, &s.TotalCharges, &s.TotalPaid, &s.ServiceCount, &s.FirstDate, &s.LastDate, &s.GeneratedBy, &s.CreatedAt)
}

func (h *Handler) ListSuperbills(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(r.Context(), selectSuperbill+`WHERE s.patient_id = $1 ORDER BY s.created_at DESC`, patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load superbills")
		return
	}
	defer rows.Close()

	result := []Superbill{}

	for rows.Next() {
		var s Superbill
		if err := scanSuperbill(rows, &s); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load superbills")
			return
		}
		result = append(result, s)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateSuperbill(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var req struct {
		ChargeIDs []string `json:"charge_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.ChargeIDs) == 0 || len(req.ChargeIDs) > 100 {
		writeError(w, http.StatusBadRequest, "select between 1 and 100 services")
		return
	}

	seen := map[string]bool{}
	for i, id := range req.ChargeIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if !isUUID(id) || seen[id] {
			writeError(w, http.StatusBadRequest, "invalid or duplicate service")
			return
		}
		seen[id] = true
		req.ChargeIDs[i] = id
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create superbill")
		return
	}
	defer tx.Rollback(r.Context())

	// Only this patient's eligible services (prevents cross-patient IDs).
	rows, err := tx.Query(
		r.Context(),
		selectCharge+`WHERE c.patient_id = $1 AND c.id = ANY($2::uuid[]) AND `+superbillEligibleSQL+`
		ORDER BY c.date_of_service, c.created_at`,
		patientID, req.ChargeIDs,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create superbill")
		return
	}

	var charges []Charge
	for rows.Next() {
		var c Charge
		if err := scanCharge(rows, &c); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "could not create superbill")
			return
		}
		charges = append(charges, c)
	}
	rows.Close()

	if len(charges) != len(req.ChargeIDs) {
		writeError(w, http.StatusBadRequest, "superbills can include only this patient's active direct or out-of-network services")
		return
	}

	// Reuse the claim snapshot builder for patient and practice data.
	var practice PracticeBillingProfile
	if err := scanPracticeProfile(tx.QueryRow(r.Context(), selectPracticeProfile), &practice); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create superbill")
		return
	}

	data := superbillData{
		Practice: SnapshotPractice{
			Name: practice.PracticeName, NPI: practice.NPI, TaxID: practice.TaxID, TaxIDType: practice.TaxIDType,
			Phone:   practice.Phone,
			Address: SnapshotAddress{Address1: practice.Address1, Address2: practice.Address2, City: practice.City, State: practice.State, Zip: practice.Zip},
		},
		GeneratedAt: time.Now().Format("2006-01-02 15:04 MST"),
	}

	var patientSex string
	if err := tx.QueryRow(
		r.Context(),
		`
		SELECT COALESCE(first_name, ''), COALESCE(middle_name, ''), last_name,
			COALESCE(TO_CHAR(date_of_birth, 'YYYY-MM-DD'), ''), COALESCE(administrative_sex, ''),
			COALESCE(address_1, ''), COALESCE(address_2, ''), COALESCE(city, ''), UPPER(COALESCE(state, '')), COALESCE(zip, '')
		FROM patients WHERE id = $1
		`,
		patientID,
	).Scan(
		&data.Patient.FirstName, &data.Patient.MiddleName, &data.Patient.LastName, &data.Patient.DateOfBirth, &patientSex,
		&data.Patient.Address.Address1, &data.Patient.Address.Address2, &data.Patient.Address.City, &data.Patient.Address.State, &data.Patient.Address.Zip,
	); err != nil {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}
	data.Patient.Sex = normalizeSex(patientSex)

	positions := map[string]int{}
	var totalFees, totalPaid int64

	for _, c := range charges {
		var letters strings.Builder

		for _, d := range c.Diagnoses {
			pos, ok := positions[d.ICD10Code]
			if !ok {
				pos = len(data.Diagnoses) + 1
				positions[d.ICD10Code] = pos
				data.Diagnoses = append(data.Diagnoses, ClaimDiagnosis{Position: pos, Letter: diagnosisLetter(min(pos, 26)), ICD10Code: d.ICD10Code, Description: d.Description})
			}
			letters.WriteString(diagnosisLetter(min(pos, 26)))
		}

		var npi string
		_ = tx.QueryRow(r.Context(), `SELECT COALESCE(npi, '') FROM clinician_billing_profiles WHERE user_id = $1`, c.ClinicianID).Scan(&npi)

		paid := mustCents(c.Balances.PatientPayments)
		totalFees += mustCents(c.TotalCharge)
		totalPaid += paid

		data.Lines = append(data.Lines, superbillLine{
			ChargeID: c.ID, DateOfService: c.DateOfService, ServiceCode: c.ServiceCode, Description: c.ServiceDescription,
			Modifiers: c.Modifiers, Units: c.Units, PlaceOfService: c.PlaceOfService, Diagnoses: letters.String(),
			Fee: c.TotalCharge, Paid: formatMoney(paid), Clinician: c.ClinicianName, ClinicianNPI: npi,
		})
	}

	data.TotalFees = formatMoney(totalFees)
	data.TotalPaid = formatMoney(totalPaid)

	pdf, sum, err := renderSuperbill(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not render superbill")
		return
	}

	snapshotJSON, _ := json.Marshal(data)

	var id string
	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO superbills (patient_id, total_charges, total_paid, snapshot, pdf, sha256, generated_by)
		VALUES ($1, $2::numeric, $3::numeric, $4, $5, $6, $7)
		RETURNING id
		`,
		patientID, data.TotalFees, data.TotalPaid, snapshotJSON, pdf, sum, nullIfEmpty(currentUserID(r)),
	).Scan(&id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create superbill")
		return
	}

	for _, c := range charges {
		if _, err := tx.Exec(
			r.Context(),
			`INSERT INTO superbill_charges (superbill_id, charge_id, patient_id) VALUES ($1, $2, $3)`,
			id, c.ID, patientID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not create superbill")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create superbill")
		return
	}

	var s Superbill
	if err := scanSuperbill(h.db.QueryRow(r.Context(), selectSuperbill+`WHERE s.id = $1`, id), &s); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load superbill")
		return
	}

	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) DownloadSuperbill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "superbill not found")
		return
	}

	var pdf []byte
	var created time.Time

	err := h.db.QueryRow(r.Context(), `SELECT pdf, created_at FROM superbills WHERE id = $1`, id).Scan(&pdf, &created)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "superbill not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load superbill")
		return
	}

	writePDF(w, fmt.Sprintf("superbill-%s.pdf", created.Format("2006-01-02")), pdf)
}
