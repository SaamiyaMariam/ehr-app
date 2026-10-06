package integration

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
)

func (f *fixture) csv(path string) (header []string, rows [][]string, raw string) {
	f.t.Helper()

	res := f.biller.get(f.t, path).mustStatus(f.t, http.StatusOK)

	records, err := csv.NewReader(strings.NewReader(string(res.Raw))).ReadAll()
	if err != nil || len(records) == 0 {
		f.t.Fatalf("not valid CSV (%v): %q", err, string(res.Raw))
	}

	return records[0], records[1:], string(res.Raw)
}

func TestDashboardCountsMatchTheListsTheyLinkTo(t *testing.T) {
	f := newFixture(t)

	counts := func() map[string]float64 {
		res := f.biller.get(t, "/api/billing/dashboard").mustStatus(t, http.StatusOK)
		out := map[string]float64{}
		for k, v := range res.Get("claims").(map[string]any) {
			out[k] = v.(float64)
		}
		return out
	}

	before := counts()

	// A rejected claim, a ready external claim and a paper claim waiting to be mailed.
	rejected := f.submittedClaim(f.insuranceCharge(1, "0"))
	external := f.submittedClaim(f.insuranceCharge(1, "0"))
	paper := f.submittedClaim(f.insuranceCharge(1, "0"))

	for _, q := range []string{
		`UPDATE claims SET status = 'rejected_new' WHERE id = '` + rejected.ID + `'`,
		`UPDATE claims SET status = 'ready' WHERE id = '` + external.ID + `'`,
		`UPDATE claims SET status = 'paper_generated', submission_method = 'paper' WHERE id = '` + paper.ID + `'`,
	} {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}

	after := counts()

	for queue, want := range map[string]float64{"rejected": 1, "external_pending": 1, "paper_generated": 1, "paper_pending": 1, "pending": 1} {
		if after[queue]-before[queue] != want {
			t.Errorf("%s: dashboard moved by %v, want %v", queue, after[queue]-before[queue], want)
		}
	}

	// Every dashboard number equals the total of the filtered claim list it links to.
	for queue, n := range after {
		list := f.biller.get(t, "/api/claims?queue="+queue+"&page_size=1").mustStatus(t, http.StatusOK)
		if list.Get("total").(float64) != n {
			t.Errorf("queue %s: dashboard says %v but the list has %v", queue, n, list.Get("total"))
		}
	}

	f.biller.get(t, "/api/claims?queue=nonsense").mustStatus(t, http.StatusBadRequest)

	res := f.biller.get(t, "/api/billing/dashboard").mustStatus(t, http.StatusOK)
	if res.Bool("clearinghouse_configured") {
		t.Error("no clearinghouse is configured in tests")
	}
	for _, key := range []string{"patient_balance", "insurance_balance", "unallocated_patient_credit"} {
		if res.Str(key) == "" {
			t.Errorf("dashboard is missing %s", key)
		}
	}

	if r := (client{}).get(t, "/api/billing/dashboard"); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous dashboard: %d", r.Status)
	}
}

