package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// claimFixture is a fixture whose data passes claim validation: practice and
// clinician billing profiles, a patient with address / sex, a self policy
// with signature on file, and a diagnosis.
type claimFixture struct {
	*fixture
	diagnosisID string
}

func newClaimFixture(t testing.TB) *claimFixture {
	t.Helper()
	f := newFixture(t)

	f.biller.put(t, "/api/billing/practice-profile", map[string]any{
		"practice_name": "Test Practice", "npi": "1234567893", "tax_id": "123456789", "tax_id_type": "ein",
		"address_1": "1 Main St", "city": "Springfield", "state": "IL", "zip": "62701", "phone": "5550100",
	}).mustStatus(t, http.StatusOK)

	f.biller.put(t, "/api/users/"+f.clinician.userID+"/billing-profile", map[string]any{
		"npi": "1234567893", "taxonomy_code": "103T00000X",
	}).mustStatus(t, http.StatusOK)

	// Replace the plain patient / policy with claim-ready ones.
	f.patientID = f.clinician.post(t, "/api/patients", map[string]any{
		"first_name": "Claim", "last_name": unique("Ready"), "date_of_birth": "1985-04-12", "administrative_sex": "female",
		"address_1": "5 Patient Way", "city": "Springfield", "state": "IL", "zip": "62701",
		"assigned_clinician_id": f.clinician.userID,
	}).mustStatus(t, http.StatusCreated).Str("id")

	f.policyID = f.newPolicyWith(f.patientID, f.payerID, "primary", map[string]any{
		"relationship_to_policy_holder": "self", "signature_on_file": true, "policy_group": "GRP1",
	})

	dx := f.biller.post(t, "/api/patients/"+f.patientID+"/diagnoses", map[string]any{
		"icd10_code": "F41.1", "description": "Generalized anxiety disorder", "is_primary": true,
	}).mustStatus(t, http.StatusCreated).Str("id")

	return &claimFixture{fixture: f, diagnosisID: dx}
}

func (c *claimFixture) charge(units int, patientShare, authorizationID string) string {
	c.t.Helper()

	body := map[string]any{
		"clinician_id": c.clinician.userID, "service_code_id": c.serviceID, "date_of_service": c.dos,
		"units": units, "place_of_service": "11", "insurance_policy_id": c.policyID, "diagnosis_ids": []string{c.diagnosisID},
	}
	if patientShare != "" {
		body["patient_responsibility"] = patientShare
	}
	if authorizationID != "" {
		body["prior_authorization_id"] = authorizationID
	}

	return c.biller.post(c.t, "/api/patients/"+c.patientID+"/charges", body).mustStatus(c.t, http.StatusCreated).Str("id")
}

func (c *claimFixture) authorization(uses int) string {
	c.t.Helper()

	return c.biller.post(c.t, "/api/insurance-policies/"+c.policyID+"/prior-authorizations", map[string]any{
		"authorization_code": unique("AUTH"), "applies_to_any_service_code": true,
		"start_date": "2020-01-01", "expiration_date": time.Now().AddDate(1, 0, 0).Format("2006-01-02"),
		"uses_allowed": uses, "uses_remaining": uses,
	}).mustStatus(c.t, http.StatusCreated).Str("id")
}

func (c *claimFixture) usesRemaining(authorizationID string) float64 {
	c.t.Helper()

	res := c.biller.get(c.t, "/api/prior-authorizations/"+authorizationID).mustStatus(c.t, http.StatusOK)
	v, _ := res.Get("uses_remaining").(float64)

	return v
}

// =========================================================
// PATIENT BILLING POLICY CREATION
// =========================================================

