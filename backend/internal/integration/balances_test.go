package integration

import (
	"context"
	"net/http"
	"testing"
)

type summary struct{ patient, insurance, total, credit string }

func (f *fixture) summary() summary {
	f.t.Helper()

	res := f.biller.get(f.t, "/api/patients/"+f.patientID+"/billing-summary").mustStatus(f.t, http.StatusOK)

	return summary{
		patient: res.Str("patient_balance"), insurance: res.Str("insurance_balance"),
		total: res.Str("total_outstanding"), credit: res.Str("unallocated_patient_credit"),
	}
}

func (f *fixture) assertSummary(want summary) {
	f.t.Helper()

	if got := f.summary(); got != want {
		f.t.Fatalf("summary = %+v, want %+v", got, want)
	}

	assertReconciled(f.t)
}

func (f *fixture) patientPayment(amount string, allocate bool) response {
	f.t.Helper()

	return f.biller.post(f.t, "/api/patients/"+f.patientID+"/payments", map[string]any{
		"payment_date": f.dos, "amount": amount, "method": "cash", "idempotency_key": newKey(), "auto_allocate": allocate,
	}).mustStatus(f.t, http.StatusCreated)
}

func TestBalanceNewDirectChargeIsFullyPatientOwed(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1)

	f.assertBalances(charge, "100.00", "0.00")
	f.assertSummary(summary{"100.00", "0.00", "100.00", "0.00"})
}

func TestBalanceNewInsuranceChargeInitialSplitUsesPolicyCopay(t *testing.T) {
	f := newFixture(t)
	policy := f.newPolicyWith(f.patientID, f.payerID, "secondary", map[string]any{"copay": "25.00"})
	charge := f.chargeFor(f.patientID, policy, 1, "")

	f.assertBalances(charge, "25.00", "75.00")
	f.assertSummary(summary{"25.00", "75.00", "100.00", "0.00"})

	// Without a copay the whole charge starts on insurance.
	other := f.insuranceCharge(1, "")
	f.assertBalances(other, "0.00", "100.00")
}

func TestBalancePatientPaymentReducesPatientOnly(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "30.00")

	f.patientPayment("30.00", true)

	f.assertBalances(charge, "0.00", "70.00")
	f.assertSummary(summary{"0.00", "70.00", "70.00", "0.00"})
}

func TestBalanceInsurancePaymentReducesInsuranceOnly(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "30.00")
	claim := f.submittedClaim(charge)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "70.00",
		line(claim.ID, claim.LineIDs[charge], "70.00", nil),
	)).mustStatus(t, http.StatusCreated)

	f.assertBalances(charge, "30.00", "0.00")
	f.assertSummary(summary{"30.00", "0.00", "30.00", "0.00"})
}

func TestBalanceDeductibleTransferPreservesTotalResponsibility(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0.00")
	claim := f.submittedClaim(charge)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "40.00",
		line(claim.ID, claim.LineIDs[charge], "40.00", map[string]any{
			"transfers": []map[string]any{{"reason": "deductible", "amount": "60.00"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	res := f.biller.get(t, "/api/charges/"+charge+"/ledger").mustStatus(t, http.StatusOK)

	if res.Str("breakdown.patient_responsibility") != "60.00" || res.Str("breakdown.insurance_responsibility") != "40.00" {
		t.Fatalf("responsibility after transfer: %s", string(res.Raw))
	}
	// Moving responsibility never changes the total: 60 + 40 = 100.
	if res.Str("breakdown.original_charge") != "100.00" || res.Str("breakdown.transfers_to_patient") != "60.00" {
		t.Fatalf("breakdown: %s", string(res.Raw))
	}

	f.assertSummary(summary{"60.00", "0.00", "60.00", "0.00"})
}

func TestBalanceWriteoffsReduceTheRightSide(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "40.00") // patient 40, insurance 60

	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "patient", "adjustment_type": "small_balance_writeoff", "amount": "15.00", "reason": "small balance",
	}).mustStatus(t, http.StatusCreated)
	f.assertSummary(summary{"25.00", "60.00", "85.00", "0.00"})

	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "contractual_writeoff", "amount": "20.00", "reason": "contract",
	}).mustStatus(t, http.StatusCreated)
	f.assertSummary(summary{"25.00", "40.00", "65.00", "0.00"})

	res := f.biller.get(t, "/api/charges/"+charge+"/ledger").mustStatus(t, http.StatusOK)
	if res.Str("breakdown.writeoffs") != "35.00" {
		t.Fatalf("write-offs = %s, want 35.00", res.Str("breakdown.writeoffs"))
	}
}

