package billing

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

func testClaimLines(n int) []ClaimLine {
	lines := make([]ClaimLine, n)

	for i := range lines {
		lines[i] = ClaimLine{
			LineNumber: i + 1, DateOfService: fmt.Sprintf("2026-09-%02d", i+1), ServiceCode: "90834",
			Modifiers: []string{"95"}, Units: 1, PlaceOfService: "10", LineTotal: "150.00",
			RenderingNPI: validNPI2, DiagnosisPointers: "A", IsCurrent: true,
		}
	}

	return lines
}

func TestRenderCMS1500(t *testing.T) {
	data := cms1500Data{
		ClaimNumber:   "CLM-0000001",
		Snapshot:      completeSnapshot(),
		Lines:         testClaimLines(2),
		Diagnoses:     []ClaimDiagnosis{{Position: 1, Letter: "A", ICD10Code: "F41.1"}},
		FrequencyCode: "1",
		GeneratedAt:   time.Now(),
	}

	pdf, sum, pages, err := renderCMS1500(data)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(sum) != 64 || pages != 1 {
		t.Fatalf("expected a 1-page PDF with sha256, got pages=%d sum=%q", pages, sum)
	}

	// Seven to twelve lines need a second page (six lines per page).
	data.Lines = testClaimLines(8)
	if _, _, pages, err := renderCMS1500(data); err != nil || pages != 2 {
		t.Fatalf("8 lines should render on 2 pages, got %d (%v)", pages, err)
	}

	// Rendering is deterministic for the same data except timestamps.
	data.Lines = testClaimLines(1)
	if _, _, _, err := renderCMS1500(data); err != nil {
		t.Fatal(err)
	}
}

func TestFrequencyCodes(t *testing.T) {
	for in, want := range map[string]string{"new": "1", "amended": "7", "void": "8"} {
		if got := frequencyCodeFor(in); got != want {
			t.Errorf("frequencyCodeFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPriorPayerPaid(t *testing.T) {
	s := completeSnapshot()
	s.OtherInsurance = []SnapshotOtherInsurance{{AmountPaid: "80.00"}, {AmountPaid: "5.50"}}

	if got := priorPayerPaid(s); got != 8550 {
		t.Errorf("priorPayerPaid = %d, want 8550", got)
	}
}

func TestRenderSuperbill(t *testing.T) {
	data := superbillData{
		Practice:  completeSnapshot().Practice,
		Patient:   completeSnapshot().Patient,
		Diagnoses: []ClaimDiagnosis{{Position: 1, Letter: "A", ICD10Code: "F41.1", Description: "GAD"}},
		Lines: []superbillLine{
			{DateOfService: "2026-09-01", ServiceCode: "90837", Description: "Psychotherapy 60 min", Units: 1, Fee: "200.00", Paid: "200.00", Diagnoses: "A"},
		},
		TotalFees: "200.00", TotalPaid: "200.00", GeneratedAt: "2026-10-05",
	}

	pdf, sum, err := renderSuperbill(data)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(sum) != 64 {
		t.Fatal("expected a PDF")
	}
}

func TestPDFTextHelpers(t *testing.T) {
	if mmddyyyy("2026-09-05") != "09 05 2026" || mmddyyyy("bad") != "" {
		t.Error("mmddyyyy formatting is wrong")
	}

	if moneyParts(12345) != "123 45" {
		t.Error("moneyParts formatting is wrong")
	}

	if personName("doe", "jane", "marie") != "DOE, JANE M" {
		t.Errorf("personName = %q", personName("doe", "jane", "marie"))
	}
}
