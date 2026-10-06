package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	modifierPattern       = regexp.MustCompile(`^[A-Z0-9]{2}$`)
	placeOfServicePattern = regexp.MustCompile(`^[0-9]{2}$`)
)

// ChargeInput is the write model for creating / editing a billable service.
type ChargeInput struct {
	ClinicianID          string   `json:"clinician_id"`
	ServiceCodeID        string   `json:"service_code_id"`
	DateOfService        string   `json:"date_of_service"`
	Units                int      `json:"units"`
	Modifiers            []string `json:"modifiers"`
	PlaceOfService       string   `json:"place_of_service"`
	InsurancePolicyID    string   `json:"insurance_policy_id"`
	BillingMethod        string   `json:"billing_method"`
	PriorAuthorizationID string   `json:"prior_authorization_id"`
	// PatientResponsibility "" means "use the default split".
	PatientResponsibility string   `json:"patient_responsibility"`
	DiagnosisIDs          []string `json:"diagnosis_ids"`
	Notes                 string   `json:"notes"`
}

type ChargeDiagnosis struct {
	ID          string `json:"id"`
	ICD10Code   string `json:"icd10_code"`
	Description string `json:"description"`
	Pointer     int    `json:"pointer"`
}

// Charge is the read model. Responsibility fields are the initial split;
// the *_balance fields come from the billing_charge_balances view.
type Charge struct {
	ID          string `json:"id"`
	PatientID   string `json:"patient_id"`
	PatientName string `json:"patient_name"`

	ClinicianID   string `json:"clinician_id"`
	ClinicianName string `json:"clinician_name"`

	ServiceCodeID      string `json:"service_code_id"`
	ServiceCode        string `json:"service_code"`
	ServiceDescription string `json:"service_description"`

	DateOfService  string   `json:"date_of_service"`
	Units          int      `json:"units"`
	Modifiers      []string `json:"modifiers"`
	PlaceOfService string   `json:"place_of_service"`

	BillingMethod     string `json:"billing_method"`
	InsurancePolicyID string `json:"insurance_policy_id"`
	PayerID           string `json:"payer_id"`
	PayerName         string `json:"payer_name"`
	PolicyPriority    string `json:"policy_priority"`

	PriorAuthorizationID   string `json:"prior_authorization_id"`
	PriorAuthorizationCode string `json:"prior_authorization_code"`

	RatePerUnit    string `json:"rate_per_unit"`
	RateSource     string `json:"rate_source"`
	RateScheduleID string `json:"rate_schedule_id"`
	TotalCharge    string `json:"total_charge"`

	PatientResponsibility   string `json:"patient_responsibility"`
	InsuranceResponsibility string `json:"insurance_responsibility"`

	Balances ChargeBalances `json:"balances"`

	Status        string `json:"status"`
	DisplayStatus string `json:"display_status"`

	Diagnoses    []ChargeDiagnosis `json:"diagnoses"`
	DiagnosisIDs []string          `json:"diagnosis_ids"`

	Notes      string `json:"notes"`
	VoidReason string `json:"void_reason"`
	VoidedAt   string `json:"voided_at"`
	CreatedAt  string `json:"created_at"`

	// LockReason is non-empty when billing-relevant fields can no longer be
	// edited (e.g. the charge is on a submitted claim or has payments).
	LockReason string `json:"lock_reason"`
}

type ChargeBalances struct {
	PatientResponsibility   string `json:"patient_responsibility"`
	InsuranceResponsibility string `json:"insurance_responsibility"`
	PatientPayments         string `json:"patient_payments"`
	InsurancePayments       string `json:"insurance_payments"`
	PatientAdjustments      string `json:"patient_adjustments"`
	InsuranceAdjustments    string `json:"insurance_adjustments"`
	PatientBalance          string `json:"patient_balance"`
	InsuranceBalance        string `json:"insurance_balance"`
	TotalBalance            string `json:"total_balance"`
}

func cleanChargeInput(in *ChargeInput) {
	in.ClinicianID = strings.ToLower(strings.TrimSpace(in.ClinicianID))
	in.ServiceCodeID = strings.ToLower(strings.TrimSpace(in.ServiceCodeID))
	in.DateOfService = strings.TrimSpace(in.DateOfService)
	in.PlaceOfService = strings.TrimSpace(in.PlaceOfService)
	in.InsurancePolicyID = strings.ToLower(strings.TrimSpace(in.InsurancePolicyID))
	in.BillingMethod = strings.ToLower(strings.TrimSpace(in.BillingMethod))
	in.PriorAuthorizationID = strings.ToLower(strings.TrimSpace(in.PriorAuthorizationID))
	in.PatientResponsibility = strings.TrimSpace(in.PatientResponsibility)
	in.Notes = strings.TrimSpace(in.Notes)

	modifiers := make([]string, 0, len(in.Modifiers))
	for _, m := range in.Modifiers {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m != "" {
			modifiers = append(modifiers, m)
		}
	}
	in.Modifiers = modifiers

	ids := make([]string, 0, len(in.DiagnosisIDs))
	for _, id := range in.DiagnosisIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" {
			ids = append(ids, id)
		}
	}
	in.DiagnosisIDs = ids

	if in.PlaceOfService == "" {
		in.PlaceOfService = "11"
	}
}

