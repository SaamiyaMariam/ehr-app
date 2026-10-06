-- =========================================================
-- COMPOSITE KEYS (object-level integrity for remittance posting)
-- =========================================================

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'claims_id_payer_unique') THEN
        ALTER TABLE claims ADD CONSTRAINT claims_id_payer_unique UNIQUE (id, payer_id);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'claim_lines_id_claim_charge_unique') THEN
        ALTER TABLE claim_lines ADD CONSTRAINT claim_lines_id_claim_charge_unique UNIQUE (id, claim_id, charge_id);
    END IF;
END
$$;


-- =========================================================
-- INSURANCE PAYMENTS (manually posted remittances / EOBs)
-- =========================================================

-- amount may be 0 (zero-dollar EOB / denial acknowledgement).
CREATE TABLE IF NOT EXISTS insurance_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payer_id UUID NOT NULL REFERENCES payers(id) ON DELETE RESTRICT,

    payment_date DATE NOT NULL,
    amount NUMERIC(12, 2) NOT NULL CHECK (amount >= 0),

    payment_type VARCHAR(20) NOT NULL
        CHECK (payment_type IN ('check', 'eft', 'virtual_card', 'other')),
    reference_number VARCHAR(100),
    notes TEXT,

    status VARCHAR(10) NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'voided')),
    void_reason TEXT,
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,

    idempotency_key UUID NOT NULL UNIQUE,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT insurance_payments_id_payer_unique UNIQUE (id, payer_id),
    CONSTRAINT insurance_payments_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS insurance_payments_payer_idx ON insurance_payments (payer_id, payment_date);
CREATE INDEX IF NOT EXISTS insurance_payments_date_idx ON insurance_payments (payment_date);

-- One row per adjudicated claim line on a remittance. The composite keys
-- guarantee the payment's payer is the claim's payer and that the line,
-- claim and charge all belong together.
CREATE TABLE IF NOT EXISTS insurance_payment_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id UUID NOT NULL,
    payer_id UUID NOT NULL,
    claim_id UUID NOT NULL,
    claim_line_id UUID NOT NULL,
    charge_id UUID NOT NULL REFERENCES billing_charges(id) ON DELETE RESTRICT,

    amount_paid NUMERIC(12, 2) NOT NULL CHECK (amount_paid >= 0),
    allowed_amount NUMERIC(12, 2) CHECK (allowed_amount >= 0),

    -- The payer has finished adjudicating this line (paid, partially paid or
    -- denied). Remaining insurance responsibility can then move on to the
    -- next payer, the patient, or a write-off.
    is_final BOOLEAN NOT NULL DEFAULT TRUE,

    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'voided')),
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT insurance_payment_allocations_payment_fkey
        FOREIGN KEY (payment_id, payer_id) REFERENCES insurance_payments (id, payer_id) ON DELETE RESTRICT,
    CONSTRAINT insurance_payment_allocations_claim_fkey
        FOREIGN KEY (claim_id, payer_id) REFERENCES claims (id, payer_id) ON DELETE RESTRICT,
    CONSTRAINT insurance_payment_allocations_line_fkey
        FOREIGN KEY (claim_line_id, claim_id, charge_id) REFERENCES claim_lines (id, claim_id, charge_id) ON DELETE RESTRICT,

    CONSTRAINT insurance_payment_allocations_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS insurance_payment_allocations_payment_idx ON insurance_payment_allocations (payment_id);
CREATE INDEX IF NOT EXISTS insurance_payment_allocations_charge_idx ON insurance_payment_allocations (charge_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS insurance_payment_allocations_line_idx ON insurance_payment_allocations (claim_line_id) WHERE status = 'active';


-- =========================================================
-- ADJUSTMENTS / WRITE-OFFS
-- =========================================================

-- Reduce one party's balance on a charge without money changing hands.
-- Types ending in _writeoff are reported as write-offs.
CREATE TABLE IF NOT EXISTS billing_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    charge_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    party VARCHAR(10) NOT NULL CHECK (party IN ('patient', 'insurance')),
    adjustment_type VARCHAR(30) NOT NULL CHECK (adjustment_type IN (
        'contractual_writeoff',
        'payer_adjustment',
        'small_balance_writeoff',
        'bad_debt_writeoff',
        'courtesy_writeoff',
        'manual_adjustment'
    )),
    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    reason TEXT,
    reference VARCHAR(100),

    insurance_payment_id UUID REFERENCES insurance_payments(id) ON DELETE RESTRICT,
    insurance_allocation_id UUID REFERENCES insurance_payment_allocations(id) ON DELETE RESTRICT,

    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'voided')),
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    void_reason TEXT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT billing_adjustments_charge_fkey
        FOREIGN KEY (charge_id, patient_id) REFERENCES billing_charges (id, patient_id) ON DELETE RESTRICT,
    CONSTRAINT billing_adjustments_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS billing_adjustments_charge_idx ON billing_adjustments (charge_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS billing_adjustments_payment_idx ON billing_adjustments (insurance_payment_id);


-- =========================================================
-- RESPONSIBILITY TRANSFERS
-- =========================================================

-- Moves responsibility between insurance and patient (deductible, copay,
-- coinsurance, non-covered, corrections) as an auditable event instead of
-- overwriting the charge's split.
CREATE TABLE IF NOT EXISTS responsibility_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    charge_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    from_party VARCHAR(10) NOT NULL CHECK (from_party IN ('patient', 'insurance')),
    to_party VARCHAR(10) NOT NULL CHECK (to_party IN ('patient', 'insurance')),
    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    reason VARCHAR(20) NOT NULL CHECK (reason IN ('deductible', 'copay', 'coinsurance', 'noncovered', 'correction', 'other')),
    note TEXT,

    insurance_payment_id UUID REFERENCES insurance_payments(id) ON DELETE RESTRICT,
    insurance_allocation_id UUID REFERENCES insurance_payment_allocations(id) ON DELETE RESTRICT,

    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'voided')),
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    void_reason TEXT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT responsibility_transfers_charge_fkey
        FOREIGN KEY (charge_id, patient_id) REFERENCES billing_charges (id, patient_id) ON DELETE RESTRICT,
    CONSTRAINT responsibility_transfers_direction_check CHECK (from_party <> to_party),
    CONSTRAINT responsibility_transfers_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS responsibility_transfers_charge_idx ON responsibility_transfers (charge_id) WHERE status = 'active';


