package billing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	validPriorities = map[string]bool{
		"primary":    true,
		"secondary":  true,
		"tertiary":   true,
		"quaternary": true,
	}

	validAppointmentLimitTypes = map[string]bool{
		"number":    true,
		"unlimited": true,
		"unknown":   true,
	}

	validRelationships = map[string]bool{
		"":       true,
		"self":   true,
		"spouse": true,
		"child":  true,
		"other":  true,
	}

	validSexes = map[string]bool{
		"":        true,
		"male":    true,
		"female":  true,
		"unknown": true,
	}

	// Matches NUMERIC(10, 2): non-negative, up to 8 integer digits and 2 decimals.
	moneyPattern = regexp.MustCompile(`^\d{1,8}(\.\d{1,2})?$`)
)

type InsurancePolicy struct {
	ID        string `json:"id"`
	PatientID string `json:"patient_id"`

	PayerID   string `json:"payer_id"`
	PayerName string `json:"payer_name"`

	Priority string `json:"priority"`

	MemberID    string `json:"member_id"`
	PolicyGroup string `json:"policy_group"`
	PlanName    string `json:"plan_name"`

	PolicyComments string `json:"policy_comments"`

	SignatureOnFile bool `json:"signature_on_file"`

	CoverageStart string `json:"coverage_start"`
	CoverageEnd   string `json:"coverage_end"`

	// Decimal amounts are strings to avoid float rounding, e.g. "25.00".
	Copay      string `json:"copay"`
	Deductible string `json:"deductible"`

	AppointmentLimitType   string `json:"appointment_limit_type"`
	AppointmentsAllowed    *int   `json:"appointments_allowed"`
	AppointmentsExpiration string `json:"appointments_expiration"`

	RelationshipToPolicyHolder string `json:"relationship_to_policy_holder"`

	PolicyHolderFirstName  string `json:"policy_holder_first_name"`
	PolicyHolderMiddleName string `json:"policy_holder_middle_name"`
	PolicyHolderLastName   string `json:"policy_holder_last_name"`

	PolicyHolderDateOfBirth string `json:"policy_holder_date_of_birth"`
	PolicyHolderSex         string `json:"policy_holder_sex"`

	PolicyHolderAddress1 string `json:"policy_holder_address_1"`
	PolicyHolderAddress2 string `json:"policy_holder_address_2"`
	PolicyHolderCity     string `json:"policy_holder_city"`
	PolicyHolderState    string `json:"policy_holder_state"`
	PolicyHolderZip      string `json:"policy_holder_zip"`

	MSPQualification string `json:"msp_qualification"`

	IsActive bool `json:"is_active"`
}

type InsurancePolicyListItem struct {
	ID            string `json:"id"`
	PayerID       string `json:"payer_id"`
	PayerName     string `json:"payer_name"`
	Priority      string `json:"priority"`
	MemberID      string `json:"member_id"`
	PlanName      string `json:"plan_name"`
	CoverageStart string `json:"coverage_start"`
	CoverageEnd   string `json:"coverage_end"`
	IsActive      bool   `json:"is_active"`
}

func cleanPolicy(p *InsurancePolicy) {
	p.PayerID = strings.TrimSpace(p.PayerID)
	p.Priority = strings.ToLower(strings.TrimSpace(p.Priority))

	p.MemberID = strings.TrimSpace(p.MemberID)
	p.PolicyGroup = strings.TrimSpace(p.PolicyGroup)
	p.PlanName = strings.TrimSpace(p.PlanName)
	p.PolicyComments = strings.TrimSpace(p.PolicyComments)

	p.CoverageStart = strings.TrimSpace(p.CoverageStart)
	p.CoverageEnd = strings.TrimSpace(p.CoverageEnd)

	p.Copay = strings.TrimSpace(p.Copay)
	p.Deductible = strings.TrimSpace(p.Deductible)

	p.AppointmentLimitType = strings.ToLower(strings.TrimSpace(p.AppointmentLimitType))
	p.AppointmentsExpiration = strings.TrimSpace(p.AppointmentsExpiration)

	p.RelationshipToPolicyHolder = strings.ToLower(strings.TrimSpace(p.RelationshipToPolicyHolder))

	p.PolicyHolderFirstName = strings.TrimSpace(p.PolicyHolderFirstName)
	p.PolicyHolderMiddleName = strings.TrimSpace(p.PolicyHolderMiddleName)
	p.PolicyHolderLastName = strings.TrimSpace(p.PolicyHolderLastName)

	p.PolicyHolderDateOfBirth = strings.TrimSpace(p.PolicyHolderDateOfBirth)
	p.PolicyHolderSex = strings.ToLower(strings.TrimSpace(p.PolicyHolderSex))

	p.PolicyHolderAddress1 = strings.TrimSpace(p.PolicyHolderAddress1)
	p.PolicyHolderAddress2 = strings.TrimSpace(p.PolicyHolderAddress2)
	p.PolicyHolderCity = strings.TrimSpace(p.PolicyHolderCity)
	p.PolicyHolderState = strings.TrimSpace(p.PolicyHolderState)
	p.PolicyHolderZip = strings.TrimSpace(p.PolicyHolderZip)

	p.MSPQualification = strings.TrimSpace(p.MSPQualification)
}

