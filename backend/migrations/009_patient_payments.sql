-- =========================================================
-- PATIENT PAYMENTS
-- =========================================================

-- Money received from patients. No card data is ever stored: card
-- payments processed elsewhere are recorded as method 'external_card'
-- with an optional processor reference.
--
-- Payments are voided, never deleted. idempotency_key makes a retried
-- "post payment" request return the original payment instead of posting
-- the money twice.

CREATE TABLE IF NOT EXISTS patient_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,

    payment_date DATE NOT NULL,
    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),

    method VARCHAR(20) NOT NULL
        CHECK (method IN ('cash', 'check', 'external_card', 'external_other')),
    reference_number VARCHAR(100),
    check_number VARCHAR(50),
    notes TEXT,

    status VARCHAR(10) NOT NULL DEFAULT 'posted' CHECK (status IN ('posted', 'voided')),
    void_reason TEXT,
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,

    idempotency_key UUID NOT NULL UNIQUE,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT patient_payments_id_patient_unique UNIQUE (id, patient_id),
    CONSTRAINT patient_payments_check_number_check CHECK (method <> 'check' OR check_number IS NOT NULL),
    CONSTRAINT patient_payments_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS patient_payments_patient_idx ON patient_payments (patient_id, payment_date);
CREATE INDEX IF NOT EXISTS patient_payments_date_idx ON patient_payments (payment_date);

-- How a payment is applied to charges. Unapplied money is patient credit.
CREATE TABLE IF NOT EXISTS patient_payment_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id UUID NOT NULL,
    charge_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),

    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'voided')),
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Payment and charge must belong to the same patient.
    CONSTRAINT patient_payment_allocations_payment_fkey
        FOREIGN KEY (payment_id, patient_id) REFERENCES patient_payments (id, patient_id) ON DELETE RESTRICT,
    CONSTRAINT patient_payment_allocations_charge_fkey
        FOREIGN KEY (charge_id, patient_id) REFERENCES billing_charges (id, patient_id) ON DELETE RESTRICT,

    CONSTRAINT patient_payment_allocations_void_check CHECK ((status = 'voided') = (voided_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS patient_payment_allocations_payment_idx ON patient_payment_allocations (payment_id);
CREATE INDEX IF NOT EXISTS patient_payment_allocations_charge_idx ON patient_payment_allocations (charge_id) WHERE status = 'active';

-- Money returned to the patient out of a payment's unapplied credit.
CREATE TABLE IF NOT EXISTS patient_payment_refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    refund_date DATE NOT NULL,
    method VARCHAR(20) NOT NULL
        CHECK (method IN ('cash', 'check', 'external_card', 'external_other')),
    reference_number VARCHAR(100),
    reason TEXT NOT NULL,

    idempotency_key UUID NOT NULL UNIQUE,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT patient_payment_refunds_payment_fkey
        FOREIGN KEY (payment_id, patient_id) REFERENCES patient_payments (id, patient_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS patient_payment_refunds_payment_idx ON patient_payment_refunds (payment_id);


-- =========================================================
-- CHARGE BALANCES (v2: patient payments)
-- =========================================================

CREATE OR REPLACE VIEW billing_charge_balances AS
WITH patient_paid AS (
    SELECT a.charge_id, SUM(a.amount) AS amount
    FROM patient_payment_allocations a
    JOIN patient_payments p ON p.id = a.payment_id
    WHERE a.status = 'active' AND p.status = 'posted'
    GROUP BY a.charge_id
)
SELECT
    c.id AS charge_id,
    c.patient_id,
    c.total_charge::numeric(12, 2) AS total_charge,

    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.patient_responsibility END)::numeric(12, 2)
        AS patient_responsibility,
    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.insurance_responsibility END)::numeric(12, 2)
        AS insurance_responsibility,

    COALESCE(pp.amount, 0)::numeric(12, 2) AS patient_payments,
    0::numeric(12, 2) AS insurance_payments,
    0::numeric(12, 2) AS patient_adjustments,
    0::numeric(12, 2) AS insurance_adjustments,

    ((CASE WHEN c.status = 'voided' THEN 0 ELSE c.patient_responsibility END) - COALESCE(pp.amount, 0))::numeric(12, 2)
        AS patient_balance,
    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.insurance_responsibility END)::numeric(12, 2)
        AS insurance_balance,
    ((CASE WHEN c.status = 'voided' THEN 0 ELSE c.total_charge END) - COALESCE(pp.amount, 0))::numeric(12, 2)
        AS total_balance
FROM billing_charges c
LEFT JOIN patient_paid pp ON pp.charge_id = c.id;
