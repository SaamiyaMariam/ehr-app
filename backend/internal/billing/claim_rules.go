package billing

import (
	"fmt"
	"regexp"
	"time"
)

// =========================================================
// CLAIM SNAPSHOT
// =========================================================

// ClaimSnapshot is the demographic / insurance / provider data captured when
// a claim is built. It is stored as JSONB and never re-read from live tables
// once the claim leaves the editable states.
type ClaimSnapshot struct {
	CapturedAt string `json:"captured_at"`

	Patient  SnapshotPerson   `json:"patient"`
	Insured  SnapshotInsured  `json:"insured"`
	Payer    SnapshotPayer    `json:"payer"`
	Practice SnapshotPractice `json:"practice"`
	Policy   SnapshotPolicy   `json:"policy"`

	// Other coverage (e.g. the primary payer on a secondary claim).
	OtherInsurance []SnapshotOtherInsurance `json:"other_insurance"`
}

type SnapshotAddress struct {
	Address1 string `json:"address_1"`
	Address2 string `json:"address_2"`
	City     string `json:"city"`
	State    string `json:"state"`
	Zip      string `json:"zip"`
}

type SnapshotPerson struct {
	FirstName     string          `json:"first_name"`
	MiddleName    string          `json:"middle_name"`
	LastName      string          `json:"last_name"`
	DateOfBirth   string          `json:"date_of_birth"`
	Sex           string          `json:"sex"`
	Phone         string          `json:"phone"`
	AccountNumber string          `json:"account_number"`
	Address       SnapshotAddress `json:"address"`
}

type SnapshotInsured struct {
	Relationship     string          `json:"relationship"`
	FirstName        string          `json:"first_name"`
	MiddleName       string          `json:"middle_name"`
	LastName         string          `json:"last_name"`
	DateOfBirth      string          `json:"date_of_birth"`
	Sex              string          `json:"sex"`
	Address          SnapshotAddress `json:"address"`
	MemberID         string          `json:"member_id"`
	PolicyGroup      string          `json:"policy_group"`
	PlanName         string          `json:"plan_name"`
	SignatureOnFile  bool            `json:"signature_on_file"`
	MSPQualification string          `json:"msp_qualification"`
}

type SnapshotPayer struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	PayerID       string          `json:"payer_id"`
	InsuranceType string          `json:"insurance_type"`
	InNetwork     bool            `json:"in_network"`
	Phone         string          `json:"phone"`
	Address       SnapshotAddress `json:"address"`
}

type SnapshotPractice struct {
	Name         string          `json:"name"`
	NPI          string          `json:"npi"`
	TaxID        string          `json:"tax_id"`
	TaxIDType    string          `json:"tax_id_type"`
	TaxonomyCode string          `json:"taxonomy_code"`
	Phone        string          `json:"phone"`
	Address      SnapshotAddress `json:"address"`
}

type SnapshotPolicy struct {
	ID            string `json:"id"`
	Priority      string `json:"priority"`
	CoverageStart string `json:"coverage_start"`
	CoverageEnd   string `json:"coverage_end"`
}

type SnapshotOtherInsurance struct {
	Sequence       string `json:"sequence"`
	PayerName      string `json:"payer_name"`
	MemberID       string `json:"member_id"`
	PolicyGroup    string `json:"policy_group"`
	InsuredName    string `json:"insured_name"`
	ClaimNumber    string `json:"claim_number"`
	AmountPaid     string `json:"amount_paid"`
	AdjudicatedFor string `json:"adjudicated_for"`
}

// ClaimLineForValidation carries a line plus live authorization usage data.
type ClaimLineForValidation struct {
	LineNumber         int
	DateOfService      string
	ServiceCode        string
	Units              int
	LineTotal          int64
	DiagnosisPointers  string
	RenderingName      string
	RenderingNPI       string
	PriorAuthorization string
	// Uses still needed from the authorization at submission (0 when
	// already consumed or no limit applies) and what remains.
	AuthorizationUsesNeeded    int
	AuthorizationUsesRemaining *int
	AuthorizationActive        bool
}

type ClaimValidation struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

func (v ClaimValidation) Valid() bool {
	return len(v.Errors) == 0
}

var (
	statePattern = regexp.MustCompile(`^[A-Z]{2}$`)
	zipPattern   = regexp.MustCompile(`^[0-9]{5}(-?[0-9]{4})?$`)
	taxIDPattern = regexp.MustCompile(`^[0-9]{9}$`)
)