// validateChargeInput checks the request alone; today is injected for tests.
func validateChargeInput(in *ChargeInput, today time.Time) string {
	dos, ok := parseDate(in.DateOfService)
	if !ok {
		return "date of service must be a valid date (YYYY-MM-DD)"
	}

	if isFutureDate(dos, today) {
		return "date of service cannot be in the future"
	}

	if in.Units < 1 || in.Units > 999 {
		return "units must be between 1 and 999"
	}

	if len(in.Modifiers) > 4 {
		return "a service can have at most 4 modifiers"
	}

	seen := map[string]bool{}
	for _, m := range in.Modifiers {
		if !modifierPattern.MatchString(m) {
			return "modifiers must be 2 letters or digits"
		}
		if seen[m] {
			return "modifiers must not repeat"
		}
		seen[m] = true
	}

	if !placeOfServicePattern.MatchString(in.PlaceOfService) {
		return "place of service must be a 2-digit code"
	}

	if in.InsurancePolicyID != "" && !isUUID(in.InsurancePolicyID) {
		return "insurance policy not found"
	}

	if in.PriorAuthorizationID != "" {
		if !isUUID(in.PriorAuthorizationID) {
			return "prior authorization not found"
		}
		if in.InsurancePolicyID == "" {
			return "a prior authorization requires an insurance policy"
		}
	}

	if len(in.DiagnosisIDs) > 4 {
		return "a service can reference at most 4 diagnoses"
	}

	seenDx := map[string]bool{}
	for _, id := range in.DiagnosisIDs {
		if !isUUID(id) {
			return "diagnosis not found"
		}
		if seenDx[id] {
			return "each diagnosis can be referenced only once"
		}
		seenDx[id] = true
	}

	if in.PatientResponsibility != "" {
		if _, ok := parseMoney(in.PatientResponsibility); !ok {
			return "patient responsibility must be a non-negative amount with at most 2 decimal places"
		}
	}

	if len(in.Notes) > 2000 {
		return "notes must be 2000 characters or fewer"
	}

	return ""
}

// splitResponsibility returns the initial patient / insurance split.
// Direct: the patient owes everything. Insurance: the requested patient
// share if given, otherwise the policy copay capped at the total (never an
// invented deductible or coinsurance).
func splitResponsibility(total int64, billingMethod string, requestedPatient *int64, copay *int64) (patient int64, insurance int64, message string) {
	if billingMethod == "direct" {
		if requestedPatient != nil && *requestedPatient != total {
			return 0, 0, "direct billing makes the patient responsible for the full charge"
		}
		return total, 0, ""
	}

	if requestedPatient != nil {
		if *requestedPatient > total {
			return 0, 0, "patient responsibility cannot exceed the total charge"
		}
		return *requestedPatient, total - *requestedPatient, ""
	}

	patient = 0
	if copay != nil {
		patient = min(*copay, total)
	}

	return patient, total - patient, ""
}

type priorAuthorizationInfo struct {
	PolicyID           string
	IsActive           bool
	StartDate          string
	ExpirationDate     string
	AppliesToAny       bool
	UsesRemaining      *int
	ServiceCodeIDs     map[string]bool
	AuthorizationCode  string
	UsageSetting       string
	PolicyPatientMatch bool
}

// checkPriorAuthorizationForCharge validates linking an authorization to a
// charge. It never consumes a use; that happens at claim submission.
func checkPriorAuthorizationForCharge(pa priorAuthorizationInfo, policyID, serviceCodeID, dateOfService string) string {
	if pa.PolicyID != policyID || !pa.PolicyPatientMatch {
		return "prior authorization does not belong to the selected insurance policy"
	}

	if !pa.IsActive {
		return "prior authorization is disabled"
	}

	if pa.StartDate != "" && dateOfService < pa.StartDate {
		return "prior authorization is not yet valid on the date of service"
	}

	if pa.ExpirationDate != "" && dateOfService > pa.ExpirationDate {
		return "prior authorization is expired for the date of service"
	}

	if !pa.AppliesToAny && !pa.ServiceCodeIDs[serviceCodeID] {
		return "prior authorization does not cover this service code"
	}

	if pa.UsesRemaining != nil && *pa.UsesRemaining <= 0 {
		return "prior authorization has no uses remaining"
	}

	return ""
}