func TestBalancePatientRefundRestoresBalanceOnceUnapplied(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1)

	paid := f.patientPayment("100.00", true)
	f.assertBalances(charge, "0.00", "0.00")
	f.assertSummary(summary{"0.00", "0.00", "0.00", "0.00"})

	// Money applied to a service cannot be refunded directly.
	paymentID := paid.Str("payment.id")
	f.biller.post(t, "/api/patient-payments/"+paymentID+"/refunds", map[string]any{
		"amount": "100.00", "refund_date": f.dos, "method": "cash", "reason": "test", "idempotency_key": newKey(),
	}).mustStatus(t, http.StatusBadRequest)

	// Unapply: the service is owed again and the money becomes credit.
	f.biller.post(t, "/api/patient-payment-allocations/"+paid.Str("payment.allocations.0.id")+"/void", nil).mustStatus(t, http.StatusOK)
	f.assertSummary(summary{"100.00", "0.00", "100.00", "100.00"})

	// Refunding the credit leaves the balance owed and removes the credit.
	f.biller.post(t, "/api/patient-payments/"+paymentID+"/refunds", map[string]any{
		"amount": "100.00", "refund_date": f.dos, "method": "cash", "reason": "patient request", "idempotency_key": newKey(),
	}).mustStatus(t, http.StatusOK)
	f.assertSummary(summary{"100.00", "0.00", "100.00", "0.00"})
}

func TestBalanceInsuranceVoidRestoresInsuranceBalance(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0.00")
	claim := f.submittedClaim(charge)

	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00",
		line(claim.ID, claim.LineIDs[charge], "100.00", nil),
	)).mustStatus(t, http.StatusCreated)
	f.assertSummary(summary{"0.00", "0.00", "0.00", "0.00"})

	f.biller.post(t, "/api/insurance-payments/"+res.Str("payment.id")+"/void", map[string]any{"reason": "bounced"}).mustStatus(t, http.StatusOK)
	f.assertSummary(summary{"0.00", "100.00", "100.00", "0.00"})
}

func TestBalanceVoidedChargeHasNoBalance(t *testing.T) {
	f := newFixture(t)
	keep := f.directCharge(1)
	gone := f.directCharge(2)

	f.assertSummary(summary{"300.00", "0.00", "300.00", "0.00"})

	f.biller.post(t, "/api/charges/"+gone+"/void", map[string]any{"reason": "entered in error"}).mustStatus(t, http.StatusOK)

	f.assertBalances(gone, "0.00", "0.00")
	f.assertBalances(keep, "100.00", "0.00")
	f.assertSummary(summary{"100.00", "0.00", "100.00", "0.00"})
}

func TestBalanceUnallocatedPaymentIsCreditNotAReduction(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1)

	f.patientPayment("40.00", false)

	// The charge is untouched; the money is credit and is not counted twice.
	f.assertBalances(charge, "100.00", "0.00")
	f.assertSummary(summary{"100.00", "0.00", "100.00", "40.00"})

	// Applying it moves the credit onto the charge exactly once.
	f.biller.post(t, "/api/patient-payments/"+f.biller.get(t, "/api/patients/"+f.patientID+"/payments").Str("0.id")+"/allocations",
		map[string]any{"auto_allocate": true}).mustStatus(t, http.StatusOK)
	f.assertSummary(summary{"60.00", "0.00", "60.00", "0.00"})
}

