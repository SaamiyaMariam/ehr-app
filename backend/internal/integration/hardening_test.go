package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// parallel runs fn n times at once and returns each call's status.
func parallel(n int, fn func(i int) int) []int {
	var wg sync.WaitGroup
	statuses := make([]int, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = fn(i)
		}(i)
	}
	wg.Wait()

	return statuses
}

func countStatus(statuses []int, want int) int {
	n := 0
	for _, s := range statuses {
		if s == want {
			n++
		}
	}

	return n
}

func queryInt(t testing.TB, sql string, args ...any) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n
}

// =========================================================
// CONCURRENCY
// =========================================================

func TestConcurrentPatientPaymentsNeverOverApplyABalance(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1) // $100 owed

	statuses := parallel(6, func(int) int {
		return f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
			"payment_date": f.dos, "amount": "100.00", "method": "cash", "idempotency_key": newKey(), "auto_allocate": true,
		}).Status
	})

	if countStatus(statuses, http.StatusCreated) != 6 {
		t.Fatalf("every payment is real money and must post: %v", statuses)
	}

	// Together they apply exactly the balance, no more; the rest is credit.
	applied := queryInt(t, `SELECT (COALESCE(SUM(amount), 0) * 100)::int FROM patient_payment_allocations WHERE charge_id = $1 AND status = 'active'`, charge)
	if applied != 10000 {
		t.Fatalf("applied %d cents to a $100 balance", applied)
	}

	f.assertBalances(charge, "0.00", "0.00")
	f.assertSummary(summary{"0.00", "0.00", "0.00", "500.00"})
}

func TestConcurrentClaimSubmissionHappensOnce(t *testing.T) {
	c := newClaimFixture(t)
	pa := c.authorization(3)
	charge := c.charge(1, "0.00", pa)
	claim := c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated)

	statuses := parallel(6, func(int) int {
		return c.biller.post(t, "/api/claims/"+claim.Str("id")+"/mark-external", map[string]any{"reference": "EXT-RACE"}).Status
	})

	if countStatus(statuses, http.StatusOK) != 1 {
		t.Fatalf("exactly one submission may succeed: %v", statuses)
	}

	if n := queryInt(t, `SELECT COUNT(*) FROM claim_submissions WHERE claim_id = $1`, claim.Str("id")); n != 1 {
		t.Fatalf("%d submission rows", n)
	}

	if got := c.usesRemaining(pa); got != 2 {
		t.Fatalf("authorization uses remaining = %v, want 2 (consumed once)", got)
	}
}

func TestConcurrentAuthorizationUseIsNotSpentTwice(t *testing.T) {
	c := newClaimFixture(t)
	pa := c.authorization(1) // a single use left

	var claims []string
	for i := 0; i < 2; i++ {
		charge := c.charge(1, "0.00", pa)
		claims = append(claims, c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated).Str("id"))
	}

	statuses := parallel(2, func(i int) int {
		return c.biller.post(t, "/api/claims/"+claims[i]+"/mark-external", map[string]any{}).Status
	})

	if countStatus(statuses, http.StatusOK) != 1 {
		t.Fatalf("only one claim may spend the last use: %v", statuses)
	}

	if got := c.usesRemaining(pa); got != 0 {
		t.Fatalf("uses remaining = %v, want 0 and never negative", got)
	}
}

func TestConcurrentResponsibilityTransfersCannotMoveTheSameBalanceTwice(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0.00") // insurance owes $100

	statuses := parallel(6, func(int) int {
		return f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
			"from_party": "insurance", "to_party": "patient", "amount": "100.00", "reason": "noncovered",
		}).Status
	})

	if countStatus(statuses, http.StatusCreated) != 1 {
		t.Fatalf("exactly one transfer of the full balance may succeed: %v", statuses)
	}

	f.assertBalances(charge, "100.00", "0.00")
	assertReconciled(t)
}

