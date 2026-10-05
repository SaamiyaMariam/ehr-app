package billing

import (
	"strings"
	"testing"
	"time"
)

// Valid NPIs with correct check digits (CMS published examples).
const (
	validNPI1 = "1234567893"
	validNPI2 = "1245319599"
)

func TestValidNPI(t *testing.T) {
	for _, npi := range []string{validNPI1, validNPI2} {
		if !validNPI(npi) {
			t.Errorf("%s should be valid", npi)
		}
	}

	for _, npi := range []string{"1234567890", "123456789", "12345678901", "abcdefghij", ""} {
		if validNPI(npi) {
			t.Errorf("%s should be invalid", npi)
		}
	}
}

func completeSnapshot() ClaimSnapshot {
	addr := SnapshotAddress{Address1: "1 Main St", City: "Springfield", State: "IL", Zip: "62701"}

	return ClaimSnapshot{
		Patient: SnapshotPerson{FirstName: "Jane", LastName: "Doe", DateOfBirth: "1990-01-01", Sex: "F", Address: addr},
		Insured: SnapshotInsured{Relationship: "self", FirstName: "Jane", LastName: "Doe", DateOfBirth: "1990-01-01", Sex: "F", Address: addr, MemberID: "M1", SignatureOnFile: true},
		Payer:   SnapshotPayer{Name: "Acme", PayerID: "12345", InsuranceType: "group_health_plan", Address: addr},
		Practice: SnapshotPractice{
			Name: "Practice", NPI: validNPI1, TaxID: "123456789", Phone: "555-1234", Address: addr,
		},
		Policy: SnapshotPolicy{CoverageStart: "2026-01-01", CoverageEnd: "2026-12-31"},
	}
}

func completeLine() ClaimLineForValidation {
	return ClaimLineForValidation{
		LineNumber: 1, DateOfService: "2026-09-01", ServiceCode: "90834", Units: 1,
		LineTotal: 15000, DiagnosisPointers: "A", RenderingName: "Dr Smith", RenderingNPI: validNPI2,
	}
}

var claimToday = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

func hasMessage(list []string, fragment string) bool {
	for _, m := range list {
		if strings.Contains(m, fragment) {
			return true
		}
	}
	return false
}

func TestValidateClaimComplete(t *testing.T) {
	v := validateClaim(completeSnapshot(), "electronic", []string{"F41.1"}, []ClaimLineForValidation{completeLine()}, claimToday)

	if !v.Valid() || len(v.Warnings) != 0 {
		t.Fatalf("expected a clean claim, got errors=%v warnings=%v", v.Errors, v.Warnings)
	}
}

func TestValidateClaimErrors(t *testing.T) {
	tests := map[string]struct {
		mutate   func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, method *string)
		fragment string
	}{
		"patient dob": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { s.Patient.DateOfBirth = "" }, "Patient date of birth"},
		"patient address": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			s.Patient.Address.City = ""
		}, "Patient address"},
		"member id": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { s.Insured.MemberID = "" }, "member ID"},
		"relationship": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			s.Insured.Relationship = ""
		}, "relationship"},
		"insured name (spouse)": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			s.Insured.Relationship = "spouse"
			s.Insured.FirstName = ""
		}, "Insured first and last name"},
		"payer id electronic": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { s.Payer.PayerID = "" }, "Payer ID is required"},
		"practice npi": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			s.Practice.NPI = "1234567890"
		}, "practice NPI"},
		"practice tax id": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { s.Practice.TaxID = "" }, "tax ID"},
		"no diagnoses":    {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { *dx = nil }, "diagnosis"},
		"zero amount":     {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { l.LineTotal = 0 }, "greater than zero"},
		"no pointer":      {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { l.DiagnosisPointers = "" }, "pointer"},
		"rendering npi":   {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { l.RenderingNPI = "" }, "needs a valid NPI"},
		"before coverage": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			l.DateOfService = "2025-12-31"
		}, "before the policy coverage"},
		"after coverage": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			s.Policy.CoverageEnd = "2026-08-31"
		}, "after the policy coverage"},
		"future dos": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			l.DateOfService = "2026-10-06"
		}, "in the future"},
		"bad method": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) { *m = "fax" }, "Submission method"},
		"pa uses": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			l.PriorAuthorization = "PA1"
			l.AuthorizationActive = true
			l.AuthorizationUsesNeeded = 2
			l.AuthorizationUsesRemaining = uses(1)
		}, "enough uses"},
		"pa disabled": {func(s *ClaimSnapshot, l *ClaimLineForValidation, dx *[]string, m *string) {
			l.PriorAuthorization = "PA1"
			l.AuthorizationUsesNeeded = 1
		}, "is disabled"},
	}

	for name, tt := range tests {
		s := completeSnapshot()
		l := completeLine()
		dx := []string{"F41.1"}
		method := "electronic"
		tt.mutate(&s, &l, &dx, &method)

		v := validateClaim(s, method, dx, []ClaimLineForValidation{l}, claimToday)

		if v.Valid() || !hasMessage(v.Errors, tt.fragment) {
			t.Errorf("%s: expected error containing %q, got %v", name, tt.fragment, v.Errors)
		}
	}
}

