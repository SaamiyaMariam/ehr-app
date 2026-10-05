package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// $100 service, patient share $20 => insurance owes $80.

func TestInsurancePaymentFullPaymentResolvesClaim(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "20.00")
	claim := f.submittedClaim(charge)

	f.assertBalances(charge, "20.00", "80.00")

	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "80.00",
		line(claim.ID, claim.LineIDs[charge], "80.00", map[string]any{"allowed_amount": "100.00"}),
	)).mustStatus(t, http.StatusCreated)

	f.assertBalances(charge, "20.00", "0.00")

	if got := res.Str("claims.0.status"); got != "paid" {
		t.Fatalf("claim outcome = %q, want paid", got)
	}
	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid", got)
	}
	if res.Str("payment.unallocated") != "0.00" || res.Str("payment.allocated") != "80.00" {
		t.Fatalf("unexpected allocation totals: %s", string(res.Raw))
	}
}

func TestInsurancePaymentPartialKeepsClaimOpen(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "20.00")
	claim := f.submittedClaim(charge)

	// A partial payment the payer is not finished with.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "50.00",
		line(claim.ID, claim.LineIDs[charge], "50.00", map[string]any{"is_final": false}),
	)).mustStatus(t, http.StatusCreated)

	f.assertBalances(charge, "20.00", "30.00")

	if got := f.claimStatus(claim.ID); got != "submitted" {
		t.Fatalf("claim status = %q, want submitted (still open)", got)
	}

	// Finally adjudicated, but $30 of insurance balance is unresolved: still open.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "0",
		line(claim.ID, claim.LineIDs[charge], "0", map[string]any{"is_final": true}),
	)).mustStatus(t, http.StatusCreated)

	if got := f.claimStatus(claim.ID); got != "submitted" {
		t.Fatalf("claim with remaining insurance balance = %q, want submitted", got)
	}

	// Resolve the remainder with a contractual write-off: now it resolves.
	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "0",
		line(claim.ID, claim.LineIDs[charge], "0", map[string]any{
			"adjustments": []map[string]any{{"type": "contractual_writeoff", "amount": "30.00", "reason": "contract rate"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	f.assertBalances(charge, "20.00", "0.00")

	if got := res.Str("claims.0.status"); got != "paid" {
		t.Fatalf("claim outcome = %q, want paid", got)
	}
}

func TestInsurancePaymentMultiLine(t *testing.T) {
	f := newFixture(t)
	a := f.insuranceCharge(1, "0")
	b := f.insuranceCharge(2, "0")
	claim := f.submittedClaim(a, b)

	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "300.00",
		line(claim.ID, claim.LineIDs[a], "100.00", nil),
		line(claim.ID, claim.LineIDs[b], "200.00", nil),
	)).mustStatus(t, http.StatusCreated)

	if res.Len("payment.allocations") != 2 {
		t.Fatalf("want 2 allocations: %s", string(res.Raw))
	}

	f.assertBalances(a, "0.00", "0.00")
	f.assertBalances(b, "0.00", "0.00")

	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid", got)
	}
}