func parseDate(value string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", value)
	return t, err == nil
}

// validatePolicy checks only what must be true for a policy to be stored.
// Policies may be saved incomplete; claim-level completeness is validated
// later by the Claims module. It also applies defaults and returns a
// user-facing message, or "" when the policy is valid.
func validatePolicy(p *InsurancePolicy) string {
	if p.PayerID == "" {
		return "payer is required"
	}

	if !isUUID(p.PayerID) {
		return "payer not found"
	}

	if p.Priority == "" {
		p.Priority = "primary"
	}

	if !validPriorities[p.Priority] {
		return "priority must be primary, secondary, tertiary or quaternary"
	}

	dates := []struct {
		label string
		value string
	}{
		{"coverage start", p.CoverageStart},
		{"coverage end", p.CoverageEnd},
		{"appointments expiration", p.AppointmentsExpiration},
		{"policy holder date of birth", p.PolicyHolderDateOfBirth},
	}

	for _, d := range dates {
		if d.value == "" {
			continue
		}

		if _, ok := parseDate(d.value); !ok {
			return d.label + " must be a valid date (YYYY-MM-DD)"
		}
	}

	if p.CoverageStart != "" && p.CoverageEnd != "" {
		start, _ := parseDate(p.CoverageStart)
		end, _ := parseDate(p.CoverageEnd)

		if end.Before(start) {
			return "coverage end cannot be before coverage start"
		}
	}

	if p.Copay != "" && !moneyPattern.MatchString(p.Copay) {
		return "copay must be a non-negative amount with at most 2 decimal places"
	}

	if p.Deductible != "" && !moneyPattern.MatchString(p.Deductible) {
		return "deductible must be a non-negative amount with at most 2 decimal places"
	}

	if p.AppointmentLimitType == "" {
		p.AppointmentLimitType = "unknown"
	}

	if !validAppointmentLimitTypes[p.AppointmentLimitType] {
		return "appointment limit type must be number, unlimited or unknown"
	}

	if p.AppointmentLimitType == "number" {
		if p.AppointmentsAllowed != nil &&
			(*p.AppointmentsAllowed < 0 || *p.AppointmentsAllowed > math.MaxInt32) {
			return "appointments allowed must be zero or more"
		}
	} else {
		// Allowance details only apply to a numeric limit.
		p.AppointmentsAllowed = nil
		p.AppointmentsExpiration = ""
	}

	if !validRelationships[p.RelationshipToPolicyHolder] {
		return "relationship to policy holder must be self, spouse, child or other"
	}

	if !validSexes[p.PolicyHolderSex] {
		return "policy holder sex must be male, female or unknown"
	}

	return ""
}

// policyWriteArgs returns the editable columns in the order used by the
// INSERT and UPDATE statements ($1..$26).
func policyWriteArgs(p *InsurancePolicy) []any {
	return []any{
		p.PayerID,
		p.Priority,
		p.MemberID,
		p.PolicyGroup,
		p.PlanName,
		p.PolicyComments,
		p.SignatureOnFile,
		p.CoverageStart,
		p.CoverageEnd,
		p.Copay,
		p.Deductible,
		p.AppointmentLimitType,
		p.AppointmentsAllowed,
		p.AppointmentsExpiration,
		p.RelationshipToPolicyHolder,
		p.PolicyHolderFirstName,
		p.PolicyHolderMiddleName,
		p.PolicyHolderLastName,
		p.PolicyHolderDateOfBirth,
		p.PolicyHolderSex,
		p.PolicyHolderAddress1,
		p.PolicyHolderAddress2,
		p.PolicyHolderCity,
		p.PolicyHolderState,
		p.PolicyHolderZip,
		p.MSPQualification,
	}
}

// checkPayer reports whether a payer exists and is enabled.
func (h *Handler) checkPayer(ctx context.Context, payerID string) (found bool, active bool, err error) {
	err = h.db.QueryRow(
		ctx,
		`SELECT is_active FROM payers WHERE id = $1`,
		payerID,
	).Scan(&active)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}

	if err != nil {
		return false, false, err
	}

	return true, active, nil
}

