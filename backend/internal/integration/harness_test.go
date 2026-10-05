package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ehr-backend/internal/server"
	"ehr-backend/internal/testdb"
)

const testJWTSecret = "integration-test-secret-integration-test-secret"

var (
	pool    *pgxpool.Pool
	baseURL string
	router  *server.Router
	counter atomic.Int64
)

func TestMain(m *testing.M) {
	p, err := testdb.Open("integration")
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration: DB-backed tests will be skipped:", err)
		os.Exit(m.Run())
	}

	pool = p

	handler, rt := server.New(pool, testJWTSecret)
	router = rt

	srv := httptest.NewServer(handler)
	baseURL = srv.URL

	code := m.Run()

	srv.Close()
	pool.Close()
	os.Exit(code)
}

func needDB(t testing.TB) {
	t.Helper()

	if pool == nil {
		t.Skip("no test database available")
	}
}

func unique(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, counter.Add(1))
}

// =========================================================
// HTTP client
// =========================================================

type response struct {
	Status int
	Raw    []byte
	body   any
}

func (r response) JSON() any {
	if r.body == nil && len(r.Raw) > 0 {
		_ = json.Unmarshal(r.Raw, &r.body)
	}

	return r.body
}

// Get walks a dotted path ("payment.allocations.0.id") through the JSON body.
func (r response) Get(path string) any {
	cur := r.JSON()

	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			cur = node[part]
		case []any:
			var i int
			if _, err := fmt.Sscanf(part, "%d", &i); err != nil || i < 0 || i >= len(node) {
				return nil
			}
			cur = node[i]
		default:
			return nil
		}
	}

	return cur
}

func (r response) Str(path string) string {
	if v, ok := r.Get(path).(string); ok {
		return v
	}

	return ""
}

func (r response) Bool(path string) bool {
	v, _ := r.Get(path).(bool)
	return v
}

func (r response) Len(path string) int {
	if v, ok := r.Get(path).([]any); ok {
		return len(v)
	}

	return -1
}

func (r response) Error() string { return r.Str("error") }

type client struct {
	token  string
	userID string
}

func (c client) do(t testing.TB, method, path string, body any) response {
	t.Helper()

	var reader io.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	return response{Status: resp.StatusCode, Raw: raw}
}

func (c client) get(t testing.TB, path string) response { t.Helper(); return c.do(t, "GET", path, nil) }
func (c client) put(t testing.TB, path string, body any) response {
	t.Helper()
	return c.do(t, "PUT", path, body)
}
func (c client) post(t testing.TB, path string, body any) response {
	t.Helper()
	return c.do(t, "POST", path, body)
}

// mustOK fails the test unless the response has one of the wanted statuses.
func (r response) mustStatus(t testing.TB, want ...int) response {
	t.Helper()

	for _, w := range want {
		if r.Status == w {
			return r
		}
	}

	t.Fatalf("unexpected status %d (want %v): %s", r.Status, want, string(r.Raw))

	return r
}

// =========================================================
// Users
// =========================================================

