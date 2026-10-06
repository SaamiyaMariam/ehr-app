package billing

import "testing"

const (
	testServiceCodeA = "11111111-1111-4111-8111-111111111111"
	testServiceCodeB = "22222222-2222-4222-8222-222222222222"
)

func validPriorAuthorization() PriorAuthorization {
	return PriorAuthorization{
		AuthorizationCode: "AUTH-12345",
		ServiceCodeIDs:    []string{testServiceCodeA, testServiceCodeB},
		StartDate:         "2026-01-01",
		ExpirationDate:    "2026-12-31",
		UsesAllowed:       intPtr(20),
		UsesRemaining:     intPtr(20),
		UsageSetting:      "once_per_service",
		Comments:          "20 visits approved",
	}
}

func TestValidatePriorAuthorizationAcceptsValid(t *testing.T) {
	pa := validPriorAuthorization()

	if message := validatePriorAuthorization(&pa); message != "" {
		t.Fatalf("expected valid prior authorization, got %q", message)
	}
}

func TestValidatePriorAuthorizationAcceptsMinimal(t *testing.T) {
	pa := PriorAuthorization{AuthorizationCode: "A1", AppliesToAnyServiceCode: true}

	if message := validatePriorAuthorization(&pa); message != "" {
		t.Fatalf("expected minimal prior authorization to be valid, got %q", message)
	}

	if pa.UsageSetting != "once_per_service" {
		t.Errorf("expected default usage setting once_per_service, got %q", pa.UsageSetting)
	}
}

func TestValidatePriorAuthorizationRejectsInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(pa *PriorAuthorization)
	}{
		{"missing authorization code", func(pa *PriorAuthorization) { pa.AuthorizationCode = "" }},
		{"invalid usage setting", func(pa *PriorAuthorization) { pa.UsageSetting = "weekly" }},
		{"negative uses allowed", func(pa *PriorAuthorization) { pa.UsesAllowed = intPtr(-1); pa.UsesRemaining = nil }},
		{"negative uses remaining", func(pa *PriorAuthorization) { pa.UsesRemaining = intPtr(-1) }},
		{"remaining greater than allowed", func(pa *PriorAuthorization) { pa.UsesRemaining = intPtr(21) }},
		{"expiration before start", func(pa *PriorAuthorization) { pa.ExpirationDate = "2025-12-31" }},
		{"invalid start date", func(pa *PriorAuthorization) { pa.StartDate = "2026-02-30" }},
		{"no service code when any is false", func(pa *PriorAuthorization) { pa.ServiceCodeIDs = nil }},
		{"invalid service code id", func(pa *PriorAuthorization) { pa.ServiceCodeIDs = []string{"90834"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pa := validPriorAuthorization()
			tt.mutate(&pa)
			cleanPriorAuthorization(&pa)

			if message := validatePriorAuthorization(&pa); message == "" {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidatePriorAuthorizationAnyClearsMappings(t *testing.T) {
	pa := validPriorAuthorization()
	pa.AppliesToAnyServiceCode = true
	pa.ServiceCodeIDs = append(pa.ServiceCodeIDs, "not-a-uuid")

	if message := validatePriorAuthorization(&pa); message != "" {
		t.Fatalf("expected valid prior authorization, got %q", message)
	}

	// Must be empty but non-nil so the SQL cleanup receives '{}' not NULL.
	if pa.ServiceCodeIDs == nil || len(pa.ServiceCodeIDs) != 0 {
		t.Errorf("expected service code mappings to be cleared, got %v", pa.ServiceCodeIDs)
	}
}

func TestCleanPriorAuthorizationDeduplicatesServiceCodes(t *testing.T) {
	pa := PriorAuthorization{
		ServiceCodeIDs: []string{" " + testServiceCodeA, testServiceCodeA, "", "11111111-1111-4111-8111-111111111111"},
	}

	cleanPriorAuthorization(&pa)

	if len(pa.ServiceCodeIDs) != 1 || pa.ServiceCodeIDs[0] != testServiceCodeA {
		t.Errorf("expected one deduplicated service code, got %v", pa.ServiceCodeIDs)
	}
}

func TestCheckServiceCodeSelection(t *testing.T) {
	found := map[string]bool{
		testServiceCodeA: true,
		testServiceCodeB: false, // disabled
	}

	if message := checkServiceCodeSelection([]string{testServiceCodeA}, found, nil); message != "" {
		t.Errorf("active code should be assignable, got %q", message)
	}

	if message := checkServiceCodeSelection(
		[]string{"33333333-3333-4333-8333-333333333333"}, found, nil,
	); message == "" {
		t.Errorf("unknown service code should be rejected")
	}

	if message := checkServiceCodeSelection([]string{testServiceCodeB}, found, nil); message == "" {
		t.Errorf("disabled service code should not be newly assignable")
	}

	existing := map[string]bool{testServiceCodeB: true}

	if message := checkServiceCodeSelection([]string{testServiceCodeA, testServiceCodeB}, found, existing); message != "" {
		t.Errorf("disabled service code already mapped should be retained, got %q", message)
	}
}

func TestStatusChangeError(t *testing.T) {
	if statusChangeError(true, false) == "" {
		t.Errorf("enabling while insurance policy is disabled should be rejected")
	}

	if statusChangeError(true, true) != "" {
		t.Errorf("enabling while insurance policy is enabled should be allowed")
	}

	if statusChangeError(false, false) != "" {
		t.Errorf("disabling should always be allowed")
	}
}
