package billing

import (
	"strings"
	"testing"
	"time"
)

const (
	testClaimID  = "11111111-1111-4111-8111-111111111111"
	testLineA    = "22222222-2222-4222-8222-222222222222"
	testLineB    = "33333333-3333-4333-8333-333333333333"
	remitPayerID = "44444444-4444-4444-8444-444444444444"
)

func TestParseRemittanceLine(t *testing.T) {
	good := RemittanceLine{
		ClaimID: testClaimID, ClaimLineID: testLineA, AmountPaid: "50.5", AllowedAmount: "80",
		Adjustments: []RemittanceAdjustment{{Type: "contractual_writeoff", Amount: "20"}},
		Transfers:   []RemittanceTransfer{{Reason: "deductible", Amount: "10.25"}},
	}

	p, message := parseRemittanceLine(good)
	if message != "" {
		t.Fatalf("expected valid line, got %q", message)
	}

	if p.Paid != 5050 || p.adjusted() != 2000 || p.transferred() != 1025 || p.resolved() != 8075 || !p.Final {
		t.Fatalf("unexpected parse: %+v", p)
	}

	if p.Allowed == nil || *p.Allowed != 8000 {
		t.Fatalf("allowed amount not parsed: %+v", p.Allowed)
	}

	notFinal := false
	cases := map[string]func(*RemittanceLine){
		"bad claim id":           func(l *RemittanceLine) { l.ClaimID = "nope" },
		"bad line id":            func(l *RemittanceLine) { l.ClaimLineID = "" },
		"negative paid":          func(l *RemittanceLine) { l.AmountPaid = "-1" },
		"three decimals":         func(l *RemittanceLine) { l.AmountPaid = "1.001" },
		"text paid":              func(l *RemittanceLine) { l.AmountPaid = "ten" },
		"negative allowed":       func(l *RemittanceLine) { l.AllowedAmount = "-5" },
		"allowed three decimals": func(l *RemittanceLine) { l.AllowedAmount = "5.555" },
		"zero adjustment":        func(l *RemittanceLine) { l.Adjustments[0].Amount = "0" },
		"adjustment type":        func(l *RemittanceLine) { l.Adjustments[0].Type = "gift" },
		"negative transfer":      func(l *RemittanceLine) { l.Transfers[0].Amount = "-3" },
		"transfer reason":        func(l *RemittanceLine) { l.Transfers[0].Reason = "correction" },
		"too many adjustments": func(l *RemittanceLine) {
			l.Adjustments = make([]RemittanceAdjustment, 21)
		},
		"empty non-final line": func(l *RemittanceLine) {
			l.AmountPaid, l.Adjustments, l.Transfers, l.IsFinal = "0", nil, nil, &notFinal
		},
	}

	for name, mutate := range cases {
		line := good
		line.Adjustments = append([]RemittanceAdjustment(nil), good.Adjustments...)
		line.Transfers = append([]RemittanceTransfer(nil), good.Transfers...)
		mutate(&line)

		if _, message := parseRemittanceLine(line); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}

	// A $0, final, no-adjustment line is a valid denial acknowledgement.
	final := RemittanceLine{ClaimID: testClaimID, ClaimLineID: testLineA}
	if _, message := parseRemittanceLine(final); message != "" {
		t.Errorf("zero denial line rejected: %s", message)
	}
}

