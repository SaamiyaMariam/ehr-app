package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// =========================================================
// TYPES
// =========================================================

type ClaimLine struct {
	ID                     string   `json:"id"`
	ChargeID               string   `json:"charge_id"`
	LineNumber             int      `json:"line_number"`
	DateOfService          string   `json:"date_of_service"`
	ServiceCode            string   `json:"service_code"`
	ServiceDescription     string   `json:"service_description"`
	Units                  int      `json:"units"`
	Modifiers              []string `json:"modifiers"`
	PlaceOfService         string   `json:"place_of_service"`
	Rate                   string   `json:"rate"`
	LineTotal              string   `json:"line_total"`
	RenderingClinicianID   string   `json:"rendering_clinician_id"`
	RenderingName          string   `json:"rendering_name"`
	RenderingNPI           string   `json:"rendering_npi"`
	DiagnosisPointers      string   `json:"diagnosis_pointers"`
	PriorAuthorizationID   string   `json:"prior_authorization_id"`
	PriorAuthorizationCode string   `json:"prior_authorization_code"`
	IsCurrent              bool     `json:"is_current"`
	// Insurance money posted against this line by this claim's payer.
	InsurancePaid    string `json:"insurance_paid"`
	InsuranceBalance string `json:"insurance_balance"`
	Adjudicated      bool   `json:"adjudicated"`
}

type ClaimDiagnosis struct {
	Position    int    `json:"position"`
	Letter      string `json:"letter"`
	ICD10Code   string `json:"icd10_code"`
	Description string `json:"description"`
}

type ClaimHistoryEvent struct {
	ID         string `json:"id"`
	ClaimID    string `json:"claim_id"`
	EventType  string `json:"event_type"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Source     string `json:"source"`
	Message    string `json:"message"`
	CreatedBy  string `json:"created_by"`
	CreatedAt  string `json:"created_at"`
}

type ClaimComment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
}

type Claim struct {
	ID                      string           `json:"id"`
	ClaimNumber             string           `json:"claim_number"`
	PatientID               string           `json:"patient_id"`
	PatientName             string           `json:"patient_name"`
	InsurancePolicyID       string           `json:"insurance_policy_id"`
	PayerID                 string           `json:"payer_id"`
	PayerName               string           `json:"payer_name"`
	Sequence                string           `json:"sequence"`
	SubmissionMethod        string           `json:"submission_method"`
	Status                  string           `json:"status"`
	ResubmissionType        string           `json:"resubmission_type"`
	PayerClaimControlNumber string           `json:"payer_claim_control_number"`
	PreviousClaimID         string           `json:"previous_claim_id"`
	TotalBilled             string           `json:"total_billed"`
	FirstDateOfService      string           `json:"first_date_of_service"`
	LastDateOfService       string           `json:"last_date_of_service"`
	Validation              *ClaimValidation `json:"validation"`
	ValidatedAt             string           `json:"validated_at"`
	ExternalReference       string           `json:"external_reference"`
	SubmittedAt             string           `json:"submitted_at"`
	CreatedAt               string           `json:"created_at"`
	UpdatedAt               string           `json:"updated_at"`

	Snapshot  *ClaimSnapshot      `json:"snapshot,omitempty"`
	Lines     []ClaimLine         `json:"lines,omitempty"`
	Diagnoses []ClaimDiagnosis    `json:"diagnoses,omitempty"`
	History   []ClaimHistoryEvent `json:"history,omitempty"`
	Comments  []ClaimComment      `json:"comments,omitempty"`
	Documents []ClaimDocument     `json:"documents,omitempty"`
}

var claimSequences = []string{"primary", "secondary", "tertiary", "quaternary"}

func diagnosisLetter(position int) string {
	return string(rune('A' + position - 1))
}

// =========================================================
// HISTORY / STATUS HELPERS
// =========================================================

func recordClaimHistory(ctx context.Context, q queryRower, claimID, eventType, from, to, source, message, userID string) error {
	_, err := q.Exec(
		ctx,
		`
		INSERT INTO claim_history (claim_id, event_type, from_status, to_status, source, message, created_by)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, ''), $7)
		`,
		claimID, eventType, from, to, source, message, nullIfEmpty(userID),
	)

	return err
}

var errInvalidClaimTransition = errors.New("invalid claim status transition")

// transitionClaim moves a (locked) claim between statuses through the
// central state machine and records the change in claim_history.
func transitionClaim(ctx context.Context, tx pgx.Tx, claimID, from, to, eventType, source, message, userID string) error {
	if !canTransitionClaim(from, to) {
		return fmt.Errorf("%w: %s → %s", errInvalidClaimTransition, from, to)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE claims SET status = $1, updated_at = NOW() WHERE id = $2`,
		to, claimID,
	); err != nil {
		return err
	}

	return recordClaimHistory(ctx, tx, claimID, eventType, from, to, source, message, userID)
}