func loadPriorAuthorizationInfo(ctx context.Context, q queryRower, id, patientID string) (priorAuthorizationInfo, error) {
	info := priorAuthorizationInfo{ServiceCodeIDs: map[string]bool{}}
	var serviceCodes []string

	err := q.QueryRow(
		ctx,
		`
		SELECT
			pa.insurance_policy_id::text,
			p.patient_id = $2,
			pa.is_active,
			COALESCE(TO_CHAR(pa.start_date, 'YYYY-MM-DD'), ''),
			COALESCE(TO_CHAR(pa.expiration_date, 'YYYY-MM-DD'), ''),
			pa.applies_to_any_service_code,
			pa.uses_remaining,
			pa.authorization_code,
			pa.usage_setting,
			COALESCE(
				(SELECT array_agg(m.service_code_id::text)
				 FROM prior_authorization_service_codes m
				 WHERE m.prior_authorization_id = pa.id),
				'{}'
			)
		FROM prior_authorizations pa
		JOIN insurance_policies p ON p.id = pa.insurance_policy_id
		WHERE pa.id = $1
		`,
		id,
		patientID,
	).Scan(
		&info.PolicyID, &info.PolicyPatientMatch, &info.IsActive,
		&info.StartDate, &info.ExpirationDate, &info.AppliesToAny,
		&info.UsesRemaining, &info.AuthorizationCode, &info.UsageSetting, &serviceCodes,
	)
	if err != nil {
		return info, err
	}

	for _, sc := range serviceCodes {
		info.ServiceCodeIDs[sc] = true
	}

	return info, nil
}

// chargeDisplayStatusSQL derives a charge's display status. Extended by the
// claims and payment modules; voided / closed / open are the base states.
const chargeDisplayStatusSQL = `
	CASE
		WHEN c.status = 'voided' THEN 'voided'
		WHEN EXISTS (
			SELECT 1 FROM claim_lines cl
			JOIN claims cm ON cm.id = cl.claim_id
			WHERE cl.charge_id = c.id AND cl.is_current
			  AND cm.status NOT IN ('paid', 'voided')
		) THEN 'on_claim'
		WHEN b.total_balance = 0 THEN 'closed'
		ELSE 'open'
	END
`

const selectCharge = `
	SELECT
		c.id, c.patient_id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
		c.clinician_id, u.first_name || ' ' || u.last_name,
		c.service_code_id, sc.code, sc.description,
		TO_CHAR(c.date_of_service, 'YYYY-MM-DD'),
		c.units,
		array_remove(ARRAY[c.modifier_1, c.modifier_2, c.modifier_3, c.modifier_4], NULL),
		c.place_of_service,
		c.billing_method,
		COALESCE(c.insurance_policy_id::text, ''),
		COALESCE(c.payer_id::text, ''),
		COALESCE(py.payer_name, ''),
		COALESCE(ip.priority, ''),
		COALESCE(c.prior_authorization_id::text, ''),
		COALESCE(pa.authorization_code, ''),
		c.rate_per_unit::text, c.rate_source, COALESCE(c.rate_schedule_id::text, ''),
		c.total_charge::text,
		c.patient_responsibility::text, c.insurance_responsibility::text,
		b.patient_responsibility::text, b.insurance_responsibility::text,
		b.patient_payments::text, b.insurance_payments::text,
		b.patient_adjustments::text, b.insurance_adjustments::text,
		b.patient_balance::text, b.insurance_balance::text, b.total_balance::text,
		c.status,
		` + chargeDisplayStatusSQL + `,
		COALESCE(c.notes, ''),
		COALESCE(c.void_reason, ''),
		COALESCE(TO_CHAR(c.voided_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''),
		TO_CHAR(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'),
		COALESCE(
			(SELECT json_agg(json_build_object(
				'id', d.id, 'icd10_code', d.icd10_code,
				'description', d.description, 'pointer', cd.pointer
			) ORDER BY cd.pointer)
			FROM billing_charge_diagnoses cd
			JOIN patient_diagnoses d ON d.id = cd.diagnosis_id
			WHERE cd.charge_id = c.id),
			'[]'::json
		)
	FROM billing_charges c
	JOIN billing_charge_balances b ON b.charge_id = c.id
	JOIN patients pt ON pt.id = c.patient_id
	JOIN users u ON u.id = c.clinician_id
	JOIN service_codes sc ON sc.id = c.service_code_id
	LEFT JOIN payers py ON py.id = c.payer_id
	LEFT JOIN insurance_policies ip ON ip.id = c.insurance_policy_id
	LEFT JOIN prior_authorizations pa ON pa.id = c.prior_authorization_id
`

