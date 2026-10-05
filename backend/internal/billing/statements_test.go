package billing

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestValidateStatementRequest(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 30, 0, 0, time.FixedZone("PKT", 5*3600))

	ok := []StatementRequest{
		{Type: "open_balance"},
		{Type: " OPEN_BALANCE ", StartDate: "2026-01-01", EndDate: "ignored"},
		{Type: "date_range", StartDate: "2026-09-01", EndDate: "2026-10-06"},
		{Type: "date_range", StartDate: "2026-10-06", EndDate: "2026-10-06"},
	}

	for _, r := range ok {
		r := r
		if message := validateStatementRequest(&r, now); message != "" {
			t.Errorf("%+v rejected: %s", r, message)
		}
	}

	// Open balance statements carry no period.
	open := StatementRequest{Type: "open_balance", StartDate: "2026-01-01"}
	validateStatementRequest(&open, now)
	if open.StartDate != "" || open.EndDate != "" {
		t.Errorf("open balance kept a period: %+v", open)
	}

	bad := map[string]StatementRequest{
		"unknown type": {Type: "monthly"},
		"no dates":     {Type: "date_range"},
		"end first":    {Type: "date_range", StartDate: "2026-02-01", EndDate: "2026-01-01"},
		"future end":   {Type: "date_range", StartDate: "2026-10-01", EndDate: "2026-10-07"},
		"too old":      {Type: "date_range", StartDate: "1999-12-31", EndDate: "2026-01-01"},
		"too long":     {Type: "date_range", StartDate: "2000-01-01", EndDate: "2026-01-01"},
		"long comment": {Type: "open_balance", Comment: strings.Repeat("x", 501)},
		"bad date":     {Type: "date_range", StartDate: "01/01/2026", EndDate: "2026-02-01"},
	}

	for name, r := range bad {
		r := r
		if message := validateStatementRequest(&r, now); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestRangeStatementLines(t *testing.T) {
	ev := func(on, kind string, effect int64, amount int64) statementEvent {
		return statementEvent{OccurredOn: on, EventType: kind, Effect: effect, Amount: amount, ServiceCode: "90837", Description: "Therapy", DateOfService: on}
	}

	events := []statementEvent{
		ev("2026-08-20", "charge", 1, 10000),          // before: carried in
		ev("2026-08-25", "patient_payment", -1, 2000), // before: carried in
		ev("2026-09-01", "charge", 1, 15000),          // first day
		ev("2026-09-10", "transfer_in", 1, 3000),
		ev("2026-09-15", "patient_payment", -1, 5000),
		ev("2026-09-30", "adjustment", -1, 1000), // last day
		ev("2026-10-01", "charge", 1, 99999),     // after: excluded
	}

	lines, previous, charges, payments, adjustments, balance := rangeStatementLines(events, "2026-09-01", "2026-09-30")

	if previous != 8000 || charges != 18000 || payments != 5000 || adjustments != 1000 || balance != 20000 {
		t.Fatalf("totals: previous %d charges %d payments %d adjustments %d balance %d", previous, charges, payments, adjustments, balance)
	}

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines in the period, got %d", len(lines))
	}

	wantRunning := []string{"230.00", "260.00", "210.00", "200.00"}
	for i, l := range lines {
		if l.Balance != wantRunning[i] {
			t.Errorf("line %d running balance %s, want %s", i, l.Balance, wantRunning[i])
		}
	}

	if previous+charges-payments-adjustments != balance {
		t.Error("balance must equal previous + charges - payments - adjustments")
	}

	if !strings.Contains(lines[1].Description, "Patient responsibility: ") {
		t.Errorf("transfer description: %q", lines[1].Description)
	}
}

func TestPDFMoney(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"0.00":       "$0.00",
		"5":          "$5.00",
		"1234567.89": "$1,234,567.89",
		"-45.10":     "-$45.10",
		"999.99":     "$999.99",
		"1000.00":    "$1,000.00",
	}

	for in, want := range cases {
		if got := pdfMoney(in); got != want {
			t.Errorf("pdfMoney(%q) = %q, want %q", in, got, want)
		}
	}
}

func sampleSnapshot(kind string, lines int) StatementSnapshot {
	s := StatementSnapshot{
		StatementNumber: "STM-0001001", Type: kind, StatementDate: "2026-10-05",
		Practice:         StatementPractice{Name: "Test Practice", Phone: "555-0100", Address: SnapshotAddress{Address1: "1 Main St", City: "Springfield", State: "IL", Zip: "62701"}},
		Patient:          StatementPatient{Name: "Pat Example", AccountNumber: "A-1", Address: SnapshotAddress{Address1: "9 Oak Ave", City: "Springfield", State: "IL", Zip: "62701"}},
		PreviousBalance:  "0.00",
		TotalCharges:     "100.00",
		TotalPayments:    "0.00",
		TotalAdjustments: "0.00",
		BalanceDue:       "100.00",
		CreditOnAccount:  "10.00",
		AmountDue:        "90.00",
		Comment:          "Thank you.",
	}

	if kind == "date_range" {
		s.StartDate, s.EndDate = "2026-09-01", "2026-09-30"
	}

	for i := 0; i < lines; i++ {
		s.Lines = append(s.Lines, StatementLine{
			Date: "2026-09-01", Description: fmt.Sprintf("90837 - Psychotherapy session %d", i),
			Charge: "100.00", Responsibility: "100.00", Balance: "100.00",
		})
	}

	return s
}

func TestRenderStatementPDFIsDeterministicAndPaginates(t *testing.T) {
	for _, kind := range []string{"open_balance", "date_range"} {
		a, sumA, err := renderStatementPDF(sampleSnapshot(kind, 3))
		if err != nil {
			t.Fatal(err)
		}
		b, sumB, _ := renderStatementPDF(sampleSnapshot(kind, 3))

		if !bytes.Equal(a, b) || sumA != sumB {
			t.Errorf("%s: the same snapshot must render to identical bytes", kind)
		}

		if !bytes.HasPrefix(a, []byte("%PDF-")) {
			t.Errorf("%s: not a PDF", kind)
		}

		for _, want := range []string{"STM-0001001", "Pat Example", "$90.00", "Thank you."} {
			if !bytes.Contains(a, []byte(want)) {
				t.Errorf("%s: PDF is missing %q", kind, want)
			}
		}

		// A long statement flows onto more pages and still ends with its totals.
		long, _, err := renderStatementPDF(sampleSnapshot(kind, 120))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(long, []byte("/Type /Page")) < 4 {
			t.Errorf("%s: expected a multi-page PDF", kind)
		}
		if !bytes.Contains(long, []byte("continued")) || !bytes.Contains(long, []byte("Amount due")) {
			t.Errorf("%s: long statement lost its continuation header or totals", kind)
		}
	}

	// Several statements in one document (batch convenience PDF).
	combined, _, err := renderStatementPDF(sampleSnapshot("open_balance", 2), sampleSnapshot("date_range", 2))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(combined, []byte("(STATEMENT)")) != 2 {
		t.Errorf("combined PDF should contain two statements")
	}
}
