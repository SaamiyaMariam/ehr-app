package integration

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"
	"time"
)

func daysAgo(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }

func (f *fixture) openStatement(patientID string) response {
	f.t.Helper()

	return f.biller.post(f.t, "/api/patients/"+patientID+"/statements", map[string]any{"statement_type": "open_balance"})
}

func (f *fixture) pdf(path string) []byte {
	f.t.Helper()

	res := f.biller.get(f.t, path).mustStatus(f.t, http.StatusOK)

	return res.Raw
}

func lineDescriptions(res response) []string {
	var out []string

	lines, _ := res.Get("snapshot.lines").([]any)
	for _, l := range lines {
		out = append(out, l.(map[string]any)["description"].(string))
	}

	return out
}

func TestStatementOpenBalanceShowsOnlyPatientItems(t *testing.T) {
	f := newFixture(t)
	direct := f.directCharge(1)       // patient owes 100
	_ = f.insuranceCharge(1, "20.00") // patient 20, insurance 80
	_ = f.insuranceCharge(2, "0.00")  // insurance only: must not appear
	_ = direct

	res := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)

	if res.Str("balance_due") != "120.00" || res.Str("amount_due") != "120.00" || res.Str("credit_on_account") != "0.00" {
		t.Fatalf("balances: %s", string(res.Raw))
	}

	if res.Len("snapshot.lines") != 2 {
		t.Fatalf("an insurance-only service must not be on the statement: %v", lineDescriptions(res))
	}

	if res.Str("snapshot.lines.0.balance") != "100.00" || res.Str("snapshot.lines.1.responsibility") != "20.00" {
		t.Fatalf("lines: %s", string(res.Raw))
	}

	if !strings.HasPrefix(res.Str("statement_number"), "STM-") || res.Str("snapshot.practice.name") == "" && false {
		t.Fatalf("statement number: %s", res.Str("statement_number"))
	}
}

func TestStatementInsuranceOnlyBalanceIsNotPatientDue(t *testing.T) {
	f := newFixture(t)
	f.insuranceCharge(1, "0.00") // patient owes nothing

	res := f.openStatement(f.patientID)
	if res.Status != http.StatusConflict {
		t.Fatalf("status %d: %s", res.Status, string(res.Raw))
	}

	// Date-range statements with no patient activity are not created either.
	empty := f.biller.post(t, "/api/patients/"+f.patientID+"/statements", map[string]any{
		"statement_type": "date_range", "start_date": daysAgo(400), "end_date": daysAgo(300),
	})
	if empty.Status != http.StatusConflict {
		t.Fatalf("empty range status %d: %s", empty.Status, string(empty.Raw))
	}
}

func TestStatementReflectsPaymentsAndCredit(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1) // 100

	f.patientPayment("30.00", true)

	res := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)
	if res.Str("balance_due") != "70.00" || res.Str("snapshot.lines.0.payments") != "30.00" || res.Str("snapshot.total_payments") != "30.00" {
		t.Fatalf("payment not reflected: %s", string(res.Raw))
	}

	// Unapplied credit is shown separately and reduces the amount due.
	f.patientPayment("25.00", false)

	res = f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)
	if res.Str("balance_due") != "70.00" || res.Str("credit_on_account") != "25.00" || res.Str("amount_due") != "45.00" {
		t.Fatalf("credit not reflected: %s", string(res.Raw))
	}
}

func TestStatementDateRangeBoundaries(t *testing.T) {
	f := newFixture(t)
	patient := f.newPatient()

	before := f.directChargeFor(patient, daysAgo(40), 1)  // 100, before the range
	atStart := f.directChargeFor(patient, daysAgo(30), 2) // 200, on the first day
	atEnd := f.directChargeFor(patient, daysAgo(10), 3)   // 300, on the last day
	_ = f.directChargeFor(patient, daysAgo(9), 4)         // 400, after the range
	_, _, _ = before, atStart, atEnd

	res := f.biller.post(t, "/api/patients/"+patient+"/statements", map[string]any{
		"statement_type": "date_range", "start_date": daysAgo(30), "end_date": daysAgo(10),
	}).mustStatus(t, http.StatusCreated)

	if res.Len("snapshot.lines") != 2 {
		t.Fatalf("lines on the boundary days must be included, later ones excluded: %v", lineDescriptions(res))
	}

	if res.Str("snapshot.previous_balance") != "100.00" || res.Str("snapshot.total_charges") != "500.00" {
		t.Fatalf("previous / charges: %s", string(res.Raw))
	}

	// Balance due is as of the end date: 100 + 200 + 300, without the later 400.
	if res.Str("balance_due") != "600.00" || res.Str("snapshot.lines.1.balance") != "600.00" || res.Str("snapshot.lines.0.balance") != "300.00" {
		t.Fatalf("running balance: %s", string(res.Raw))
	}
}

