-- =========================================================
-- CLAIM DOCUMENTS (generated CMS-1500 PDFs)
-- =========================================================

-- Each generation is kept as a new version; downloads read the latest.
CREATE TABLE IF NOT EXISTS claim_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,
    document_type VARCHAR(20) NOT NULL DEFAULT 'cms1500' CHECK (document_type IN ('cms1500')),
    version INTEGER NOT NULL CHECK (version > 0),

    -- CMS-1500 item 22 frequency: 1 original, 7 replacement, 8 void.
    frequency_code VARCHAR(1) NOT NULL CHECK (frequency_code IN ('1', '7', '8')),

    pdf BYTEA NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    page_count INTEGER NOT NULL CHECK (page_count > 0),

    generated_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT claim_documents_version_unique UNIQUE (claim_id, document_type, version)
);


-- =========================================================
-- SUPERBILLS
-- =========================================================

-- A superbill is a patient-facing itemized statement of services for
-- self-submission to (usually out-of-network) insurance. The PDF and the
-- line data used to build it are stored as an immutable record.
CREATE TABLE IF NOT EXISTS superbills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,

    total_charges NUMERIC(12, 2) NOT NULL CHECK (total_charges >= 0),
    total_paid NUMERIC(12, 2) NOT NULL CHECK (total_paid >= 0),

    snapshot JSONB NOT NULL,
    pdf BYTEA NOT NULL,
    sha256 VARCHAR(64) NOT NULL,

    generated_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT superbills_id_patient_unique UNIQUE (id, patient_id)
);

CREATE INDEX IF NOT EXISTS superbills_patient_idx ON superbills (patient_id, created_at);

CREATE TABLE IF NOT EXISTS superbill_charges (
    superbill_id UUID NOT NULL,
    charge_id UUID NOT NULL,
    patient_id UUID NOT NULL,

    PRIMARY KEY (superbill_id, charge_id),

    CONSTRAINT superbill_charges_superbill_fkey
        FOREIGN KEY (superbill_id, patient_id)
        REFERENCES superbills (id, patient_id)
        ON DELETE RESTRICT,

    CONSTRAINT superbill_charges_charge_fkey
        FOREIGN KEY (charge_id, patient_id)
        REFERENCES billing_charges (id, patient_id)
        ON DELETE RESTRICT
);