func TestBalanceMultipleChargesReconcileToPatientTotals(t *testing.T) {
	f := newFixture(t)
	a := f.insuranceCharge(1, "20.00") // 20 / 80
	b := f.insuranceCharge(2, "50.00") // 50 / 150
	c := f.directCharge(1)             // 100 / 0
	claim := f.submittedClaim(a, b)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00",
		line(claim.ID, claim.LineIDs[a], "80.00", nil),
		line(claim.ID, claim.LineIDs[b], "20.00", map[string]any{"is_final": false}),
	)).mustStatus(t, http.StatusCreated)
	f.patientPayment("60.00", true) // oldest first: a's 20, b's 40

	patient, insurance := int64(0), int64(0)
	for _, id := range []string{a, b, c} {
		p, i := f.balances(id)
		patient += cents(t, p)
		insurance += cents(t, i)
	}

	got := f.summary()
	if cents(t, got.patient) != patient || cents(t, got.insurance) != insurance || cents(t, got.total) != patient+insurance {
		t.Fatalf("summary %+v does not equal the sum of open charge balances (patient %d insurance %d)", got, patient, insurance)
	}

	// Ledger events explain the same totals.
	ledger := f.biller.get(t, "/api/patients/"+f.patientID+"/ledger?page_size=200").mustStatus(t, http.StatusOK)
	var sumPatient, sumInsurance int64
	events, _ := ledger.Get("items").([]any)
	for _, e := range events {
		ev := e.(map[string]any)
		amount := cents(t, ev["amount"].(string)) * int64(ev["effect"].(float64))
		if ev["party"] == "patient" {
			sumPatient += amount
		} else {
			sumInsurance += amount
		}
	}
	if sumPatient != patient || sumInsurance != insurance {
		t.Fatalf("ledger sums patient %d insurance %d, want %d / %d", sumPatient, sumInsurance, patient, insurance)
	}

	assertReconciled(t)
}

func TestBalanceEndpointsAreProtectedAndScoped(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1)

	if res := (client{}).get(t, "/api/patients/"+f.patientID+"/billing-summary"); res.Status != http.StatusUnauthorized {
		t.Fatalf("summary without token: %d", res.Status)
	}

	f.biller.get(t, "/api/patients/00000000-0000-4000-8000-000000000000/billing-summary").mustStatus(t, http.StatusNotFound)
	f.biller.get(t, "/api/patients/not-a-uuid/billing-summary").mustStatus(t, http.StatusNotFound)
	f.biller.get(t, "/api/patients/"+f.patientID+"/ledger?party=nobody").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, "/api/charges/00000000-0000-4000-8000-000000000000/ledger").mustStatus(t, http.StatusNotFound)

	// A charge's ledger never leaks another patient's events through the patient ledger.
	other := f.newPatient()
	otherPolicy := f.newPolicy(other, f.payerID, "primary")
	otherCharge := f.chargeFor(other, otherPolicy, 1, "0")

	res := f.biller.get(t, "/api/patients/"+f.patientID+"/ledger?charge_id="+otherCharge).mustStatus(t, http.StatusOK)
	if res.Len("items") != 0 {
		t.Fatalf("another patient's charge appeared in this patient's ledger: %s", string(res.Raw))
	}
}

// The reconciliation script must actually detect broken ledgers; these
// corruptions are made inside a transaction that is always rolled back.
func TestReconciliationScriptDetectsCorruption(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0.00")
	claim := f.submittedClaim(charge)

	paid := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "60.00",
		line(claim.ID, claim.LineIDs[charge], "60.00", map[string]any{"is_final": false}),
	)).mustStatus(t, http.StatusCreated)
	assertReconciled(t)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	// Allocate more than the payment holds and more than the charge owes.
	exec(`UPDATE insurance_payment_allocations SET amount_paid = 150.00 WHERE payment_id = $1`, paid.Str("payment.id"))
	// A claim marked paid although its line was never finally adjudicated.
	exec(`UPDATE claims SET status = 'paid' WHERE id = $1`, claim.ID)

	found := map[string]bool{}
	for _, v := range reconcile(t, tx) {
		found[v.Check] = true
	}

	for _, want := range []string{
		"insurance_payment_overallocated", "negative_insurance_balance", "paid_claim_not_resolved",
	} {
		if !found[want] {
			t.Errorf("reconciliation did not report %q (found %v)", want, found)
		}
	}
}

func cents(t testing.TB, value string) int64 {
	t.Helper()

	var whole, frac int64
	negative := false

	if len(value) > 0 && value[0] == '-' {
		negative = true
		value = value[1:]
	}

	dot := -1
	for i, c := range value {
		if c == '.' {
			dot = i
			break
		}
	}

	if dot < 0 || len(value)-dot-1 != 2 {
		t.Fatalf("not a money value: %q", value)
	}

	for _, c := range value[:dot] {
		whole = whole*10 + int64(c-'0')
	}
	for _, c := range value[dot+1:] {
		frac = frac*10 + int64(c-'0')
	}

	total := whole*100 + frac
	if negative {
		return -total
	}

	return total
}
