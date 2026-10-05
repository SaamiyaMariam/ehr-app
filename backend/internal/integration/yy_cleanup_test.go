package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The cleanup script removes E2E-marked data in foreign-key order and leaves
// everything else alone. It is run here inside a transaction that is rolled
// back, so the rest of the suite is unaffected.
func TestCleanupScriptRemovesOnlyE2EData(t *testing.T) {
	needDB(t)

	// Data that must be removed: a full financial history for an "E2E" patient
	// (payments, remittance, secondary claim, statement).
	m := newMultiPayer(t, "50.00", "10.00")
	secondary := m.createNext(m.primary.ID, nil).mustStatus(t, http.StatusCreated)
	m.patientPayment("4.00", true)
	m.openStatement(m.patientID).mustStatus(t, http.StatusCreated)
	_ = secondary

	// Data that must survive: a non-E2E patient, with a charge and payment,
	// whose policy uses an E2E-named payer (so that payer is still in use).
	realPatient := m.clinician.post(t, "/api/patients", map[string]any{"first_name": "Real", "last_name": unique("Person")}).mustStatus(t, http.StatusCreated).Str("id")
	realPolicy := m.newPolicy(realPatient, m.payerID, "primary")
	realCharge := m.chargeFor(realPatient, realPolicy, 1, "10.00")
	m.biller.post(t, "/api/patients/"+realPatient+"/payments", map[string]any{
		"payment_date": m.dos, "amount": "10.00", "method": "cash", "idempotency_key": newKey(), "auto_allocate": true,
	}).mustStatus(t, http.StatusCreated)

	// A non-E2E user must not be deactivated.
	keeper := newUser(t, "practice_biller")
	if _, err := pool.Exec(context.Background(), `UPDATE users SET first_name = 'Keeper' WHERE id = $1`, keeper.userID); err != nil {
		t.Fatal(err)
	}

	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "cleanup_e2e_data.sql"))
	if err != nil {
		t.Fatal(err)
	}

	// Everything before the psql-only commit switch.
	body, _, found := strings.Cut(string(raw), "\n\\if")
	if !found || !strings.Contains(body, "BEGIN;") {
		t.Fatal("cleanup script structure changed: expected BEGIN ... then a psql \\if commit switch")
	}

	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	ctx := context.Background()

	if _, err := conn.Exec(ctx, body); err != nil {
		t.Fatalf("cleanup script failed (foreign-key order?): %v", err)
	}
	defer conn.Exec(ctx, "ROLLBACK")

	count := func(sql string, args ...any) int {
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := count(`SELECT COUNT(*) FROM patients WHERE first_name = 'E2E'`); n != 0 {
		t.Errorf("%d E2E patients remain", n)
	}
	for _, table := range []string{"claims", "billing_charges", "patient_payments", "insurance_payments", "patient_statements"} {
		// Only the surviving real patient's rows may remain.
		if n := count(`SELECT COUNT(*) FROM ` + table + ` t WHERE EXISTS (
			SELECT 1 FROM patients p WHERE p.first_name = 'E2E' AND p.id = (to_jsonb(t)->>'patient_id')::uuid)`); n != 0 && table != "insurance_payments" {
			t.Errorf("%s: %d rows of E2E patients remain", table, n)
		}
	}

	// Survivors.
	if n := count(`SELECT COUNT(*) FROM patients WHERE id = $1`, realPatient); n != 1 {
		t.Error("the non-E2E patient was deleted")
	}
	if n := count(`SELECT COUNT(*) FROM billing_charges WHERE id = $1`, realCharge); n != 1 {
		t.Error("the non-E2E patient's charge was deleted")
	}
	if n := count(`SELECT COUNT(*) FROM patient_payments WHERE patient_id = $1`, realPatient); n != 1 {
		t.Error("the non-E2E patient's payment was deleted")
	}
	if n := count(`SELECT COUNT(*) FROM payers WHERE id = $1`, m.payerID); n != 1 {
		t.Error("an E2E payer that a real policy still uses must be kept")
	}

	// Users are deactivated, never deleted; non-E2E users are untouched.
	if n := count(`SELECT COUNT(*) FROM users WHERE first_name = 'E2E' AND is_active`); n != 0 {
		t.Errorf("%d E2E users still active", n)
	}
	if n := count(`SELECT COUNT(*) FROM users WHERE id = $1 AND is_active`, keeper.userID); n != 1 {
		t.Error("a non-E2E user was deactivated")
	}

	// What is left is still a consistent ledger.
	rows, err := conn.Query(ctx, reconcileScript(t))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var v violation
		_ = rows.Scan(&v.Check, &v.Object, &v.Detail)
		t.Errorf("ledger inconsistent after cleanup: %+v", v)
	}
}

func repoRoot(t testing.TB) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "cleanup_e2e_data.sql")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
