-- =========================================================
-- PATIENT BILLING SETTINGS
-- =========================================================

CREATE TABLE IF NOT EXISTS patient_billing_settings (
    patient_id UUID PRIMARY KEY REFERENCES patients(id) ON DELETE CASCADE,

    billing_comments TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-- =========================================================
-- INSURANCE POLICIES
-- =========================================================

-- A patient may hold several policies at the same priority over time
-- (historical / future coverage), so (patient_id, priority) is NOT unique.
-- Policies are disabled via is_active rather than deleted.

CREATE TABLE IF NOT EXISTS insurance_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
    payer_id UUID NOT NULL REFERENCES payers(id) ON DELETE RESTRICT,

    priority VARCHAR(20) NOT NULL DEFAULT 'primary'
        CHECK (priority IN ('primary', 'secondary', 'tertiary', 'quaternary')),

    member_id VARCHAR(100),
    policy_group VARCHAR(100),
    plan_name VARCHAR(255),

    policy_comments TEXT,

    signature_on_file BOOLEAN NOT NULL DEFAULT FALSE,

    coverage_start DATE,
    coverage_end DATE,

    copay NUMERIC(10, 2) CHECK (copay >= 0),
    deductible NUMERIC(10, 2) CHECK (deductible >= 0),

    appointment_limit_type VARCHAR(20) NOT NULL DEFAULT 'unknown'
        CHECK (appointment_limit_type IN ('number', 'unlimited', 'unknown')),
    appointments_allowed INTEGER CHECK (appointments_allowed >= 0),
    appointments_expiration DATE,

    relationship_to_policy_holder VARCHAR(50),

    policy_holder_first_name VARCHAR(100),
    policy_holder_middle_name VARCHAR(100),
    policy_holder_last_name VARCHAR(100),

    policy_holder_date_of_birth DATE,
    policy_holder_sex VARCHAR(50),

    policy_holder_address_1 VARCHAR(255),
    policy_holder_address_2 VARCHAR(255),
    policy_holder_city VARCHAR(100),
    policy_holder_state VARCHAR(100),
    policy_holder_zip VARCHAR(30),

    msp_qualification VARCHAR(100),

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT insurance_policies_coverage_dates_check
        CHECK (
            coverage_start IS NULL
            OR coverage_end IS NULL
            OR coverage_end >= coverage_start
        )
);

CREATE INDEX IF NOT EXISTS insurance_policies_patient_id_idx
    ON insurance_policies (patient_id);

CREATE INDEX IF NOT EXISTS insurance_policies_payer_id_idx
    ON insurance_policies (payer_id);

CREATE INDEX IF NOT EXISTS insurance_policies_patient_active_idx
    ON insurance_policies (patient_id, is_active);
