-- =========================================================
-- PRACTICE BILLING PROFILE (singleton) / CLINICIAN BILLING PROFILES
-- =========================================================

-- Billing-provider data printed on claims. No banking or payment
-- credentials are stored here.

CREATE TABLE IF NOT EXISTS practice_billing_profile (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),

    practice_name VARCHAR(150),
    npi VARCHAR(10) CHECK (npi ~ '^[0-9]{10}$'),
    tax_id VARCHAR(9) CHECK (tax_id ~ '^[0-9]{9}$'),
    tax_id_type VARCHAR(3) NOT NULL DEFAULT 'ein' CHECK (tax_id_type IN ('ein', 'ssn')),
    taxonomy_code VARCHAR(10) CHECK (taxonomy_code ~ '^[0-9A-Z]{9}X$'),

    address_1 VARCHAR(255),
    address_2 VARCHAR(255),
    city VARCHAR(100),
    state VARCHAR(2) CHECK (state ~ '^[A-Z]{2}$'),
    zip VARCHAR(10) CHECK (zip ~ '^[0-9]{5}(-?[0-9]{4})?$'),
    phone VARCHAR(30),

    billing_contact_name VARCHAR(150),
    billing_contact_email VARCHAR(255),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO practice_billing_profile (id)
VALUES (TRUE)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS clinician_billing_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    npi VARCHAR(10) CHECK (npi ~ '^[0-9]{10}$'),
    taxonomy_code VARCHAR(10) CHECK (taxonomy_code ~ '^[0-9A-Z]{9}X$'),
    license_number VARCHAR(50),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-- =========================================================
-- CLAIMS
-- =========================================================

CREATE SEQUENCE IF NOT EXISTS claim_number_seq START 1001;

-- snapshot holds patient / insured / payer / practice data captured when
-- the claim was built, so later edits elsewhere never change a claim.
-- Only draft / validation_error / ready claims may refresh their snapshot.

CREATE TABLE IF NOT EXISTS claims (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_number VARCHAR(20) NOT NULL UNIQUE
        DEFAULT 'CLM-' || LPAD(nextval('claim_number_seq')::text, 7, '0'),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,
    insurance_policy_id UUID NOT NULL,
    payer_id UUID NOT NULL REFERENCES payers(id) ON DELETE RESTRICT,

    sequence VARCHAR(20) NOT NULL DEFAULT 'primary'
        CHECK (sequence IN ('primary', 'secondary', 'tertiary', 'quaternary')),

    submission_method VARCHAR(20) NOT NULL
        CHECK (submission_method IN ('electronic', 'paper', 'external')),

    status VARCHAR(30) NOT NULL DEFAULT 'draft' CHECK (status IN (
        'draft',
        'validation_error',
        'ready',
        'pending_submission',
        'submitted',
        'sent',
        'paper_generated',
        'externally_submitted',
        'rejected_new',
        'rejected',
        'resubmitted',
        'paid',
        'voided'
    )),

    resubmission_type VARCHAR(10) NOT NULL DEFAULT 'new'
        CHECK (resubmission_type IN ('new', 'amended', 'void')),
    payer_claim_control_number VARCHAR(50),

    -- Previous-sequence claim this claim follows (secondary after primary).
    previous_claim_id UUID REFERENCES claims(id) ON DELETE RESTRICT,

    total_billed NUMERIC(12, 2) NOT NULL CHECK (total_billed >= 0),

    snapshot JSONB NOT NULL,
    validation JSONB,
    validated_at TIMESTAMPTZ,

    external_reference VARCHAR(100),
    submitted_at TIMESTAMPTZ,
    -- Status to restore if every payment that marked the claim paid is voided.
    pre_paid_status VARCHAR(30),

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT claims_id_patient_unique UNIQUE (id, patient_id),

    CONSTRAINT claims_policy_patient_fkey
        FOREIGN KEY (insurance_policy_id, patient_id)
        REFERENCES insurance_policies (id, patient_id)
        ON DELETE RESTRICT,

    CONSTRAINT claims_resubmission_control_number_check CHECK (
        resubmission_type = 'new' OR payer_claim_control_number IS NOT NULL
    )
);

CREATE INDEX IF NOT EXISTS claims_patient_idx ON claims (patient_id);
CREATE INDEX IF NOT EXISTS claims_payer_idx ON claims (payer_id);
CREATE INDEX IF NOT EXISTS claims_status_idx ON claims (status);
CREATE INDEX IF NOT EXISTS claims_created_idx ON claims (created_at);