func scanCharge(row pgx.Row, c *Charge) error {
	err := row.Scan(
		&c.ID, &c.PatientID, &c.PatientName,
		&c.ClinicianID, &c.ClinicianName,
		&c.ServiceCodeID, &c.ServiceCode, &c.ServiceDescription,
		&c.DateOfService,
		&c.Units,
		&c.Modifiers,
		&c.PlaceOfService,
		&c.BillingMethod,
		&c.InsurancePolicyID,
		&c.PayerID,
		&c.PayerName,
		&c.PolicyPriority,
		&c.PriorAuthorizationID,
		&c.PriorAuthorizationCode,
		&c.RatePerUnit, &c.RateSource, &c.RateScheduleID,
		&c.TotalCharge,
		&c.PatientResponsibility, &c.InsuranceResponsibility,
		&c.Balances.PatientResponsibility, &c.Balances.InsuranceResponsibility,
		&c.Balances.PatientPayments, &c.Balances.InsurancePayments,
		&c.Balances.PatientAdjustments, &c.Balances.InsuranceAdjustments,
		&c.Balances.PatientBalance, &c.Balances.InsuranceBalance, &c.Balances.TotalBalance,
		&c.Status,
		&c.DisplayStatus,
		&c.Notes,
		&c.VoidReason,
		&c.VoidedAt,
		&c.CreatedAt,
		&c.Diagnoses,
	)
	if err != nil {
		return err
	}

	if c.Modifiers == nil {
		c.Modifiers = []string{}
	}

	c.DiagnosisIDs = make([]string, 0, len(c.Diagnoses))
	for _, d := range c.Diagnoses {
		c.DiagnosisIDs = append(c.DiagnosisIDs, d.ID)
	}

	return nil
}

func (h *Handler) getCharge(ctx context.Context, q queryRower, id string) (Charge, error) {
	var c Charge

	if err := scanCharge(q.QueryRow(ctx, selectCharge+`WHERE c.id = $1`, id), &c); err != nil {
		return c, err
	}

	reason, err := chargeLockReason(ctx, q, id)
	c.LockReason = reason

	return c, err
}

// chargeLockReason explains why a charge's billing fields can no longer be
// edited or the charge voided: it is on a current (non-voided) claim, or
// money has been applied to it. Notes remain editable.
func chargeLockReason(ctx context.Context, q queryRower, chargeID string) (string, error) {
	var onClaim, hasFinancial bool

	err := q.QueryRow(
		ctx,
		// The derived table pins $1's type even when no fragment uses it.
		`SELECT `+chargeOnClaimSQL+`, `+chargeFinancialActivitySQL+` FROM (SELECT $1::uuid AS charge_id) pinned`,
		chargeID,
	).Scan(&onClaim, &hasFinancial)
	if err != nil {
		return "", err
	}

	switch {
	case onClaim:
		return "This service is on an insurance claim. Use the claim's correction / resubmission workflow instead of editing it.", nil
	case hasFinancial:
		return "Payments, adjustments or responsibility transfers have been posted to this service. Reverse them before editing or voiding.", nil
	}

	return "", nil
}

// chargeOnClaimSQL is TRUE when charge $1 is on a current, non-voided
// claim (including paid claims, which are historical records).
const chargeOnClaimSQL = `
	EXISTS (
		SELECT 1 FROM claim_lines cl
		JOIN claims cm ON cm.id = cl.claim_id
		WHERE cl.charge_id = $1 AND cl.is_current AND cm.status <> 'voided'
	)
`

// chargeFinancialActivitySQL is TRUE when any active money movement
// references charge $1.
const chargeFinancialActivitySQL = `
	(
		EXISTS (
			SELECT 1 FROM patient_payment_allocations a
			JOIN patient_payments p ON p.id = a.payment_id
			WHERE a.charge_id = $1 AND a.status = 'active' AND p.status = 'posted'
		)
		OR EXISTS (
			SELECT 1 FROM insurance_payment_allocations a
			JOIN insurance_payments p ON p.id = a.payment_id
			WHERE a.charge_id = $1 AND a.status = 'active' AND p.status = 'posted'
		)
		OR EXISTS (SELECT 1 FROM billing_adjustments WHERE charge_id = $1 AND status = 'active')
		OR EXISTS (SELECT 1 FROM responsibility_transfers WHERE charge_id = $1 AND status = 'active')
	)
`