func TestPatientBillingPolicyCreation(t *testing.T) {
	f := newFixture(t)

	// Settings round-trip.
	f.biller.put(t, "/api/patients/"+f.patientID+"/billing-settings", map[string]any{"billing_comments": "Prefers email statements"}).mustStatus(t, http.StatusOK)
	if got := f.biller.get(t, "/api/patients/"+f.patientID+"/billing-settings").Str("billing_comments"); got != "Prefers email statements" {
		t.Fatalf("billing comments = %q", got)
	}

	// An incomplete policy may be saved (claim validation checks completeness later).
	partial := f.biller.post(t, "/api/patients/"+f.patientID+"/insurance-policies", map[string]any{"payer_id": f.payerID}).mustStatus(t, http.StatusCreated)
	if partial.Str("priority") != "primary" || !partial.Bool("is_active") {
		t.Fatalf("defaults: %s", string(partial.Raw))
	}

	// The same priority can be used more than once (history / future policies).
	f.newPolicy(f.patientID, f.payerID, "primary")

	list := f.biller.get(t, "/api/patients/"+f.patientID+"/insurance-policies").mustStatus(t, http.StatusOK)
	if arr, _ := list.JSON().([]any); len(arr) < 3 {
		t.Fatalf("policies listed: %d", len(arr))
	}

	bad := []map[string]any{
		{"payer_id": "not-a-uuid"},
		{"payer_id": f.payerID, "priority": "fifth"},
		{"payer_id": f.payerID, "coverage_start": "2026-02-01", "coverage_end": "2026-01-01"},
		{"payer_id": f.payerID, "copay": "12.345"},
		{"payer_id": f.payerID, "copay": "-5"},
		{"payer_id": f.payerID, "relationship_to_policy_holder": "neighbour"},
		{},
	}
	for i, body := range bad {
		if res := f.biller.post(t, "/api/patients/"+f.patientID+"/insurance-policies", body); res.Status != http.StatusBadRequest {
			t.Errorf("case %d: %d, want 400 (%s)", i, res.Status, string(res.Raw))
		}
	}

	// Another payer must exist.
	f.biller.post(t, "/api/patients/"+f.patientID+"/insurance-policies", map[string]any{"payer_id": nilUUID}).mustStatus(t, http.StatusBadRequest)

	// Disable / enable.
	id := partial.Str("id")
	f.biller.do(t, "PATCH", "/api/insurance-policies/"+id+"/status", map[string]any{"is_active": false}).mustStatus(t, http.StatusOK)
	if f.biller.get(t, "/api/insurance-policies/"+id).Bool("is_active") {
		t.Fatal("policy should be disabled")
	}

	// Writes need the biller role.
	scheduler := newUser(t, "practice_scheduler")
	if res := scheduler.post(t, "/api/patients/"+f.patientID+"/insurance-policies", map[string]any{"payer_id": f.payerID}); res.Status != http.StatusForbidden {
		t.Fatalf("scheduler creating a policy: %d", res.Status)
	}
}

// =========================================================
// PRIOR AUTHORIZATION CASCADE AND RELATIONSHIPS
// =========================================================

