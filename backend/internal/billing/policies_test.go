package billing

import "testing"

const testPayerID = "baf2f501-ef4c-47a1-b1e6-ecb296eba1cd"

func intPtr(v int) *int { return &v }

func TestValidatePolicyAcceptsMinimalPolicy(t *testing.T) {
	p := InsurancePolicy{PayerID: testPayerID}

	if message := validatePolicy(&p); message != "" {
		t.Fatalf("expected minimal policy to be valid, got %q", message)
	}

	if p.Priority != "primary" {
		t.Errorf("expected default priority primary, got %q", p.Priority)
	}

	if p.AppointmentLimitType != "unknown" {
		t.Errorf("expected default limit type unknown, got %q", p.AppointmentLimitType)
	}
}

func TestValidatePolicyRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		policy InsurancePolicy
	}{
		{"missing payer", InsurancePolicy{}},
		{"malformed payer", InsurancePolicy{PayerID: "abc"}},
		{"bad priority", InsurancePolicy{PayerID: testPayerID, Priority: "fifth"}},
		{"bad coverage start", InsurancePolicy{PayerID: testPayerID, CoverageStart: "2026-13-01"}},
		{"bad holder dob", InsurancePolicy{PayerID: testPayerID, PolicyHolderDateOfBirth: "01/02/1990"}},
		{"end before start", InsurancePolicy{PayerID: testPayerID, CoverageStart: "2026-02-01", CoverageEnd: "2026-01-31"}},
		{"negative copay", InsurancePolicy{PayerID: testPayerID, Copay: "-5"}},
		{"three decimal copay", InsurancePolicy{PayerID: testPayerID, Copay: "5.001"}},
		{"non-numeric deductible", InsurancePolicy{PayerID: testPayerID, Deductible: "lots"}},
		{"bad limit type", InsurancePolicy{PayerID: testPayerID, AppointmentLimitType: "some"}},
		{"negative appointments", InsurancePolicy{PayerID: testPayerID, AppointmentLimitType: "number", AppointmentsAllowed: intPtr(-1)}},
		{"bad relationship", InsurancePolicy{PayerID: testPayerID, RelationshipToPolicyHolder: "cousin"}},
		{"bad sex", InsurancePolicy{PayerID: testPayerID, PolicyHolderSex: "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.policy

			if message := validatePolicy(&p); message == "" {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidatePolicyAcceptsValidValues(t *testing.T) {
	p := InsurancePolicy{
		PayerID:                    testPayerID,
		Priority:                   "secondary",
		CoverageStart:              "2026-01-01",
		CoverageEnd:                "2026-01-01",
		Copay:                      "25",
		Deductible:                 "1500.50",
		AppointmentLimitType:       "number",
		AppointmentsAllowed:        intPtr(0),
		AppointmentsExpiration:     "2026-12-31",
		RelationshipToPolicyHolder: "spouse",
		PolicyHolderSex:            "female",
		PolicyHolderDateOfBirth:    "1980-05-20",
	}

	if message := validatePolicy(&p); message != "" {
		t.Fatalf("expected valid policy, got %q", message)
	}

	if p.AppointmentsAllowed == nil || p.AppointmentsExpiration == "" {
		t.Errorf("numeric limit details should be kept")
	}
}

func TestValidatePolicyClearsAllowanceForNonNumericLimit(t *testing.T) {
	p := InsurancePolicy{
		PayerID:                testPayerID,
		AppointmentLimitType:   "unlimited",
		AppointmentsAllowed:    intPtr(10),
		AppointmentsExpiration: "2026-12-31",
	}

	if message := validatePolicy(&p); message != "" {
		t.Fatalf("expected valid policy, got %q", message)
	}

	if p.AppointmentsAllowed != nil || p.AppointmentsExpiration != "" {
		t.Errorf("allowance details should be cleared when limit type is not number")
	}
}

func TestCleanPolicyNormalizesInput(t *testing.T) {
	p := InsurancePolicy{
		PayerID:  "  " + testPayerID + " ",
		Priority: " Primary ",
		MemberID: "  ABC123 ",
	}

	cleanPolicy(&p)

	if p.PayerID != testPayerID || p.Priority != "primary" || p.MemberID != "ABC123" {
		t.Errorf("unexpected cleaned policy: %+v", p)
	}
}

func TestIsUUID(t *testing.T) {
	if !isUUID(testPayerID) {
		t.Errorf("expected valid UUID")
	}

	for _, value := range []string{"", "abc", testPayerID + "0", "zzzzzzzz-ef4c-47a1-b1e6-ecb296eba1cd"} {
		if isUUID(value) {
			t.Errorf("expected %q to be rejected", value)
		}
	}
}