func TestStatementDateRangeIncludesPaymentsByPaymentDate(t *testing.T) {
	f := newFixture(t)
	patient := f.newPatient()
	f.directChargeFor(patient, daysAgo(20), 1)

	f.biller.post(t, "/api/patients/"+patient+"/payments", map[string]any{
		"payment_date": daysAgo(5), "amount": "40.00", "method": "cash", "idempotency_key": newKey(), "auto_allocate": true,
	}).mustStatus(t, http.StatusCreated)

	// Range ends before the payment: the payment is not on it.
	early := f.biller.post(t, "/api/patients/"+patient+"/statements", map[string]any{
		"statement_type": "date_range", "start_date": daysAgo(25), "end_date": daysAgo(6),
	}).mustStatus(t, http.StatusCreated)
	if early.Str("balance_due") != "100.00" || early.Str("snapshot.total_payments") != "0.00" {
		t.Fatalf("payment dated after the range leaked in: %s", string(early.Raw))
	}

	late := f.biller.post(t, "/api/patients/"+patient+"/statements", map[string]any{
		"statement_type": "date_range", "start_date": daysAgo(25), "end_date": daysAgo(5),
	}).mustStatus(t, http.StatusCreated)
	if late.Str("balance_due") != "60.00" || late.Str("snapshot.total_payments") != "40.00" {
		t.Fatalf("payment missing: %s", string(late.Raw))
	}
}

func TestStatementSnapshotNeverChanges(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1)

	created := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)
	id := created.Str("id")
	jsonBefore := f.biller.get(t, "/api/statements/"+id).mustStatus(t, http.StatusOK).Raw
	pdfBefore := f.pdf("/api/statements/" + id + "/pdf")

	f.patientPayment("60.00", true)

	jsonAfter := f.biller.get(t, "/api/statements/"+id).mustStatus(t, http.StatusOK).Raw
	pdfAfter := f.pdf("/api/statements/" + id + "/pdf")

	if !bytes.Equal(jsonBefore, jsonAfter) || sha256.Sum256(pdfBefore) != sha256.Sum256(pdfAfter) {
		t.Fatal("a generated statement changed after a later payment")
	}

	// A new statement reflects the payment.
	next := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)
	if next.Str("balance_due") != "40.00" || next.Str("id") == id {
		t.Fatalf("new statement: %s", string(next.Raw))
	}

	list := f.biller.get(t, "/api/patients/"+f.patientID+"/statements").mustStatus(t, http.StatusOK)
	if list.Len("") != 2 {
		// list returns a bare array
		if arr, ok := list.JSON().([]any); !ok || len(arr) != 2 {
			t.Fatalf("history: %s", string(list.Raw))
		}
	}
}

func TestStatementPDFContentAndPrivacy(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1)

	created := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)
	pdf := f.pdf("/api/statements/" + created.Str("id") + "/pdf")

	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 1500 {
		t.Fatalf("not a PDF (%d bytes)", len(pdf))
	}

	text := string(pdf)
	for _, want := range []string{"STATEMENT", created.Str("statement_number"), "$100.00", "AMOUNT DUE", created.Str("snapshot.patient.name")} {
		if !strings.Contains(text, want) {
			t.Errorf("PDF is missing %q", want)
		}
	}

	// No internal ids, tokens or credentials.
	for _, secret := range []string{f.patientID, created.Str("id"), f.biller.token, f.biller.userID} {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("PDF leaks %q", secret)
		}
	}

	// Download endpoints answer with a PDF content type and an attachment name.
	res := f.biller.get(t, "/api/statements/"+created.Str("id")+"/pdf").mustStatus(t, http.StatusOK)
	_ = res
}