// newUser signs up a fresh user through the API and grants the roles in SQL.
func newUser(t testing.TB, roles ...string) client {
	t.Helper()
	needDB(t)

	name := unique("user")

	res := (client{}).post(t, "/api/auth/signup", map[string]string{
		"first_name": "Test", "last_name": strings.ToUpper(name[:1]) + name[1:],
		"username": name, "email": name + "@example.test", "password": "correct-horse-battery",
	}).mustStatus(t, http.StatusCreated)

	userID := res.Str("user.id")

	if len(roles) > 0 {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE key = ANY($2) ON CONFLICT DO NOTHING`,
			userID, roles,
		); err != nil {
			t.Fatal(err)
		}
	}

	return client{token: res.Str("token"), userID: userID}
}

// =========================================================
// Billing fixture
// =========================================================

type fixture struct {
	t          testing.TB
	biller     client
	clinician  client
	payerID    string
	payerName  string
	serviceID  string
	patientID  string
	policyID   string
	dos        string
	patientTag string
}

// newFixture builds payer, service code ($100.00 standard rate), clinician,
// patient and a primary policy.
func newFixture(t testing.TB) *fixture {
	t.Helper()
	needDB(t)

	f := &fixture{
		t:         t,
		biller:    newUser(t, "practice_biller"),
		clinician: newUser(t, "clinician"),
		dos:       time.Now().AddDate(0, 0, -20).Format("2006-01-02"),
	}

	f.payerName = unique("E2E Payer ")
	f.payerID = f.newPayer(f.payerName)
	f.serviceID = f.newServiceCode("100.00")
	f.patientID = f.newPatient()
	f.policyID = f.newPolicy(f.patientID, f.payerID, "primary")

	return f
}

func (f *fixture) newPayer(name string) string {
	f.t.Helper()

	return f.biller.post(f.t, "/api/payers", map[string]any{
		"payer_name": name, "payer_id": unique("PID"), "in_network": true, "billing_method": "external", "insurance_type": "group_health_plan",
	}).mustStatus(f.t, http.StatusCreated).Str("id")
}

func (f *fixture) newServiceCode(rate string) string {
	f.t.Helper()

	return f.biller.post(f.t, "/api/service-codes", map[string]any{
		"code": unique("SC"), "description": "Test service", "standard_rate": rate,
	}).mustStatus(f.t, http.StatusCreated).Str("id")
}

func (f *fixture) newPatient() string {
	f.t.Helper()

	name := unique("Patient")

	return f.clinician.post(f.t, "/api/patients", map[string]any{
		"first_name": "E2E", "last_name": name, "date_of_birth": "1985-04-12",
		"assigned_clinician_id": f.clinician.userID,
	}).mustStatus(f.t, http.StatusCreated).Str("id")
}

func (f *fixture) newPolicy(patientID, payerID, priority string) string {
	f.t.Helper()

	return f.biller.post(f.t, "/api/patients/"+patientID+"/insurance-policies", map[string]any{
		"payer_id": payerID, "priority": priority, "member_id": unique("MEM"), "coverage_start": "2020-01-01",
	}).mustStatus(f.t, http.StatusCreated).Str("id")
}

// insuranceCharge creates a billable service for the fixture patient on the
// primary policy. patientShare "" uses the default split.
func (f *fixture) insuranceCharge(units int, patientShare string) string {
	f.t.Helper()
	return f.chargeFor(f.patientID, f.policyID, units, patientShare)
}

func (f *fixture) chargeFor(patientID, policyID string, units int, patientShare string) string {
	f.t.Helper()

	body := map[string]any{
		"clinician_id": f.clinician.userID, "service_code_id": f.serviceID, "date_of_service": f.dos,
		"units": units, "place_of_service": "11", "insurance_policy_id": policyID,
	}
	if patientShare != "" {
		body["patient_responsibility"] = patientShare
	}

	return f.biller.post(f.t, "/api/patients/"+patientID+"/charges", body).mustStatus(f.t, http.StatusCreated).Str("id")
}

func (f *fixture) directCharge(units int) string {
	f.t.Helper()

	return f.biller.post(f.t, "/api/patients/"+f.patientID+"/charges", map[string]any{
		"clinician_id": f.clinician.userID, "service_code_id": f.serviceID, "date_of_service": f.dos,
		"units": units, "place_of_service": "11", "billing_method": "direct",
	}).mustStatus(f.t, http.StatusCreated).Str("id")
}

type claimRef struct {
	ID      string
	Number  string
	LineIDs map[string]string // charge id -> claim line id
}

// submittedClaim creates a primary claim for the charges and marks it as
// submitted (the submission workflow itself is covered by its own tests).
func (f *fixture) submittedClaim(chargeIDs ...string) claimRef {
	f.t.Helper()

	res := f.biller.post(f.t, "/api/claims", map[string]any{"charge_ids": chargeIDs}).mustStatus(f.t, http.StatusCreated)

	ref := claimRef{ID: res.Str("id"), Number: res.Str("claim_number"), LineIDs: map[string]string{}}

	lines, _ := res.Get("lines").([]any)
	for _, l := range lines {
		line := l.(map[string]any)
		ref.LineIDs[line["charge_id"].(string)] = line["id"].(string)
	}

	if _, err := pool.Exec(
		context.Background(),
		`UPDATE claims SET status = 'submitted', submitted_at = NOW() WHERE id = $1`,
		ref.ID,
	); err != nil {
		f.t.Fatal(err)
	}

	return ref
}

func (f *fixture) claimStatus(claimID string) string {
	f.t.Helper()

	return f.biller.get(f.t, "/api/claims/"+claimID).mustStatus(f.t, http.StatusOK).Str("status")
}

// balances returns the charge's balances from the API.
func (f *fixture) balances(chargeID string) (patient, insurance string) {
	f.t.Helper()

	res := f.biller.get(f.t, "/api/charges/"+chargeID).mustStatus(f.t, http.StatusOK)

	return res.Str("balances.patient_balance"), res.Str("balances.insurance_balance")
}

func (f *fixture) assertBalances(chargeID, wantPatient, wantInsurance string) {
	f.t.Helper()

	p, i := f.balances(chargeID)
	if p != wantPatient || i != wantInsurance {
		f.t.Fatalf("balances: patient %s insurance %s, want patient %s insurance %s", p, i, wantPatient, wantInsurance)
	}
}

func newKey() string {
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		panic(err)
	}

	return id
}

// payment builds an insurance payment request body.
func payment(payerID, amount string, lines ...map[string]any) map[string]any {
	return map[string]any{
		"payer_id": payerID, "payment_date": time.Now().Format("2006-01-02"), "amount": amount,
		"payment_type": "check", "reference_number": unique("CHK"), "idempotency_key": newKey(),
		"lines": lines,
	}
}

func line(claimID, lineID, paid string, extra map[string]any) map[string]any {
	l := map[string]any{"claim_id": claimID, "claim_line_id": lineID, "amount_paid": paid}
	for k, v := range extra {
		l[k] = v
	}

	return l
}