// pricedCharge is the outcome of pricing + validation, ready to persist.
type pricedCharge struct {
	in                   ChargeInput
	billingMethod        string
	payerID              string
	rate                 int64
	rateSource           string
	rateScheduleID       *string
	total                int64
	patientShare         int64
	insuranceShare       int64
	priorAuthorizationID *string
}

// priceAndValidateCharge resolves pricing and validates relationships for a
// charge on patientID. When existing is non-nil (edit) and none of the
// pricing inputs changed, the snapshotted rate is kept.
func (h *Handler) priceAndValidateCharge(ctx context.Context, tx pgx.Tx, patientID string, in ChargeInput, existing *Charge) (pricedCharge, int, string) {
	out := pricedCharge{in: in}

	repricing := existing == nil ||
		existing.ServiceCodeID != in.ServiceCodeID ||
		existing.ClinicianID != in.ClinicianID ||
		existing.InsurancePolicyID != in.InsurancePolicyID ||
		(in.BillingMethod != "" && existing.BillingMethod != in.BillingMethod)

	var copay *int64

	if repricing {
		preview, status, message := h.chargePricing(ctx, tx, RatePreviewRequest{
			PatientID:         patientID,
			ServiceCodeID:     in.ServiceCodeID,
			ClinicianID:       in.ClinicianID,
			InsurancePolicyID: in.InsurancePolicyID,
			BillingMethod:     in.BillingMethod,
			Units:             in.Units,
		})
		if message != "" {
			return out, status, message
		}

		out.billingMethod = preview.BillingMethod
		out.payerID = preview.PayerID
		out.rate = mustCents(preview.RatePerUnit)
		out.rateSource = preview.Source
		out.rateScheduleID = preview.RateScheduleID
		copay = preview.copay
	} else {
		out.billingMethod = existing.BillingMethod
		out.payerID = existing.PayerID
		out.rate = mustCents(existing.RatePerUnit)
		out.rateSource = existing.RateSource
		if existing.RateScheduleID != "" {
			id := existing.RateScheduleID
			out.rateScheduleID = &id
		}

		if in.InsurancePolicyID != "" {
			var copayText *string
			if err := tx.QueryRow(ctx, `SELECT copay::text FROM insurance_policies WHERE id = $1`, in.InsurancePolicyID).Scan(&copayText); err != nil {
				return out, http.StatusInternalServerError, "could not price charge"
			}
			copay = scanNullableMoney(copayText)
		}
	}

	out.total = out.rate * int64(in.Units)

	var requested *int64
	if in.PatientResponsibility != "" {
		cents, _ := parseMoney(in.PatientResponsibility)
		requested = &cents
	} else if existing != nil && !repricing && mustCents(existing.TotalCharge) == out.total {
		// Unchanged total: keep the existing split unless a new one is given.
		cents := mustCents(existing.PatientResponsibility)
		requested = &cents
	}

	patientShare, insuranceShare, message := splitResponsibility(out.total, out.billingMethod, requested, copay)
	if message != "" {
		return out, http.StatusBadRequest, message
	}

	out.patientShare = patientShare
	out.insuranceShare = insuranceShare

	if in.PriorAuthorizationID != "" {
		if out.billingMethod == "direct" {
			return out, http.StatusBadRequest, "a prior authorization requires insurance billing"
		}

		pa, err := loadPriorAuthorizationInfo(ctx, tx, in.PriorAuthorizationID, patientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, http.StatusBadRequest, "prior authorization not found"
		}
		if err != nil {
			return out, http.StatusInternalServerError, "could not validate prior authorization"
		}

		// An unchanged link may stay even if the authorization later ran out
		// of uses (the use may be this very charge's).
		unchanged := existing != nil && existing.PriorAuthorizationID == in.PriorAuthorizationID &&
			existing.ServiceCodeID == in.ServiceCodeID && existing.DateOfService == in.DateOfService
		if message := checkPriorAuthorizationForCharge(pa, in.InsurancePolicyID, in.ServiceCodeID, in.DateOfService); message != "" && !unchanged {
			return out, http.StatusBadRequest, message
		}

		id := in.PriorAuthorizationID
		out.priorAuthorizationID = &id
	}

	if len(in.DiagnosisIDs) > 0 {
		var count int
		if err := tx.QueryRow(
			ctx,
			`
			SELECT COUNT(*) FROM patient_diagnoses
			WHERE patient_id = $1 AND id = ANY($2::uuid[])
			  AND (is_active OR id = ANY($3::uuid[]))
			`,
			patientID,
			in.DiagnosisIDs,
			existingDiagnosisIDs(existing),
		).Scan(&count); err != nil {
			return out, http.StatusInternalServerError, "could not validate diagnoses"
		}

		if count != len(in.DiagnosisIDs) {
			return out, http.StatusBadRequest, "diagnoses must be active diagnoses of this patient"
		}
	}

	return out, http.StatusOK, ""
}