func (h *Handler) getPolicy(ctx context.Context, id string) (InsurancePolicy, error) {
	var p InsurancePolicy

	err := h.db.QueryRow(
		ctx,
		`
		SELECT
			p.id,
			p.patient_id,
			p.payer_id,
			y.payer_name,
			p.priority,
			COALESCE(p.member_id, ''),
			COALESCE(p.policy_group, ''),
			COALESCE(p.plan_name, ''),
			COALESCE(p.policy_comments, ''),
			p.signature_on_file,
			COALESCE(TO_CHAR(p.coverage_start, 'YYYY-MM-DD'), ''),
			COALESCE(TO_CHAR(p.coverage_end, 'YYYY-MM-DD'), ''),
			COALESCE(p.copay::text, ''),
			COALESCE(p.deductible::text, ''),
			p.appointment_limit_type,
			p.appointments_allowed,
			COALESCE(TO_CHAR(p.appointments_expiration, 'YYYY-MM-DD'), ''),
			COALESCE(p.relationship_to_policy_holder, ''),
			COALESCE(p.policy_holder_first_name, ''),
			COALESCE(p.policy_holder_middle_name, ''),
			COALESCE(p.policy_holder_last_name, ''),
			COALESCE(TO_CHAR(p.policy_holder_date_of_birth, 'YYYY-MM-DD'), ''),
			COALESCE(p.policy_holder_sex, ''),
			COALESCE(p.policy_holder_address_1, ''),
			COALESCE(p.policy_holder_address_2, ''),
			COALESCE(p.policy_holder_city, ''),
			COALESCE(p.policy_holder_state, ''),
			COALESCE(p.policy_holder_zip, ''),
			COALESCE(p.msp_qualification, ''),
			p.is_active
		FROM insurance_policies p
		JOIN payers y ON y.id = p.payer_id
		WHERE p.id = $1
		`,
		id,
	).Scan(
		&p.ID,
		&p.PatientID,
		&p.PayerID,
		&p.PayerName,
		&p.Priority,
		&p.MemberID,
		&p.PolicyGroup,
		&p.PlanName,
		&p.PolicyComments,
		&p.SignatureOnFile,
		&p.CoverageStart,
		&p.CoverageEnd,
		&p.Copay,
		&p.Deductible,
		&p.AppointmentLimitType,
		&p.AppointmentsAllowed,
		&p.AppointmentsExpiration,
		&p.RelationshipToPolicyHolder,
		&p.PolicyHolderFirstName,
		&p.PolicyHolderMiddleName,
		&p.PolicyHolderLastName,
		&p.PolicyHolderDateOfBirth,
		&p.PolicyHolderSex,
		&p.PolicyHolderAddress1,
		&p.PolicyHolderAddress2,
		&p.PolicyHolderCity,
		&p.PolicyHolderState,
		&p.PolicyHolderZip,
		&p.MSPQualification,
		&p.IsActive,
	)

	return p, err
}

