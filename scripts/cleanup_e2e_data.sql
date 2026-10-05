-- Remove end-to-end test records from the development database.
--
-- SAFE BY DEFAULT: this is a DRY RUN. It deletes inside a transaction, prints
-- how many rows each step removed, and then ROLLS BACK. To really delete:
--
--   psql "$DATABASE_URL" -f scripts/cleanup_e2e_data.sql              -- dry run
--   psql "$DATABASE_URL" -v commit=1 -f scripts/cleanup_e2e_data.sql  -- delete
--
-- WHAT IT TARGETS (nothing else is touched):
--   patients       first_name = 'E2E'                       (the browser / API tests
--                                                            create patients named "E2E ...")
--   payers         payer_name LIKE 'E2E%'
--   service codes  code LIKE 'E2E%'  OR  description LIKE 'E2E%'
--   users          first_name = 'E2E'   -> DEACTIVATED, not deleted (history references them)
--
-- Everything owned by an E2E patient is removed in foreign-key order:
-- statements, superbills, insurance and patient payments with their
-- allocations / adjustments / transfers / refunds, claims with lines,
-- history, documents and submissions, prior-authorization usage, charges,
-- authorizations, policies, diagnoses, cash rates, billing settings.
--
-- E2E payers and service codes are removed only when nothing that is NOT an
-- E2E record still references them; otherwise they are left in place.
-- Patients, payers, codes and users that do not carry the E2E marker (for
-- example "Jane Doe-Smith", "Acme Health 2", 90837, or the smoketest user)
-- are never deleted. If anything unexpected still references a row, a foreign
-- key stops the script and the whole transaction is rolled back.
--
-- Financial records are normally append-only; this script is for
-- disposable test data only and must never be pointed at production.

BEGIN;

CREATE TEMP TABLE _cleanup_report (step text, rows_removed bigint) ON COMMIT DROP;

CREATE TEMP TABLE _e2e_patients ON COMMIT DROP AS
    SELECT id FROM patients WHERE first_name = 'E2E';

CREATE TEMP TABLE _e2e_payers ON COMMIT DROP AS
    SELECT id FROM payers WHERE payer_name LIKE 'E2E%';

CREATE TEMP TABLE _e2e_codes ON COMMIT DROP AS
    SELECT id FROM service_codes WHERE code LIKE 'E2E%' OR description LIKE 'E2E%';

CREATE TEMP TABLE _e2e_charges ON COMMIT DROP AS
    SELECT id FROM billing_charges WHERE patient_id IN (SELECT id FROM _e2e_patients);

CREATE TEMP TABLE _e2e_claims ON COMMIT DROP AS
    SELECT id FROM claims WHERE patient_id IN (SELECT id FROM _e2e_patients);