func existingDiagnosisIDs(existing *Charge) []string {
	if existing == nil {
		return []string{}
	}
	return existing.DiagnosisIDs
}

func modifierAt(modifiers []string, i int) *string {
	if i < len(modifiers) {
		return &modifiers[i]
	}
	return nil
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func saveChargeDiagnoses(ctx context.Context, tx pgx.Tx, chargeID, patientID string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM billing_charge_diagnoses WHERE charge_id = $1`, chargeID); err != nil {
		return err
	}

	for i, id := range ids {
		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO billing_charge_diagnoses (charge_id, diagnosis_id, patient_id, pointer)
			VALUES ($1, $2, $3, $4)
			`,
			chargeID, id, patientID, i+1,
		); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) CreateCharge(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var in ChargeInput

	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanChargeInput(&in)

	if message := validateChargeInput(&in, time.Now()); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create charge")
		return
	}
	defer tx.Rollback(r.Context())

	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`, patientID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create charge")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	priced, status, message := h.priceAndValidateCharge(r.Context(), tx, patientID, in, nil)
	if message != "" {
		writeError(w, status, message)
		return
	}

	var id string

	err = tx.QueryRow(
		r.Context(),
		`
		INSERT INTO billing_charges (
			patient_id, clinician_id, service_code_id, date_of_service, units,
			modifier_1, modifier_2, modifier_3, modifier_4, place_of_service,
			billing_method, insurance_policy_id, payer_id, prior_authorization_id,
			rate_per_unit, rate_source, rate_schedule_id, total_charge,
			patient_responsibility, insurance_responsibility, notes, created_by
		)
		VALUES (
			$1, $2, $3, $4::date, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14,
			$15::numeric, $16, $17, $18::numeric,
			$19::numeric, $20::numeric, $21, $22
		)
		RETURNING id
		`,
		patientID, in.ClinicianID, in.ServiceCodeID, in.DateOfService, in.Units,
		modifierAt(in.Modifiers, 0), modifierAt(in.Modifiers, 1), modifierAt(in.Modifiers, 2), modifierAt(in.Modifiers, 3), in.PlaceOfService,
		priced.billingMethod, nullIfEmpty(in.InsurancePolicyID), nullIfEmpty(priced.payerID), priced.priorAuthorizationID,
		formatMoney(priced.rate), priced.rateSource, priced.rateScheduleID, formatMoney(priced.total),
		formatMoney(priced.patientShare), formatMoney(priced.insuranceShare), nullIfEmpty(in.Notes), nullIfEmpty(currentUserID(r)),
	).Scan(&id)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create charge")
		return
	}

	if err := saveChargeDiagnoses(r.Context(), tx, id, patientID, in.DiagnosisIDs); err != nil {
		writeError(w, http.StatusBadRequest, "could not save charge diagnoses")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create charge")
		return
	}

	created, err := h.getCharge(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charge")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) GetCharge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	c, err := h.getCharge(r.Context(), h.db, id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charge")
		return
	}

	writeJSON(w, http.StatusOK, c)
}

// chargeBillingFieldsChanged reports whether an edit touches anything other
// than notes.
func chargeBillingFieldsChanged(existing Charge, in ChargeInput) bool {
	if existing.ClinicianID != in.ClinicianID ||
		existing.ServiceCodeID != in.ServiceCodeID ||
		existing.DateOfService != in.DateOfService ||
		existing.Units != in.Units ||
		existing.PlaceOfService != in.PlaceOfService ||
		existing.InsurancePolicyID != in.InsurancePolicyID ||
		existing.PriorAuthorizationID != in.PriorAuthorizationID ||
		(in.BillingMethod != "" && existing.BillingMethod != in.BillingMethod) ||
		strings.Join(existing.Modifiers, ",") != strings.Join(in.Modifiers, ",") ||
		strings.Join(existing.DiagnosisIDs, ",") != strings.Join(in.DiagnosisIDs, ",") {
		return true
	}

	if in.PatientResponsibility != "" {
		requested, _ := parseMoney(in.PatientResponsibility)
		if requested != mustCents(existing.PatientResponsibility) {
			return true
		}
	}

	return false
}

func (h *Handler) UpdateCharge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	var in ChargeInput

	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanChargeInput(&in)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update charge")
		return
	}
	defer tx.Rollback(r.Context())

	var locked bool
	if err := tx.QueryRow(r.Context(), `SELECT TRUE FROM billing_charges WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "charge not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update charge")
		return
	}

	existing, err := h.getCharge(r.Context(), tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update charge")
		return
	}

	if existing.Status == "voided" {
		writeError(w, http.StatusConflict, "voided charges cannot be edited")
		return
	}

	billingChanged := chargeBillingFieldsChanged(existing, in)

	if billingChanged && existing.LockReason != "" {
		writeError(w, http.StatusConflict, existing.LockReason)
		return
	}

	if len(in.Notes) > 2000 {
		writeError(w, http.StatusBadRequest, "notes must be 2000 characters or fewer")
		return
	}

	if !billingChanged {
		if _, err := tx.Exec(
			r.Context(),
			`UPDATE billing_charges SET notes = $1, updated_at = NOW() WHERE id = $2`,
			nullIfEmpty(in.Notes),
			id,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update charge")
			return
		}
	} else {
		// The original DOS may predate today's rules; only validate fully.
		if message := validateChargeInput(&in, time.Now()); message != "" {
			writeError(w, http.StatusBadRequest, message)
			return
		}

		priced, status, message := h.priceAndValidateCharge(r.Context(), tx, existing.PatientID, in, &existing)
		if message != "" {
			writeError(w, status, message)
			return
		}

		// patient_id is never changed by an update.
		if _, err := tx.Exec(
			r.Context(),
			`
			UPDATE billing_charges SET
				clinician_id = $1, service_code_id = $2, date_of_service = $3::date, units = $4,
				modifier_1 = $5, modifier_2 = $6, modifier_3 = $7, modifier_4 = $8,
				place_of_service = $9, billing_method = $10, insurance_policy_id = $11,
				payer_id = $12, prior_authorization_id = $13,
				rate_per_unit = $14::numeric, rate_source = $15, rate_schedule_id = $16,
				total_charge = $17::numeric,
				patient_responsibility = $18::numeric, insurance_responsibility = $19::numeric,
				notes = $20, updated_at = NOW()
			WHERE id = $21
			`,
			in.ClinicianID, in.ServiceCodeID, in.DateOfService, in.Units,
			modifierAt(in.Modifiers, 0), modifierAt(in.Modifiers, 1), modifierAt(in.Modifiers, 2), modifierAt(in.Modifiers, 3),
			in.PlaceOfService, priced.billingMethod, nullIfEmpty(in.InsurancePolicyID),
			nullIfEmpty(priced.payerID), priced.priorAuthorizationID,
			formatMoney(priced.rate), priced.rateSource, priced.rateScheduleID,
			formatMoney(priced.total),
			formatMoney(priced.patientShare), formatMoney(priced.insuranceShare),
			nullIfEmpty(in.Notes), id,
		); err != nil {
			writeError(w, http.StatusBadRequest, "could not update charge")
			return
		}

		if err := saveChargeDiagnoses(r.Context(), tx, id, existing.PatientID, in.DiagnosisIDs); err != nil {
			writeError(w, http.StatusBadRequest, "could not save charge diagnoses")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update charge")
		return
	}

	updated, err := h.getCharge(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charge")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) VoidCharge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Reason = strings.TrimSpace(req.Reason)

	if req.Reason == "" || len(req.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "a void reason (up to 500 characters) is required")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void charge")
		return
	}
	defer tx.Rollback(r.Context())

	var status string
	err = tx.QueryRow(r.Context(), `SELECT status FROM billing_charges WHERE id = $1 FOR UPDATE`, id).Scan(&status)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void charge")
		return
	}

	if status == "voided" {
		writeError(w, http.StatusConflict, "charge is already voided")
		return
	}

	reason, err := chargeLockReason(r.Context(), tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void charge")
		return
	}

	if reason != "" {
		writeError(w, http.StatusConflict, reason)
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`
		UPDATE billing_charges
		SET status = 'voided', void_reason = $1, voided_at = NOW(), voided_by = $2, updated_at = NOW()
		WHERE id = $3
		`,
		req.Reason,
		nullIfEmpty(currentUserID(r)),
		id,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not void charge")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not void charge")
		return
	}

	voided, err := h.getCharge(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charge")
		return
	}

	writeJSON(w, http.StatusOK, voided)
}