// lockClaim locks a claim row for the rest of the transaction and returns
// its status.
func lockClaim(ctx context.Context, tx pgx.Tx, claimID string) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM claims WHERE id = $1 FOR UPDATE`, claimID).Scan(&status)
	return status, err
}

// =========================================================
// SNAPSHOT CONSTRUCTION
// =========================================================

func normalizeSex(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "m":
		return "M"
	case "female", "f":
		return "F"
	case "":
		return ""
	default:
		return "U"
	}
}

// buildClaimSnapshot reads the live patient / policy / payer / practice
// data that a claim captures.
func buildClaimSnapshot(ctx context.Context, q queryRower, patientID, policyID string) (ClaimSnapshot, error) {
	s := ClaimSnapshot{
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		OtherInsurance: []SnapshotOtherInsurance{},
	}

	var patientSex string

	err := q.QueryRow(
		ctx,
		`
		SELECT
			COALESCE(first_name, ''), COALESCE(middle_name, ''), last_name,
			COALESCE(TO_CHAR(date_of_birth, 'YYYY-MM-DD'), ''),
			COALESCE(administrative_sex, ''),
			COALESCE(NULLIF(mobile_phone, ''), NULLIF(home_phone, ''), NULLIF(work_phone, ''), ''),
			COALESCE(account_number, ''),
			COALESCE(address_1, ''), COALESCE(address_2, ''), COALESCE(city, ''),
			UPPER(COALESCE(state, '')), COALESCE(zip, '')
		FROM patients WHERE id = $1
		`,
		patientID,
	).Scan(
		&s.Patient.FirstName, &s.Patient.MiddleName, &s.Patient.LastName,
		&s.Patient.DateOfBirth, &patientSex, &s.Patient.Phone, &s.Patient.AccountNumber,
		&s.Patient.Address.Address1, &s.Patient.Address.Address2, &s.Patient.Address.City,
		&s.Patient.Address.State, &s.Patient.Address.Zip,
	)
	if err != nil {
		return s, fmt.Errorf("load patient: %w", err)
	}

	s.Patient.Sex = normalizeSex(patientSex)

	var insuredSex string

	err = q.QueryRow(
		ctx,
		`
		SELECT
			p.id, p.priority,
			COALESCE(TO_CHAR(p.coverage_start, 'YYYY-MM-DD'), ''),
			COALESCE(TO_CHAR(p.coverage_end, 'YYYY-MM-DD'), ''),
			COALESCE(p.relationship_to_policy_holder, ''),
			COALESCE(p.policy_holder_first_name, ''), COALESCE(p.policy_holder_middle_name, ''),
			COALESCE(p.policy_holder_last_name, ''),
			COALESCE(TO_CHAR(p.policy_holder_date_of_birth, 'YYYY-MM-DD'), ''),
			COALESCE(p.policy_holder_sex, ''),
			COALESCE(p.policy_holder_address_1, ''), COALESCE(p.policy_holder_address_2, ''),
			COALESCE(p.policy_holder_city, ''), UPPER(COALESCE(p.policy_holder_state, '')),
			COALESCE(p.policy_holder_zip, ''),
			COALESCE(p.member_id, ''), COALESCE(p.policy_group, ''), COALESCE(p.plan_name, ''),
			p.signature_on_file, COALESCE(p.msp_qualification, ''),
			y.id, y.payer_name, COALESCE(y.payer_id, ''), COALESCE(y.insurance_type, ''),
			y.in_network, COALESCE(y.phone, ''),
			COALESCE(y.address_1, ''), COALESCE(y.address_2, ''), COALESCE(y.city, ''),
			UPPER(COALESCE(y.state, '')), COALESCE(y.zip, '')
		FROM insurance_policies p
		JOIN payers y ON y.id = p.payer_id
		WHERE p.id = $1 AND p.patient_id = $2
		`,
		policyID, patientID,
	).Scan(
		&s.Policy.ID, &s.Policy.Priority, &s.Policy.CoverageStart, &s.Policy.CoverageEnd,
		&s.Insured.Relationship,
		&s.Insured.FirstName, &s.Insured.MiddleName, &s.Insured.LastName,
		&s.Insured.DateOfBirth, &insuredSex,
		&s.Insured.Address.Address1, &s.Insured.Address.Address2,
		&s.Insured.Address.City, &s.Insured.Address.State, &s.Insured.Address.Zip,
		&s.Insured.MemberID, &s.Insured.PolicyGroup, &s.Insured.PlanName,
		&s.Insured.SignatureOnFile, &s.Insured.MSPQualification,
		&s.Payer.ID, &s.Payer.Name, &s.Payer.PayerID, &s.Payer.InsuranceType,
		&s.Payer.InNetwork, &s.Payer.Phone,
		&s.Payer.Address.Address1, &s.Payer.Address.Address2, &s.Payer.Address.City,
		&s.Payer.Address.State, &s.Payer.Address.Zip,
	)
	if err != nil {
		return s, fmt.Errorf("load policy: %w", err)
	}

	s.Insured.Sex = normalizeSex(insuredSex)

	// When the patient is the insured, fill gaps from the patient record.
	if s.Insured.Relationship == "self" {
		if s.Insured.FirstName == "" && s.Insured.LastName == "" {
			s.Insured.FirstName = s.Patient.FirstName
			s.Insured.MiddleName = s.Patient.MiddleName
			s.Insured.LastName = s.Patient.LastName
		}
		if s.Insured.DateOfBirth == "" {
			s.Insured.DateOfBirth = s.Patient.DateOfBirth
		}
		if s.Insured.Sex == "" {
			s.Insured.Sex = s.Patient.Sex
		}
		if s.Insured.Address.Address1 == "" {
			s.Insured.Address = s.Patient.Address
		}
	}

	var practice PracticeBillingProfile
	if err := scanPracticeProfile(q.QueryRow(ctx, selectPracticeProfile), &practice); err != nil {
		return s, fmt.Errorf("load practice profile: %w", err)
	}

	s.Practice = SnapshotPractice{
		Name:         practice.PracticeName,
		NPI:          practice.NPI,
		TaxID:        practice.TaxID,
		TaxIDType:    practice.TaxIDType,
		TaxonomyCode: practice.TaxonomyCode,
		Phone:        practice.Phone,
		Address: SnapshotAddress{
			Address1: practice.Address1, Address2: practice.Address2,
			City: practice.City, State: practice.State, Zip: practice.Zip,
		},
	}

	return s, nil
}

// =========================================================
// LINE CONSTRUCTION
// =========================================================

type claimChargeRow struct {
	ID, PatientID, Status, BillingMethod, InsurancePolicyID, PayerID string
	DateOfService, ServiceCodeID, ServiceCode, ServiceDescription    string
	Units                                                            int
	Modifiers                                                        []string
	PlaceOfService, Rate, Total                                      string
	ClinicianID, ClinicianName, ClinicianNPI, ClinicianTaxonomy      string
	PriorAuthorizationID, PriorAuthorizationCode                     string
	Diagnoses                                                        []ChargeDiagnosis
	CreatedAt                                                        time.Time
}

// lockChargesForClaim loads and row-locks the requested charges (in id
// order, so concurrent claim builds cannot deadlock).
func lockChargesForClaim(ctx context.Context, tx pgx.Tx, chargeIDs []string) ([]claimChargeRow, error) {
	rows, err := tx.Query(
		ctx,
		`
		SELECT
			c.id, c.patient_id, c.status, c.billing_method,
			COALESCE(c.insurance_policy_id::text, ''), COALESCE(c.payer_id::text, ''),
			TO_CHAR(c.date_of_service, 'YYYY-MM-DD'),
			c.service_code_id, sc.code, sc.description,
			c.units,
			array_remove(ARRAY[c.modifier_1, c.modifier_2, c.modifier_3, c.modifier_4], NULL),
			c.place_of_service, c.rate_per_unit::text, c.total_charge::text,
			c.clinician_id, u.first_name || ' ' || u.last_name,
			COALESCE(cbp.npi, ''), COALESCE(cbp.taxonomy_code, ''),
			COALESCE(c.prior_authorization_id::text, ''), COALESCE(pa.authorization_code, ''),
			COALESCE(
				(SELECT json_agg(json_build_object(
					'id', d.id, 'icd10_code', d.icd10_code,
					'description', d.description, 'pointer', cd.pointer
				) ORDER BY cd.pointer)
				FROM billing_charge_diagnoses cd
				JOIN patient_diagnoses d ON d.id = cd.diagnosis_id
				WHERE cd.charge_id = c.id),
				'[]'::json
			),
			c.created_at
		FROM billing_charges c
		JOIN service_codes sc ON sc.id = c.service_code_id
		JOIN users u ON u.id = c.clinician_id
		LEFT JOIN clinician_billing_profiles cbp ON cbp.user_id = c.clinician_id
		LEFT JOIN prior_authorizations pa ON pa.id = c.prior_authorization_id
		WHERE c.id = ANY($1::uuid[])
		ORDER BY c.id
		FOR UPDATE OF c
		`,
		chargeIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []claimChargeRow

	for rows.Next() {
		var c claimChargeRow

		if err := rows.Scan(
			&c.ID, &c.PatientID, &c.Status, &c.BillingMethod, &c.InsurancePolicyID, &c.PayerID,
			&c.DateOfService, &c.ServiceCodeID, &c.ServiceCode, &c.ServiceDescription,
			&c.Units, &c.Modifiers, &c.PlaceOfService, &c.Rate, &c.Total,
			&c.ClinicianID, &c.ClinicianName, &c.ClinicianNPI, &c.ClinicianTaxonomy,
			&c.PriorAuthorizationID, &c.PriorAuthorizationCode, &c.Diagnoses, &c.CreatedAt,
		); err != nil {
			return nil, err
		}

		result = append(result, c)
	}

	return result, rows.Err()
}

type builtLine struct {
	charge   claimChargeRow
	pointers string
}

// assignClaimDiagnoses orders charges by date of service, then builds the
// claim's diagnosis list (first appearance order, unique by code) and each
// line's pointer letters. Returns an error message if more than 12
// distinct diagnoses would be needed.
func assignClaimDiagnoses(charges []claimChargeRow) ([]ClaimDiagnosis, []builtLine, string) {
	sorted := append([]claimChargeRow(nil), charges...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].DateOfService != sorted[j].DateOfService {
			return sorted[i].DateOfService < sorted[j].DateOfService
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	var diagnoses []ClaimDiagnosis
	positions := map[string]int{}
	var lines []builtLine

	for _, c := range sorted {
		var pointers strings.Builder

		for _, d := range c.Diagnoses {
			pos, ok := positions[d.ICD10Code]
			if !ok {
				pos = len(diagnoses) + 1
				if pos > 12 {
					return nil, nil, "these services use more than 12 distinct diagnoses; split them across claims"
				}
				positions[d.ICD10Code] = pos
				diagnoses = append(diagnoses, ClaimDiagnosis{
					Position: pos, Letter: diagnosisLetter(pos),
					ICD10Code: d.ICD10Code, Description: d.Description,
				})
			}

			if pointers.Len() < 4 {
				pointers.WriteString(diagnosisLetter(pos))
			}
		}

		lines = append(lines, builtLine{charge: c, pointers: pointers.String()})
	}

	return diagnoses, lines, ""
}

// writeClaimContent (re)writes a claim's diagnoses and lines.
func writeClaimContent(ctx context.Context, tx pgx.Tx, claimID, patientID, sequence string, diagnoses []ClaimDiagnosis, lines []builtLine) error {
	if _, err := tx.Exec(ctx, `DELETE FROM claim_diagnoses WHERE claim_id = $1`, claimID); err != nil {
		return err
	}

	for _, d := range diagnoses {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO claim_diagnoses (claim_id, position, icd10_code, description) VALUES ($1, $2, $3, $4)`,
			claimID, d.Position, d.ICD10Code, d.Description,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM claim_lines WHERE claim_id = $1`, claimID); err != nil {
		return err
	}

	for i, l := range lines {
		c := l.charge

		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO claim_lines (
				claim_id, charge_id, patient_id, sequence, line_number, date_of_service,
				service_code_id, service_code, service_description, units,
				modifier_1, modifier_2, modifier_3, modifier_4, place_of_service,
				rate, line_total, rendering_clinician_id, rendering_name, rendering_npi,
				rendering_taxonomy, diagnosis_pointers, prior_authorization_id, prior_authorization_code
			)
			VALUES (
				$1, $2, $3, $4, $5, $6::date,
				$7, $8, $9, $10,
				$11, $12, $13, $14, $15,
				$16::numeric, $17::numeric, $18, $19, NULLIF($20, ''),
				NULLIF($21, ''), $22, $23, NULLIF($24, '')
			)
			`,
			claimID, c.ID, patientID, sequence, i+1, c.DateOfService,
			c.ServiceCodeID, c.ServiceCode, c.ServiceDescription, c.Units,
			modifierAt(c.Modifiers, 0), modifierAt(c.Modifiers, 1), modifierAt(c.Modifiers, 2), modifierAt(c.Modifiers, 3), c.PlaceOfService,
			c.Rate, c.Total, c.ClinicianID, c.ClinicianName, c.ClinicianNPI,
			c.ClinicianTaxonomy, l.pointers, nullIfEmpty(c.PriorAuthorizationID), c.PriorAuthorizationCode,
		); err != nil {
			return err
		}
	}

	return nil
}