func TestConcurrentRefundsCannotRefundTheSameCreditTwice(t *testing.T) {
	f := newFixture(t)

	paid := f.patientPayment("100.00", false)
	paymentID := paid.Str("payment.id")

	statuses := parallel(6, func(int) int {
		return f.biller.post(t, "/api/patient-payments/"+paymentID+"/refunds", map[string]any{
			"amount": "100.00", "refund_date": f.dos, "method": "cash", "reason": "race", "idempotency_key": newKey(),
		}).Status
	})

	if countStatus(statuses, http.StatusOK) != 1 {
		t.Fatalf("exactly one full refund may succeed: %v", statuses)
	}

	if n := queryInt(t, `SELECT COUNT(*) FROM patient_payment_refunds WHERE payment_id = $1`, paymentID); n != 1 {
		t.Fatalf("%d refunds recorded", n)
	}

	f.assertSummary(summary{"0.00", "0.00", "0.00", "0.00"})
}

func TestConcurrentVoidsHappenOnce(t *testing.T) {
	f := newFixture(t)

	patientPaid := f.patientPayment("50.00", false)
	statuses := parallel(5, func(int) int {
		return f.biller.post(t, "/api/patient-payments/"+patientPaid.Str("payment.id")+"/void", map[string]any{"reason": "race"}).Status
	})
	if countStatus(statuses, http.StatusOK) != 1 || countStatus(statuses, http.StatusConflict) != 4 {
		t.Fatalf("patient payment voids: %v", statuses)
	}

	charge := f.insuranceCharge(1, "0.00")
	claim := f.submittedClaim(charge)
	ins := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00", line(claim.ID, claim.LineIDs[charge], "100.00", nil))).mustStatus(t, http.StatusCreated)

	statuses = parallel(5, func(int) int {
		return f.biller.post(t, "/api/insurance-payments/"+ins.Str("payment.id")+"/void", map[string]any{"reason": "race"}).Status
	})
	if countStatus(statuses, http.StatusOK) != 1 || countStatus(statuses, http.StatusConflict) != 4 {
		t.Fatalf("insurance payment voids: %v", statuses)
	}

	f.assertBalances(charge, "0.00", "100.00")
	assertReconciled(t)
}

func TestConcurrentAllocationsCannotExceedAnInsurancePayment(t *testing.T) {
	f := newFixture(t)

	const lines = 4

	charges := make([]string, lines)
	for i := range charges {
		charges[i] = f.insuranceCharge(1, "0.00") // each owes $100 on insurance
	}
	claim := f.submittedClaim(charges...)

	// A $100 remittance created first, then allocated to four lines at once.
	created := f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00")).mustStatus(t, http.StatusCreated)
	id := created.Str("payment.id")

	statuses := parallel(lines, func(i int) int {
		return f.biller.post(t, "/api/insurance-payments/"+id+"/allocations", map[string]any{
			"lines": []map[string]any{line(claim.ID, claim.LineIDs[charges[i]], "100.00", map[string]any{"is_final": false})},
		}).Status
	})

	if countStatus(statuses, http.StatusOK) != 1 {
		t.Fatalf("a $100 payment can fund exactly one $100 line: %v", statuses)
	}

	if n := queryInt(t, `SELECT (COALESCE(SUM(amount_paid), 0) * 100)::int FROM insurance_payment_allocations WHERE payment_id = $1 AND status = 'active'`, id); n != 10000 {
		t.Fatalf("allocated %d cents on a $100 payment", n)
	}

	assertReconciled(t)
}

// =========================================================
// IDEMPOTENCY
// =========================================================

