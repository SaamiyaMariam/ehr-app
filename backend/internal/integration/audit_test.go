package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Regression tests for the findings of the independent code audit.

func TestInsuranceActionsAreRefusedOnSelfPayServices(t *testing.T) {
	f := newFixture(t)
	charge := f.directCharge(1) // $100, no payer

	// A transfer to insurance would strand the money: no claim, no payer.
	res := f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
		"from_party": "patient", "to_party": "insurance", "amount": "40.00", "reason": "correction",
	})
	if res.Status != http.StatusBadRequest || !strings.Contains(res.Error(), "self-pay") {
		t.Fatalf("patient -> insurance on a self-pay service: %d %s", res.Status, string(res.Raw))
	}

	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "contractual_writeoff", "amount": "10.00", "reason": "x",
	}).mustStatus(t, http.StatusBadRequest)

	f.biller.post(t, "/api/charges/"+charge+"/transfers", map[string]any{
		"from_party": "insurance", "to_party": "patient", "amount": "10.00", "reason": "noncovered",
	}).mustStatus(t, http.StatusBadRequest)

	f.assertBalances(charge, "100.00", "0.00")

	// Patient-side actions still work.
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "patient", "adjustment_type": "courtesy_writeoff", "amount": "10.00", "reason": "courtesy",
	}).mustStatus(t, http.StatusCreated)
	f.assertBalances(charge, "90.00", "0.00")

	assertReconciled(t)
}

func TestStandaloneInsuranceActionsAreRefusedWhileAFollowOnClaimIsOpen(t *testing.T) {
	m := newMultiPayer(t, "50.00", "") // $50 forwarded to the next payer

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	markSubmitted(t, secondary.Str("id"))
	m.assertBalances(m.charge, "0.00", "50.00")

	// Billing the patient for what payer 2 still owes would bill it twice.
	res := m.biller.post(t, "/api/charges/"+m.charge+"/transfers", map[string]any{
		"from_party": "insurance", "to_party": "patient", "amount": "50.00", "reason": "noncovered",
	})
	if res.Status != http.StatusBadRequest || !strings.Contains(res.Error(), secondary.Str("claim_number")) {
		t.Fatalf("transfer while the follow-on claim is open: %d %s", res.Status, string(res.Raw))
	}

	m.biller.post(t, "/api/charges/"+m.charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "contractual_writeoff", "amount": "10.00", "reason": "contract",
	}).mustStatus(t, http.StatusBadRequest)

	m.assertBalances(m.charge, "0.00", "50.00")

	// Payer 2 can still be paid.
	m.biller.post(t, "/api/insurance-payments", payment(m.payer2, "50.00",
		line(secondary.Str("id"), lineOf(secondary), "50.00", nil))).mustStatus(t, http.StatusCreated)
	m.assertBalances(m.charge, "0.00", "0.00")

	assertReconciled(t)
}

func TestStandaloneInsuranceActionsWorkAgainOnceTheFollowOnClaimIsCancelled(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	m.biller.post(t, "/api/claims/"+secondary.Str("id")+"/cancel", map[string]any{"reason": "wrong policy"}).mustStatus(t, http.StatusOK)

	m.biller.post(t, "/api/charges/"+m.charge+"/transfers", map[string]any{
		"from_party": "insurance", "to_party": "patient", "amount": "50.00", "reason": "noncovered",
	}).mustStatus(t, http.StatusCreated)
	m.assertBalances(m.charge, "50.00", "0.00")

	assertReconciled(t)
}

func TestFollowOnSnapshotUsesTheAllowedAmountOnceNotPerPayment(t *testing.T) {
	f := newFixture(t)
	payer2 := f.newPayer(unique("Secondary Payer "))
	f.newPolicy(f.patientID, payer2, "secondary")
	charge := f.insuranceCharge(1, "0.00")
	claim := f.submittedClaim(charge)

	// The payer allowed $80 on this line and paid it in two instalments.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "30.00",
		line(claim.ID, claim.LineIDs[charge], "30.00", map[string]any{"allowed_amount": "80.00"}))).mustStatus(t, http.StatusCreated)
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "30.00",
		line(claim.ID, claim.LineIDs[charge], "30.00", map[string]any{"allowed_amount": "80.00", "is_final": true}))).mustStatus(t, http.StatusCreated)

	next := f.biller.post(t, "/api/claims/"+claim.ID+"/next-sequence", nil).mustStatus(t, http.StatusCreated)
	if got := next.Str("snapshot.adjudication.lines.0.previous_allowed"); got != "80.00" {
		t.Fatalf("previous allowed = %s, want 80.00 (the payer's allowed amount, not the sum of payments)", got)
	}
}