// validNPI checks a 10-digit NPI with the Luhn check digit computed over the
// "80840" health-industry prefix.
func validNPI(npi string) bool {
	if len(npi) != 10 {
		return false
	}

	digits := "80840" + npi[:9]
	sum := 0
	double := true

	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if d < 0 || d > 9 {
			return false
		}

		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}

		sum += d
		double = !double
	}

	check := (10 - sum%10) % 10

	return npi[9] >= '0' && npi[9] <= '9' && int(npi[9]-'0') == check
}

// validateClaim is the central, database-free claim validator. Errors block
// submission; warnings do not.
//
// Boundary: this checks the data the practice controls for completeness and
// internal consistency (CMS-1500 required items). It is NOT payer-specific
// edit checking and NOT X12 837 compliance validation; a clearinghouse or
// payer may still reject a claim that passes here.
func validateClaim(
	s ClaimSnapshot,
	submissionMethod string,
	diagnoses []string,
	lines []ClaimLineForValidation,
	today time.Time,
) ClaimValidation {
	v := ClaimValidation{Errors: []string{}, Warnings: []string{}}
	errf := func(format string, args ...any) { v.Errors = append(v.Errors, fmt.Sprintf(format, args...)) }
	warnf := func(format string, args ...any) { v.Warnings = append(v.Warnings, fmt.Sprintf(format, args...)) }

	if !validEnum(submissionMethod, submissionMethods...) {
		errf("Submission method is invalid.")
	}

	// Patient (CMS-1500 items 2, 3, 5)
	p := s.Patient
	if p.FirstName == "" || p.LastName == "" {
		errf("Patient first and last name are required.")
	}
	if p.DateOfBirth == "" {
		errf("Patient date of birth is required.")
	}
	if p.Sex == "" {
		warnf("Patient sex is not recorded.")
	}
	checkAddress(p.Address, "Patient", errf, warnf)

	// Insured / subscriber (items 1a, 4, 6, 7, 11)
	in := s.Insured
	if in.MemberID == "" {
		errf("Insured member ID is required.")
	}
	if in.Relationship == "" {
		errf("Patient relationship to insured is required.")
	} else if in.Relationship != "self" {
		if in.FirstName == "" || in.LastName == "" {
			errf("Insured first and last name are required when the patient is not the insured.")
		}
		if in.DateOfBirth == "" {
			errf("Insured date of birth is required when the patient is not the insured.")
		}
		if in.Address.Address1 == "" {
			warnf("Insured address is not recorded.")
		}
	}
	if in.Sex == "" {
		warnf("Insured sex is not recorded.")
	}
	if !in.SignatureOnFile {
		warnf("Signature on file is not indicated for this policy.")
	}

	// Payer
	if s.Payer.Name == "" {
		errf("Payer name is required.")
	}
	if s.Payer.PayerID == "" {
		if submissionMethod == "electronic" {
			errf("Payer ID is required for electronic claims.")
		} else {
			warnf("Payer ID is not recorded.")
		}
	}
	if submissionMethod == "paper" && s.Payer.Address.Address1 == "" {
		warnf("Payer mailing address is not recorded.")
	}
	if s.Payer.InsuranceType == "" {
		warnf("Payer insurance type is not set (CMS-1500 item 1).")
	}

	// Billing provider (items 25, 33)
	pr := s.Practice
	if pr.Name == "" {
		errf("Practice billing name is required (Billing Settings).")
	}
	if !validNPI(pr.NPI) {
		errf("A valid practice NPI is required (Billing Settings).")
	}
	if !taxIDPattern.MatchString(pr.TaxID) {
		errf("Practice tax ID is required (Billing Settings).")
	}
	checkAddress(pr.Address, "Practice", errf, warnf)
	if pr.Phone == "" {
		warnf("Practice phone is not recorded.")
	}

	// Diagnoses (item 21)
	if len(diagnoses) == 0 {
		errf("At least one diagnosis is required.")
	}
	if len(diagnoses) > 12 {
		errf("A claim can carry at most 12 diagnoses; split the services across claims.")
	}

	// Service lines (item 24)
	if len(lines) == 0 {
		errf("A claim needs at least one service line.")
	}

	todayText := today.Format("2006-01-02")
	timelyLimit := today.AddDate(-1, 0, 0).Format("2006-01-02")

	for _, l := range lines {
		label := fmt.Sprintf("Line %d", l.LineNumber)

		if l.DateOfService == "" {
			errf("%s: date of service is required.", label)
		} else {
			if l.DateOfService > todayText {
				errf("%s: date of service is in the future.", label)
			}
			if l.DateOfService < timelyLimit {
				warnf("%s: date of service is over a year old; check the payer's timely filing limit.", label)
			}
			if s.Policy.CoverageStart != "" && l.DateOfService < s.Policy.CoverageStart {
				errf("%s: date of service is before the policy coverage start.", label)
			}
			if s.Policy.CoverageEnd != "" && l.DateOfService > s.Policy.CoverageEnd {
				errf("%s: date of service is after the policy coverage end.", label)
			}
		}

		if l.ServiceCode == "" {
			errf("%s: service code is required.", label)
		}
		if l.Units < 1 {
			errf("%s: units must be at least 1.", label)
		}
		if l.LineTotal <= 0 {
			errf("%s: billed amount must be greater than zero.", label)
		}
		if l.DiagnosisPointers == "" {
			errf("%s: at least one diagnosis pointer is required.", label)
		}
		if l.RenderingName == "" {
			errf("%s: rendering clinician is required.", label)
		}
		if !validNPI(l.RenderingNPI) {
			errf("%s: rendering clinician %s needs a valid NPI (user billing profile).", label, l.RenderingName)
		}

		if l.PriorAuthorization != "" && l.AuthorizationUsesNeeded > 0 {
			if !l.AuthorizationActive {
				errf("%s: prior authorization %s is disabled.", label, l.PriorAuthorization)
			} else if l.AuthorizationUsesRemaining != nil && *l.AuthorizationUsesRemaining < l.AuthorizationUsesNeeded {
				errf("%s: prior authorization %s does not have enough uses remaining.", label, l.PriorAuthorization)
			}
		}
	}

	return v
}