// =========================================================
// VALIDATION (loads data, calls the pure validator)
// =========================================================

func (h *Handler) runClaimValidation(ctx context.Context, q queryRower, claimID string) (ClaimValidation, error) {
	var snapshotJSON []byte
	var method string

	if err := q.QueryRow(ctx, `SELECT snapshot, submission_method FROM claims WHERE id = $1`, claimID).Scan(&snapshotJSON, &method); err != nil {
		return ClaimValidation{}, err
	}

	var snapshot ClaimSnapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		return ClaimValidation{}, err
	}

	var diagnoses []string

	rows, err := q.Query(ctx, `SELECT icd10_code FROM claim_diagnoses WHERE claim_id = $1 ORDER BY position`, claimID)
	if err != nil {
		return ClaimValidation{}, err
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return ClaimValidation{}, err
		}
		diagnoses = append(diagnoses, code)
	}
	rows.Close()

	rows, err = q.Query(
		ctx,
		`
		SELECT
			cl.line_number, TO_CHAR(cl.date_of_service, 'YYYY-MM-DD'), cl.service_code, cl.units,
			cl.line_total::text, cl.diagnosis_pointers, cl.rendering_name, COALESCE(cl.rendering_npi, ''),
			COALESCE(cl.prior_authorization_code, ''),
			COALESCE(pa.usage_setting, ''), pa.uses_remaining, COALESCE(pa.is_active, FALSE),
			EXISTS (
				SELECT 1 FROM prior_authorization_usage u
				WHERE u.prior_authorization_id = cl.prior_authorization_id AND u.charge_id = cl.charge_id
			)
		FROM claim_lines cl
		LEFT JOIN prior_authorizations pa ON pa.id = cl.prior_authorization_id
		WHERE cl.claim_id = $1
		ORDER BY cl.line_number
		`,
		claimID,
	)
	if err != nil {
		return ClaimValidation{}, err
	}
	defer rows.Close()

	var lines []ClaimLineForValidation

	for rows.Next() {
		var l ClaimLineForValidation
		var total, usage string
		var consumed bool

		if err := rows.Scan(
			&l.LineNumber, &l.DateOfService, &l.ServiceCode, &l.Units,
			&total, &l.DiagnosisPointers, &l.RenderingName, &l.RenderingNPI,
			&l.PriorAuthorization, &usage, &l.AuthorizationUsesRemaining, &l.AuthorizationActive, &consumed,
		); err != nil {
			return ClaimValidation{}, err
		}

		l.LineTotal = mustCents(total)

		if l.PriorAuthorization != "" && !consumed {
			l.AuthorizationUsesNeeded = authorizationUsesForLine(usage, l.Units)
		}

		lines = append(lines, l)
	}

	if err := rows.Err(); err != nil {
		return ClaimValidation{}, err
	}

	return validateClaim(snapshot, method, diagnoses, lines, time.Now()), nil
}