func TestPriorAuthorizationCascadeAndRelationships(t *testing.T) {
	c := newClaimFixture(t)
	pa := c.authorization(3)

	// Charges can reference the authorization of their own policy.
	charge := c.charge(1, "0.00", pa)
	if got := c.biller.get(t, "/api/charges/"+charge).Str("prior_authorization_id"); got != pa {
		t.Fatalf("charge authorization = %q", got)
	}

	// Another patient's policy / authorization cannot be substituted.
	otherPatient := c.newPatient()
	otherPolicy := c.newPolicy(otherPatient, c.payerID, "primary")
	otherPA := c.biller.post(t, "/api/insurance-policies/"+otherPolicy+"/prior-authorizations", map[string]any{
		"authorization_code": unique("OTHER"), "applies_to_any_service_code": true,
	}).mustStatus(t, http.StatusCreated).Str("id")

	bodyFor := func(policyID, authorizationID string) map[string]any {
		return map[string]any{
			"clinician_id": c.clinician.userID, "service_code_id": c.serviceID, "date_of_service": c.dos, "units": 1,
			"place_of_service": "11", "insurance_policy_id": policyID, "prior_authorization_id": authorizationID,
		}
	}
	c.biller.post(t, "/api/patients/"+c.patientID+"/charges", bodyFor(otherPolicy, "")).mustStatus(t, http.StatusBadRequest)
	c.biller.post(t, "/api/patients/"+c.patientID+"/charges", bodyFor(c.policyID, otherPA)).mustStatus(t, http.StatusBadRequest)

	// An authorization needs an existing policy; codes are validated.
	c.biller.post(t, "/api/insurance-policies/"+nilUUID+"/prior-authorizations", map[string]any{"authorization_code": "X", "applies_to_any_service_code": true}).mustStatus(t, http.StatusNotFound)
	c.biller.post(t, "/api/insurance-policies/"+c.policyID+"/prior-authorizations", map[string]any{"authorization_code": "X", "service_code_ids": []string{nilUUID}}).mustStatus(t, http.StatusBadRequest)

	// Disabling the policy cascades to its authorizations.
	res := c.biller.do(t, "PATCH", "/api/insurance-policies/"+c.policyID+"/status", map[string]any{"is_active": false}).mustStatus(t, http.StatusOK)
	if res.Get("prior_authorizations_disabled").(float64) < 1 {
		t.Fatalf("cascade did not report disabled authorizations: %s", string(res.Raw))
	}
	if c.biller.get(t, "/api/prior-authorizations/"+pa).Bool("is_active") {
		t.Fatal("authorization should have been disabled with its policy")
	}

	// It cannot be enabled while the policy is disabled; re-enabling the
	// policy does not silently re-enable it.
	if r := c.biller.do(t, "PATCH", "/api/prior-authorizations/"+pa+"/status", map[string]any{"is_active": true}); r.Status == http.StatusOK {
		t.Fatalf("authorization enabled under a disabled policy: %s", string(r.Raw))
	}
	c.biller.do(t, "PATCH", "/api/insurance-policies/"+c.policyID+"/status", map[string]any{"is_active": true}).mustStatus(t, http.StatusOK)
	if c.biller.get(t, "/api/prior-authorizations/"+pa).Bool("is_active") {
		t.Fatal("authorization must stay disabled until someone enables it")
	}
	c.biller.do(t, "PATCH", "/api/prior-authorizations/"+pa+"/status", map[string]any{"is_active": true}).mustStatus(t, http.StatusOK)
}

// =========================================================
// RATE RESOLUTION
// =========================================================