func checkAddress(a SnapshotAddress, who string, errf, warnf func(string, ...any)) {
	if a.Address1 == "" || a.City == "" || a.State == "" || a.Zip == "" {
		errf("%s address (street, city, state, ZIP) is required.", who)
		return
	}

	if !statePattern.MatchString(a.State) {
		warnf("%s state should be a 2-letter code.", who)
	}

	if !zipPattern.MatchString(a.Zip) {
		warnf("%s ZIP code format looks invalid.", who)
	}
}

// =========================================================
// CLAIM STATUS MACHINE
// =========================================================

// claimTransitions is the single definition of allowed status changes.
//
//	draft / validation_error / ready  – editable, re-validatable
//	pending_submission                – handed to a clearinghouse adapter
//	submitted / sent / resubmitted    – electronic lifecycle
//	paper_generated                   – CMS-1500 produced, not yet mailed
//	externally_submitted              – user confirmed submission outside the app
//	rejected_new → rejected           – rejection received → reviewed
//	paid                              – payer finished adjudicating every line
//	voided                            – cancelled, or void resubmission sent
var claimTransitions = map[string][]string{
	"draft":                {"ready", "validation_error", "voided"},
	"validation_error":     {"ready", "validation_error", "draft", "voided"},
	"ready":                {"ready", "validation_error", "draft", "pending_submission", "submitted", "resubmitted", "paper_generated", "externally_submitted", "voided"},
	"pending_submission":   {"submitted", "rejected_new", "ready"},
	"submitted":            {"sent", "rejected_new", "paid", "draft"},
	"sent":                 {"rejected_new", "paid", "draft"},
	"resubmitted":          {"sent", "rejected_new", "paid", "draft", "voided"},
	"paper_generated":      {"paper_generated", "submitted", "resubmitted", "rejected_new", "paid", "draft"},
	"externally_submitted": {"rejected_new", "paid", "draft", "voided"},
	"rejected_new":         {"rejected", "draft"},
	"rejected":             {"draft"},
	"paid":                 {"submitted", "sent", "resubmitted", "paper_generated", "externally_submitted", "draft"},
	"voided":               {},
}

func canTransitionClaim(from, to string) bool {
	for _, allowed := range claimTransitions[from] {
		if allowed == to {
			return true
		}
	}

	return false
}

// claimEditable reports whether a claim's content may still change.
func claimEditable(status string) bool {
	return status == "draft" || status == "validation_error" || status == "ready"
}

// claimHasBeenSubmitted reports whether a claim ever left the practice
// (affects whether a resubmission needs a payer control number).
func claimInSubmittedState(status string) bool {
	switch status {
	case "pending_submission", "submitted", "sent", "resubmitted", "paper_generated",
		"externally_submitted", "rejected_new", "rejected", "paid":
		return true
	}

	return false
}

// authorizationUsesForLine is how many authorization uses a billed line
// consumes: one per service, or one per billed unit.
func authorizationUsesForLine(usageSetting string, units int) int {
	if usageSetting == "per_unit" {
		return units
	}

	return 1
}