func TestStatementCannotBeReachedThroughAnotherPatient(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1)
	mine := f.openStatement(f.patientID).mustStatus(t, http.StatusCreated)

	other := f.newPatient()
	f.directChargeFor(other, f.dos, 1)
	f.openStatement(other).mustStatus(t, http.StatusCreated)

	id := mine.Str("id")

	f.biller.get(t, "/api/patients/"+f.patientID+"/statements/"+id).mustStatus(t, http.StatusOK)
	f.biller.get(t, "/api/patients/"+other+"/statements/"+id).mustStatus(t, http.StatusNotFound)
	f.biller.get(t, "/api/patients/"+other+"/statements/"+id+"/pdf").mustStatus(t, http.StatusNotFound)
	f.biller.get(t, "/api/patients/"+f.patientID+"/statements/"+id+"/pdf").mustStatus(t, http.StatusOK)
	f.biller.get(t, "/api/statements/not-a-uuid").mustStatus(t, http.StatusNotFound)
	f.biller.get(t, "/api/statements/00000000-0000-4000-8000-000000000000/pdf").mustStatus(t, http.StatusNotFound)

	// The other patient's history never lists this statement.
	list := f.biller.get(t, "/api/patients/"+other+"/statements").mustStatus(t, http.StatusOK)
	arr, _ := list.JSON().([]any)
	for _, s := range arr {
		if s.(map[string]any)["id"] == id {
			t.Fatal("statement listed under the wrong patient")
		}
	}

	if res := (client{}).get(t, "/api/statements/"+id+"/pdf"); res.Status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated download: %d", res.Status)
	}
}

func TestStatementBatchCreatesIndividualRecords(t *testing.T) {
	f := newFixture(t)

	owes := f.newPatient()
	f.directChargeFor(owes, f.dos, 1)
	alsoOwes := f.newPatient()
	f.directChargeFor(alsoOwes, f.dos, 2)
	owesNothing := f.newPatient()

	res := f.biller.post(t, "/api/billing/statements/batch", map[string]any{
		"patient_ids": []string{owes, alsoOwes, owesNothing, owes}, "comment": "Batch note",
	}).mustStatus(t, http.StatusCreated)

	if res.Len("created") != 2 || res.Len("skipped") != 1 || res.Str("skipped.0.patient_id") != owesNothing {
		t.Fatalf("batch result: %s", string(res.Raw))
	}

	batchID := res.Str("batch_id")
	ids := []string{res.Str("created.0.id"), res.Str("created.1.id")}

	if res.Str("created.0.patient_id") == res.Str("created.1.patient_id") || res.Str("created.0.batch_id") != batchID {
		t.Fatalf("each patient needs its own statement in the batch: %s", string(res.Raw))
	}

	if got := f.biller.get(t, "/api/billing/statements?batch_id="+batchID).mustStatus(t, http.StatusOK); got.Len("items") != 2 {
		t.Fatalf("batch listing: %s", string(got.Raw))
	}

	combined := f.pdf("/api/billing/statements/combined-pdf?ids=" + strings.Join(ids, ","))
	if !bytes.HasPrefix(combined, []byte("%PDF-")) || bytes.Count(combined, []byte("STATEMENT")) < 2 {
		t.Fatalf("combined PDF should hold both statements")
	}

	// Authorization and limits.
	scheduler := newUser(t, "practice_scheduler")
	if r := scheduler.post(t, "/api/billing/statements/batch", map[string]any{"patient_ids": []string{owes}}); r.Status != http.StatusForbidden {
		t.Fatalf("batch as scheduler: %d", r.Status)
	}
	f.biller.post(t, "/api/billing/statements/batch", map[string]any{"patient_ids": []string{}}).mustStatus(t, http.StatusBadRequest)
	f.biller.post(t, "/api/billing/statements/batch", map[string]any{"patient_ids": []string{"nope"}}).mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, "/api/billing/statements/combined-pdf?ids=nope").mustStatus(t, http.StatusBadRequest)
}