func TestRateResolution(t *testing.T) {
	f := newFixture(t) // standard rate $100.00, payer in network

	preview := func(body map[string]any) response {
		body["patient_id"], body["service_code_id"], body["clinician_id"], body["units"] = f.patientID, f.serviceID, f.clinician.userID, 1
		return f.biller.post(t, "/api/billing/rate-preview", body).mustStatus(t, http.StatusOK)
	}

	// Direct billing without a cash rate: the standard rate.
	direct := preview(map[string]any{"billing_method": "direct"})
	if direct.Str("rate_per_unit") != "100.00" || direct.Str("source") != "standard_rate" {
		t.Fatalf("direct: %s", string(direct.Raw))
	}

	// A patient cash rate beats the standard rate for direct billing only.
	f.biller.put(t, "/api/patients/"+f.patientID+"/cash-rates", map[string]any{"rates": []map[string]any{{"service_code_id": f.serviceID, "rate": "75.00"}}}).mustStatus(t, http.StatusOK)
	if got := preview(map[string]any{"billing_method": "direct"}); got.Str("rate_per_unit") != "75.00" || got.Str("source") != "patient_cash_rate" {
		t.Fatalf("cash rate: %s", string(got.Raw))
	}
	if got := preview(map[string]any{"insurance_policy_id": f.policyID}); got.Str("rate_per_unit") != "100.00" {
		t.Fatalf("insurance billing must ignore the patient's cash rate: %s", string(got.Raw))
	}

	// A single active payer schedule applies to insurance billing.
	schedule := f.biller.post(t, "/api/payers/"+f.payerID+"/rate-schedules", map[string]any{
		"name": unique("Contract "), "use_standard_practice_rates": false,
		"items": []map[string]any{{"service_code_id": f.serviceID, "custom_rate": "80.00"}},
	}).mustStatus(t, http.StatusCreated)
	if got := preview(map[string]any{"insurance_policy_id": f.policyID}); got.Str("rate_per_unit") != "80.00" || got.Str("source") != "payer_rate_schedule" {
		t.Fatalf("payer schedule: %s", string(got.Raw))
	}

	// A clinician-specific schedule outranks the payer's single schedule.
	second := f.biller.post(t, "/api/payers/"+f.payerID+"/rate-schedules", map[string]any{
		"name": unique("Senior "), "use_standard_practice_rates": false,
		"items": []map[string]any{{"service_code_id": f.serviceID, "custom_rate": "90.00"}},
	}).mustStatus(t, http.StatusCreated)
	f.biller.put(t, "/api/payers/"+f.payerID+"/clinician-rate-schedules", map[string]any{
		"assignments": []map[string]any{{"clinician_id": f.clinician.userID, "rate_schedule_id": second.Str("id")}},
	}).mustStatus(t, http.StatusOK)
	if got := preview(map[string]any{"insurance_policy_id": f.policyID}); got.Str("rate_per_unit") != "90.00" {
		t.Fatalf("clinician schedule: %s", string(got.Raw))
	}

	// A schedule of another payer cannot be assigned.
	otherPayer := f.newPayer(unique("Rates Other "))
	foreign := f.biller.post(t, "/api/payers/"+otherPayer+"/rate-schedules", map[string]any{"name": unique("Foreign "), "use_standard_practice_rates": true}).mustStatus(t, http.StatusCreated)
	f.biller.put(t, "/api/payers/"+f.payerID+"/clinician-rate-schedules", map[string]any{
		"assignments": []map[string]any{{"clinician_id": f.clinician.userID, "rate_schedule_id": foreign.Str("id")}},
	}).mustStatus(t, http.StatusBadRequest)

	// The charge snapshots the rate: changing rates later never alters it.
	charge := f.insuranceCharge(1, "0.00")
	before := f.biller.get(t, "/api/charges/"+charge).mustStatus(t, http.StatusOK)
	if before.Str("rate_per_unit") != "90.00" || before.Str("rate_source") != "payer_rate_schedule" || before.Str("rate_schedule_id") != second.Str("id") {
		t.Fatalf("charge snapshot: %s", string(before.Raw))
	}

	f.biller.put(t, "/api/service-codes/"+f.serviceID, map[string]any{"code": unique("SC"), "description": "Test service", "standard_rate": "500.00"}).mustStatus(t, http.StatusOK)
	f.biller.do(t, "PATCH", "/api/rate-schedules/"+second.Str("id")+"/status", map[string]any{"is_active": false}).mustStatus(t, http.StatusOK)
	_ = schedule

	after := f.biller.get(t, "/api/charges/"+charge).mustStatus(t, http.StatusOK)
	if after.Str("rate_per_unit") != "90.00" || after.Str("total_charge") != "90.00" {
		t.Fatalf("the charge's rate changed after rates were edited: %s", string(after.Raw))
	}

	// Invalid inputs.
	f.biller.post(t, "/api/billing/rate-preview", map[string]any{"patient_id": "x", "service_code_id": f.serviceID, "clinician_id": f.clinician.userID, "units": 1}).mustStatus(t, http.StatusBadRequest)
	f.biller.post(t, "/api/billing/rate-preview", map[string]any{"patient_id": f.patientID, "service_code_id": f.serviceID, "clinician_id": f.clinician.userID, "units": -1}).mustStatus(t, http.StatusBadRequest)
}