func TestInsuranceAgingBucketBoundariesAndSeparation(t *testing.T) {
	f := newFixture(t)

	cases := []struct {
		days   int
		bucket string
	}{{30, "0-30"}, {31, "31-60"}, {60, "31-60"}, {61, "61-90"}, {90, "61-90"}, {91, "91+"}}

	for _, c := range cases {
		patient := f.newPatient()
		policy := f.newPolicy(patient, f.payerID, "primary")
		charge := f.biller.post(t, "/api/patients/"+patient+"/charges", map[string]any{
			"clinician_id": f.clinician.userID, "service_code_id": f.serviceID, "date_of_service": daysAgo(c.days),
			"units": 1, "place_of_service": "11", "insurance_policy_id": policy, "patient_responsibility": "20.00",
		}).mustStatus(t, http.StatusCreated).Str("id")

		// Insurance aging shows only the insurance side ($80), patient aging only the patient side ($20).
		ins := f.biller.get(t, "/api/billing/reports/insurance-aging?patient_id="+patient).mustStatus(t, http.StatusOK)
		if ins.Len("items") != 1 || ins.Str("items.0.charge_id") != charge || ins.Str("items.0.bucket") != c.bucket || ins.Str("items.0.insurance_balance") != "80.00" {
			t.Fatalf("%d days: insurance aging row %s", c.days, string(ins.Raw))
		}
		if ins.Str("totals.total") != "80.00" || ins.Str("items.0.claim_sequence") != "" {
			t.Fatalf("%d days: insurance totals / unbilled attribution %s", c.days, string(ins.Raw))
		}

		pat := f.biller.get(t, "/api/billing/reports/patient-aging?patient_id="+patient).mustStatus(t, http.StatusOK)
		key := map[string]string{"0-30": "totals.bucket_0_30", "31-60": "totals.bucket_31_60", "61-90": "totals.bucket_61_90", "91+": "totals.bucket_91_plus"}[c.bucket]
		if pat.Str("totals.total") != "20.00" || pat.Str(key) != "20.00" {
			t.Fatalf("%d days: patient aging %s", c.days, string(pat.Raw))
		}
	}
}

func TestInsuranceAgingFiltersAndClaimAttribution(t *testing.T) {
	f := newFixture(t)
	charge := f.insuranceCharge(1, "0")
	other := f.newPayer(unique("Aging Other Payer "))
	otherPatient := f.newPatient()
	otherPolicy := f.newPolicy(otherPatient, other, "primary")
	otherCharge := f.chargeFor(otherPatient, otherPolicy, 1, "0")
	_ = otherCharge

	base := "/api/billing/reports/insurance-aging?patient_id=" + f.patientID

	// Unbilled until a claim exists.
	if res := f.biller.get(t, base+"&sequence=unbilled").mustStatus(t, http.StatusOK); res.Len("items") != 1 {
		t.Fatalf("unbilled: %s", string(res.Raw))
	}
	if res := f.biller.get(t, base+"&sequence=primary").mustStatus(t, http.StatusOK); res.Len("items") != 0 {
		t.Fatalf("primary before a claim: %s", string(res.Raw))
	}

	claim := f.submittedClaim(charge)

	res := f.biller.get(t, base+"&sequence=primary").mustStatus(t, http.StatusOK)
	if res.Len("items") != 1 || res.Str("items.0.claim_number") != claim.Number || res.Str("items.0.claim_sequence") != "primary" {
		t.Fatalf("primary attribution: %s", string(res.Raw))
	}

	cases := map[string]int{
		"&payer_id=" + f.payerID:                             1,
		"&payer_id=" + other:                                 0,
		"&clinician_id=" + f.clinician.userID:                1,
		"&clinician_id=00000000-0000-4000-8000-000000000000": 0,
		"&bucket=0-30":                                       1,
		"&bucket=91%2B":                                      0,
	}
	for q, want := range cases {
		if got := f.biller.get(t, base+q).mustStatus(t, http.StatusOK); got.Len("items") != want {
			t.Errorf("%s: %d rows, want %d", q, got.Len("items"), want)
		}
	}

	// A second payer's balance is attributed to that payer.
	byPayer := f.biller.get(t, "/api/billing/reports/insurance-aging?payer_id="+other+"&patient_id="+otherPatient).mustStatus(t, http.StatusOK)
	if byPayer.Len("items") != 1 || byPayer.Str("items.0.payer_name") == "" {
		t.Fatalf("other payer row: %s", string(byPayer.Raw))
	}

	// Paid in full: it leaves the report.
	f.biller.post(t, "/api/insurance-payments", payment(f.payerID, "100.00", line(claim.ID, claim.LineIDs[charge], "100.00", nil))).mustStatus(t, http.StatusCreated)
	if res := f.biller.get(t, base).mustStatus(t, http.StatusOK); res.Len("items") != 0 || res.Str("totals.total") != "0.00" {
		t.Fatalf("paid service still aged: %s", string(res.Raw))
	}

	f.biller.get(t, base+"&sequence=fifth").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, base+"&bucket=weekly").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, "/api/billing/reports/insurance-aging?payer_id=nope").mustStatus(t, http.StatusBadRequest)
}