func TestRepeatedRequestsDoNotRepeatFinancialEffects(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1)

	key := newKey()
	body := map[string]any{"payment_date": f.dos, "amount": "40.00", "method": "cash", "idempotency_key": key, "auto_allocate": true}

	first := f.biller.post(t, "/api/patients/"+f.patientID+"/payments", body).mustStatus(t, http.StatusCreated)
	again := f.biller.post(t, "/api/patients/"+f.patientID+"/payments", body).mustStatus(t, http.StatusOK)

	if !again.Bool("duplicate") || again.Str("payment.id") != first.Str("payment.id") {
		t.Fatalf("a repeated payment request must return the original: %s", string(again.Raw))
	}
	f.assertBalances(charge, "60.00", "0.00")

	// A key cannot be reused for a different patient.
	other := f.newPatient()
	if res := f.biller.post(t, "/api/patients/"+other+"/payments", body); res.Status != http.StatusConflict {
		t.Fatalf("key reused for another patient: %d", res.Status)
	}

	// A repeated refund with the same key refunds once.
	credit := f.patientPayment("30.00", false)
	refund := map[string]any{"amount": "30.00", "refund_date": f.dos, "method": "cash", "reason": "retry", "idempotency_key": newKey()}
	f.biller.post(t, "/api/patient-payments/"+credit.Str("payment.id")+"/refunds", refund).mustStatus(t, http.StatusOK)
	f.biller.post(t, "/api/patient-payments/"+credit.Str("payment.id")+"/refunds", refund).mustStatus(t, http.StatusOK)

	if n := queryInt(t, `SELECT COUNT(*) FROM patient_payment_refunds WHERE payment_id = $1`, credit.Str("payment.id")); n != 1 {
		t.Fatalf("%d refunds after a retried request", n)
	}

	// Voiding twice is a conflict, not a second reversal.
	f.biller.post(t, "/api/patient-payments/"+first.Str("payment.id")+"/void", map[string]any{"reason": "oops"}).mustStatus(t, http.StatusOK)
	f.biller.post(t, "/api/patient-payments/"+first.Str("payment.id")+"/void", map[string]any{"reason": "oops"}).mustStatus(t, http.StatusConflict)
	f.assertBalances(charge, "100.00", "0.00")

	// Keys must be UUIDs.
	f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
		"payment_date": f.dos, "amount": "1.00", "method": "cash", "idempotency_key": "not-a-key",
	}).mustStatus(t, http.StatusBadRequest)
}

// =========================================================
// INPUT VALIDATION SWEEP
// =========================================================

func TestFinancialEndpointsRejectMalformedInput(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1)

	payments := []map[string]any{
		{"payment_date": f.dos, "amount": "-5", "method": "cash"},
		{"payment_date": f.dos, "amount": "5.001", "method": "cash"},
		{"payment_date": f.dos, "amount": "abc", "method": "cash"},
		{"payment_date": f.dos, "amount": "0", "method": "cash"},
		{"payment_date": "2999-01-01", "amount": "5", "method": "cash"},
		{"payment_date": "not-a-date", "amount": "5", "method": "cash"},
		{"payment_date": f.dos, "amount": "5", "method": "bitcoin"},
		{"payment_date": f.dos, "amount": "5", "method": "check"},
		{"payment_date": f.dos, "amount": "5", "method": "external_card", "reference_number": "4111 1111 1111 1111"},
		{"payment_date": f.dos, "amount": "99999999999", "method": "cash"},
		{"payment_date": f.dos, "amount": "5", "method": "cash", "auto_allocate": true, "allocations": []map[string]any{{"charge_id": charge, "amount": "1"}}},
	}
	for i, p := range payments {
		p["idempotency_key"] = newKey()
		if res := f.biller.post(t, "/api/patients/"+f.patientID+"/payments", p); res.Status != http.StatusBadRequest {
			t.Errorf("payment case %d: %d, want 400 (%s)", i, res.Status, string(res.Raw))
		}
	}

	// Oversized lists are refused.
	many := make([]map[string]any, 201)
	for i := range many {
		many[i] = map[string]any{"charge_id": charge, "amount": "0.01"}
	}
	f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
		"payment_date": f.dos, "amount": "5", "method": "cash", "idempotency_key": newKey(), "allocations": many,
	}).mustStatus(t, http.StatusBadRequest)

	// Pagination bounds and malformed ids never 500.
	for _, path := range []string{
		"/api/billing/transactions?page=-3&page_size=0", "/api/billing/transactions?page=abc",
		"/api/claims?page_size=99999", "/api/insurance-payments?page=0", "/api/patients/xyz/ledger",
		"/api/statements/xyz", "/api/charges/xyz", "/api/claims/xyz", "/api/insurance-payments/xyz",
		"/api/billing/reports/collections?from=a&to=b", "/api/billing/outstanding-insurance?page=-1",
	} {
		if res := f.biller.get(t, path); res.Status >= 500 {
			t.Errorf("%s: %d %s", path, res.Status, string(res.Raw))
		}
	}

	// Comments and notes are bounded.
	claim := f.submittedClaimFor(t, f.insuranceCharge(1, "0.00"))
	f.biller.post(t, "/api/claims/"+claim+"/comments", map[string]any{"comment": string(make([]byte, 2001))}).mustStatus(t, http.StatusBadRequest)
}