// =========================================================
// CHARGE CREATION
// =========================================================

func TestChargeCreationRules(t *testing.T) {
	f := newFixture(t)

	// Direct: patient owes everything.
	direct := f.biller.get(t, "/api/charges/"+f.directCharge(2)).mustStatus(t, http.StatusOK)
	if direct.Str("total_charge") != "200.00" || direct.Str("patient_responsibility") != "200.00" || direct.Str("insurance_responsibility") != "0.00" {
		t.Fatalf("direct charge: %s", string(direct.Raw))
	}

	// Insurance: default split is the policy copay, never an invented deductible.
	copay := f.newPolicyWith(f.patientID, f.payerID, "secondary", map[string]any{"copay": "25.00"})
	insured := f.biller.get(t, "/api/charges/"+f.chargeFor(f.patientID, copay, 1, "")).mustStatus(t, http.StatusOK)
	if insured.Str("patient_responsibility") != "25.00" || insured.Str("insurance_responsibility") != "75.00" {
		t.Fatalf("split: %s", string(insured.Raw))
	}

	base := map[string]any{
		"clinician_id": f.clinician.userID, "service_code_id": f.serviceID, "date_of_service": f.dos, "units": 1, "place_of_service": "11",
		"insurance_policy_id": f.policyID,
	}
	with := func(k string, v any) map[string]any {
		out := map[string]any{}
		for key, value := range base {
			out[key] = value
		}
		out[k] = v
		return out
	}

	bad := map[string]map[string]any{
		"zero units":            with("units", 0),
		"too many units":        with("units", 1000),
		"future date":           with("date_of_service", time.Now().AddDate(0, 0, 2).Format("2006-01-02")),
		"bad date":              with("date_of_service", "01/02/2026"),
		"bad modifier":          with("modifiers", []string{"abc"}),
		"five modifiers":        with("modifiers", []string{"AA", "BB", "CC", "DD", "EE"}),
		"bad place of service":  with("place_of_service", "X"),
		"patient share > total": with("patient_responsibility", "100.01"),
		"patient share cents":   with("patient_responsibility", "10.001"),
		"unknown clinician":     with("clinician_id", nilUUID),
		"a non-clinician":       with("clinician_id", f.biller.userID),
		"unknown service":       with("service_code_id", nilUUID),
		"unknown policy":        with("insurance_policy_id", nilUUID),
		"unknown diagnosis":     with("diagnosis_ids", []string{nilUUID}),
		"long notes":            with("notes", strings.Repeat("n", 2001)),
	}
	for name, body := range bad {
		if res := f.biller.post(t, "/api/patients/"+f.patientID+"/charges", body); res.Status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400 (%s)", name, res.Status, string(res.Raw))
		}
	}

	// Billers write charges; schedulers cannot.
	scheduler := newUser(t, "practice_scheduler")
	if res := scheduler.post(t, "/api/patients/"+f.patientID+"/charges", base); res.Status != http.StatusForbidden {
		t.Fatalf("scheduler: %d", res.Status)
	}

	// A charge is voided, never deleted, and voiding needs a reason.
	charge := f.directCharge(1)
	f.biller.post(t, "/api/charges/"+charge+"/void", map[string]any{"reason": ""}).mustStatus(t, http.StatusBadRequest)
	f.biller.post(t, "/api/charges/"+charge+"/void", map[string]any{"reason": "entered twice"}).mustStatus(t, http.StatusOK)
	f.biller.post(t, "/api/charges/"+charge+"/void", map[string]any{"reason": "again"}).mustStatus(t, http.StatusConflict)
	if got := f.biller.get(t, "/api/charges/"+charge).Str("display_status"); got != "voided" {
		t.Fatalf("display status = %s", got)
	}

	assertReconciled(t)
}

// =========================================================
// CLAIM CREATION, VALIDATION AND STATUS TRANSITIONS
// =========================================================