func TestPatientAgingReportFilters(t *testing.T) {
	f := newFixture(t)
	patient := f.newPatient()
	f.directChargeFor(patient, daysAgo(10), 1)  // 100 in 0-30
	f.directChargeFor(patient, daysAgo(100), 2) // 200 in 91+

	res := f.biller.get(t, "/api/billing/reports/patient-aging?patient_id="+patient).mustStatus(t, http.StatusOK)
	if res.Str("items.0.bucket_0_30") != "100.00" || res.Str("items.0.bucket_91_plus") != "200.00" || res.Str("totals.total") != "300.00" || res.Str("basis") != "date_of_service" {
		t.Fatalf("patient aging: %s", string(res.Raw))
	}

	for q, want := range map[string]int{"&min_balance=300.00": 1, "&min_balance=300.01": 0, "&bucket=91%2B": 1, "&bucket=31-60": 0} {
		if got := f.biller.get(t, "/api/billing/reports/patient-aging?patient_id="+patient+q).mustStatus(t, http.StatusOK); got.Len("items") != want {
			t.Errorf("%s: %d rows, want %d", q, got.Len("items"), want)
		}
	}
}

func TestTransactionSearchExpandedFilters(t *testing.T) {
	f := newFixture(t)

	owes := f.directCharge(1)               // patient 100, no claim
	billed := f.insuranceCharge(1, "20.00") // patient 20 / insurance 80
	claim := f.submittedClaim(billed)
	closed := f.directCharge(1)
	f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
		"payment_date": f.dos, "amount": "100.00", "method": "cash", "idempotency_key": newKey(),
		"allocations": []map[string]any{{"charge_id": closed, "amount": "100.00"}},
	}).mustStatus(t, http.StatusCreated)

	base := "/api/billing/transactions?page_size=50&patient_id=" + f.patientID

	ids := func(query string) map[string]bool {
		res := f.biller.get(t, base+query).mustStatus(t, http.StatusOK)
		set := map[string]bool{}
		items, _ := res.Get("items").([]any)
		for _, i := range items {
			set[i.(map[string]any)["id"].(string)] = true
		}
		return set
	}

	check := func(query string, want ...string) {
		t.Helper()
		got := ids(query)
		if len(got) != len(want) {
			t.Errorf("%s: %d results, want %d", query, len(got), len(want))
		}
		for _, id := range want {
			if !got[id] {
				t.Errorf("%s: missing %s", query, id)
			}
		}
	}

	check("&claim_status=externally_submitted")
	check("&claim_status=submitted", billed)
	check("&claim_status=none", owes, closed)
	check("&has_insurance_balance=true", billed)
	check("&has_patient_balance=true", owes, billed)
	check("&balance=open", owes, billed)
	check("&balance=closed", closed)
	check("&balance=open&has_insurance_balance=true", billed)
	check("&billing_method=direct", owes, closed)

	_ = claim

	f.biller.get(t, base+"&claim_status=bogus").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, base+"&balance=maybe").mustStatus(t, http.StatusBadRequest)
	f.biller.get(t, base+"&has_patient_balance=false").mustStatus(t, http.StatusBadRequest)

	// Pagination is bounded.
	res := f.biller.get(t, "/api/billing/transactions?page_size=100000").mustStatus(t, http.StatusOK)
	if res.Get("page_size").(float64) > 200 {
		t.Fatalf("page size is not capped: %v", res.Get("page_size"))
	}
}