func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policies")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			p.id,
			p.payer_id,
			y.payer_name,
			p.priority,
			COALESCE(p.member_id, ''),
			COALESCE(p.plan_name, ''),
			COALESCE(TO_CHAR(p.coverage_start, 'YYYY-MM-DD'), ''),
			COALESCE(TO_CHAR(p.coverage_end, 'YYYY-MM-DD'), ''),
			p.is_active
		FROM insurance_policies p
		JOIN payers y ON y.id = p.payer_id
		WHERE p.patient_id = $1
		ORDER BY
			p.is_active DESC,
			ARRAY_POSITION(
				ARRAY['primary', 'secondary', 'tertiary', 'quaternary']::varchar[],
				p.priority
			),
			p.coverage_start DESC NULLS FIRST,
			p.created_at DESC
		`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policies")
		return
	}
	defer rows.Close()

	result := make([]InsurancePolicyListItem, 0)

	for rows.Next() {
		var item InsurancePolicyListItem

		if err := rows.Scan(
			&item.ID,
			&item.PayerID,
			&item.PayerName,
			&item.Priority,
			&item.MemberID,
			&item.PlanName,
			&item.CoverageStart,
			&item.CoverageEnd,
			&item.IsActive,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load insurance policies")
			return
		}

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policies")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var policy InsurancePolicy

	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPolicy(&policy)

	if message := validatePolicy(&policy); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create insurance policy")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	found, active, err := h.checkPayer(r.Context(), policy.PayerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create insurance policy")
		return
	}

	if !found {
		writeError(w, http.StatusBadRequest, "payer not found")
		return
	}

	if !active {
		writeError(w, http.StatusBadRequest, "payer is disabled")
		return
	}

	// The patient always comes from the URL ($27), never from the body.
	args := append(policyWriteArgs(&policy), patientID)

	var id string

	err = h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO insurance_policies (
			patient_id,
			payer_id,
			priority,
			member_id,
			policy_group,
			plan_name,
			policy_comments,
			signature_on_file,
			coverage_start,
			coverage_end,
			copay,
			deductible,
			appointment_limit_type,
			appointments_allowed,
			appointments_expiration,
			relationship_to_policy_holder,
			policy_holder_first_name,
			policy_holder_middle_name,
			policy_holder_last_name,
			policy_holder_date_of_birth,
			policy_holder_sex,
			policy_holder_address_1,
			policy_holder_address_2,
			policy_holder_city,
			policy_holder_state,
			policy_holder_zip,
			msp_qualification
		)
		VALUES (
			$27,
			$1,
			$2,
			NULLIF($3, ''),
			NULLIF($4, ''),
			NULLIF($5, ''),
			NULLIF($6, ''),
			$7,
			NULLIF($8, '')::date,
			NULLIF($9, '')::date,
			NULLIF($10, '')::numeric,
			NULLIF($11, '')::numeric,
			$12,
			$13,
			NULLIF($14, '')::date,
			NULLIF($15, ''),
			NULLIF($16, ''),
			NULLIF($17, ''),
			NULLIF($18, ''),
			NULLIF($19, '')::date,
			NULLIF($20, ''),
			NULLIF($21, ''),
			NULLIF($22, ''),
			NULLIF($23, ''),
			NULLIF($24, ''),
			NULLIF($25, ''),
			NULLIF($26, '')
		)
		RETURNING id
		`,
		args...,
	).Scan(&id)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create insurance policy")
		return
	}

	created, err := h.getPolicy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policy")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	policy, err := h.getPolicy(r.Context(), id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policy")
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	var policy InsurancePolicy

	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPolicy(&policy)

	if message := validatePolicy(&policy); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	var currentPayerID string

	err := h.db.QueryRow(
		r.Context(),
		`SELECT payer_id FROM insurance_policies WHERE id = $1`,
		id,
	).Scan(&currentPayerID)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update insurance policy")
		return
	}

	// A policy may keep a payer that was disabled after it was created,
	// but may only be moved to a payer that is currently enabled.
	if !strings.EqualFold(policy.PayerID, currentPayerID) {
		found, active, err := h.checkPayer(r.Context(), policy.PayerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not update insurance policy")
			return
		}

		if !found {
			writeError(w, http.StatusBadRequest, "payer not found")
			return
		}

		if !active {
			writeError(w, http.StatusBadRequest, "payer is disabled")
			return
		}
	}

	// patient_id and is_active are deliberately not updatable here.
	args := append(policyWriteArgs(&policy), id)

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE insurance_policies
		SET
			payer_id = $1,
			priority = $2,
			member_id = NULLIF($3, ''),
			policy_group = NULLIF($4, ''),
			plan_name = NULLIF($5, ''),
			policy_comments = NULLIF($6, ''),
			signature_on_file = $7,
			coverage_start = NULLIF($8, '')::date,
			coverage_end = NULLIF($9, '')::date,
			copay = NULLIF($10, '')::numeric,
			deductible = NULLIF($11, '')::numeric,
			appointment_limit_type = $12,
			appointments_allowed = $13,
			appointments_expiration = NULLIF($14, '')::date,
			relationship_to_policy_holder = NULLIF($15, ''),
			policy_holder_first_name = NULLIF($16, ''),
			policy_holder_middle_name = NULLIF($17, ''),
			policy_holder_last_name = NULLIF($18, ''),
			policy_holder_date_of_birth = NULLIF($19, '')::date,
			policy_holder_sex = NULLIF($20, ''),
			policy_holder_address_1 = NULLIF($21, ''),
			policy_holder_address_2 = NULLIF($22, ''),
			policy_holder_city = NULLIF($23, ''),
			policy_holder_state = NULLIF($24, ''),
			policy_holder_zip = NULLIF($25, ''),
			msp_qualification = NULLIF($26, ''),
			updated_at = NOW()
		WHERE id = $27
		`,
		args...,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update insurance policy")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	updated, err := h.getPolicy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance policy")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) SetPolicyActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeError(w, http.StatusBadRequest, "is_active is required")
		return
	}

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE insurance_policies
		SET
			is_active = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		*req.IsActive,
		id,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update insurance policy status")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"is_active": *req.IsActive,
	})
}
