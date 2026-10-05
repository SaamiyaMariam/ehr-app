package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

type multiPayer struct {
	*fixture
	payer2     string
	policy2    string
	charge     string
	primary    claimRef
	primaryPay response
}

// primaryAdjudicated builds a patient with primary and secondary policies and
// a $100 service whose primary claim the payer finally adjudicated: it paid
// `paid`, moved `toPatient` to the patient, and left the rest eligible.
func newMultiPayer(t *testing.T, paid, toPatient string) *multiPayer {
	t.Helper()

	f := newFixture(t)
	m := &multiPayer{fixture: f}

	m.payer2 = f.newPayer(unique("Secondary Payer "))
	m.policy2 = f.newPolicy(f.patientID, m.payer2, "secondary")
	m.charge = f.insuranceCharge(1, "0.00")
	m.primary = f.submittedClaim(m.charge)

	extra := map[string]any{"is_final": true}
	if toPatient != "" {
		extra["transfers"] = []map[string]any{{"reason": "deductible", "amount": toPatient}}
	}

	m.primaryPay = f.biller.post(t, "/api/insurance-payments", payment(f.payerID, paid,
		line(m.primary.ID, m.primary.LineIDs[m.charge], paid, extra),
	)).mustStatus(t, http.StatusCreated)

	return m
}

func (m *multiPayer) eligibility(claimID string) response {
	m.t.Helper()
	return m.biller.get(m.t, "/api/claims/"+claimID+"/next-sequence").mustStatus(m.t, http.StatusOK)
}

func (m *multiPayer) createNext(claimID string, body map[string]any) response {
	m.t.Helper()
	return m.biller.post(m.t, "/api/claims/"+claimID+"/next-sequence", body)
}

// markSubmitted lets a follow-on claim receive remittances (the submission
// workflow itself is covered by its own tests).
func markSubmitted(t *testing.T, claimID string) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `UPDATE claims SET status = 'submitted', submitted_at = NOW() WHERE id = $1`, claimID); err != nil {
		t.Fatal(err)
	}
}

func lineOf(res response) string { return res.Str("lines.0.id") }

func TestSecondaryEligibleAfterPartialPrimaryPayment(t *testing.T) {
	m := newMultiPayer(t, "50.00", "20.00") // pays 50, 20 to the patient, 30 left for the secondary

	// Explicit patient responsibility moved, not double-billed: only the 30 left is eligible.
	m.assertBalances(m.charge, "20.00", "30.00")

	ev := m.eligibility(m.primary.ID)
	if !ev.Bool("can_create") || ev.Str("next_sequence") != "secondary" || ev.Str("eligible_amount") != "30.00" || ev.Str("policy.id") != m.policy2 {
		t.Fatalf("expected eligible: %s", string(ev.Raw))
	}

	created := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)

	if created.Str("sequence") != "secondary" || created.Str("payer_id") != m.payer2 || created.Str("insurance_policy_id") != m.policy2 || created.Str("previous_claim_id") != m.primary.ID {
		t.Fatalf("secondary claim: %s", string(created.Raw))
	}
	if created.Str("total_billed") != "100.00" || created.Len("lines") != 1 {
		t.Fatalf("billed amount / lines: %s", string(created.Raw))
	}

	// The claim preserves what the primary decided, as a snapshot.
	adj := "snapshot.adjudication."
	if created.Str(adj+"previous_claim_number") != m.primary.Number || created.Str(adj+"total_eligible") != "30.00" {
		t.Fatalf("adjudication snapshot: %s", string(created.Raw))
	}
	for key, want := range map[string]string{
		"lines.0.billed": "100.00", "lines.0.previous_paid": "50.00", "lines.0.transferred_to_patient": "20.00",
		"lines.0.previous_adjustments": "0.00", "lines.0.eligible_amount": "30.00",
	} {
		if got := created.Str(adj + key); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if created.Str("snapshot.other_insurance.0.claim_number") != m.primary.Number || created.Str("snapshot.other_insurance.0.amount_paid") != "50.00" || created.Str("snapshot.other_insurance.0.sequence") != "primary" {
		t.Fatalf("other insurance: %s", string(created.Raw))
	}

	// Primary is now fully handled: its remainder lives on the secondary claim.
	if got := m.claimStatus(m.primary.ID); got != "paid" {
		t.Fatalf("primary claim = %q, want paid (forwarded)", got)
	}
	primary := m.biller.get(t, "/api/claims/"+m.primary.ID).mustStatus(t, http.StatusOK)
	if primary.Str("follow_up_claims.0.claim_number") != created.Str("claim_number") {
		t.Fatalf("primary does not link to its follow-on claim: %s", string(primary.Raw))
	}

	assertReconciled(t)
}