func TestCSVExportEscapingAndFilters(t *testing.T) {
	f := newFixture(t)

	// Names with commas, quotes, a newline and a spreadsheet formula.
	trickyPayer := f.newPayer(`=HYPERLINK("http://evil.test","x"), Payer "Quoted"`)

	person := f.clinician.post(t, "/api/patients", map[string]any{
		"first_name": "Ann", "last_name": "O'Brien, \"Jr\"\nSecond", "date_of_birth": "1990-01-01",
	}).mustStatus(t, http.StatusCreated).Str("id")
	policy := f.newPolicy(person, trickyPayer, "primary")
	charge := f.chargeFor(person, policy, 2, "30.00") // total 200: patient 30, insurance 170
	claim := f.submittedClaim(charge)
	_ = claim

	f.directCharge(1) // belongs to another patient: must be excluded by the filter

	header, rows, raw := f.csv("/api/billing/transactions/export?patient_id=" + person)

	wantHeader := []string{"Patient", "Date of Service", "Clinician", "Service Code", "Payer", "Charge", "Patient Responsibility",
		"Insurance Responsibility", "Patient Paid", "Insurance Paid", "Adjustments", "Patient Balance", "Insurance Balance", "Claim Status", "Charge Status"}
	if strings.Join(header, "|") != strings.Join(wantHeader, "|") {
		t.Fatalf("header: %v", header)
	}

	if len(rows) != 1 {
		t.Fatalf("export must respect the patient filter, got %d rows:\n%s", len(rows), raw)
	}

	row := rows[0]
	if !strings.Contains(row[0], "O'Brien, \"Jr\"\nSecond") {
		t.Errorf("patient name did not survive CSV escaping: %q", row[0])
	}
	if !strings.HasPrefix(row[4], "'=HYPERLINK") {
		t.Errorf("formula in payer name must be neutralised: %q", row[4])
	}
	if row[5] != "200.00" || row[6] != "30.00" || row[7] != "170.00" || row[11] != "30.00" || row[12] != "170.00" || row[13] != "submitted" || row[14] != "active" {
		t.Errorf("amounts / status: %v", row)
	}

	// Active filters apply: paid-off rows disappear with has_insurance_balance=true.
	_, rows, _ = f.csv("/api/billing/transactions/export?patient_id=" + person + "&has_insurance_balance=true")
	if len(rows) != 1 {
		t.Errorf("filtered export: %d rows", len(rows))
	}
	_, rows, _ = f.csv("/api/billing/transactions/export?patient_id=" + person + "&balance=closed")
	if len(rows) != 0 {
		t.Errorf("closed filter should return nothing: %d rows", len(rows))
	}

	// Disposition / content type, validation and authentication.
	res := f.biller.get(t, "/api/billing/transactions/export?patient_id="+person).mustStatus(t, http.StatusOK)
	_ = res
	f.biller.get(t, "/api/billing/transactions/export?balance=maybe").mustStatus(t, http.StatusBadRequest)
	if r := (client{}).get(t, "/api/billing/transactions/export"); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous export: %d", r.Status)
	}
}

