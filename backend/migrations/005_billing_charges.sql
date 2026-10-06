-- =========================================================
-- COMPOSITE KEYS FOR OBJECT-LEVEL INTEGRITY
-- =========================================================

-- Lets child rows prove (via composite FKs) that a referenced policy belongs
-- to the same patient, preventing cross-patient ID substitution.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'insurance_policies_id_patient_unique'
    ) THEN
        ALTER TABLE insurance_policies
            ADD CONSTRAINT insurance_policies_id_patient_unique UNIQUE (id, patient_id);
    END IF;
END
$$;


-- =========================================================
-- PATIENT DIAGNOSES
-- =========================================================

-- Manually entered ICD-10-CM codes (format-validated; no bundled catalog).

CREATE TABLE IF NOT EXISTS patient_diagnoses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE CASCADE,

    icd10_code VARCHAR(8) NOT NULL
        CHECK (icd10_code ~ '^[A-Z][0-9][0-9A-Z](\.[0-9A-Z]{1,4})?$'),
    description VARCHAR(255) NOT NULL,

    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT patient_diagnoses_id_patient_unique UNIQUE (id, patient_id),
    CONSTRAINT patient_diagnoses_primary_active_check CHECK (NOT is_primary OR is_active)
);

CREATE UNIQUE INDEX IF NOT EXISTS patient_diagnoses_active_code_unique
    ON patient_diagnoses (patient_id, icd10_code)
    WHERE is_active;

CREATE UNIQUE INDEX IF NOT EXISTS patient_diagnoses_one_primary
    ON patient_diagnoses (patient_id)
    WHERE is_primary;


-- =========================================================
-- BILLING CHARGES (billable services)
-- =========================================================

-- patient_responsibility / insurance_responsibility are the INITIAL split
-- of total_charge. Later responsibility transfers, payments and adjustments
-- are separate auditable records; balances are derived, never stored.
--
-- rate_per_unit / rate_source / rate_schedule_id are a snapshot taken when
-- the charge is priced; later rate changes never alter existing charges.
--
-- Charges are voided, never deleted.