// submittedClaimFor creates a claim for the charge and returns its id.
func (f *fixture) submittedClaimFor(t testing.TB, charge string) string {
	t.Helper()
	return f.submittedClaim(charge).ID
}

// Paper claims: generating the CMS-1500 again prints another copy but never
// repeats the state change or consumes the authorization a second time.
func TestPaperClaimGenerationAndMailingAreIdempotent(t *testing.T) {
	c := newClaimFixture(t)

	// Bill this payer on paper before the charge is created.
	c.biller.put(t, "/api/payers/"+c.payerID, map[string]any{
		"payer_name": c.payerName, "billing_method": "paper", "in_network": true, "insurance_type": "group_health_plan",
		"address_1": "1 Payer Plaza", "city": "Springfield", "state": "IL", "zip": "62701",
	}).mustStatus(t, http.StatusOK)

	pa := c.authorization(3)
	charge := c.charge(1, "0.00", pa)
	claim := c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated)
	id := claim.Str("id")

	if claim.Str("submission_method") != "paper" || claim.Str("status") != "ready" {
		t.Fatalf("paper claim: %s", string(claim.Raw))
	}

	first := c.biller.post(t, "/api/claims/"+id+"/cms1500", map[string]any{}).mustStatus(t, http.StatusCreated)
	if first.Str("claim.status") != "paper_generated" || c.usesRemaining(pa) != 2 {
		t.Fatalf("after the first generation: %s, uses %v", first.Str("claim.status"), c.usesRemaining(pa))
	}

	// Generating again: another copy of the document, no second state change or use.
	c.biller.post(t, "/api/claims/"+id+"/cms1500", map[string]any{}).mustStatus(t, http.StatusCreated)
	if got := c.claimStatus(id); got != "paper_generated" || c.usesRemaining(pa) != 2 {
		t.Fatalf("after a repeat: %s, uses %v", got, c.usesRemaining(pa))
	}
	if n := queryInt(t, `SELECT COUNT(*) FROM claim_documents WHERE claim_id = $1`, id); n != 2 {
		t.Fatalf("%d document versions", n)
	}
	if n := queryInt(t, `SELECT COUNT(*) FROM claim_history WHERE claim_id = $1 AND event_type = 'cms1500_generated'`, id); n != 1 {
		t.Fatalf("the state change was recorded %d times", n)
	}

	// Marking mailed happens once.
	c.biller.post(t, "/api/claims/"+id+"/mark-mailed", map[string]any{}).mustStatus(t, http.StatusOK)
	c.biller.post(t, "/api/claims/"+id+"/mark-mailed", map[string]any{}).mustStatus(t, http.StatusConflict)
	if n := queryInt(t, `SELECT COUNT(*) FROM claim_submissions WHERE claim_id = $1`, id); n != 1 {
		t.Fatalf("%d submission rows", n)
	}
	if c.usesRemaining(pa) != 2 {
		t.Fatal("mailing consumed the authorization again")
	}
}