// revalidateClaim validates an editable claim inside tx, stores the result
// and moves it to ready / validation_error.
func (h *Handler) revalidateClaim(ctx context.Context, tx pgx.Tx, claimID, status, userID string) (ClaimValidation, error) {
	v, err := h.runClaimValidation(ctx, tx, claimID)
	if err != nil {
		return v, err
	}

	validationJSON, _ := json.Marshal(v)

	if _, err := tx.Exec(
		ctx,
		`UPDATE claims SET validation = $1, validated_at = NOW(), updated_at = NOW() WHERE id = $2`,
		validationJSON, claimID,
	); err != nil {
		return v, err
	}

	target := "ready"
	message := fmt.Sprintf("Validation passed with %d warning(s).", len(v.Warnings))

	if !v.Valid() {
		target = "validation_error"
		message = fmt.Sprintf("Validation failed: %d error(s), %d warning(s).", len(v.Errors), len(v.Warnings))
	}

	return v, transitionClaim(ctx, tx, claimID, status, target, "validated", "system", message, userID)
}

// =========================================================
// PRIOR AUTHORIZATION CONSUMPTION
// =========================================================

var errInsufficientAuthorizationUses = errors.New("prior authorization does not have enough uses remaining")

// consumeClaimAuthorizations records authorization usage for every current
// line of a claim. It is idempotent: the (authorization, charge) unique key
// means a service consumes its authorization at most once, so repeated
// generation, downloads or resubmissions never double-decrement.
func consumeClaimAuthorizations(ctx context.Context, tx pgx.Tx, claimID, milestone, userID string) error {
	rows, err := tx.Query(
		ctx,
		`
		SELECT cl.charge_id, cl.prior_authorization_id, cl.units, pa.usage_setting
		FROM claim_lines cl
		JOIN prior_authorizations pa ON pa.id = cl.prior_authorization_id
		WHERE cl.claim_id = $1 AND cl.is_current
		ORDER BY pa.id, cl.line_number
		FOR UPDATE OF pa
		`,
		claimID,
	)
	if err != nil {
		return err
	}

	type use struct {
		chargeID, authorizationID, usageSetting string
		units                                   int
	}

	var uses []use

	for rows.Next() {
		var u use
		if err := rows.Scan(&u.chargeID, &u.authorizationID, &u.units, &u.usageSetting); err != nil {
			rows.Close()
			return err
		}
		uses = append(uses, u)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return err
	}

	for _, u := range uses {
		needed := authorizationUsesForLine(u.usageSetting, u.units)

		tag, err := tx.Exec(
			ctx,
			`
			INSERT INTO prior_authorization_usage (
				prior_authorization_id, charge_id, claim_id, uses_consumed, usage_setting, milestone, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (prior_authorization_id, charge_id) DO NOTHING
			`,
			u.authorizationID, u.chargeID, claimID, needed, u.usageSetting, milestone, nullIfEmpty(userID),
		)
		if err != nil {
			return err
		}

		if tag.RowsAffected() == 0 {
			continue // already consumed for this service
		}

		tag, err = tx.Exec(
			ctx,
			`
			UPDATE prior_authorizations
			SET uses_remaining = uses_remaining - $2, updated_at = NOW()
			WHERE id = $1 AND (uses_remaining IS NULL OR uses_remaining >= $2)
			`,
			u.authorizationID, needed,
		)
		if err != nil {
			return err
		}

		if tag.RowsAffected() == 0 {
			return errInsufficientAuthorizationUses
		}
	}

	return nil
}

// =========================================================
// READ
// =========================================================