CREATE TABLE IF NOT EXISTS billing_charges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,
    clinician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    service_code_id UUID NOT NULL REFERENCES service_codes(id) ON DELETE RESTRICT,

    date_of_service DATE NOT NULL,

    units INTEGER NOT NULL CHECK (units BETWEEN 1 AND 999),

    modifier_1 VARCHAR(2) CHECK (modifier_1 ~ '^[A-Z0-9]{2}$'),
    modifier_2 VARCHAR(2) CHECK (modifier_2 ~ '^[A-Z0-9]{2}$'),
    modifier_3 VARCHAR(2) CHECK (modifier_3 ~ '^[A-Z0-9]{2}$'),
    modifier_4 VARCHAR(2) CHECK (modifier_4 ~ '^[A-Z0-9]{2}$'),

    place_of_service VARCHAR(2) NOT NULL DEFAULT '11'
        CHECK (place_of_service ~ '^[0-9]{2}$'),

    billing_method VARCHAR(40) NOT NULL CHECK (billing_method IN (
        'direct',
        'insurance_in_network_electronic',
        'insurance_in_network_paper',
        'insurance_in_network_external',
        'insurance_out_of_network_electronic',
        'insurance_out_of_network_paper',
        'insurance_out_of_network_external'
    )),

    insurance_policy_id UUID,
    payer_id UUID REFERENCES payers(id) ON DELETE RESTRICT,
    prior_authorization_id UUID REFERENCES prior_authorizations(id) ON DELETE RESTRICT,

    rate_per_unit NUMERIC(10, 2) NOT NULL CHECK (rate_per_unit >= 0),
    rate_source VARCHAR(30) NOT NULL
        CHECK (rate_source IN ('patient_cash_rate', 'payer_rate_schedule', 'standard_rate')),
    rate_schedule_id UUID REFERENCES payer_rate_schedules(id) ON DELETE RESTRICT,

    total_charge NUMERIC(12, 2) NOT NULL CHECK (total_charge >= 0),

    patient_responsibility NUMERIC(12, 2) NOT NULL CHECK (patient_responsibility >= 0),
    insurance_responsibility NUMERIC(12, 2) NOT NULL CHECK (insurance_responsibility >= 0),

    status VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'voided')),
    void_reason TEXT,
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE RESTRICT,

    notes TEXT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT billing_charges_id_patient_unique UNIQUE (id, patient_id),

    -- The policy must belong to the charge's patient.
    CONSTRAINT billing_charges_policy_patient_fkey
        FOREIGN KEY (insurance_policy_id, patient_id)
        REFERENCES insurance_policies (id, patient_id)
        ON DELETE RESTRICT,

    CONSTRAINT billing_charges_total_check
        CHECK (total_charge = units * rate_per_unit),

    CONSTRAINT billing_charges_responsibility_check
        CHECK (patient_responsibility + insurance_responsibility = total_charge),

    CONSTRAINT billing_charges_direct_check CHECK (
        (billing_method = 'direct'
            AND insurance_policy_id IS NULL
            AND payer_id IS NULL
            AND prior_authorization_id IS NULL
            AND insurance_responsibility = 0)
        OR
        (billing_method <> 'direct'
            AND insurance_policy_id IS NOT NULL
            AND payer_id IS NOT NULL)
    ),

    CONSTRAINT billing_charges_void_check CHECK (
        (status = 'voided') = (voided_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS billing_charges_patient_dos_idx
    ON billing_charges (patient_id, date_of_service);
CREATE INDEX IF NOT EXISTS billing_charges_dos_idx
    ON billing_charges (date_of_service);
CREATE INDEX IF NOT EXISTS billing_charges_clinician_idx
    ON billing_charges (clinician_id);
CREATE INDEX IF NOT EXISTS billing_charges_payer_idx
    ON billing_charges (payer_id);
CREATE INDEX IF NOT EXISTS billing_charges_policy_idx
    ON billing_charges (insurance_policy_id);
CREATE INDEX IF NOT EXISTS billing_charges_prior_authorization_idx
    ON billing_charges (prior_authorization_id);


-- Ordered diagnosis pointers (1-4) per charge.
CREATE TABLE IF NOT EXISTS billing_charge_diagnoses (
    charge_id UUID NOT NULL,
    diagnosis_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    pointer SMALLINT NOT NULL CHECK (pointer BETWEEN 1 AND 4),

    PRIMARY KEY (charge_id, diagnosis_id),
    CONSTRAINT billing_charge_diagnoses_pointer_unique UNIQUE (charge_id, pointer),

    CONSTRAINT billing_charge_diagnoses_charge_fkey
        FOREIGN KEY (charge_id, patient_id)
        REFERENCES billing_charges (id, patient_id)
        ON DELETE CASCADE,

    -- The diagnosis must belong to the charge's patient.
    CONSTRAINT billing_charge_diagnoses_diagnosis_fkey
        FOREIGN KEY (diagnosis_id, patient_id)
        REFERENCES patient_diagnoses (id, patient_id)
        ON DELETE RESTRICT
);


-- =========================================================
-- CHARGE BALANCES (derived)
-- =========================================================

-- Single definition of per-charge balances. Later migrations replace this
-- view (same columns) as payments, adjustments and transfers are added.
-- Voided charges contribute nothing.

CREATE OR REPLACE VIEW billing_charge_balances AS
SELECT
    c.id AS charge_id,
    c.patient_id,
    c.total_charge::numeric(12, 2) AS total_charge,

    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.patient_responsibility END)::numeric(12, 2)
        AS patient_responsibility,
    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.insurance_responsibility END)::numeric(12, 2)
        AS insurance_responsibility,

    0::numeric(12, 2) AS patient_payments,
    0::numeric(12, 2) AS insurance_payments,
    0::numeric(12, 2) AS patient_adjustments,
    0::numeric(12, 2) AS insurance_adjustments,

    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.patient_responsibility END)::numeric(12, 2)
        AS patient_balance,
    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.insurance_responsibility END)::numeric(12, 2)
        AS insurance_balance,
    (CASE WHEN c.status = 'voided' THEN 0 ELSE c.total_charge END)::numeric(12, 2)
        AS total_balance
FROM billing_charges c;