func TestValidateInsurancePaymentInput(t *testing.T) {
	today := time.Date(2026, 10, 5, 0, 30, 0, 0, time.FixedZone("PKT", 5*3600))

	valid := func() InsurancePaymentInput {
		return InsurancePaymentInput{
			PayerID: remitPayerID, PaymentDate: "2026-10-05", Amount: "0", PaymentType: "eft",
			IdempotencyKey: testClaimID, ReferenceNumber: "EFT-1",
			Lines: []RemittanceLine{{ClaimID: testClaimID, ClaimLineID: testLineA, AmountPaid: "0"}},
		}
	}

	in := valid()
	if _, message := validateInsurancePaymentInput(&in, today); message != "" {
		t.Fatalf("a $0 EOB dated today must be valid, got %q", message)
	}

	cases := map[string]func(*InsurancePaymentInput){
		"no idempotency key":  func(p *InsurancePaymentInput) { p.IdempotencyKey = "" },
		"no payer":            func(p *InsurancePaymentInput) { p.PayerID = "" },
		"future date":         func(p *InsurancePaymentInput) { p.PaymentDate = "2026-10-06" },
		"bad date":            func(p *InsurancePaymentInput) { p.PaymentDate = "10/05/2026" },
		"negative amount":     func(p *InsurancePaymentInput) { p.Amount = "-1" },
		"three decimals":      func(p *InsurancePaymentInput) { p.Amount = "5.001" },
		"bad type":            func(p *InsurancePaymentInput) { p.PaymentType = "bitcoin" },
		"card number":         func(p *InsurancePaymentInput) { p.ReferenceNumber = "4111111111111111" },
		"long notes":          func(p *InsurancePaymentInput) { p.Notes = strings.Repeat("x", 2001) },
		"too many lines":      func(p *InsurancePaymentInput) { p.Lines = make([]RemittanceLine, 201) },
		"duplicate lines":     func(p *InsurancePaymentInput) { p.Lines = append(p.Lines, p.Lines[0]) },
		"empty amount string": func(p *InsurancePaymentInput) { p.Amount = "" },
	}

	for name, mutate := range cases {
		p := valid()
		p.Lines = append([]RemittanceLine(nil), p.Lines...)
		mutate(&p)

		if _, message := validateInsurancePaymentInput(&p, today); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestCheckRemittanceBalances(t *testing.T) {
	targets := map[string]remittanceTarget{
		testLineA: {LineID: testLineA, ChargeID: "chargeA"},
		testLineB: {LineID: testLineB, ChargeID: "chargeB"},
	}
	balances := map[string]int64{"chargeA": 8000, "chargeB": 3000}

	line := func(id string, paid, adj, transfer int64) parsedLine {
		l := parsedLine{ClaimLineID: id, Paid: paid, Final: true}
		if adj > 0 {
			l.Adjustments = []parsedAdjustment{{Type: "contractual_writeoff", Amount: adj}}
		}
		if transfer > 0 {
			l.Transfers = []parsedTransfer{{Reason: "deductible", Amount: transfer}}
		}
		return l
	}

	ok := []parsedLine{line(testLineA, 5000, 2000, 1000), line(testLineB, 3000, 0, 0)}
	if message := checkRemittanceBalances(ok, targets, balances, 8000); message != "" {
		t.Fatalf("expected valid, got %q", message)
	}

	cases := map[string]struct {
		lines     []parsedLine
		available int64
	}{
		"paid exceeds payment":          {[]parsedLine{line(testLineA, 5000, 0, 0), line(testLineB, 3000, 0, 0)}, 7999},
		"resolved exceeds the balance":  {[]parsedLine{line(testLineA, 5000, 2000, 1001)}, 9000},
		"balance of another line":       {[]parsedLine{line(testLineB, 3000, 1, 0)}, 9000},
		"same charge on two lines adds": {[]parsedLine{line(testLineA, 5000, 0, 0), {ClaimLineID: testLineA, Paid: 3001}}, 9000},
	}

	for name, c := range cases {
		if message := checkRemittanceBalances(c.lines, targets, balances, c.available); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestCheckRemittanceTargets(t *testing.T) {
	good := remittanceTarget{
		LineID: testLineA, ClaimID: testClaimID, ChargeID: "c", PayerID: remitPayerID,
		ClaimNumber: "CLM-1", ClaimStatus: "submitted", ChargeStatus: "active", IsCurrent: true, LineTotal: 10000,
	}
	lines := []parsedLine{{ClaimID: testClaimID, ClaimLineID: testLineA}}

	if message := checkRemittanceTargets(lines, map[string]remittanceTarget{testLineA: good}, remitPayerID); message != "" {
		t.Fatalf("expected valid, got %q", message)
	}

	mutations := map[string]func(*remittanceTarget){
		"other payer":           func(t *remittanceTarget) { t.PayerID = "55555555-5555-4555-8555-555555555555" },
		"other claim":           func(t *remittanceTarget) { t.ClaimID = "66666666-6666-4666-8666-666666666666" },
		"draft claim":           func(t *remittanceTarget) { t.ClaimStatus = "draft" },
		"rejected claim":        func(t *remittanceTarget) { t.ClaimStatus = "rejected" },
		"voided claim":          func(t *remittanceTarget) { t.ClaimStatus = "voided" },
		"released line":         func(t *remittanceTarget) { t.IsCurrent = false },
		"voided charge":         func(t *remittanceTarget) { t.ChargeStatus = "voided" },
		"allowed above billing": nil,
	}

	for name, mutate := range mutations {
		target := good
		l := lines
		if mutate != nil {
			mutate(&target)
		} else {
			over := int64(10001)
			l = []parsedLine{{ClaimID: testClaimID, ClaimLineID: testLineA, Allowed: &over}}
		}

		if message := checkRemittanceTargets(l, map[string]remittanceTarget{testLineA: target}, remitPayerID); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}

	if message := checkRemittanceTargets(lines, map[string]remittanceTarget{}, remitPayerID); message == "" {
		t.Error("missing line must be rejected")
	}
}

func TestClaimResolved(t *testing.T) {
	cases := []struct {
		name  string
		lines []lineResolution
		want  bool
	}{
		{"no lines", nil, false},
		{"paid and final, nothing left", []lineResolution{{Adjudicated: true}}, true},
		{"final but insurance balance remains", []lineResolution{{Adjudicated: true, InsuranceBalance: 3000}}, false},
		{"final, balance forwarded to next payer", []lineResolution{{Adjudicated: true, InsuranceBalance: 3000, Forwarded: true}}, true},
		{"payment received but not final", []lineResolution{{Adjudicated: false}}, false},
		{"one line unresolved", []lineResolution{{Adjudicated: true}, {Adjudicated: true, InsuranceBalance: 1}}, false},
		{"all lines resolved", []lineResolution{{Adjudicated: true}, {Adjudicated: true, InsuranceBalance: 5, Forwarded: true}}, true},
	}

	for _, c := range cases {
		if got := claimResolved(c.lines); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestIsFutureDate(t *testing.T) {
	// 00:30 on Oct 6 in UTC+5 is still Oct 5 in UTC; "today" must not be future.
	local := time.Date(2026, 10, 6, 0, 30, 0, 0, time.FixedZone("PKT", 5*3600))
	oct6, _ := parseDate("2026-10-06")
	oct7, _ := parseDate("2026-10-07")

	if isFutureDate(oct6, local) {
		t.Error("today's date must not be in the future")
	}
	if !isFutureDate(oct7, local) {
		t.Error("tomorrow must be in the future")
	}
}

func TestParseAdjustmentPartyRules(t *testing.T) {
	if _, message := parseAdjustment("contractual_writeoff", "10", "r", "", "patient"); message == "" {
		t.Error("contractual write-offs must be insurance-only")
	}
	if _, message := parseAdjustment("courtesy_writeoff", "10", "r", "", "patient"); message != "" {
		t.Errorf("courtesy write-off on the patient side should be valid: %s", message)
	}
}