func TestValidateClaimWarningsOnly(t *testing.T) {
	s := completeSnapshot()
	s.Payer.PayerID = ""
	s.Insured.SignatureOnFile = false
	l := completeLine()
	l.DateOfService = "2025-09-01"
	s.Policy.CoverageStart = ""

	v := validateClaim(s, "paper", []string{"F41.1"}, []ClaimLineForValidation{l}, claimToday)

	if !v.Valid() {
		t.Fatalf("warnings must not block a paper claim: %v", v.Errors)
	}

	for _, fragment := range []string{"Payer ID", "Signature on file", "timely filing"} {
		if !hasMessage(v.Warnings, fragment) {
			t.Errorf("expected warning %q, got %v", fragment, v.Warnings)
		}
	}

	// Already-consumed authorization needs no further uses.
	l2 := completeLine()
	l2.PriorAuthorization = "PA1"
	l2.AuthorizationUsesNeeded = 0
	l2.AuthorizationUsesRemaining = uses(0)
	if v := validateClaim(completeSnapshot(), "paper", []string{"F41.1"}, []ClaimLineForValidation{l2}, claimToday); !v.Valid() {
		t.Errorf("consumed authorization should not block resubmission: %v", v.Errors)
	}
}

func TestClaimTransitions(t *testing.T) {
	allowed := [][2]string{
		{"draft", "ready"}, {"draft", "validation_error"}, {"validation_error", "ready"},
		{"ready", "paper_generated"}, {"ready", "externally_submitted"}, {"ready", "submitted"},
		{"paper_generated", "paper_generated"}, {"paper_generated", "submitted"},
		{"submitted", "sent"}, {"sent", "rejected_new"}, {"rejected_new", "rejected"},
		{"rejected", "draft"}, {"submitted", "paid"}, {"paid", "submitted"},
		{"ready", "resubmitted"}, {"draft", "voided"},
	}

	for _, pair := range allowed {
		if !canTransitionClaim(pair[0], pair[1]) {
			t.Errorf("%s → %s should be allowed", pair[0], pair[1])
		}
	}

	forbidden := [][2]string{
		{"draft", "paid"}, {"draft", "submitted"}, {"validation_error", "paper_generated"},
		{"rejected_new", "paid"}, {"voided", "draft"}, {"voided", "ready"},
		{"paid", "voided"}, {"submitted", "ready"}, {"externally_submitted", "submitted"},
		{"unknown", "ready"},
	}

	for _, pair := range forbidden {
		if canTransitionClaim(pair[0], pair[1]) {
			t.Errorf("%s → %s should be forbidden", pair[0], pair[1])
		}
	}

	// Every target state must itself be a known state.
	for from, targets := range claimTransitions {
		for _, to := range targets {
			if _, ok := claimTransitions[to]; !ok {
				t.Errorf("%s → %s targets an undefined state", from, to)
			}
		}
	}
}