const selectClaim = `
	SELECT
		cm.id, cm.claim_number, cm.patient_id,
		COALESCE(pt.first_name || ' ', '') || pt.last_name,
		cm.insurance_policy_id, cm.payer_id, py.payer_name,
		cm.sequence, cm.submission_method, cm.status, cm.resubmission_type,
		COALESCE(cm.payer_claim_control_number, ''), COALESCE(cm.previous_claim_id::text, ''),
		cm.total_billed::text,
		COALESCE((SELECT TO_CHAR(MIN(date_of_service), 'YYYY-MM-DD') FROM claim_lines WHERE claim_id = cm.id), ''),
		COALESCE((SELECT TO_CHAR(MAX(date_of_service), 'YYYY-MM-DD') FROM claim_lines WHERE claim_id = cm.id), ''),
		cm.validation,
		COALESCE(TO_CHAR(cm.validated_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''),
		COALESCE(cm.external_reference, ''),
		COALESCE(TO_CHAR(cm.submitted_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''),
		TO_CHAR(cm.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'),
		TO_CHAR(cm.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
	FROM claims cm
	JOIN patients pt ON pt.id = cm.patient_id
	JOIN payers py ON py.id = cm.payer_id
`

func scanClaim(row pgx.Row, c *Claim) error {
	return row.Scan(
		&c.ID, &c.ClaimNumber, &c.PatientID, &c.PatientName,
		&c.InsurancePolicyID, &c.PayerID, &c.PayerName,
		&c.Sequence, &c.SubmissionMethod, &c.Status, &c.ResubmissionType,
		&c.PayerClaimControlNumber, &c.PreviousClaimID, &c.TotalBilled,
		&c.FirstDateOfService, &c.LastDateOfService,
		&c.Validation, &c.ValidatedAt, &c.ExternalReference, &c.SubmittedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
}

func (h *Handler) getClaim(ctx context.Context, q queryRower, id string) (Claim, error) {
	var c Claim

	if err := scanClaim(q.QueryRow(ctx, selectClaim+`WHERE cm.id = $1`, id), &c); err != nil {
		return c, err
	}

	var snapshotJSON []byte
	if err := q.QueryRow(ctx, `SELECT snapshot FROM claims WHERE id = $1`, id).Scan(&snapshotJSON); err != nil {
		return c, err
	}

	var snapshot ClaimSnapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		return c, err
	}
	if snapshot.OtherInsurance == nil {
		snapshot.OtherInsurance = []SnapshotOtherInsurance{}
	}
	c.Snapshot = &snapshot

	rows, err := q.Query(
		ctx,
		`
		SELECT
			cl.id, cl.charge_id, cl.line_number, TO_CHAR(cl.date_of_service, 'YYYY-MM-DD'),
			cl.service_code, cl.service_description, cl.units,
			array_remove(ARRAY[cl.modifier_1, cl.modifier_2, cl.modifier_3, cl.modifier_4], NULL),
			cl.place_of_service, cl.rate::text, cl.line_total::text,
			cl.rendering_clinician_id, cl.rendering_name, COALESCE(cl.rendering_npi, ''),
			cl.diagnosis_pointers, COALESCE(cl.prior_authorization_id::text, ''),
			COALESCE(cl.prior_authorization_code, ''), cl.is_current,
			`+claimLinePaymentSQL+`
		FROM claim_lines cl
		JOIN claims cm ON cm.id = cl.claim_id
		JOIN billing_charge_balances b ON b.charge_id = cl.charge_id
		WHERE cl.claim_id = $1
		ORDER BY cl.line_number
		`,
		id,
	)
	if err != nil {
		return c, err
	}

	c.Lines = []ClaimLine{}

	for rows.Next() {
		var l ClaimLine

		if err := rows.Scan(
			&l.ID, &l.ChargeID, &l.LineNumber, &l.DateOfService,
			&l.ServiceCode, &l.ServiceDescription, &l.Units, &l.Modifiers,
			&l.PlaceOfService, &l.Rate, &l.LineTotal,
			&l.RenderingClinicianID, &l.RenderingName, &l.RenderingNPI,
			&l.DiagnosisPointers, &l.PriorAuthorizationID, &l.PriorAuthorizationCode, &l.IsCurrent,
			&l.InsurancePaid, &l.InsuranceBalance, &l.Adjudicated,
		); err != nil {
			rows.Close()
			return c, err
		}

		if l.Modifiers == nil {
			l.Modifiers = []string{}
		}

		c.Lines = append(c.Lines, l)
	}
	rows.Close()

	rows, err = q.Query(ctx, `SELECT position, icd10_code, description FROM claim_diagnoses WHERE claim_id = $1 ORDER BY position`, id)
	if err != nil {
		return c, err
	}

	c.Diagnoses = []ClaimDiagnosis{}

	for rows.Next() {
		var d ClaimDiagnosis
		if err := rows.Scan(&d.Position, &d.ICD10Code, &d.Description); err != nil {
			rows.Close()
			return c, err
		}
		d.Letter = diagnosisLetter(d.Position)
		c.Diagnoses = append(c.Diagnoses, d)
	}
	rows.Close()

	c.Documents, err = h.listClaimDocuments(ctx, q, id)
	if err != nil {
		return c, err
	}

	c.History, err = h.loadClaimHistory(ctx, q, `WHERE h.claim_id = $1`, id)
	if err != nil {
		return c, err
	}

	rows, err = q.Query(
		ctx,
		`
		SELECT cc.id, COALESCE(u.first_name || ' ' || u.last_name, ''), cc.comment,
			TO_CHAR(cc.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM claim_comments cc
		LEFT JOIN users u ON u.id = cc.author_id
		WHERE cc.claim_id = $1
		ORDER BY cc.created_at
		`,
		id,
	)
	if err != nil {
		return c, err
	}
	defer rows.Close()

	c.Comments = []ClaimComment{}

	for rows.Next() {
		var cc ClaimComment
		if err := rows.Scan(&cc.ID, &cc.Author, &cc.Comment, &cc.CreatedAt); err != nil {
			return c, err
		}
		c.Comments = append(c.Comments, cc)
	}

	return c, rows.Err()
}