// ListPatientTransactions returns a patient's charges (newest first).
func (h *Handler) ListPatientTransactions(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing transactions")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectCharge+`
		WHERE c.patient_id = $1
		ORDER BY c.date_of_service DESC, c.created_at DESC
		LIMIT 1000
		`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing transactions")
		return
	}
	defer rows.Close()

	result := make([]Charge, 0)

	for rows.Next() {
		var c Charge

		if err := scanCharge(rows, &c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load billing transactions")
			return
		}

		result = append(result, c)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing transactions")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================
// PRACTICE-WIDE TRANSACTION SEARCH
// =========================================================

type chargeFilter struct {
	where []string
	args  []any
}

func (f *chargeFilter) add(clause string, value any) {
	f.args = append(f.args, value)
	f.where = append(f.where, fmt.Sprintf(clause, len(f.args)))
}

// buildChargeFilter turns query parameters into SQL conditions. Invalid
// values produce an error message rather than being silently ignored.
func buildChargeFilter(r *http.Request) (*chargeFilter, string) {
	q := r.URL.Query()
	f := &chargeFilter{}

	for _, key := range []string{"patient_id", "clinician_id", "payer_id", "service_code_id"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if !isUUID(v) {
				return nil, key + " is invalid"
			}
			column := map[string]string{
				"patient_id": "c.patient_id", "clinician_id": "c.clinician_id",
				"payer_id": "c.payer_id", "service_code_id": "c.service_code_id",
			}[key]
			f.add(column+" = $%d", v)
		}
	}

	if v := strings.TrimSpace(q.Get("from")); v != "" {
		if _, ok := parseDate(v); !ok {
			return nil, "from must be a valid date"
		}
		f.add("c.date_of_service >= $%d::date", v)
	}

	if v := strings.TrimSpace(q.Get("to")); v != "" {
		if _, ok := parseDate(v); !ok {
			return nil, "to must be a valid date"
		}
		f.add("c.date_of_service <= $%d::date", v)
	}

	if v := strings.TrimSpace(q.Get("billing_method")); v != "" {
		switch v {
		case "insurance":
			f.where = append(f.where, "c.billing_method <> 'direct'")
		default:
			if !validEnum(v, validChargeBillingMethods...) {
				return nil, "billing_method is invalid"
			}
			f.add("c.billing_method = $%d", v)
		}
	}

	if v := strings.TrimSpace(q.Get("status")); v != "" {
		if !validEnum(v, "open", "closed", "voided", "on_claim") {
			return nil, "status is invalid"
		}
		f.add("("+chargeDisplayStatusSQL+") = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			return nil, "patient search is too long"
		}
		f.add("(p.first_name || ' ' || p.last_name) ILIKE '%%' || $%d || '%%'", v)
	}

	// Claim status of the claim currently billing the service; "none" finds
	// services that are on no claim.
	if v := strings.TrimSpace(q.Get("claim_status")); v != "" {
		if v == "none" {
			f.where = append(f.where, `NOT EXISTS (
				SELECT 1 FROM claim_lines cl JOIN claims cm ON cm.id = cl.claim_id
				WHERE cl.charge_id = c.id AND cl.is_current AND cm.status <> 'voided')`)
		} else {
			statuses := strings.Split(v, ",")
			for _, s := range statuses {
				if _, ok := claimTransitions[s]; !ok {
					return nil, "claim_status is invalid"
				}
			}
			f.add(`EXISTS (
				SELECT 1 FROM claim_lines cl JOIN claims cm ON cm.id = cl.claim_id
				WHERE cl.charge_id = c.id AND cl.is_current AND cm.status = ANY($%d))`, statuses)
		}
	}

	for key, clause := range map[string]string{
		"has_patient_balance":   "b.patient_balance > 0",
		"has_insurance_balance": "b.insurance_balance > 0",
	} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if v != "true" {
				return nil, key + " must be true"
			}
			f.where = append(f.where, clause)
		}
	}

	// Total open / closed: closed means active with nothing owed by anyone.
	if v := strings.TrimSpace(q.Get("balance")); v != "" {
		switch v {
		case "open":
			f.where = append(f.where, "b.total_balance <> 0")
		case "closed":
			f.where = append(f.where, "(c.status = 'active' AND b.total_balance = 0)")
		default:
			return nil, "balance must be open or closed"
		}
	}

	return f, ""
}

func (h *Handler) SearchTransactions(w http.ResponseWriter, r *http.Request) {
	filter, message := buildChargeFilter(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(filter.where) > 0 {
		where = "WHERE " + strings.Join(filter.where, " AND ")
	}

	matching := `
		SELECT c.id
		FROM billing_charges c
		JOIN billing_charge_balances b ON b.charge_id = c.id
		JOIN patients p ON p.id = c.patient_id
	` + where

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM (`+matching+`) m`, filter.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not search transactions")
		return
	}

	args := append(filter.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectCharge+`
		WHERE c.id IN (`+matching+`)
		ORDER BY c.date_of_service DESC, c.created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not search transactions")
		return
	}
	defer rows.Close()

	result := pagedResult[Charge]{Items: []Charge{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var c Charge

		if err := scanCharge(rows, &c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not search transactions")
			return
		}

		result.Items = append(result.Items, c)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not search transactions")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
