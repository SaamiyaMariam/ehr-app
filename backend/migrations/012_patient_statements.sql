-- =========================================================
-- PATIENT STATEMENTS
-- =========================================================

-- A statement is an immutable record of what a patient owed when it was
-- generated: the exact figures and lines (snapshot) and the PDF built from
-- them. Posting a payment afterwards never changes an old statement;
-- generating again creates a new statement.

CREATE SEQUENCE IF NOT EXISTS statement_number_seq START 1001;

CREATE TABLE IF NOT EXISTS patient_statements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Patient-facing statement number (the PDF never shows internal ids).
    statement_number VARCHAR(20) NOT NULL UNIQUE
        DEFAULT 'STM-' || LPAD(nextval('statement_number_seq')::text, 7, '0'),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,

    -- open_balance: services with an open patient balance right now.
    -- date_range:   patient-side activity between start_date and end_date,
    --               with the balance as of end_date.
    statement_type VARCHAR(20) NOT NULL CHECK (statement_type IN ('open_balance', 'date_range')),
    statement_date DATE NOT NULL,
    start_date DATE,
    end_date DATE,

    -- Patient balance when generated (as of end_date for a date-range
    -- statement) and unapplied patient credit on the account.
    balance_due NUMERIC(12, 2) NOT NULL CHECK (balance_due >= 0),
    credit_on_account NUMERIC(12, 2) NOT NULL DEFAULT 0 CHECK (credit_on_account >= 0),

    comment TEXT,

    snapshot JSONB NOT NULL,
    pdf BYTEA NOT NULL,
    sha256 VARCHAR(64) NOT NULL,

    -- Groups statements generated together by a batch run.
    batch_id UUID,

    generated_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT patient_statements_id_patient_unique UNIQUE (id, patient_id),
    CONSTRAINT patient_statements_range_check CHECK (
        (statement_type = 'open_balance' AND start_date IS NULL AND end_date IS NULL)
        OR (statement_type = 'date_range' AND start_date IS NOT NULL AND end_date IS NOT NULL AND end_date >= start_date)
    )
);

CREATE INDEX IF NOT EXISTS patient_statements_patient_idx ON patient_statements (patient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS patient_statements_batch_idx ON patient_statements (batch_id) WHERE batch_id IS NOT NULL;


-- =========================================================
-- CHARGE AGING (patient and insurance balances by age)
-- =========================================================

-- Aging basis: DATE OF SERVICE. A balance's age is today minus the date of
-- service of the charge it belongs to. Buckets: 0-30, 31-60, 61-90, 91+ days.
-- Reads billing_charge_balances, so it follows the same ledger as everything
-- else. Used by statement candidates and the aging reports.

CREATE OR REPLACE VIEW billing_charge_aging AS
SELECT
    b.charge_id,
    b.patient_id,
    c.clinician_id,
    c.payer_id,
    c.date_of_service,
    (CURRENT_DATE - c.date_of_service)::integer AS age_days,
    (CASE
        WHEN CURRENT_DATE - c.date_of_service <= 30 THEN '0-30'
        WHEN CURRENT_DATE - c.date_of_service <= 60 THEN '31-60'
        WHEN CURRENT_DATE - c.date_of_service <= 90 THEN '61-90'
        ELSE '91+'
    END)::text AS bucket,
    b.patient_balance,
    b.insurance_balance
FROM billing_charge_balances b
JOIN billing_charges c ON c.id = b.charge_id
WHERE c.status = 'active';
