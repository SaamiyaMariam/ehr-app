-- =========================================================
-- SERVICE CODES
-- =========================================================

-- code is intentionally NOT unique: the same billing code may exist in
-- several configurations / descriptions. Payer rate schedules come later.

CREATE TABLE IF NOT EXISTS service_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    code VARCHAR(50) NOT NULL,
    description TEXT NOT NULL,

    is_add_on BOOLEAN NOT NULL DEFAULT FALSE,
    allow_multiple_units BOOLEAN NOT NULL DEFAULT FALSE,

    standard_rate NUMERIC(10, 2) CHECK (standard_rate >= 0),

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS service_codes_code_idx
    ON service_codes (code);


-- =========================================================
-- PRIOR AUTHORIZATIONS
-- =========================================================

-- uses_remaining is not decremented here; claim submission / CMS-1500
-- generation will do that in a later module.

CREATE TABLE IF NOT EXISTS prior_authorizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    insurance_policy_id UUID NOT NULL
        REFERENCES insurance_policies(id) ON DELETE CASCADE,

    authorization_code VARCHAR(100) NOT NULL,

    applies_to_any_service_code BOOLEAN NOT NULL DEFAULT FALSE,

    start_date DATE,
    expiration_date DATE,

    uses_allowed INTEGER CHECK (uses_allowed >= 0),
    uses_remaining INTEGER CHECK (uses_remaining >= 0),

    usage_setting VARCHAR(30) NOT NULL DEFAULT 'once_per_service'
        CHECK (usage_setting IN ('once_per_service', 'per_unit')),

    comments TEXT,

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT prior_authorizations_uses_check
        CHECK (
            uses_allowed IS NULL
            OR uses_remaining IS NULL
            OR uses_remaining <= uses_allowed
        ),

    CONSTRAINT prior_authorizations_dates_check
        CHECK (
            start_date IS NULL
            OR expiration_date IS NULL
            OR expiration_date >= start_date
        )
);

CREATE INDEX IF NOT EXISTS prior_authorizations_policy_id_idx
    ON prior_authorizations (insurance_policy_id);

CREATE INDEX IF NOT EXISTS prior_authorizations_policy_active_idx
    ON prior_authorizations (insurance_policy_id, is_active);


-- =========================================================
-- PRIOR AUTHORIZATION <-> SERVICE CODES
-- =========================================================

-- Empty for authorizations with applies_to_any_service_code = TRUE.
-- Service codes are disabled, never deleted, so RESTRICT preserves history.

CREATE TABLE IF NOT EXISTS prior_authorization_service_codes (
    prior_authorization_id UUID NOT NULL
        REFERENCES prior_authorizations(id) ON DELETE CASCADE,
    service_code_id UUID NOT NULL
        REFERENCES service_codes(id) ON DELETE RESTRICT,

    PRIMARY KEY (prior_authorization_id, service_code_id)
);

CREATE INDEX IF NOT EXISTS prior_authorization_service_codes_service_code_idx
    ON prior_authorization_service_codes (service_code_id);
