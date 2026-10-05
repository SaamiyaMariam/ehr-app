package billing

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSubmissionTarget(t *testing.T) {
	tests := []struct {
		method, resubmission string
		previously           bool
		want                 string
	}{
		{"paper", "new", false, "submitted"},
		{"electronic", "new", false, "submitted"},
		{"external", "new", false, "externally_submitted"},
		{"paper", "amended", true, "resubmitted"},
		{"electronic", "new", true, "resubmitted"},
		{"external", "amended", true, "externally_submitted"},
		{"paper", "void", true, "voided"},
		{"external", "void", true, "voided"},
	}

	for _, tt := range tests {
		if got := submissionTarget(tt.method, tt.resubmission, tt.previously); got != tt.want {
			t.Errorf("submissionTarget(%s, %s, %v) = %s, want %s", tt.method, tt.resubmission, tt.previously, got, tt.want)
		}

		// Every target must be reachable through the state machine from the
		// state a claim is in when it is submitted.
		from := "ready"
		if tt.method == "paper" {
			from = "paper_generated"
		}
		if !canTransitionClaim(from, tt.want) {
			t.Errorf("%s → %s is not an allowed transition", from, tt.want)
		}
	}
}

func TestCleanSubmissionRequest(t *testing.T) {
	today := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	req := SubmissionRequest{}
	if message := cleanSubmissionRequest(&req, today); message != "" || req.SubmittedOn != "2026-10-05" {
		t.Fatalf("empty date should default to today, got %q %q", req.SubmittedOn, message)
	}

	for _, bad := range []SubmissionRequest{
		{SubmittedOn: "2026-10-06"},
		{SubmittedOn: "10/05/2026"},
		{Reference: strings.Repeat("x", 101)},
		{Comment: strings.Repeat("x", 2001)},
	} {
		b := bad
		if cleanSubmissionRequest(&b, today) == "" {
			t.Errorf("expected rejection for %+v", bad)
		}
	}
}

func TestClearinghouseNotConfiguredByDefault(t *testing.T) {
	os.Setenv("CLEARINGHOUSE_PROVIDER", "made-up-vendor")
	defer os.Unsetenv("CLEARINGHOUSE_PROVIDER")

	if clearinghouseFromEnv() != nil {
		t.Fatal("an unknown provider must not enable electronic submission")
	}
}

func TestBuildClearinghousePayload(t *testing.T) {
	snapshot := completeSnapshot()
	claim := Claim{
		ClaimNumber: "CLM-1", ResubmissionType: "amended", PayerClaimControlNumber: "ICN9",
		TotalBilled: "300.00", Snapshot: &snapshot,
		Diagnoses: []ClaimDiagnosis{{ICD10Code: "F41.1"}},
		Lines: []ClaimLine{
			{LineNumber: 1, ServiceCode: "90834", LineTotal: "150.00", IsCurrent: true, PriorAuthorizationCode: "PA1"},
			{LineNumber: 2, ServiceCode: "90837", LineTotal: "150.00", IsCurrent: false},
		},
	}

	p := buildClearinghousePayload(claim)

	if !strings.Contains(p.Format, "not X12") {
		t.Error("payload must be labeled as not X12")
	}

	if p.FrequencyCode != "7" || p.OriginalRef != "ICN9" {
		t.Errorf("replacement claim frequency/ref wrong: %+v", p)
	}

	if len(p.ServiceLines) != 1 || p.PriorAuthorization != "PA1" || len(p.Diagnoses) != 1 {
		t.Errorf("only current lines should be included: %+v", p.ServiceLines)
	}

	if p.OtherInsurance == nil {
		t.Error("other insurance should be an empty list, not null")
	}
}
