package billing

import (
	"testing"
	"time"
)

var testToday = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

func validChargeInput() ChargeInput {
	return ChargeInput{
		ClinicianID:   testServiceCodeA,
		ServiceCodeID: testServiceCodeB,
		DateOfService: "2026-09-15",
		Units:         1,
		Modifiers:     []string{"hj", " 95 "},
		DiagnosisIDs:  []string{testServiceCodeA},
	}
}

func TestValidateChargeInput(t *testing.T) {
	in := validChargeInput()
	cleanChargeInput(&in)

	if message := validateChargeInput(&in, testToday); message != "" {
		t.Fatalf("expected valid charge, got %q", message)
	}

	if in.PlaceOfService != "11" || in.Modifiers[0] != "HJ" || in.Modifiers[1] != "95" {
		t.Errorf("cleaning should default POS and normalize modifiers: %+v", in)
	}

	tests := map[string]func(*ChargeInput){
		"future date":          func(c *ChargeInput) { c.DateOfService = "2026-10-06" },
		"bad date":             func(c *ChargeInput) { c.DateOfService = "09/15/2026" },
		"zero units":           func(c *ChargeInput) { c.Units = 0 },
		"too many units":       func(c *ChargeInput) { c.Units = 1000 },
		"bad modifier":         func(c *ChargeInput) { c.Modifiers = []string{"ABC"} },
		"duplicate modifier":   func(c *ChargeInput) { c.Modifiers = []string{"HJ", "HJ"} },
		"five modifiers":       func(c *ChargeInput) { c.Modifiers = []string{"A1", "A2", "A3", "A4", "A5"} },
		"bad place of service": func(c *ChargeInput) { c.PlaceOfService = "1" },
		"five diagnoses": func(c *ChargeInput) {
			c.DiagnosisIDs = []string{testServiceCodeA, testServiceCodeB, testPayerID, "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"}
		},
		"duplicate diagnosis":          func(c *ChargeInput) { c.DiagnosisIDs = []string{testServiceCodeA, testServiceCodeA} },
		"PA without policy":            func(c *ChargeInput) { c.PriorAuthorizationID = testServiceCodeA },
		"negative patient amount":      func(c *ChargeInput) { c.PatientResponsibility = "-1" },
		"three decimal patient amount": func(c *ChargeInput) { c.PatientResponsibility = "1.001" },
	}

	for name, mutate := range tests {
		c := validChargeInput()
		cleanChargeInput(&c)
		mutate(&c)
		cleanChargeInput(&c)

		if message := validateChargeInput(&c, testToday); message == "" {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestSplitResponsibility(t *testing.T) {
	p, i, msg := splitResponsibility(15000, "direct", nil, cents(2500))
	if msg != "" || p != 15000 || i != 0 {
		t.Errorf("direct: got %d/%d %q", p, i, msg)
	}

	if _, _, msg := splitResponsibility(15000, "direct", cents(100), nil); msg == "" {
		t.Errorf("direct with partial patient share should be rejected")
	}

	p, i, _ = splitResponsibility(15000, "insurance_in_network_paper", nil, cents(2500))
	if p != 2500 || i != 12500 {
		t.Errorf("copay default: got %d/%d", p, i)
	}

	p, i, _ = splitResponsibility(2000, "insurance_in_network_paper", nil, cents(2500))
	if p != 2000 || i != 0 {
		t.Errorf("copay capped at total: got %d/%d", p, i)
	}

	p, i, _ = splitResponsibility(15000, "insurance_in_network_paper", nil, nil)
	if p != 0 || i != 15000 {
		t.Errorf("no copay → insurance owes all: got %d/%d", p, i)
	}

	p, i, _ = splitResponsibility(15000, "insurance_in_network_paper", cents(4000), cents(2500))
	if p != 4000 || i != 11000 {
		t.Errorf("explicit split wins: got %d/%d", p, i)
	}

	if _, _, msg := splitResponsibility(15000, "insurance_in_network_paper", cents(15001), nil); msg == "" {
		t.Errorf("patient share above total should be rejected")
	}

	// Invariant: shares always sum to the total.
	for _, total := range []int64{0, 1, 999, 15000} {
		for _, copay := range []int64{0, 500, 20000} {
			p, i, _ := splitResponsibility(total, "insurance_out_of_network_external", nil, cents(copay))
			if p+i != total || p < 0 || i < 0 {
				t.Errorf("split(%d, copay %d) = %d/%d does not reconcile", total, copay, p, i)
			}
		}
	}
}

func uses(v int) *int { return &v }

func TestCheckPriorAuthorizationForCharge(t *testing.T) {
	base := priorAuthorizationInfo{
		PolicyID:           "policy-1",
		PolicyPatientMatch: true,
		IsActive:           true,
		StartDate:          "2026-01-01",
		ExpirationDate:     "2026-12-31",
		ServiceCodeIDs:     map[string]bool{"sc-1": true},
		UsesRemaining:      uses(3),
	}

	if message := checkPriorAuthorizationForCharge(base, "policy-1", "sc-1", "2026-06-01"); message != "" {
		t.Fatalf("expected valid, got %q", message)
	}

	tests := map[string]struct {
		mutate func(*priorAuthorizationInfo)
		policy string
		code   string
		dos    string
	}{
		"other policy":      {func(p *priorAuthorizationInfo) {}, "policy-2", "sc-1", "2026-06-01"},
		"other patient":     {func(p *priorAuthorizationInfo) { p.PolicyPatientMatch = false }, "policy-1", "sc-1", "2026-06-01"},
		"disabled":          {func(p *priorAuthorizationInfo) { p.IsActive = false }, "policy-1", "sc-1", "2026-06-01"},
		"before start":      {func(p *priorAuthorizationInfo) {}, "policy-1", "sc-1", "2025-12-31"},
		"after expiration":  {func(p *priorAuthorizationInfo) {}, "policy-1", "sc-1", "2027-01-01"},
		"service not added": {func(p *priorAuthorizationInfo) {}, "policy-1", "sc-2", "2026-06-01"},
		"no uses remaining": {func(p *priorAuthorizationInfo) { p.UsesRemaining = uses(0) }, "policy-1", "sc-1", "2026-06-01"},
	}

	for name, tt := range tests {
		pa := base
		pa.ServiceCodeIDs = map[string]bool{"sc-1": true}
		tt.mutate(&pa)

		if message := checkPriorAuthorizationForCharge(pa, tt.policy, tt.code, tt.dos); message == "" {
			t.Errorf("%s: expected error", name)
		}
	}

	anyCode := base
	anyCode.AppliesToAny = true
	if message := checkPriorAuthorizationForCharge(anyCode, "policy-1", "sc-9", "2026-06-01"); message != "" {
		t.Errorf("any-service authorization should cover sc-9, got %q", message)
	}

	unlimited := base
	unlimited.UsesRemaining = nil
	if message := checkPriorAuthorizationForCharge(unlimited, "policy-1", "sc-1", "2026-06-01"); message != "" {
		t.Errorf("no use limit should be valid, got %q", message)
	}
}

func TestNormalizeICD10(t *testing.T) {
	for in, want := range map[string]string{"f411": "F41.1", " F41.1 ": "F41.1", "f32a": "F32.A", "z00": "Z00", "U071": "U07.1"} {
		if got := normalizeICD10(in); got != want {
			t.Errorf("normalizeICD10(%q) = %q, want %q", in, got, want)
		}
	}

	for _, bad := range []string{"41.1", "F4", "FF1.1", "F41.12345"} {
		d := Diagnosis{ICD10Code: bad, Description: "x"}
		if validateDiagnosis(&d) == "" {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