func TestZeroDollarEOBWithDeductibleTransfer(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0.00") // insurance owes $100
	claim := f.submittedClaim(charge)

	// Payer pays nothing: $70 contractual, $30 deductible becomes patient responsibility.
	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "0",
		line(claim.ID, claim.LineIDs[charge], "0", map[string]any{
			"allowed_amount": "30.00",
			"adjustments":    []map[string]any{{"type": "contractual_writeoff", "amount": "70.00"}},
			"transfers":      []map[string]any{{"reason": "deductible", "amount": "30.00"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	if res.Str("payment.amount") != "0.00" {
		t.Fatalf("zero-dollar payment amount = %s", res.Str("payment.amount"))
	}

	f.assertBalances(charge, "30.00", "0.00")

	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid", got)
	}

	// Responsibility moved, it was not created: total owed is $100 - $70 write-off.
	c := f.biller.get(t, "/api/charges/"+charge).mustStatus(t, http.StatusOK)
	if c.Str("balances.total_balance") != "30.00" {
		t.Fatalf("total balance = %s, want 30.00", c.Str("balances.total_balance"))
	}
}

func TestInsurancePaymentRejections(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "20.00") // insurance owes 80
	claim := f.submittedClaim(charge)
	lineID := claim.LineIDs[charge]

	// Another patient with their own claim, billed to the same payer.
	otherPatient := f.newPatient()
	otherPolicy := f.newPolicy(otherPatient, f.payerID, "primary")
	otherCharge := f.chargeFor(otherPatient, otherPolicy, 1, "0")
	otherClaim := f.submittedClaim(otherCharge)

	// A claim billed to a different payer.
	otherPayer := f.newPayer(unique("Other Payer "))
	payerPatient := f.newPatient()
	payerPolicy := f.newPolicy(payerPatient, otherPayer, "primary")
	payerCharge := f.chargeFor(payerPatient, payerPolicy, 1, "0")
	payerClaim := f.submittedClaim(payerCharge)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"allocation greater than payment", payment(f.payerID, "50.00", line(claim.ID, lineID, "60.00", nil))},
		{"paid more than the insurance balance", payment(f.payerID, "100.00", line(claim.ID, lineID, "90.00", nil))},
		{"claim of another patient under this claim id", payment(f.payerID, "10.00", line(claim.ID, otherClaim.LineIDs[otherCharge], "10.00", nil))},
		{"line of another patient with its own claim id still needs the same payer", payment(otherPayer, "10.00", line(otherClaim.ID, otherClaim.LineIDs[otherCharge], "10.00", nil))},
		{"claim billed to a different payer", payment(f.payerID, "10.00", line(payerClaim.ID, payerClaim.LineIDs[payerCharge], "10.00", nil))},
		{"three decimal places", payment(f.payerID, "10.001", line(claim.ID, lineID, "10.00", nil))},
		{"three decimal places on a line", payment(f.payerID, "10.00", line(claim.ID, lineID, "10.001", nil))},
		{"negative payment", payment(f.payerID, "-5.00")},
		{"negative line", payment(f.payerID, "10.00", line(claim.ID, lineID, "-1.00", nil))},
		{"negative adjustment", payment(f.payerID, "0", line(claim.ID, lineID, "0", map[string]any{
			"adjustments": []map[string]any{{"type": "contractual_writeoff", "amount": "-10.00"}}}))},
		{"zero adjustment", payment(f.payerID, "0", line(claim.ID, lineID, "0", map[string]any{
			"adjustments": []map[string]any{{"type": "contractual_writeoff", "amount": "0"}}}))},
		{"unknown adjustment type", payment(f.payerID, "0", line(claim.ID, lineID, "0", map[string]any{
			"adjustments": []map[string]any{{"type": "free_money", "amount": "10.00"}}}))},
		{"transfer beyond the balance", payment(f.payerID, "0", line(claim.ID, lineID, "0", map[string]any{
			"transfers": []map[string]any{{"reason": "deductible", "amount": "80.01"}}}))},
		{"allowed above billed", payment(f.payerID, "10.00", line(claim.ID, lineID, "10.00", map[string]any{"allowed_amount": "100.01"}))},
		{"duplicate line", payment(f.payerID, "20.00", line(claim.ID, lineID, "10.00", nil), line(claim.ID, lineID, "10.00", nil))},
		{"unknown payer", payment("00000000-0000-4000-8000-000000000000", "10.00", line(claim.ID, lineID, "10.00", nil))},
		{"card number in reference", func() map[string]any {
			p := payment(f.payerID, "10.00", line(claim.ID, lineID, "10.00", nil))
			p["reference_number"] = "4111 1111 1111 1111"
			return p
		}()},
	}

	for _, c := range cases {
		res := f.biller.post(t, "/api/insurance-payments", c.body)
		if res.Status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", c.name, res.Status, string(res.Raw))
		}
	}

	// Nothing from the rejected requests was posted.
	f.assertBalances(charge, "20.00", "80.00")
	f.assertBalances(otherCharge, "0.00", "100.00")

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM insurance_payments WHERE payer_id IN ($1, $2)`, f.payerID, otherPayer).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d payments were stored by rejected requests", count)
	}
}

func TestInsurancePaymentRollsBackWhenOneLineIsInvalid(t *testing.T) {
	f := newFixture(t)
	a := f.insuranceCharge(1, "0")
	b := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(a, b)

	// Line A is fine; line B overpays the service. Nothing may be posted.
	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "500.00",
		line(claim.ID, claim.LineIDs[a], "100.00", nil),
		line(claim.ID, claim.LineIDs[b], "150.00", nil),
	))
	if res.Status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", res.Status, string(res.Raw))
	}

	f.assertBalances(a, "0.00", "100.00")
	f.assertBalances(b, "0.00", "100.00")

	var payments, allocations int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM insurance_payments WHERE payer_id = $1`, f.payerID).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM insurance_payment_allocations WHERE payer_id = $1`, f.payerID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if payments != 0 || allocations != 0 {
		t.Fatalf("half-posted remittance: %d payments, %d allocations", payments, allocations)
	}
}

func TestInsurancePaymentIdempotency(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	body := payment(f.payerID, "40.00", line(claim.ID, claim.LineIDs[charge], "40.00", map[string]any{"is_final": false}))

	first := f.biller.post(t, "/api/insurance-payments", body).mustStatus(t, http.StatusCreated)
	again := f.biller.post(t, "/api/insurance-payments", body).mustStatus(t, http.StatusOK)

	if !again.Bool("duplicate") || again.Str("payment.id") != first.Str("payment.id") {
		t.Fatalf("replay must return the original payment: %s", string(again.Raw))
	}

	f.assertBalances(charge, "0.00", "60.00")
}

func TestInsurancePaymentConcurrentPostingsCannotOverspend(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0") // insurance owes $100
	claim := f.submittedClaim(charge)

	const workers = 6

	var wg sync.WaitGroup
	statuses := make([]int, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := payment(f.payerID, "100.00", line(claim.ID, claim.LineIDs[charge], "100.00", map[string]any{"is_final": false}))
			statuses[i] = f.biller.post(t, "/api/insurance-payments", body).Status
		}(i)
	}
	wg.Wait()

	created := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			created++
		}
	}

	if created != 1 {
		t.Fatalf("exactly one concurrent $100 posting may succeed, got %d (%v)", created, statuses)
	}

	f.assertBalances(charge, "0.00", "0.00")
}

func TestInsurancePaymentVoidRestoresBalancesAndClaim(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	res := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "70.00",
		line(claim.ID, claim.LineIDs[charge], "70.00", map[string]any{
			"transfers": []map[string]any{{"reason": "coinsurance", "amount": "30.00"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	paymentID := res.Str("payment.id")

	f.assertBalances(charge, "30.00", "0.00")

	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid", got)
	}

	// The patient pays what was transferred: the remittance can no longer be voided.
	pay := f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
		"payment_date": f.dos, "amount": "30.00", "method": "cash", "idempotency_key": newKey(), "auto_allocate": true,
	}).mustStatus(t, http.StatusCreated)

	blocked := f.biller.post(t, "/api/insurance-payments/"+paymentID+"/void", map[string]any{"reason": "entered in error"})
	if blocked.Status != http.StatusConflict {
		t.Fatalf("void with dependent patient payment: status %d: %s", blocked.Status, string(blocked.Raw))
	}

	f.assertBalances(charge, "0.00", "0.00")

	// Unapply the patient payment, then the void is allowed.
	allocID := pay.Str("payment.allocations.0.id")
	f.biller.post(t, "/api/patient-payment-allocations/"+allocID+"/void", nil).mustStatus(t, http.StatusOK)

	f.biller.post(t, "/api/insurance-payments/"+paymentID+"/void", map[string]any{"reason": "entered in error"}).mustStatus(t, http.StatusOK)

	f.assertBalances(charge, "0.00", "100.00")

	if got := f.claimStatus(claim.ID); got != "submitted" {
		t.Fatalf("claim status after void = %q, want submitted", got)
	}

	// Voiding twice is a conflict, and history keeps both events.
	f.biller.post(t, "/api/insurance-payments/"+paymentID+"/void", map[string]any{"reason": "again"}).mustStatus(t, http.StatusConflict)

	var events int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM claim_history WHERE claim_id = $1 AND event_type IN ('payment_posted', 'payment_voided', 'paid', 'payment_reversed')`, claim.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 4 {
		t.Fatalf("claim history events = %d, want 4", events)
	}
}