func TestAssignClaimDiagnoses(t *testing.T) {
	t0 := time.Now()
	charges := []claimChargeRow{
		{ID: "b", DateOfService: "2026-09-02", Total: "100.00", CreatedAt: t0, Diagnoses: []ChargeDiagnosis{{ICD10Code: "F32.9"}, {ICD10Code: "F41.1"}}},
		{ID: "a", DateOfService: "2026-09-01", Total: "100.00", CreatedAt: t0, Diagnoses: []ChargeDiagnosis{{ICD10Code: "F41.1"}}},
	}

	dx, lines, message := assignClaimDiagnoses(charges)
	if message != "" {
		t.Fatal(message)
	}

	if len(dx) != 2 || dx[0].ICD10Code != "F41.1" || dx[1].ICD10Code != "F32.9" {
		t.Fatalf("diagnoses should be unique, in first-appearance order: %+v", dx)
	}

	if lines[0].charge.ID != "a" || lines[0].pointers != "A" || lines[1].pointers != "BA" {
		t.Fatalf("lines should be DOS-ordered with mapped pointers: %+v", lines)
	}

	var many []ChargeDiagnosis
	for i := 0; i < 13; i++ {
		many = append(many, ChargeDiagnosis{ICD10Code: "F" + string(rune('A'+i))})
	}
	if _, _, message := assignClaimDiagnoses([]claimChargeRow{{Diagnoses: many}}); message == "" {
		t.Error("more than 12 diagnoses should be rejected")
	}
}

func TestCheckPrimaryClaimCharges(t *testing.T) {
	base := claimChargeRow{PatientID: "p1", Status: "active", BillingMethod: "insurance_in_network_paper", InsurancePolicyID: "pol1", PayerID: "pay1"}

	cov, message := checkPrimaryClaimCharges([]claimChargeRow{base, base}, 2)
	if message != "" || cov.PolicyID != "pol1" || cov.SubmissionMethod != "paper" {
		t.Fatalf("expected paper coverage, got %+v %q", cov, message)
	}

	other := func(mutate func(*claimChargeRow)) []claimChargeRow {
		c := base
		mutate(&c)
		return []claimChargeRow{base, c}
	}

	cases := map[string][]claimChargeRow{
		"other patient": other(func(c *claimChargeRow) { c.PatientID = "p2" }),
		"voided":        other(func(c *claimChargeRow) { c.Status = "voided" }),
		"direct":        other(func(c *claimChargeRow) { c.BillingMethod = "direct" }),
		"other policy":  other(func(c *claimChargeRow) { c.InsurancePolicyID = "pol2" }),
		"mixed method":  other(func(c *claimChargeRow) { c.BillingMethod = "insurance_in_network_external" }),
	}

	for name, charges := range cases {
		if _, message := checkPrimaryClaimCharges(charges, 2); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}

	if _, message := checkPrimaryClaimCharges([]claimChargeRow{base}, 2); message == "" {
		t.Error("missing charges should be rejected")
	}
}

func TestResubmissionRules(t *testing.T) {
	if validateResubmission("new", "") != "" {
		t.Error("new claims need no control number")
	}

	for _, kind := range []string{"amended", "void"} {
		if validateResubmission(kind, "") == "" {
			t.Errorf("%s needs a control number", kind)
		}
		if validateResubmission(kind, "ICN123") != "" {
			t.Errorf("%s with control number should be valid", kind)
		}
	}

	if validateResubmission("replace", "x") == "" {
		t.Error("unknown resubmission type should be rejected")
	}

	if authorizationUsesForLine("once_per_service", 4) != 1 || authorizationUsesForLine("per_unit", 4) != 4 {
		t.Error("authorization use calculation is wrong")
	}
}