func TestClaimCreationValidationAndLifecycle(t *testing.T) {
	c := newClaimFixture(t)
	pa := c.authorization(5)
	charge := c.charge(1, "0.00", pa)

	claim := c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusCreated)
	id := claim.Str("id")

	// Complete data: the claim validates straight to ready.
	if claim.Str("status") != "ready" || claim.Len("validation.errors") != 0 || claim.Str("sequence") != "primary" {
		t.Fatalf("a complete claim should be ready: %s", string(claim.Raw))
	}

	// The same service cannot go on two primary claims.
	c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{charge}}).mustStatus(t, http.StatusConflict)

	// Missing data produces precise errors and blocks submission.
	other := c.charge(1, "0.00", "")
	if _, err := pool.Exec(context.Background(), `DELETE FROM billing_charge_diagnoses WHERE charge_id = $1`, other); err != nil {
		t.Fatal(err)
	}
	draft := c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{other}}).mustStatus(t, http.StatusCreated)
	if draft.Str("status") != "validation_error" || draft.Len("validation.errors") == 0 {
		t.Fatalf("a claim without diagnoses must fail validation: %s", string(draft.Raw))
	}
	if blocked := c.biller.post(t, "/api/claims/"+draft.Str("id")+"/mark-external", map[string]any{}); blocked.Status == http.StatusOK {
		t.Fatalf("an invalid claim was submitted: %s", string(blocked.Raw))
	}

	// Authorization usage is not consumed until the claim leaves the practice.
	if got := c.usesRemaining(pa); got != 5 {
		t.Fatalf("authorization used before submission: %v", got)
	}

	// Illegal transitions are refused (not yet mailed / not rejected).
	c.biller.post(t, "/api/claims/"+id+"/mark-mailed", map[string]any{}).mustStatus(t, http.StatusConflict)
	c.biller.post(t, "/api/claims/"+id+"/mark-rejection-reviewed", map[string]any{}).mustStatus(t, http.StatusConflict)
	c.biller.post(t, "/api/claims/"+id+"/record-rejection", map[string]any{"reason": "too early"}).mustStatus(t, http.StatusConflict)

	// Submit outside the app: status moves once, authorization is consumed once.
	sent := c.biller.post(t, "/api/claims/"+id+"/mark-external", map[string]any{"reference": "EXT-1"}).mustStatus(t, http.StatusOK)
	if sent.Str("status") != "externally_submitted" || c.usesRemaining(pa) != 4 {
		t.Fatalf("after submission: status %s, uses remaining %v", sent.Str("status"), c.usesRemaining(pa))
	}

	// Submitting again is a conflict and consumes nothing more.
	c.biller.post(t, "/api/claims/"+id+"/mark-external", map[string]any{"reference": "EXT-1"}).mustStatus(t, http.StatusConflict)
	if c.usesRemaining(pa) != 4 {
		t.Fatal("a repeated submission consumed the authorization again")
	}

	// Rejection -> review -> resubmission -> submit again: still one use.
	c.biller.post(t, "/api/claims/"+id+"/record-rejection", map[string]any{"reason": "member id mismatch"}).mustStatus(t, http.StatusOK)
	if got := c.claimStatus(id); got != "rejected_new" {
		t.Fatalf("status = %s", got)
	}
	c.biller.post(t, "/api/claims/"+id+"/mark-rejection-reviewed", map[string]any{"comment": "fixed member id"}).mustStatus(t, http.StatusOK)
	c.biller.post(t, "/api/claims/"+id+"/start-resubmission", map[string]any{"resubmission_type": "amended", "payer_claim_control_number": "PCN123"}).mustStatus(t, http.StatusOK)
	if got := c.claimStatus(id); got != "draft" {
		t.Fatalf("reopened claim = %s", got)
	}

	c.biller.post(t, "/api/claims/"+id+"/validate", map[string]any{}).mustStatus(t, http.StatusOK)
	c.biller.post(t, "/api/claims/"+id+"/mark-external", map[string]any{"reference": "EXT-2"}).mustStatus(t, http.StatusOK)

	if c.usesRemaining(pa) != 4 {
		t.Fatalf("resubmission consumed the authorization again: %v", c.usesRemaining(pa))
	}

	history := c.biller.get(t, "/api/claims/"+id).mustStatus(t, http.StatusOK)
	if history.Len("history") < 6 {
		t.Fatalf("claim history is missing events: %s", string(history.Raw))
	}

	// Only never-sent claims can be cancelled.
	c.biller.post(t, "/api/claims/"+id+"/cancel", map[string]any{"reason": "no"}).mustStatus(t, http.StatusConflict)
	c.biller.post(t, "/api/claims/"+draft.Str("id")+"/cancel", map[string]any{"reason": "bad data"}).mustStatus(t, http.StatusOK)

	scheduler := newUser(t, "practice_scheduler")
	if res := scheduler.post(t, "/api/claims/"+id+"/validate", map[string]any{}); res.Status != http.StatusForbidden {
		t.Fatalf("scheduler validating: %d", res.Status)
	}
}