func TestInsurancePaymentAuthorization(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	body := payment(f.payerID, "10.00", line(claim.ID, claim.LineIDs[charge], "10.00", map[string]any{"is_final": false}))

	if res := (client{}).post(t, "/api/insurance-payments", body); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: status %d, want 401", res.Status)
	}

	scheduler := newUser(t, "practice_scheduler")
	if res := scheduler.post(t, "/api/insurance-payments", body); res.Status != http.StatusForbidden {
		t.Fatalf("wrong role: status %d, want 403", res.Status)
	}

	nobody := newUser(t)
	if res := nobody.post(t, "/api/insurance-payments", body); res.Status != http.StatusForbidden {
		t.Fatalf("no roles: status %d, want 403", res.Status)
	}

	f.assertBalances(charge, "0.00", "100.00")

	res := f.biller.post(t, "/api/insurance-payments", body).mustStatus(t, http.StatusCreated)

	// Reads need a token but not the biller role.
	if got := (client{}).get(t, "/api/insurance-payments"); got.Status != http.StatusUnauthorized {
		t.Fatalf("list without token: %d", got.Status)
	}
	scheduler.get(t, "/api/insurance-payments/"+res.Str("payment.id")).mustStatus(t, http.StatusOK)

	// Mutations of adjustments / transfers are biller-only as well.
	if got := scheduler.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{}); got.Status != http.StatusForbidden {
		t.Fatalf("adjustment wrong role: %d", got.Status)
	}
	if got := scheduler.post(t, "/api/insurance-payments/"+res.Str("payment.id")+"/void", map[string]any{"reason": "x"}); got.Status != http.StatusForbidden {
		t.Fatalf("void wrong role: %d", got.Status)
	}
}