func TestSecondaryNotEligibleReasons(t *testing.T) {
	t.Run("primary fully paid", func(t *testing.T) {
		m := newMultiPayer(t, "100.00", "")
		ev := m.eligibility(m.primary.ID)
		if ev.Bool("can_create") || ev.Str("reason") != "No remaining insurance responsibility" {
			t.Fatalf("%s", string(ev.Raw))
		}
		m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusConflict)
	})

	t.Run("no secondary policy", func(t *testing.T) {
		f := newFixture(t)
		charge := f.insuranceCharge(1, "0.00")
		claim := f.submittedClaim(charge)
		f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "40.00", line(claim.ID, claim.LineIDs[charge], "40.00", nil))).mustStatus(t, http.StatusCreated)

		ev := f.biller.get(t, "/api/claims/"+claim.ID+"/next-sequence").mustStatus(t, http.StatusOK)
		if ev.Bool("can_create") || ev.Str("reason") == "" || !contains(ev.Str("reason"), "No secondary") {
			t.Fatalf("%s", string(ev.Raw))
		}
	})

	t.Run("secondary policy disabled", func(t *testing.T) {
		m := newMultiPayer(t, "40.00", "")
		m.biller.do(t, "PATCH", "/api/insurance-policies/"+m.policy2+"/status", map[string]any{"is_active": false}).mustStatus(t, http.StatusOK)

		ev := m.eligibility(m.primary.ID)
		if ev.Bool("can_create") || !contains(ev.Str("reason"), "No secondary") {
			t.Fatalf("%s", string(ev.Raw))
		}
	})

	t.Run("coverage ended before the service date", func(t *testing.T) {
		m := newMultiPayer(t, "40.00", "")
		m.biller.put(t, "/api/insurance-policies/"+m.policy2, map[string]any{
			"payer_id": m.payer2, "priority": "secondary", "member_id": "M2", "coverage_start": "2010-01-01", "coverage_end": "2015-12-31",
		}).mustStatus(t, http.StatusOK)

		ev := m.eligibility(m.primary.ID)
		if ev.Bool("can_create") || !contains(ev.Str("reason"), "Coverage inactive") {
			t.Fatalf("%s", string(ev.Raw))
		}
		m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusConflict)
	})

	t.Run("primary has not finished adjudicating", func(t *testing.T) {
		f := newFixture(t)
		f.payer2Policy(t)
		charge := f.insuranceCharge(1, "0.00")
		claim := f.submittedClaim(charge)
		f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "40.00",
			line(claim.ID, claim.LineIDs[charge], "40.00", map[string]any{"is_final": false}))).mustStatus(t, http.StatusCreated)

		ev := f.biller.get(t, "/api/claims/"+claim.ID+"/next-sequence").mustStatus(t, http.StatusOK)
		if ev.Bool("can_create") || !contains(ev.Str("reason"), "has not finished adjudicating") {
			t.Fatalf("%s", string(ev.Raw))
		}
	})

	t.Run("claim not yet submitted", func(t *testing.T) {
		f := newFixture(t)
		f.payer2Policy(t)
		charge := f.insuranceCharge(1, "0.00")
		draft := f.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated)

		ev := f.biller.get(t, "/api/claims/"+draft.Str("id")+"/next-sequence").mustStatus(t, http.StatusOK)
		if ev.Bool("can_create") || !contains(ev.Str("reason"), "not been adjudicated") {
			t.Fatalf("%s", string(ev.Raw))
		}
	})
}

func contains(s, part string) bool {
	return len(part) == 0 || (len(s) >= len(part) && indexOf(s, part) >= 0)
}

func indexOf(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}

	return -1
}

func (f *fixture) payer2Policy(t *testing.T) (payerID, policyID string) {
	t.Helper()

	payerID = f.newPayer(unique("Secondary Payer "))
	return payerID, f.newPolicy(f.patientID, payerID, "secondary")
}