-- Claim lines snapshot the billed service. is_current is cleared when the
-- claim is voided, so the charge can be billed again; one current line per
-- charge per sequence prevents double billing the same payer sequence.
CREATE TABLE IF NOT EXISTS claim_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_id UUID NOT NULL,
    charge_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    sequence VARCHAR(20) NOT NULL
        CHECK (sequence IN ('primary', 'secondary', 'tertiary', 'quaternary')),

    line_number SMALLINT NOT NULL CHECK (line_number BETWEEN 1 AND 50),

    date_of_service DATE NOT NULL,

    service_code_id UUID NOT NULL REFERENCES service_codes(id) ON DELETE RESTRICT,
    service_code VARCHAR(50) NOT NULL,
    service_description TEXT NOT NULL,

    units INTEGER NOT NULL CHECK (units > 0),
    modifier_1 VARCHAR(2),
    modifier_2 VARCHAR(2),
    modifier_3 VARCHAR(2),
    modifier_4 VARCHAR(2),
    place_of_service VARCHAR(2) NOT NULL,

    rate NUMERIC(10, 2) NOT NULL CHECK (rate >= 0),
    line_total NUMERIC(12, 2) NOT NULL CHECK (line_total >= 0),

    rendering_clinician_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    rendering_name VARCHAR(255) NOT NULL,
    rendering_npi VARCHAR(10),
    rendering_taxonomy VARCHAR(10),

    -- Letters A-L referencing claim_diagnoses.position (CMS-1500 24E).
    diagnosis_pointers VARCHAR(4) NOT NULL DEFAULT '' CHECK (diagnosis_pointers ~ '^[A-L]{0,4}$'),

    prior_authorization_id UUID REFERENCES prior_authorizations(id) ON DELETE RESTRICT,
    prior_authorization_code VARCHAR(100),

    is_current BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT claim_lines_claim_fkey
        FOREIGN KEY (claim_id, patient_id)
        REFERENCES claims (id, patient_id)
        ON DELETE RESTRICT,

    CONSTRAINT claim_lines_charge_fkey
        FOREIGN KEY (charge_id, patient_id)
        REFERENCES billing_charges (id, patient_id)
        ON DELETE RESTRICT,

    CONSTRAINT claim_lines_line_number_unique UNIQUE (claim_id, line_number),
    CONSTRAINT claim_lines_charge_claim_unique UNIQUE (claim_id, charge_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS claim_lines_current_charge_sequence_unique
    ON claim_lines (charge_id, sequence)
    WHERE is_current;

CREATE INDEX IF NOT EXISTS claim_lines_charge_idx ON claim_lines (charge_id);


CREATE TABLE IF NOT EXISTS claim_diagnoses (
    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 12),

    icd10_code VARCHAR(8) NOT NULL,
    description VARCHAR(255) NOT NULL,

    PRIMARY KEY (claim_id, position)
);


-- Append-only lifecycle log.
CREATE TABLE IF NOT EXISTS claim_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,

    event_type VARCHAR(40) NOT NULL,
    from_status VARCHAR(30),
    to_status VARCHAR(30),

    source VARCHAR(20) NOT NULL DEFAULT 'user'
        CHECK (source IN ('user', 'system', 'clearinghouse', 'payer')),
    message TEXT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS claim_history_claim_idx ON claim_history (claim_id, created_at);
CREATE INDEX IF NOT EXISTS claim_history_created_idx ON claim_history (created_at);


CREATE TABLE IF NOT EXISTS claim_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,
    author_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    comment TEXT NOT NULL CHECK (length(comment) BETWEEN 1 AND 2000),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS claim_comments_claim_idx ON claim_comments (claim_id, created_at);


-- =========================================================
-- PRIOR AUTHORIZATION USAGE LEDGER
-- =========================================================

-- One row per (authorization, charge): a billed service consumes its
-- authorization at most once, no matter how often it is resubmitted.
CREATE TABLE IF NOT EXISTS prior_authorization_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    prior_authorization_id UUID NOT NULL REFERENCES prior_authorizations(id) ON DELETE RESTRICT,
    charge_id UUID NOT NULL REFERENCES billing_charges(id) ON DELETE RESTRICT,
    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,

    uses_consumed INTEGER NOT NULL CHECK (uses_consumed > 0),
    usage_setting VARCHAR(30) NOT NULL,
    milestone VARCHAR(40) NOT NULL,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT prior_authorization_usage_once UNIQUE (prior_authorization_id, charge_id)
);