func TestOutstandingInsuranceListsPostableLines(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "20.00")
	claim := f.submittedClaim(charge)

	res := f.biller.get(t, "/api/billing/outstanding-insurance?payer_id="+f.payerID).mustStatus(t, http.StatusOK)

	if res.Len("items") != 1 || res.Str("items.0.claim_line_id") != claim.LineIDs[charge] || res.Str("items.0.insurance_balance") != "80.00" {
		t.Fatalf("unexpected outstanding lines: %s", string(res.Raw))
	}

	// A fully resolved line leaves the list.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "80.00",
		line(claim.ID, claim.LineIDs[charge], "80.00", nil),
	)).mustStatus(t, http.StatusCreated)

	res = f.biller.get(t, "/api/billing/outstanding-insurance?payer_id="+f.payerID).mustStatus(t, http.StatusOK)
	if res.Len("items") != 0 {
		t.Fatalf("resolved line should not be outstanding: %s", string(res.Raw))
	}

	f.biller.get(t, "/api/billing/outstanding-insurance?payer_id=not-a-uuid").mustStatus(t, http.StatusBadRequest)
}

func TestStandaloneAdjustmentsAndTransfers(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "20.00")
	claim := f.submittedClaim(charge)

	// Insurance write-off of part of the balance.
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "payer_adjustment", "amount": "10.00", "reason": "payer correction",
	}).mustStatus(t, http.StatusCreated)
	f.assertBalances(charge, "20.00", "70.00")

	// Over-adjusting is refused.
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "payer_adjustment", "amount": "70.01", "reason": "too much",
	}).mustStatus(t, http.StatusBadRequest)

	// Contractual write-offs never apply to the patient side.
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "patient", "adjustment_type": "contractual_writeoff", "amount": "5.00", "reason": "no",
	}).mustStatus(t, http.StatusBadRequest)

	// A reason is mandatory for the audit trail.
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "patient", "adjustment_type": "courtesy_writeoff", "amount": "5.00",
	}).mustStatus(t, http.StatusBadRequest)

	// Patient courtesy write-off.
	adj := f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "patient", "adjustment_type": "courtesy_writeoff", "amount": "5.00", "reason": "hardship",
	}).mustStatus(t, http.StatusCreated)
	f.assertBalances(charge, "15.00", "70.00")
	_ = adj

	// Transfer insurance -> patient and back.
	f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
		"from_party": "insurance", "to_party": "patient", "amount": "70.00", "reason": "noncovered",
	}).mustStatus(t, http.StatusCreated)
	f.assertBalances(charge, "85.00", "0.00")

	// Cannot move more than the patient owes.
	f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
		"from_party": "patient", "to_party": "insurance", "amount": "85.01", "reason": "correction",
	}).mustStatus(t, http.StatusBadRequest)

	f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
		"from_party": "patient", "to_party": "insurance", "amount": "10.00", "reason": "correction",
	}).mustStatus(t, http.StatusCreated)
	f.assertBalances(charge, "75.00", "10.00")

	// The charge cannot be voided or edited while financial activity exists.
	f.biller.post(t, "/api/charges/"+charge+"/void", map[string]any{"reason": "oops"}).mustStatus(t, http.StatusConflict)

	_ = claim
}