func TestDateRangeStatementShowsOverpaymentAsCreditNotAnError(t *testing.T) {
	f := newFixture(t)
	patient := f.newPatient()
	f.directChargeFor(patient, daysAgo(10), 1)

	// A deposit dated before the service it was applied to.
	f.biller.post(t, "/api/patients/"+patient+"/payments", map[string]any{
		"payment_date": daysAgo(20), "amount": "100.00", "method": "cash", "idempotency_key": newKey(), "auto_allocate": true,
	}).mustStatus(t, http.StatusCreated)

	s := f.biller.post(t, "/api/patients/"+patient+"/statements", map[string]any{
		"statement_type": "date_range", "start_date": daysAgo(25), "end_date": daysAgo(15),
	}).mustStatus(t, http.StatusCreated)

	if s.Str("balance_due") != "0.00" || s.Str("snapshot.credit_on_account") != "100.00" || s.Str("snapshot.amount_due") != "0.00" {
		t.Fatalf("balance %s credit %s due %s", s.Str("balance_due"), s.Str("snapshot.credit_on_account"), s.Str("snapshot.amount_due"))
	}
}

func TestRefundKeyCannotBeReusedForAnotherPayment(t *testing.T) {
	f := newFixture(t)
	first := f.patientPayment("30.00", false)
	second := f.patientPayment("30.00", false)

	key := newKey()
	body := func() map[string]any {
		return map[string]any{"amount": "10.00", "refund_date": f.dos, "method": "cash", "reason": "test", "idempotency_key": key}
	}

	f.biller.post(t, "/api/patient-payments/"+first.Str("payment.id")+"/refunds", body()).mustStatus(t, http.StatusOK)

	res := f.biller.post(t, "/api/patient-payments/"+second.Str("payment.id")+"/refunds", body())
	if res.Status != http.StatusConflict {
		t.Fatalf("a key from another payment's refund must not look like a successful retry: %d %s", res.Status, string(res.Raw))
	}

	if n := queryInt(t, `SELECT COUNT(*) FROM patient_payment_refunds WHERE payment_id = $1`, second.Str("payment.id")); n != 0 {
		t.Fatalf("%d refunds on the second payment", n)
	}
}

func TestSignInDoesNotRevealWhichAccountsAreDeactivated(t *testing.T) {
	u := newUser(t, "practice_biller")

	var email string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, u.userID).Scan(&email); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(context.Background(), `UPDATE users SET is_active = FALSE WHERE id = $1`, u.userID); err != nil {
		t.Fatal(err)
	}

	login := func(address, password string) response {
		return (client{}).post(t, "/api/auth/login", map[string]string{"email": address, "password": password})
	}

	wrong := login(email, "not-the-password").mustStatus(t, http.StatusUnauthorized)
	unknown := login("nobody-"+newKey()+"@example.test", "not-the-password").mustStatus(t, http.StatusUnauthorized)

	if wrong.Error() != unknown.Error() || strings.Contains(wrong.Error(), "inactive") {
		t.Fatalf("a deactivated account is distinguishable without the password: %q vs %q", wrong.Error(), unknown.Error())
	}

	// Oversized input is rejected the same way, without being remembered.
	login(strings.Repeat("a", 5000)+"@example.test", "x").mustStatus(t, http.StatusUnauthorized)
	login(email, strings.Repeat("p", 100)).mustStatus(t, http.StatusUnauthorized)
}

func TestRoleListWithRepeatedKeysIsAccepted(t *testing.T) {
	admin := adminUser(t)
	user := newUser(t)

	admin.put(t, "/api/users/"+user.userID+"/roles", map[string]any{
		"roles": []string{"practice_scheduler", "practice_scheduler"},
	}).mustStatus(t, http.StatusOK)
}