func TestSecondaryDuplicateCreationIsBlocked(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	first := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)

	// A repeat finds nothing left to bill and names the existing claim.
	again := m.createNext(m.primary.ID, nil)
	if again.Status != http.StatusConflict || !contains(again.Error(), first.Str("claim_number")) {
		t.Fatalf("duplicate: %d %s", again.Status, string(again.Raw))
	}

	ev := m.eligibility(m.primary.ID)
	if ev.Bool("can_create") || ev.Str("existing_claims.0.claim_number") != first.Str("claim_number") {
		t.Fatalf("eligibility should show the existing claim: %s", string(ev.Raw))
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM claims WHERE previous_claim_id = $1 AND status <> 'voided'`, m.primary.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("%d active secondary claims", count)
	}
}

func TestSecondaryConcurrentCreationMakesExactlyOneClaim(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	const workers = 6

	var wg sync.WaitGroup
	statuses := make([]int, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = m.createNext(m.primary.ID, nil).Status
		}(i)
	}
	wg.Wait()

	created := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			created++
		} else if s != http.StatusConflict {
			t.Errorf("unexpected status %d", s)
		}
	}

	if created != 1 {
		t.Fatalf("exactly one concurrent request may create the claim, got %d (%v)", created, statuses)
	}
}

func TestSecondaryPaymentCannotExceedEligibleAndReducesOnlyItsSequence(t *testing.T) {
	m := newMultiPayer(t, "50.00", "20.00") // 30 eligible

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	markSubmitted(t, secondary.Str("id"))
	secondaryLine := lineOf(secondary)

	// More than the eligible amount is refused.
	m.biller.post(t, "/api/insurance-payments", payment(m.payer2, "30.01",
		line(secondary.Str("id"), secondaryLine, "30.01", nil))).mustStatus(t, http.StatusBadRequest)

	// The earlier payer cannot be paid again for a service already forwarded.
	late := m.biller.post(t, "/api/insurance-payments", payment(m.payerID, "5.00",
		line(m.primary.ID, m.primary.LineIDs[m.charge], "5.00", nil)))
	if late.Status != http.StatusBadRequest || !contains(late.Error(), "already forwarded") {
		t.Fatalf("payment against a forwarded primary line: %d %s", late.Status, string(late.Raw))
	}
	m.assertBalances(m.charge, "20.00", "30.00")

	// The right payer pays the eligible amount on the secondary claim.
	res := m.biller.post(t, "/api/insurance-payments", payment(m.payer2, "30.00",
		line(secondary.Str("id"), secondaryLine, "30.00", nil))).mustStatus(t, http.StatusCreated)
	if res.Str("claims.0.status") != "paid" {
		t.Fatalf("secondary claim should resolve: %s", string(res.Raw))
	}

	m.assertBalances(m.charge, "20.00", "0.00")

	// Each claim shows only its own payer's money.
	primary := m.biller.get(t, "/api/claims/"+m.primary.ID).mustStatus(t, http.StatusOK)
	sec := m.biller.get(t, "/api/claims/"+secondary.Str("id")).mustStatus(t, http.StatusOK)
	if primary.Str("lines.0.insurance_paid") != "50.00" || sec.Str("lines.0.insurance_paid") != "30.00" {
		t.Fatalf("primary paid %s, secondary paid %s", primary.Str("lines.0.insurance_paid"), sec.Str("lines.0.insurance_paid"))
	}
	if primary.Str("status") != "paid" || sec.Str("status") != "paid" {
		t.Fatalf("statuses: %s / %s", primary.Str("status"), sec.Str("status"))
	}

	// A payer cannot be paid for another payer's claim.
	other := m.newPayer(unique("Wrong Payer "))
	m.biller.post(t, "/api/insurance-payments", payment(other, "1.00", line(secondary.Str("id"), secondaryLine, "1.00", nil))).mustStatus(t, http.StatusBadRequest)

	assertReconciled(t)
}

func TestSecondarySnapshotSurvivesLaterPrimaryChanges(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	before := secondary.Str("snapshot.adjudication.total_eligible")

	// The primary payment cannot be voided while the secondary claim depends on it.
	paymentID := m.primaryPay.Str("payment.id")
	blocked := m.biller.post(t, "/api/insurance-payments/"+paymentID+"/void", map[string]any{"reason": "mistake"})
	if blocked.Status != http.StatusConflict || !contains(blocked.Error(), secondary.Str("claim_number")) {
		t.Fatalf("void with a dependent claim: %d %s", blocked.Status, string(blocked.Raw))
	}

	// The primary claim cannot be reopened either.
	m.biller.post(t, "/api/claims/"+m.primary.ID+"/start-resubmission", map[string]any{"resubmission_type": "new"}).mustStatus(t, http.StatusConflict)

	// A later write-off on the charge changes the live balance, not the snapshot.
	m.biller.post(t, "/api/charges/"+m.charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "payer_adjustment", "amount": "10.00", "reason": "late correction",
	}).mustStatus(t, http.StatusCreated)

	after := m.biller.get(t, "/api/claims/"+secondary.Str("id")).mustStatus(t, http.StatusOK)
	if after.Str("snapshot.adjudication.total_eligible") != before || before != "50.00" {
		t.Fatalf("snapshot changed: %s -> %s", before, after.Str("snapshot.adjudication.total_eligible"))
	}
	if after.Str("snapshot.adjudication.lines.0.previous_paid") != "50.00" {
		t.Fatalf("snapshot lines changed: %s", string(after.Raw))
	}

	// Refreshing the draft claim's data keeps the earlier payer's decision.
	refreshed := m.biller.put(t, "/api/claims/"+secondary.Str("id"), map[string]any{"refresh_snapshot": true, "resubmission_type": "new"})
	if refreshed.Status == http.StatusOK && refreshed.Str("snapshot.adjudication.total_eligible") != "50.00" {
		t.Fatalf("refresh dropped the adjudication snapshot: %s", string(refreshed.Raw))
	}
}

func TestSecondaryCancelRestoresPrimaryAndAllowsRecreate(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	if m.claimStatus(m.primary.ID) != "paid" {
		t.Fatal("primary should be paid (forwarded) while the secondary exists")
	}

	m.biller.post(t, "/api/claims/"+secondary.Str("id")+"/cancel", map[string]any{"reason": "wrong policy"}).mustStatus(t, http.StatusOK)

	// The remainder is no longer forwarded, so the primary reopens its balance.
	if got := m.claimStatus(m.primary.ID); got != "submitted" {
		t.Fatalf("primary after cancelling the secondary = %q", got)
	}

	again := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	if again.Str("id") == secondary.Str("id") {
		t.Fatal("a new claim should have been created")
	}

	assertReconciled(t)
}

func TestSecondaryServiceSelection(t *testing.T) {
	f := newFixture(t)
	payer2, _ := f.payer2Policy(t)
	_ = payer2

	a := f.insuranceCharge(1, "0.00") // primary pays in full
	b := f.insuranceCharge(1, "0.00") // primary leaves 60
	c := f.insuranceCharge(1, "0.00") // primary leaves 100 (denied)
	claim := f.submittedClaim(a, b, c)

	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "140.00",
		line(claim.ID, claim.LineIDs[a], "100.00", nil),
		line(claim.ID, claim.LineIDs[b], "40.00", nil),
		line(claim.ID, claim.LineIDs[c], "0", nil),
	)).mustStatus(t, http.StatusCreated)

	ev := f.biller.get(t, "/api/claims/"+claim.ID+"/next-sequence").mustStatus(t, http.StatusOK)
	if !ev.Bool("can_create") || ev.Str("eligible_amount") != "160.00" {
		t.Fatalf("eligibility: %s", string(ev.Raw))
	}

	eligible := 0
	lines, _ := ev.Get("lines").([]any)
	for _, l := range lines {
		m := l.(map[string]any)
		if m["eligible"] == true {
			eligible++
		} else if m["charge_id"] != a {
			t.Errorf("only the fully paid service should be ineligible: %v", m)
		}
	}
	if eligible != 2 {
		t.Fatalf("%d eligible lines, want 2", eligible)
	}

	// The fully paid service cannot be selected.
	f.biller.post(t, "/api/claims/"+claim.ID+"/next-sequence", map[string]any{"charge_ids": []string{a}}).mustStatus(t, http.StatusConflict)

	// Selecting a subset bills just that service.
	created := f.biller.post(t, "/api/claims/"+claim.ID+"/next-sequence", map[string]any{"charge_ids": []string{b}}).mustStatus(t, http.StatusCreated)
	if created.Len("lines") != 1 || created.Str("lines.0.charge_id") != b || created.Str("snapshot.adjudication.total_eligible") != "60.00" {
		t.Fatalf("subset claim: %s", string(created.Raw))
	}

	// The remaining eligible service can still be billed on its own claim.
	rest := f.biller.get(t, "/api/claims/"+claim.ID+"/next-sequence").mustStatus(t, http.StatusOK)
	if !rest.Bool("can_create") || rest.Str("eligible_amount") != "100.00" {
		t.Fatalf("remaining eligibility: %s", string(rest.Raw))
	}
}

func TestTertiaryAndQuaternarySequences(t *testing.T) {
	m := newMultiPayer(t, "40.00", "") // primary pays 40, 60 left
	payer3 := m.newPayer(unique("Tertiary Payer "))
	m.newPolicy(m.patientID, payer3, "tertiary")
	payer4 := m.newPayer(unique("Quaternary Payer "))
	m.newPolicy(m.patientID, payer4, "quaternary")

	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	markSubmitted(t, secondary.Str("id"))

	// Secondary finally adjudicates, paying 20: 40 stays eligible for the tertiary.
	m.biller.post(t, "/api/insurance-payments", payment(m.payer2, "20.00",
		line(secondary.Str("id"), lineOf(secondary), "20.00", nil))).mustStatus(t, http.StatusCreated)

	ev := m.eligibility(secondary.Str("id"))
	if !ev.Bool("can_create") || ev.Str("next_sequence") != "tertiary" || ev.Str("eligible_amount") != "40.00" || ev.Str("policy.payer_id") != payer3 {
		t.Fatalf("tertiary eligibility: %s", string(ev.Raw))
	}

	tertiary := m.createNext(secondary.Str("id"), nil).mustStatus(t, http.StatusCreated)
	if tertiary.Str("sequence") != "tertiary" || tertiary.Str("previous_claim_id") != secondary.Str("id") {
		t.Fatalf("tertiary claim: %s", string(tertiary.Raw))
	}
	// Both earlier payers are recorded, in order.
	if tertiary.Str("snapshot.other_insurance.0.sequence") != "primary" || tertiary.Str("snapshot.other_insurance.1.sequence") != "secondary" {
		t.Fatalf("other insurance on the tertiary claim: %s", string(tertiary.Raw))
	}
	if m.claimStatus(secondary.Str("id")) != "paid" {
		t.Fatal("secondary claim should be paid (forwarded) once the tertiary exists")
	}

	markSubmitted(t, tertiary.Str("id"))
	m.biller.post(t, "/api/insurance-payments", payment(payer3, "10.00",
		line(tertiary.Str("id"), lineOf(tertiary), "10.00", nil))).mustStatus(t, http.StatusCreated)

	quaternary := m.createNext(tertiary.Str("id"), nil).mustStatus(t, http.StatusCreated)
	if quaternary.Str("sequence") != "quaternary" || quaternary.Str("snapshot.adjudication.total_eligible") != "30.00" {
		t.Fatalf("quaternary claim: %s", string(quaternary.Raw))
	}

	// After the quaternary there is nowhere further to go.
	markSubmitted(t, quaternary.Str("id"))
	m.biller.post(t, "/api/insurance-payments", payment(payer4, "5.00",
		line(quaternary.Str("id"), lineOf(quaternary), "5.00", nil))).mustStatus(t, http.StatusCreated)
	last := m.eligibility(quaternary.Str("id"))
	if last.Bool("can_create") || last.Str("next_sequence") != "" || !contains(last.Str("reason"), "last insurance sequence") {
		t.Fatalf("after the quaternary: %s", string(last.Raw))
	}

	// Money only ever reduced the shared insurance balance once per payer.
	m.assertBalances(m.charge, "0.00", "25.00")
	assertReconciled(t)
}

func TestSecondaryAuthorization(t *testing.T) {
	m := newMultiPayer(t, "50.00", "")

	if res := (client{}).get(t, "/api/claims/"+m.primary.ID+"/next-sequence"); res.Status != http.StatusUnauthorized {
		t.Fatalf("eligibility without a token: %d", res.Status)
	}

	scheduler := newUser(t, "practice_scheduler")
	scheduler.get(t, "/api/claims/"+m.primary.ID+"/next-sequence").mustStatus(t, http.StatusOK)

	if res := scheduler.post(t, "/api/claims/"+m.primary.ID+"/next-sequence", nil); res.Status != http.StatusForbidden {
		t.Fatalf("creating as a scheduler: %d", res.Status)
	}
	if res := (client{}).post(t, "/api/claims/"+m.primary.ID+"/next-sequence", nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("creating without a token: %d", res.Status)
	}

	m.biller.get(t, "/api/claims/not-a-uuid/next-sequence").mustStatus(t, http.StatusNotFound)
	m.biller.get(t, "/api/claims/00000000-0000-4000-8000-000000000000/next-sequence").mustStatus(t, http.StatusNotFound)
	m.biller.post(t, "/api/claims/"+m.primary.ID+"/next-sequence", map[string]any{"insurance_policy_id": "nope"}).mustStatus(t, http.StatusBadRequest)
	m.biller.post(t, "/api/claims/"+m.primary.ID+"/next-sequence", map[string]any{"charge_ids": []string{"nope"}}).mustStatus(t, http.StatusBadRequest)

	// Another patient's policy cannot be substituted.
	otherPatient := m.newPatient()
	foreign := m.newPolicy(otherPatient, m.payer2, "secondary")
	res := m.createNext(m.primary.ID, map[string]any{"insurance_policy_id": foreign})
	if res.Status != http.StatusConflict {
		t.Fatalf("policy of another patient: %d %s", res.Status, string(res.Raw))
	}
}