func TestVoidStandaloneAdjustmentRestoresBalance(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	// Final $0 adjudication, then write the whole balance off: claim resolves.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "0",
		line(claim.ID, claim.LineIDs[charge], "0", nil),
	)).mustStatus(t, http.StatusCreated)

	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "bad_debt_writeoff", "amount": "100.00", "reason": "uncollectible",
	}).mustStatus(t, http.StatusCreated)

	f.assertBalances(charge, "0.00", "0.00")

	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid after write-off", got)
	}

	var adjustmentID string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM billing_adjustments WHERE charge_id = $1 AND status = 'active'`, charge).Scan(&adjustmentID); err != nil {
		t.Fatal(err)
	}

	f.biller.post(t, "/api/billing-adjustments/"+adjustmentID+"/void", map[string]any{"reason": "mistake"}).mustStatus(t, http.StatusOK)

	f.assertBalances(charge, "0.00", "100.00")

	if got := f.claimStatus(claim.ID); got != "submitted" {
		t.Fatalf("claim status = %q, want submitted after void", got)
	}

	f.biller.post(t, "/api/billing-adjustments/"+adjustmentID+"/void", map[string]any{"reason": "again"}).mustStatus(t, http.StatusConflict)
}

func TestRemittanceAdjustmentsCannotBeVoidedIndividually(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "50.00",
		line(claim.ID, claim.LineIDs[charge], "50.00", map[string]any{
			"adjustments": []map[string]any{{"type": "contractual_writeoff", "amount": "50.00"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	var adjustmentID string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM billing_adjustments WHERE charge_id = $1`, charge).Scan(&adjustmentID); err != nil {
		t.Fatal(err)
	}

	f.biller.post(t, "/api/billing-adjustments/"+adjustmentID+"/void", map[string]any{"reason": "no"}).mustStatus(t, http.StatusConflict)
}

func TestUnsubmittedClaimsCannotReceivePayments(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")

	res := f.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated)
	lineID := res.Str("lines.0.id")

	post := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "10.00", line(res.Str("id"), lineID, "10.00", nil)))
	if post.Status != http.StatusBadRequest {
		t.Fatalf("payment on a draft claim: status %d: %s", post.Status, string(post.Raw))
	}
}

func TestAddLinesToExistingPayment(t *testing.T) {
	f := newFixture(t)
	a := f.insuranceCharge(1, "0")
	b := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(a, b)

	created := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "150.00",
		line(claim.ID, claim.LineIDs[a], "100.00", nil),
	)).mustStatus(t, http.StatusCreated)

	if created.Str("payment.unallocated") != "50.00" {
		t.Fatalf("unallocated = %s, want 50.00", created.Str("payment.unallocated"))
	}

	id := created.Str("payment.id")

	// More than what is left on the payment is refused.
	f.biller.post(t, "/api/insurance-payments/"+id+"/allocations", map[string]any{
		"lines": []map[string]any{line(claim.ID, claim.LineIDs[b], "60.00", nil)},
	}).mustStatus(t, http.StatusBadRequest)

	res := f.biller.post(t, "/api/insurance-payments/"+id+"/allocations", map[string]any{
		"lines": []map[string]any{line(claim.ID, claim.LineIDs[b], "50.00", map[string]any{
			"adjustments": []map[string]any{{"type": "contractual_writeoff", "amount": "50.00"}},
		})},
	}).mustStatus(t, http.StatusOK)

	if res.Str("payment.unallocated") != "0.00" {
		t.Fatalf("unallocated = %s", res.Str("payment.unallocated"))
	}

	f.assertBalances(b, "0.00", "0.00")

	if got := f.claimStatus(claim.ID); got != "paid" {
		t.Fatalf("claim status = %q, want paid", got)
	}
}

func TestReopeningClaimWithPostedPaymentIsBlocked(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00",
		line(claim.ID, claim.LineIDs[charge], "100.00", nil),
	)).mustStatus(t, http.StatusCreated)

	res := f.biller.post(t, "/api/claims/"+claim.ID+"/start-resubmission", map[string]any{"resubmission_type": "new"})
	if res.Status != http.StatusConflict {
		t.Fatalf("start-resubmission with posted payment: %d %s", res.Status, string(res.Raw))
	}
}

func TestPatientBalanceViewIncludesTransferredResponsibility(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	claim := f.submittedClaim(charge)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "60.00",
		line(claim.ID, claim.LineIDs[charge], "60.00", map[string]any{
			"transfers": []map[string]any{{"reason": "copay", "amount": "25.00"}, {"reason": "noncovered", "amount": "15.00"}},
		}),
	)).mustStatus(t, http.StatusCreated)

	res := f.biller.get(t, "/api/charges/"+charge).mustStatus(t, http.StatusOK)

	if res.Str("balances.patient_responsibility") != "40.00" || res.Str("balances.insurance_responsibility") != "60.00" {
		t.Fatalf("responsibility after transfer: %s", string(res.Raw))
	}
	if res.Str("balances.patient_balance") != "40.00" || res.Str("balances.insurance_balance") != "0.00" {
		t.Fatalf("balances after transfer: %s", string(res.Raw))
	}
}
