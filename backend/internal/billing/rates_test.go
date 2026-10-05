package billing

import "testing"

func cents(v int64) *int64 { return &v }
func str(v string) *string { return &v }

func TestParseAndFormatMoney(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"12", 1200, true},
		{"12.3", 1230, true},
		{"12.30", 1230, true},
		{"0.05", 5, true},
		{"1234567890.99", 123456789099, true},
		{"-1", 0, false},
		{"1.005", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"1e3", 0, false},
	}

	for _, tt := range tests {
		got, ok := parseMoney(tt.in)

		if ok != tt.ok || got != tt.want {
			t.Errorf("parseMoney(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}

	for cents, want := range map[int64]string{0: "0.00", 5: "0.05", 1230: "12.30", -250: "-2.50"} {
		if got := formatMoney(cents); got != want {
			t.Errorf("formatMoney(%d) = %q, want %q", cents, got, want)
		}
	}

	if got, _ := parseSignedMoney("-15.50"); got != -1550 {
		t.Errorf("parseSignedMoney(-15.50) = %d", got)
	}
}

func TestChooseRateDirect(t *testing.T) {
	got, err := chooseRate(rateCandidates{Direct: true, Standard: cents(15000), CashRate: cents(9000)})
	if err != nil || got.RatePerUnit != 9000 || got.Source != RateSourcePatientCash {
		t.Fatalf("direct with cash rate: got %+v, %v", got, err)
	}

	got, err = chooseRate(rateCandidates{Direct: true, Standard: cents(15000)})
	if err != nil || got.RatePerUnit != 15000 || got.Source != RateSourceStandard {
		t.Fatalf("direct without cash rate: got %+v, %v", got, err)
	}

	// A payer schedule must never apply to direct billing.
	got, _ = chooseRate(rateCandidates{Direct: true, Standard: cents(15000), ScheduleID: str("s"), ScheduleCustomRate: cents(100)})
	if got.Source != RateSourceStandard || got.RateScheduleID != nil {
		t.Fatalf("direct must ignore payer schedules: got %+v", got)
	}
}

func TestChooseRateInsurance(t *testing.T) {
	got, err := chooseRate(rateCandidates{Standard: cents(15000), ScheduleID: str("s1"), ScheduleCustomRate: cents(11000)})
	if err != nil || got.RatePerUnit != 11000 || got.Source != RateSourcePayerSchedule || *got.RateScheduleID != "s1" {
		t.Fatalf("schedule custom rate: got %+v, %v", got, err)
	}

	got, err = chooseRate(rateCandidates{Standard: cents(15000), ScheduleID: str("s1")})
	if err != nil || got.RatePerUnit != 15000 || got.Source != RateSourceStandard || *got.RateScheduleID != "s1" {
		t.Fatalf("schedule without item falls back to standard: got %+v, %v", got, err)
	}

	got, _ = chooseRate(rateCandidates{Standard: cents(15000), ScheduleID: str("s1"), ScheduleUseStandard: true, ScheduleCustomRate: cents(11000)})
	if got.RatePerUnit != 15000 || got.Source != RateSourceStandard {
		t.Fatalf("use-standard schedule ignores items: got %+v", got)
	}

	// Patient cash rates never apply to insurance billing.
	got, _ = chooseRate(rateCandidates{Standard: cents(15000), CashRate: cents(5000)})
	if got.RatePerUnit != 15000 || got.Source != RateSourceStandard {
		t.Fatalf("insurance must ignore cash rates: got %+v", got)
	}

	if _, err := chooseRate(rateCandidates{}); err != errNoRateConfigured {
		t.Fatalf("missing standard rate should error, got %v", err)
	}

	got, err = chooseRate(rateCandidates{ScheduleID: str("s1"), ScheduleCustomRate: cents(0)})
	if err != nil || got.RatePerUnit != 0 || got.Source != RateSourcePayerSchedule {
		t.Fatalf("zero custom rate is valid: got %+v, %v", got, err)
	}
}

func TestBillingMethodResolution(t *testing.T) {
	if got := resolveSubmissionMethod("external", "paper", "electronic"); got != "external" {
		t.Errorf("line override should win, got %q", got)
	}

	if got := resolveSubmissionMethod("", "paper", "electronic"); got != "paper" {
		t.Errorf("payer override should beat practice default, got %q", got)
	}

	if got := resolveSubmissionMethod("", "", "electronic"); got != "electronic" {
		t.Errorf("practice default should apply, got %q", got)
	}

	if got := insuranceBillingMethod(true, "paper"); got != "insurance_in_network_paper" {
		t.Errorf("got %q", got)
	}

	if got := insuranceBillingMethod(false, "external"); got != "insurance_out_of_network_external" {
		t.Errorf("got %q", got)
	}

	if got := submissionMethodOf("insurance_out_of_network_electronic"); got != "electronic" {
		t.Errorf("got %q", got)
	}

	if got := submissionMethodOf("direct"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestValidateRateSchedule(t *testing.T) {
	valid := RateSchedule{
		Name: "Standard contract",
		Items: []RateScheduleItem{
			{ServiceCodeID: testServiceCodeA, CustomRate: "110.00"},
			{ServiceCodeID: testServiceCodeB, CustomRate: "0"},
		},
	}

	if message := validateRateSchedule(&valid); message != "" {
		t.Fatalf("expected valid schedule, got %q", message)
	}

	tests := map[string]RateSchedule{
		"missing name":       {Items: valid.Items},
		"negative rate":      {Name: "x", Items: []RateScheduleItem{{ServiceCodeID: testServiceCodeA, CustomRate: "-1"}}},
		"blank rate":         {Name: "x", Items: []RateScheduleItem{{ServiceCodeID: testServiceCodeA, CustomRate: ""}}},
		"bad service code":   {Name: "x", Items: []RateScheduleItem{{ServiceCodeID: "90834", CustomRate: "1"}}},
		"duplicate services": {Name: "x", Items: []RateScheduleItem{{ServiceCodeID: testServiceCodeA, CustomRate: "1"}, {ServiceCodeID: testServiceCodeA, CustomRate: "2"}}},
	}

	for name, s := range tests {
		s := s
		if message := validateRateSchedule(&s); message == "" {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