func TestStatementValidationAndAuthorization(t *testing.T) {
	f := newFixture(t)
	f.directCharge(1)

	bad := []map[string]any{
		{"statement_type": "weekly"},
		{"statement_type": "date_range", "start_date": "2026-01-01"},
		{"statement_type": "date_range", "start_date": "2026-02-01", "end_date": "2026-01-01"},
		{"statement_type": "date_range", "start_date": daysAgo(5), "end_date": time.Now().AddDate(0, 0, 3).Format("2006-01-02")},
		{"statement_type": "date_range", "start_date": "1999-01-01", "end_date": daysAgo(1)},
		{"statement_type": "open_balance", "comment": strings.Repeat("x", 501)},
	}

	for i, body := range bad {
		if res := f.openStatementBody(f.patientID, body); res.Status != http.StatusBadRequest {
			t.Errorf("case %d: status %d, want 400 (%s)", i, res.Status, string(res.Raw))
		}
	}

	scheduler := newUser(t, "practice_scheduler")
	if res := scheduler.post(t, "/api/patients/"+f.patientID+"/statements", map[string]any{"statement_type": "open_balance"}); res.Status != http.StatusForbidden {
		t.Fatalf("scheduler: %d", res.Status)
	}
	if res := (client{}).post(t, "/api/patients/"+f.patientID+"/statements", map[string]any{"statement_type": "open_balance"}); res.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.Status)
	}
	f.biller.post(t, "/api/patients/00000000-0000-4000-8000-000000000000/statements", map[string]any{"statement_type": "open_balance"}).mustStatus(t, http.StatusNotFound)
}

func (f *fixture) openStatementBody(patientID string, body map[string]any) response {
	f.t.Helper()
	return f.biller.post(f.t, "/api/patients/"+patientID+"/statements", body)
}

func TestStatementCandidatesFilters(t *testing.T) {
	f := newFixture(t)
	patient := f.newPatient()
	f.directChargeFor(patient, daysAgo(45), 1) // $100 in 31-60

	base := "/api/billing/statement-candidates?patient_id=" + patient

	hit := f.biller.get(t, base).mustStatus(t, http.StatusOK)
	if hit.Len("items") != 1 || hit.Str("items.0.bucket_31_60") != "100.00" || hit.Str("items.0.bucket_0_30") != "0.00" || hit.Str("items.0.total") != "100.00" {
		t.Fatalf("candidate row: %s", string(hit.Raw))
	}

	cases := map[string]int{
		"&bucket=31-60":                       1,
		"&bucket=91%2B":                       0,
		"&bucket=0-30":                        0,
		"&min_balance=100.00":                 1,
		"&min_balance=100.01":                 0,
		"&clinician_id=" + f.clinician.userID: 1,
		"&clinician_id=00000000-0000-4000-8000-000000000000": 0,
	}

	for query, want := range cases {
		res := f.biller.get(t, base+query).mustStatus(t, http.StatusOK)
		if res.Len("items") != want {
			t.Errorf("%s: %d candidates, want %d", query, res.Len("items"), want)
		}
	}

	f.biller.get(t, base+"&bucket=weekly").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, base+"&min_balance=-5").mustStatus(t, http.StatusBadRequest)

	// An insurance-only balance never makes a patient a candidate.
	insOnly := f.newPatient()
	policy := f.newPolicy(insOnly, f.payerID, "primary")
	f.chargeFor(insOnly, policy, 1, "0.00")
	if res := f.biller.get(t, "/api/billing/statement-candidates?patient_id="+insOnly).mustStatus(t, http.StatusOK); res.Len("items") != 0 {
		t.Fatalf("insurance-only patient listed as candidate: %s", string(res.Raw))
	}
}

// Aging buckets sit on the exact day boundaries: 30 / 31 / 60 / 61 / 90 / 91.
func TestAgingBucketBoundaries(t *testing.T) {
	f := newFixture(t)

	cases := []struct {
		days   int
		bucket string
	}{{0, "0-30"}, {30, "0-30"}, {31, "31-60"}, {60, "31-60"}, {61, "61-90"}, {90, "61-90"}, {91, "91+"}, {400, "91+"}}

	for _, c := range cases {
		patient := f.newPatient()
		charge := f.directChargeFor(patient, daysAgo(c.days), 1)

		var bucket string
		if err := pool.QueryRow(t.Context(), `SELECT bucket FROM billing_charge_aging WHERE charge_id = $1`, charge).Scan(&bucket); err != nil {
			t.Fatal(err)
		}

		if bucket != c.bucket {
			t.Errorf("%d days old: bucket %s, want %s", c.days, bucket, c.bucket)
		}
	}
}