-- ---------------------------------------------------------------------
-- Statements and superbills
-- ---------------------------------------------------------------------
WITH d AS (DELETE FROM patient_statements WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_statements', COUNT(*) FROM d;

WITH d AS (DELETE FROM superbill_charges WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'superbill_charges', COUNT(*) FROM d;

WITH d AS (DELETE FROM superbills WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'superbills', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- Insurance remittances: adjustments, transfers, allocations, payments
-- ---------------------------------------------------------------------
WITH d AS (
    DELETE FROM billing_adjustments
    WHERE charge_id IN (SELECT id FROM _e2e_charges)
       OR insurance_payment_id IN (SELECT id FROM insurance_payments WHERE payer_id IN (SELECT id FROM _e2e_payers))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'billing_adjustments', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM responsibility_transfers
    WHERE charge_id IN (SELECT id FROM _e2e_charges)
       OR insurance_payment_id IN (SELECT id FROM insurance_payments WHERE payer_id IN (SELECT id FROM _e2e_payers))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'responsibility_transfers', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM insurance_payment_allocations
    WHERE charge_id IN (SELECT id FROM _e2e_charges)
       OR payment_id IN (SELECT id FROM insurance_payments WHERE payer_id IN (SELECT id FROM _e2e_payers))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'insurance_payment_allocations', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM insurance_payments p
    WHERE p.payer_id IN (SELECT id FROM _e2e_payers)
      AND NOT EXISTS (SELECT 1 FROM insurance_payment_allocations a WHERE a.payment_id = p.id)
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'insurance_payments', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- Patient payments
-- ---------------------------------------------------------------------
WITH d AS (DELETE FROM patient_payment_allocations WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_payment_allocations', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_payment_refunds WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_payment_refunds', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_payments WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_payments', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- Claims
-- ---------------------------------------------------------------------
WITH d AS (
    DELETE FROM prior_authorization_usage
    WHERE charge_id IN (SELECT id FROM _e2e_charges) OR claim_id IN (SELECT id FROM _e2e_claims)
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'prior_authorization_usage', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_comments WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_comments', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_diagnoses WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_diagnoses', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_documents WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_documents', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_history WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_history', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_submissions WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_submissions', COUNT(*) FROM d;

WITH d AS (DELETE FROM claim_lines WHERE claim_id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claim_lines', COUNT(*) FROM d;

-- Secondary claims point at the claim they follow; detach before deleting.
UPDATE claims SET previous_claim_id = NULL WHERE id IN (SELECT id FROM _e2e_claims);

WITH d AS (DELETE FROM claims WHERE id IN (SELECT id FROM _e2e_claims) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'claims', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- Charges, authorizations, policies and the patients themselves
-- ---------------------------------------------------------------------
WITH d AS (DELETE FROM billing_charge_diagnoses WHERE charge_id IN (SELECT id FROM _e2e_charges) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'billing_charge_diagnoses', COUNT(*) FROM d;

WITH d AS (DELETE FROM billing_charges WHERE id IN (SELECT id FROM _e2e_charges) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'billing_charges', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM prior_authorization_service_codes
    WHERE prior_authorization_id IN (
        SELECT pa.id FROM prior_authorizations pa
        JOIN insurance_policies ip ON ip.id = pa.insurance_policy_id
        WHERE ip.patient_id IN (SELECT id FROM _e2e_patients))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'prior_authorization_service_codes', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM prior_authorizations
    WHERE insurance_policy_id IN (SELECT id FROM insurance_policies WHERE patient_id IN (SELECT id FROM _e2e_patients))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'prior_authorizations', COUNT(*) FROM d;

WITH d AS (DELETE FROM insurance_policies WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'insurance_policies', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_cash_rates WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_cash_rates', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_diagnoses WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_diagnoses', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_billing_settings WHERE patient_id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_billing_settings', COUNT(*) FROM d;

WITH d AS (DELETE FROM patients WHERE id IN (SELECT id FROM _e2e_patients) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patients', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- E2E payers and service codes nothing else still uses
-- ---------------------------------------------------------------------
CREATE TEMP TABLE _free_payers ON COMMIT DROP AS
    SELECT id FROM _e2e_payers y
    WHERE NOT EXISTS (SELECT 1 FROM billing_charges WHERE payer_id = y.id)
      AND NOT EXISTS (SELECT 1 FROM claims WHERE payer_id = y.id)
      AND NOT EXISTS (SELECT 1 FROM insurance_policies WHERE payer_id = y.id)
      AND NOT EXISTS (SELECT 1 FROM insurance_payments WHERE payer_id = y.id);

WITH d AS (DELETE FROM payer_clinician_rate_schedules WHERE payer_id IN (SELECT id FROM _free_payers) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'payer_clinician_rate_schedules', COUNT(*) FROM d;

WITH d AS (
    DELETE FROM payer_rate_schedule_items
    WHERE rate_schedule_id IN (SELECT id FROM payer_rate_schedules WHERE payer_id IN (SELECT id FROM _free_payers))
    RETURNING 1)
INSERT INTO _cleanup_report SELECT 'payer_rate_schedule_items', COUNT(*) FROM d;

WITH d AS (DELETE FROM payer_rate_schedules WHERE payer_id IN (SELECT id FROM _free_payers) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'payer_rate_schedules', COUNT(*) FROM d;

WITH d AS (DELETE FROM payers WHERE id IN (SELECT id FROM _free_payers) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'payers', COUNT(*) FROM d;

CREATE TEMP TABLE _free_codes ON COMMIT DROP AS
    SELECT id FROM _e2e_codes c
    WHERE NOT EXISTS (SELECT 1 FROM billing_charges WHERE service_code_id = c.id)
      AND NOT EXISTS (SELECT 1 FROM claim_lines WHERE service_code_id = c.id);

WITH d AS (DELETE FROM prior_authorization_service_codes WHERE service_code_id IN (SELECT id FROM _free_codes) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'prior_authorization_service_codes (codes)', COUNT(*) FROM d;

WITH d AS (DELETE FROM payer_rate_schedule_items WHERE service_code_id IN (SELECT id FROM _free_codes) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'payer_rate_schedule_items (codes)', COUNT(*) FROM d;

WITH d AS (DELETE FROM patient_cash_rates WHERE service_code_id IN (SELECT id FROM _free_codes) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'patient_cash_rates (codes)', COUNT(*) FROM d;

WITH d AS (DELETE FROM service_codes WHERE id IN (SELECT id FROM _free_codes) RETURNING 1)
INSERT INTO _cleanup_report SELECT 'service_codes', COUNT(*) FROM d;

-- ---------------------------------------------------------------------
-- E2E users: deactivate (never delete; history references them)
-- ---------------------------------------------------------------------
WITH d AS (UPDATE users SET is_active = FALSE, updated_at = NOW() WHERE first_name = 'E2E' AND is_active RETURNING 1)
INSERT INTO _cleanup_report SELECT 'users deactivated', COUNT(*) FROM d;

SELECT step, rows_removed FROM _cleanup_report WHERE rows_removed > 0 ORDER BY 1;

-- Dry run unless -v commit=1 was given.
\if :{?commit}
    COMMIT;
    \echo 'COMMITTED: test records removed.'
\else
    ROLLBACK;
    \echo 'DRY RUN ONLY (rolled back). Re-run with -v commit=1 to delete.'
\endif