-- =========================================================
-- CHARGE BALANCES (v3: insurance payments, adjustments, transfers)
-- =========================================================
--
--   patient_responsibility   = initial patient share + transfers to patient - transfers to insurance
--   insurance_responsibility = initial insurance share - transfers to patient + transfers to insurance
--   patient_balance   = patient_responsibility   - patient payments   - patient adjustments
--   insurance_balance = insurance_responsibility - insurance payments - insurance adjustments
--   total_balance     = patient_balance + insurance_balance
--
-- Only active rows of posted payments count; voided charges contribute 0.

CREATE OR REPLACE VIEW billing_charge_balances AS
WITH patient_paid AS (
    SELECT a.charge_id, SUM(a.amount) AS amount
    FROM patient_payment_allocations a
    JOIN patient_payments p ON p.id = a.payment_id
    WHERE a.status = 'active' AND p.status = 'posted'
    GROUP BY a.charge_id
),
insurance_paid AS (
    SELECT a.charge_id, SUM(a.amount_paid) AS amount
    FROM insurance_payment_allocations a
    JOIN insurance_payments p ON p.id = a.payment_id
    WHERE a.status = 'active' AND p.status = 'posted'
    GROUP BY a.charge_id
),
adjusted AS (
    SELECT
        charge_id,
        SUM(amount) FILTER (WHERE party = 'patient') AS patient_amount,
        SUM(amount) FILTER (WHERE party = 'insurance') AS insurance_amount,
        SUM(amount) FILTER (WHERE adjustment_type LIKE '%\_writeoff') AS writeoffs
    FROM billing_adjustments
    WHERE status = 'active'
    GROUP BY charge_id
),
transferred AS (
    SELECT
        charge_id,
        SUM(amount) FILTER (WHERE to_party = 'patient') AS to_patient,
        SUM(amount) FILTER (WHERE to_party = 'insurance') AS to_insurance
    FROM responsibility_transfers
    WHERE status = 'active'
    GROUP BY charge_id
),
base AS (
    SELECT
        c.id,
        c.patient_id,
        c.total_charge,
        c.status,
        CASE WHEN c.status = 'voided' THEN 0
             ELSE c.patient_responsibility + COALESCE(t.to_patient, 0) - COALESCE(t.to_insurance, 0) END AS p_resp,
        CASE WHEN c.status = 'voided' THEN 0
             ELSE c.insurance_responsibility - COALESCE(t.to_patient, 0) + COALESCE(t.to_insurance, 0) END AS i_resp,
        COALESCE(pp.amount, 0) AS p_paid,
        COALESCE(ip.amount, 0) AS i_paid,
        COALESCE(ad.patient_amount, 0) AS p_adj,
        COALESCE(ad.insurance_amount, 0) AS i_adj,
        COALESCE(ad.writeoffs, 0) AS writeoffs,
        COALESCE(t.to_patient, 0) AS to_patient,
        COALESCE(t.to_insurance, 0) AS to_insurance
    FROM billing_charges c
    LEFT JOIN patient_paid pp ON pp.charge_id = c.id
    LEFT JOIN insurance_paid ip ON ip.charge_id = c.id
    LEFT JOIN adjusted ad ON ad.charge_id = c.id
    LEFT JOIN transferred t ON t.charge_id = c.id
)
SELECT
    id AS charge_id,
    patient_id,
    total_charge::numeric(12, 2) AS total_charge,
    p_resp::numeric(12, 2) AS patient_responsibility,
    i_resp::numeric(12, 2) AS insurance_responsibility,
    p_paid::numeric(12, 2) AS patient_payments,
    i_paid::numeric(12, 2) AS insurance_payments,
    p_adj::numeric(12, 2) AS patient_adjustments,
    i_adj::numeric(12, 2) AS insurance_adjustments,
    (p_resp - p_paid - p_adj)::numeric(12, 2) AS patient_balance,
    (i_resp - i_paid - i_adj)::numeric(12, 2) AS insurance_balance,
    ((p_resp - p_paid - p_adj) + (i_resp - i_paid - i_adj))::numeric(12, 2) AS total_balance,
    writeoffs::numeric(12, 2) AS writeoffs,
    to_patient::numeric(12, 2) AS transfers_to_patient,
    to_insurance::numeric(12, 2) AS transfers_to_insurance
FROM base;