func TestCollectionsReport(t *testing.T) {
	f := newFixture(t)

	// An isolated window in the past so other scenarios cannot affect it.
	const from, to = "2015-03-01", "2015-03-31"
	window := "/api/billing/reports/collections?from=" + from + "&to=" + to

	charge := f.insuranceCharge(1, "20.00") // insurance 80
	claim := f.submittedClaim(charge)

	post := func(date, amount string, allocate bool) response {
		return f.biller.post(t, "/api/patients/"+f.patientID+"/payments", map[string]any{
			"payment_date": date, "amount": amount, "method": "cash", "idempotency_key": newKey(), "auto_allocate": allocate,
		}).mustStatus(t, http.StatusCreated)
	}

	post("2015-03-02", "40.00", false)          // counted
	voided := post("2015-03-10", "5.00", false) // voided below: not counted
	post("2015-04-02", "99.00", false)          // outside the window
	f.biller.post(t, "/api/patient-payments/"+voided.Str("payment.id")+"/void", map[string]any{"reason": "entered twice"}).mustStatus(t, http.StatusOK)

	list := f.biller.get(t, "/api/patients/"+f.patientID+"/payments")
	var march string
	for _, p := range list.JSON().([]any) {
		m := p.(map[string]any)
		if m["payment_date"] == "2015-03-02" {
			march = m["id"].(string)
		}
	}
	f.biller.post(t, "/api/patient-payments/"+march+"/refunds", map[string]any{
		"amount": "10.00", "refund_date": "2015-03-04", "method": "cash", "reason": "overpaid", "idempotency_key": newKey(),
	}).mustStatus(t, http.StatusOK)

	ins := payment(f.payerID, "70.00", line(claim.ID, claim.LineIDs[charge], "70.00", map[string]any{"is_final": false}))
	ins["payment_date"] = "2015-03-03"
	f.biller.post(t, "/api/insurance-payments", ins).mustStatus(t, http.StatusCreated)

	res := f.biller.get(t, window).mustStatus(t, http.StatusOK)

	if res.Str("patient_payments.amount") != "40.00" || res.Get("patient_payments.count").(float64) != 1 {
		t.Errorf("patient payments: %s", string(res.Raw))
	}
	if res.Str("insurance_payments.amount") != "70.00" || res.Str("refunds.amount") != "10.00" {
		t.Errorf("insurance / refunds: %s", string(res.Raw))
	}
	// 40 + 70 - 10: refunds reduce collections; voided and out-of-window payments do not count.
	if res.Str("total_collections") != "100.00" {
		t.Errorf("total collections = %s, want 100.00", res.Str("total_collections"))
	}
	if res.Len("insurance_by_payer") != 1 || res.Len("patient_by_method") != 1 {
		t.Errorf("breakdowns: %s", string(res.Raw))
	}

	// Charges and adjustments are not collections: use today's window and compare deltas.
	today := daysAgo(0)
	todayURL := "/api/billing/reports/collections?from=" + today + "&to=" + today
	beforeRep := f.biller.get(t, todayURL).mustStatus(t, http.StatusOK)

	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "bad_debt_writeoff", "amount": "5.00", "reason": "uncollectible",
	}).mustStatus(t, http.StatusCreated)
	f.biller.post(t, "/api/charges/"+charge+"/adjustments", map[string]any{
		"party": "insurance", "adjustment_type": "payer_adjustment", "amount": "3.00", "reason": "payer correction",
	}).mustStatus(t, http.StatusCreated)
	f.directCharge(1)

	afterRep := f.biller.get(t, todayURL).mustStatus(t, http.StatusOK)

	diff := func(key string) int64 { return cents(t, afterRep.Str(key)) - cents(t, beforeRep.Str(key)) }

	if diff("writeoffs.amount") != 500 || diff("adjustments.amount") != 300 || diff("charges_created.amount") != 10000 {
		t.Errorf("deltas: writeoffs %d adjustments %d charges %d", diff("writeoffs.amount"), diff("adjustments.amount"), diff("charges_created.amount"))
	}
	if diff("patient_payments.amount") != 0 || diff("insurance_payments.amount") != 0 || diff("total_collections") != 0 {
		t.Errorf("adjustments / charges leaked into collections: %s", string(afterRep.Raw))
	}

	// Validation.
	for _, q := range []string{"", "?from=2026-01-01", "?from=2026-02-01&to=2026-01-01", "?from=bad&to=2026-01-01", "?from=2000-01-01&to=2026-01-01"} {
		f.biller.get(t, "/api/billing/reports/collections"+q).mustStatus(t, http.StatusBadRequest)
	}
	if r := (client{}).get(t, window); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous report: %d", r.Status)
	}

}
