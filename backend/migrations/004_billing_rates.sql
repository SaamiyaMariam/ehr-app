-- =========================================================
-- PRACTICE BILLING SETTINGS (singleton row)
-- =========================================================

CREATE TABLE IF NOT EXISTS practice_billing_settings (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),

    default_in_network_billing_method VARCHAR(20) NOT NULL DEFAULT 'electronic'
        CHECK (default_in_network_billing_method IN ('electronic', 'paper', 'external')),

    default_out_of_network_billing_method VARCHAR(20) NOT NULL DEFAULT 'paper'
        CHECK (default_out_of_network_billing_method IN ('electronic', 'paper', 'external')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO practice_billing_settings (id)
VALUES (TRUE)
ON CONFLICT (id) DO NOTHING;


-- =========================================================
-- PAYER BILLING METHOD / INSURANCE TYPE
-- =========================================================

-- payers.billing_method and payers.insurance_type already exist (001) but
-- were never written by the application, so every existing row is NULL.
-- NULL billing_method means "use the practice default".

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'payers_billing_method_check'
    ) THEN
        ALTER TABLE payers
            ADD CONSTRAINT payers_billing_method_check
            CHECK (
                billing_method IS NULL
                OR billing_method IN ('electronic', 'paper', 'external')
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'payers_insurance_type_check'
    ) THEN
        ALTER TABLE payers
            ADD CONSTRAINT payers_insurance_type_check
            CHECK (
                insurance_type IS NULL
                OR insurance_type IN (
                    'medicare',
                    'medicaid',
                    'tricare',
                    'champva',
                    'group_health_plan',
                    'feca_black_lung',
                    'other'
                )
            );
    END IF;
END
$$;


-- =========================================================
-- PAYER RATE SCHEDULES
-- =========================================================

-- Schedules are disabled, never deleted. Charges snapshot the resolved
-- rate, so editing a schedule never changes historical charges.

CREATE TABLE IF NOT EXISTS payer_rate_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payer_id UUID NOT NULL REFERENCES payers(id) ON DELETE RESTRICT,

    name VARCHAR(150) NOT NULL,

    use_standard_practice_rates BOOLEAN NOT NULL DEFAULT FALSE,

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Target for the composite FK that keeps clinician assignments on the
    -- same payer as their schedule.
    CONSTRAINT payer_rate_schedules_id_payer_unique UNIQUE (id, payer_id)
);

CREATE INDEX IF NOT EXISTS payer_rate_schedules_payer_idx
    ON payer_rate_schedules (payer_id, is_active);

CREATE TABLE IF NOT EXISTS payer_rate_schedule_items (
    rate_schedule_id UUID NOT NULL
        REFERENCES payer_rate_schedules(id) ON DELETE CASCADE,
    service_code_id UUID NOT NULL
        REFERENCES service_codes(id) ON DELETE RESTRICT,

    custom_rate NUMERIC(10, 2) NOT NULL CHECK (custom_rate >= 0),

    PRIMARY KEY (rate_schedule_id, service_code_id)
);

CREATE TABLE IF NOT EXISTS payer_clinician_rate_schedules (
    payer_id UUID NOT NULL REFERENCES payers(id) ON DELETE RESTRICT,
    clinician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    rate_schedule_id UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (payer_id, clinician_id),

    CONSTRAINT payer_clinician_rate_schedules_schedule_fkey
        FOREIGN KEY (rate_schedule_id, payer_id)
        REFERENCES payer_rate_schedules (id, payer_id)
        ON DELETE RESTRICT
);


-- =========================================================
-- PATIENT CASH RATES
-- =========================================================

CREATE TABLE IF NOT EXISTS patient_cash_rates (
    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
    service_code_id UUID NOT NULL REFERENCES service_codes(id) ON DELETE RESTRICT,

    rate NUMERIC(10, 2) NOT NULL CHECK (rate >= 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (patient_id, service_code_id)
);