func TestClaimRelationshipsCannotBeSubstituted(t *testing.T) {
	c := newClaimFixture(t)
	mine := c.charge(1, "0.00", "")

	// Another patient's service cannot ride along on this patient's claim.
	otherPatient := c.newPatient()
	otherPolicy := c.newPolicy(otherPatient, c.payerID, "primary")
	theirs := c.chargeFor(otherPatient, otherPolicy, 1, "0.00")

	if res := c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{mine, theirs}}); res.Status != http.StatusBadRequest {
		t.Fatalf("mixed-patient claim: %d %s", res.Status, string(res.Raw))
	}

	// A direct (self-pay) service is not claimable.
	direct := c.directCharge(1)
	c.biller.post(t, "/api/claims", map[string]any{"charge_ids": []string{direct}}).mustStatus(t, http.StatusBadRequest)

	// Claimable services are listed per patient only.
	list := c.biller.get(t, "/api/patients/"+otherPatient+"/claimable-charges").mustStatus(t, http.StatusOK)
	for _, item := range list.JSON().([]any) {
		if item.(map[string]any)["id"] == mine {
			t.Fatal("another patient's claimable list contains this patient's service")
		}
	}

	// Patient-payment allocations must target the paying patient's services.
	res := c.biller.post(t, "/api/patients/"+c.patientID+"/payments", map[string]any{
		"payment_date": c.dos, "amount": "10.00", "method": "cash", "idempotency_key": newKey(),
		"allocations": []map[string]any{{"charge_id": theirs, "amount": "10.00"}},
	})
	if res.Status != http.StatusBadRequest {
		t.Fatalf("allocating to another patient's service: %d %s", res.Status, string(res.Raw))
	}

	// Superbills only cover the patient's own services.
	sb := c.biller.post(t, "/api/patients/"+c.patientID+"/superbills", map[string]any{"charge_ids": []string{theirs}})
	if sb.Status == http.StatusCreated {
		t.Fatalf("superbill built from another patient's service: %s", string(sb.Raw))
	}

	// Diagnoses of another patient cannot be pointed at.
	otherDx := c.biller.post(t, "/api/patients/"+otherPatient+"/diagnoses", map[string]any{"icd10_code": "F32.9", "description": "Depression"}).mustStatus(t, http.StatusCreated).Str("id")
	c.biller.post(t, "/api/patients/"+c.patientID+"/charges", map[string]any{
		"clinician_id": c.clinician.userID, "service_code_id": c.serviceID, "date_of_service": c.dos, "units": 1, "place_of_service": "11",
		"insurance_policy_id": c.policyID, "diagnosis_ids": []string{otherDx},
	}).mustStatus(t, http.StatusBadRequest)

	assertReconciled(t)
}