// claimLinePaymentSQL reports (insurance paid on this claim, the charge's
// insurance balance, adjudicated?) for claim line cl / claim cm. Payments
// are introduced by the insurance payments module.
const claimLinePaymentSQL = `'0.00', b.insurance_balance::text, FALSE`

func (h *Handler) loadClaimHistory(ctx context.Context, q queryRower, where string, args ...any) ([]ClaimHistoryEvent, error) {
	rows, err := q.Query(
		ctx,
		`
		SELECT h.id, h.claim_id, h.event_type, COALESCE(h.from_status, ''), COALESCE(h.to_status, ''),
			h.source, COALESCE(h.message, ''), COALESCE(u.first_name || ' ' || u.last_name, ''),
			TO_CHAR(h.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.MSOF')
		FROM claim_history h
		LEFT JOIN users u ON u.id = h.created_by
		`+where+`
		ORDER BY h.created_at, h.id
		`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ClaimHistoryEvent{}

	for rows.Next() {
		var e ClaimHistoryEvent
		if err := rows.Scan(&e.ID, &e.ClaimID, &e.EventType, &e.FromStatus, &e.ToStatus, &e.Source, &e.Message, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}

	return result, rows.Err()
}

func (h *Handler) GetClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	c, err := h.getClaim(r.Context(), h.db, id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusOK, c)
}

// buildClaimFilter turns query parameters into SQL conditions on claims cm.
func buildClaimFilter(r *http.Request) (*chargeFilter, string) {
	q := r.URL.Query()
	f := &chargeFilter{}

	for key, column := range map[string]string{"patient_id": "cm.patient_id", "payer_id": "cm.payer_id"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if !isUUID(v) {
				return nil, key + " is invalid"
			}
			f.add(column+" = $%d", v)
		}
	}

	if v := strings.TrimSpace(q.Get("clinician_id")); v != "" {
		if !isUUID(v) {
			return nil, "clinician_id is invalid"
		}
		f.add("EXISTS (SELECT 1 FROM claim_lines x WHERE x.claim_id = cm.id AND x.rendering_clinician_id = $%d)", v)
	}

	if v := strings.TrimSpace(q.Get("status")); v != "" {
		statuses := strings.Split(v, ",")
		for _, s := range statuses {
			if _, ok := claimTransitions[s]; !ok {
				return nil, "status is invalid"
			}
		}
		f.add("cm.status = ANY($%d)", statuses)
	}

	if v := strings.TrimSpace(q.Get("sequence")); v != "" {
		if !validEnum(v, claimSequences...) {
			return nil, "sequence is invalid"
		}
		f.add("cm.sequence = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("submission_method")); v != "" {
		if !validEnum(v, submissionMethods...) {
			return nil, "submission_method is invalid"
		}
		f.add("cm.submission_method = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("claim_number")); v != "" {
		if len(v) > 20 {
			return nil, "claim_number is too long"
		}
		f.add("cm.claim_number ILIKE '%%' || $%d || '%%'", v)
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			return nil, "patient search is too long"
		}
		f.add("(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%%' || $%d || '%%'", v)
	}

	if v := strings.TrimSpace(q.Get("from")); v != "" {
		if _, ok := parseDate(v); !ok {
			return nil, "from must be a valid date"
		}
		f.add("EXISTS (SELECT 1 FROM claim_lines x WHERE x.claim_id = cm.id AND x.date_of_service >= $%d::date)", v)
	}

	if v := strings.TrimSpace(q.Get("to")); v != "" {
		if _, ok := parseDate(v); !ok {
			return nil, "to must be a valid date"
		}
		f.add("EXISTS (SELECT 1 FROM claim_lines x WHERE x.claim_id = cm.id AND x.date_of_service <= $%d::date)", v)
	}

	return f, ""
}

func (h *Handler) ListClaims(w http.ResponseWriter, r *http.Request) {
	filter, message := buildClaimFilter(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(filter.where) > 0 {
		where = "WHERE " + strings.Join(filter.where, " AND ")
	}

	var total int
	if err := h.db.QueryRow(
		r.Context(),
		`SELECT COUNT(*) FROM claims cm JOIN patients pt ON pt.id = cm.patient_id `+where,
		filter.args...,
	).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claims")
		return
	}

	args := append(filter.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectClaim+where+`
		ORDER BY cm.created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claims")
		return
	}
	defer rows.Close()

	result := pagedResult[Claim]{Items: []Claim{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var c Claim
		if err := scanClaim(rows, &c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load claims")
			return
		}
		result.Items = append(result.Items, c)
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================
// CREATE
// =========================================================

type CreateClaimRequest struct {
	ChargeIDs []string `json:"charge_ids"`
	Sequence  string   `json:"sequence"`
	// Required for non-primary claims: the policy billed at that sequence.
	InsurancePolicyID string `json:"insurance_policy_id"`
}

func cleanCreateClaimRequest(req *CreateClaimRequest) string {
	req.Sequence = strings.ToLower(strings.TrimSpace(req.Sequence))
	req.InsurancePolicyID = strings.ToLower(strings.TrimSpace(req.InsurancePolicyID))

	if req.Sequence == "" {
		req.Sequence = "primary"
	}

	if !validEnum(req.Sequence, claimSequences...) {
		return "sequence must be primary, secondary, tertiary or quaternary"
	}

	if req.InsurancePolicyID != "" && !isUUID(req.InsurancePolicyID) {
		return "insurance policy not found"
	}

	if len(req.ChargeIDs) == 0 {
		return "select at least one billable service"
	}

	if len(req.ChargeIDs) > 50 {
		return "a claim can contain at most 50 services"
	}

	seen := map[string]bool{}
	for i, id := range req.ChargeIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if !isUUID(id) {
			return "billable service not found"
		}
		if seen[id] {
			return "each billable service can appear only once"
		}
		seen[id] = true
		req.ChargeIDs[i] = id
	}

	return ""
}

// claimCoverage is the payer context a new claim bills.
type claimCoverage struct {
	PolicyID         string
	PayerID          string
	SubmissionMethod string
	PreviousClaimID  string
	OtherInsurance   []SnapshotOtherInsurance
}

// checkPrimaryClaimCharges validates the charges for a primary claim and
// returns the coverage they bill. Pure (no database).
func checkPrimaryClaimCharges(charges []claimChargeRow, requested int) (claimCoverage, string) {
	var cov claimCoverage

	if len(charges) != requested {
		return cov, "one or more billable services were not found"
	}

	patientID := charges[0].PatientID

	for _, c := range charges {
		if c.PatientID != patientID {
			return cov, "all services on a claim must belong to the same patient"
		}

		if c.Status == "voided" {
			return cov, "voided services cannot be claimed"
		}

		if c.BillingMethod == "direct" {
			return cov, "directly billed services cannot be put on an insurance claim"
		}

		method := submissionMethodOf(c.BillingMethod)

		if cov.PolicyID == "" {
			cov.PolicyID = c.InsurancePolicyID
			cov.PayerID = c.PayerID
			cov.SubmissionMethod = method
		}

		if c.InsurancePolicyID != cov.PolicyID {
			return cov, "all services on a claim must bill the same insurance policy"
		}

		if method != cov.SubmissionMethod {
			return cov, "all services on a claim must use the same submission method (electronic, paper or external)"
		}
	}

	return cov, ""
}

func (h *Handler) CreateClaim(w http.ResponseWriter, r *http.Request) {
	var req CreateClaimRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if message := cleanCreateClaimRequest(&req); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create claim")
		return
	}
	defer tx.Rollback(r.Context())

	charges, err := lockChargesForClaim(r.Context(), tx, req.ChargeIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create claim")
		return
	}

	if len(charges) == 0 {
		writeError(w, http.StatusBadRequest, "one or more billable services were not found")
		return
	}

	coverage, status, message := h.resolveClaimCoverage(r.Context(), tx, charges, req)
	if message != "" {
		writeError(w, status, message)
		return
	}

	patientID := charges[0].PatientID

	// One current claim line per charge per sequence (also a unique index).
	var already []string
	rows, err := tx.Query(
		r.Context(),
		`
		SELECT DISTINCT cm.claim_number
		FROM claim_lines cl JOIN claims cm ON cm.id = cl.claim_id
		WHERE cl.charge_id = ANY($1::uuid[]) AND cl.sequence = $2 AND cl.is_current
		`,
		req.ChargeIDs, req.Sequence,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create claim")
		return
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "could not create claim")
			return
		}
		already = append(already, n)
	}
	rows.Close()

	if len(already) > 0 {
		writeError(w, http.StatusConflict, "one or more services are already on "+req.Sequence+" claim "+strings.Join(already, ", "))
		return
	}

	diagnoses, lines, message := assignClaimDiagnoses(charges)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	snapshot, err := buildClaimSnapshot(r.Context(), tx, patientID, coverage.PolicyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build claim")
		return
	}
	if coverage.OtherInsurance != nil {
		snapshot.OtherInsurance = coverage.OtherInsurance
	}

	snapshotJSON, _ := json.Marshal(snapshot)

	var total int64
	for _, c := range charges {
		total += mustCents(c.Total)
	}

	var claimID string

	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO claims (
			patient_id, insurance_policy_id, payer_id, sequence, submission_method,
			total_billed, snapshot, previous_claim_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9)
		RETURNING id
		`,
		patientID, coverage.PolicyID, coverage.PayerID, req.Sequence, coverage.SubmissionMethod,
		formatMoney(total), snapshotJSON, nullIfEmpty(coverage.PreviousClaimID), nullIfEmpty(userID),
	).Scan(&claimID); err != nil {
		writeError(w, http.StatusBadRequest, "could not create claim")
		return
	}

	if err := writeClaimContent(r.Context(), tx, claimID, patientID, req.Sequence, diagnoses, lines); err != nil {
		writeError(w, http.StatusConflict, "could not add services to the claim (already billed at this sequence?)")
		return
	}

	if err := recordClaimHistory(r.Context(), tx, claimID, "created", "", "draft", "user",
		fmt.Sprintf("Claim created with %d service line(s).", len(lines)), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create claim")
		return
	}

	if _, err := h.revalidateClaim(r.Context(), tx, claimID, "draft", userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create claim")
		return
	}

	created, err := h.getClaim(r.Context(), h.db, claimID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// resolveClaimCoverage decides which policy / payer a new claim bills.
func (h *Handler) resolveClaimCoverage(ctx context.Context, tx pgx.Tx, charges []claimChargeRow, req CreateClaimRequest) (claimCoverage, int, string) {
	if req.Sequence != "primary" {
		return claimCoverage{}, http.StatusBadRequest, "secondary and later claims are created from the secondary billing workflow"
	}

	cov, message := checkPrimaryClaimCharges(charges, len(req.ChargeIDs))
	if message != "" {
		return cov, http.StatusBadRequest, message
	}

	if req.InsurancePolicyID != "" && req.InsurancePolicyID != cov.PolicyID {
		return cov, http.StatusBadRequest, "the selected services bill a different insurance policy"
	}

	return cov, http.StatusOK, ""
}

// =========================================================
// EDIT / VALIDATE / COMMENT / CANCEL
// =========================================================

type UpdateClaimRequest struct {
	RefreshSnapshot         bool   `json:"refresh_snapshot"`
	ResubmissionType        string `json:"resubmission_type"`
	PayerClaimControlNumber string `json:"payer_claim_control_number"`
}

// validateResubmission checks resubmission details; amended / void
// resubmissions must reference the payer's original claim control number.
func validateResubmission(resubmissionType, controlNumber string) string {
	if !validEnum(resubmissionType, "new", "amended", "void") {
		return "resubmission type must be new, amended or void"
	}

	if len(controlNumber) > 50 {
		return "payer claim control number must be 50 characters or fewer"
	}

	if resubmissionType != "new" && controlNumber == "" {
		return "the payer claim control number is required for amended and void claims"
	}

	return ""
}

func (h *Handler) UpdateClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	var req UpdateClaimRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.ResubmissionType = strings.ToLower(strings.TrimSpace(req.ResubmissionType))
	req.PayerClaimControlNumber = strings.TrimSpace(req.PayerClaimControlNumber)

	if req.ResubmissionType == "" {
		req.ResubmissionType = "new"
	}

	if message := validateResubmission(req.ResubmissionType, req.PayerClaimControlNumber); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update claim")
		return
	}
	defer tx.Rollback(r.Context())

	status, err := lockClaim(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update claim")
		return
	}

	if !claimEditable(status) {
		writeError(w, http.StatusConflict, "only draft, ready or validation-error claims can be edited; start a resubmission first")
		return
	}

	var patientID, policyID, sequence string
	var otherInsurance []SnapshotOtherInsurance
	var snapshotJSON []byte

	if err := tx.QueryRow(
		r.Context(),
		`SELECT patient_id, insurance_policy_id, sequence, snapshot FROM claims WHERE id = $1`,
		id,
	).Scan(&patientID, &policyID, &sequence, &snapshotJSON); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update claim")
		return
	}

	if req.RefreshSnapshot {
		var old ClaimSnapshot
		_ = json.Unmarshal(snapshotJSON, &old)
		otherInsurance = old.OtherInsurance

		snapshot, err := buildClaimSnapshot(r.Context(), tx, patientID, policyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not refresh claim data")
			return
		}
		if otherInsurance != nil {
			snapshot.OtherInsurance = otherInsurance
		}

		// Re-snapshot the lines from their (locked) charges too.
		var chargeIDs []string
		rows, err := tx.Query(r.Context(), `SELECT charge_id::text FROM claim_lines WHERE claim_id = $1 ORDER BY line_number`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not refresh claim data")
			return
		}
		for rows.Next() {
			var cid string
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				writeError(w, http.StatusInternalServerError, "could not refresh claim data")
				return
			}
			chargeIDs = append(chargeIDs, cid)
		}
		rows.Close()

		charges, err := lockChargesForClaim(r.Context(), tx, chargeIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not refresh claim data")
			return
		}

		diagnoses, lines, message := assignClaimDiagnoses(charges)
		if message != "" {
			writeError(w, http.StatusBadRequest, message)
			return
		}

		if err := writeClaimContent(r.Context(), tx, id, patientID, sequence, diagnoses, lines); err != nil {
			writeError(w, http.StatusInternalServerError, "could not refresh claim data")
			return
		}

		newJSON, _ := json.Marshal(snapshot)
		if _, err := tx.Exec(r.Context(), `UPDATE claims SET snapshot = $1 WHERE id = $2`, newJSON, id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not refresh claim data")
			return
		}

		if err := recordClaimHistory(r.Context(), tx, id, "snapshot_refreshed", status, status, "user",
			"Claim data refreshed from current patient, policy and practice records.", userID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update claim")
			return
		}
	}

	if _, err := tx.Exec(
		r.Context(),
		`
		UPDATE claims
		SET resubmission_type = $1, payer_claim_control_number = NULLIF($2, ''), updated_at = NOW()
		WHERE id = $3
		`,
		req.ResubmissionType, req.PayerClaimControlNumber, id,
	); err != nil {
		writeError(w, http.StatusBadRequest, "could not update claim")
		return
	}

	if _, err := h.revalidateClaim(r.Context(), tx, id, status, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update claim")
		return
	}

	updated, err := h.getClaim(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) ValidateClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}
	defer tx.Rollback(r.Context())

	status, err := lockClaim(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}

	var v ClaimValidation

	if claimEditable(status) {
		v, err = h.revalidateClaim(r.Context(), tx, id, status, currentUserID(r))
	} else {
		// Submitted claims are frozen: report only, no status change.
		v, err = h.runClaimValidation(r.Context(), tx, id)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate claim")
		return
	}

	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) AddClaimComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	var req struct {
		Comment string `json:"comment"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Comment = strings.TrimSpace(req.Comment)

	if req.Comment == "" || len(req.Comment) > 2000 {
		writeError(w, http.StatusBadRequest, "a comment of up to 2000 characters is required")
		return
	}

	tag, err := h.db.Exec(
		r.Context(),
		`
		INSERT INTO claim_comments (claim_id, author_id, comment)
		SELECT id, $2, $3 FROM claims WHERE id = $1
		`,
		id, nullIfEmpty(currentUserID(r)), req.Comment,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add comment")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	updated, err := h.getClaim(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusCreated, updated)
}

// releaseClaimLines makes a voided claim's services billable again.
func releaseClaimLines(ctx context.Context, tx pgx.Tx, claimID string) error {
	_, err := tx.Exec(ctx, `UPDATE claim_lines SET is_current = FALSE WHERE claim_id = $1`, claimID)
	return err
}

// CancelClaim voids a claim that was never submitted.
func (h *Handler) CancelClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
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
		writeError(w, http.StatusBadRequest, "a reason (up to 500 characters) is required")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel claim")
		return
	}
	defer tx.Rollback(r.Context())

	status, err := lockClaim(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel claim")
		return
	}

	var submittedBefore bool
	if err := tx.QueryRow(r.Context(), `SELECT submitted_at IS NOT NULL FROM claims WHERE id = $1`, id).Scan(&submittedBefore); err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel claim")
		return
	}

	if status == "voided" {
		writeError(w, http.StatusConflict, "claim is already voided")
		return
	}

	if !claimEditable(status) || submittedBefore {
		writeError(w, http.StatusConflict, "this claim has been submitted; send a void resubmission to the payer instead")
		return
	}

	if err := transitionClaim(r.Context(), tx, id, status, "voided", "cancelled", "user", req.Reason, currentUserID(r)); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := releaseClaimLines(r.Context(), tx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel claim")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel claim")
		return
	}

	updated, err := h.getClaim(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// ListClaimableCharges returns a patient's charges that can go on a new
// primary claim (insurance-billed, active, not on a current primary claim).
func (h *Handler) ListClaimableCharges(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectCharge+`
		WHERE c.patient_id = $1
		  AND c.status = 'active'
		  AND c.billing_method <> 'direct'
		  AND NOT EXISTS (
			SELECT 1 FROM claim_lines cl
			WHERE cl.charge_id = c.id AND cl.sequence = 'primary' AND cl.is_current
		  )
		ORDER BY c.date_of_service, c.created_at
		LIMIT 500
		`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billable services")
		return
	}
	defer rows.Close()

	result := []Charge{}

	for rows.Next() {
		var c Charge
		if err := scanCharge(rows, &c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load billable services")
			return
		}
		result = append(result, c)
	}

	writeJSON(w, http.StatusOK, result)
}
