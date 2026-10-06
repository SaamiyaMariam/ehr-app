-- =========================================================
-- CLAIM SUBMISSIONS
-- =========================================================

-- One row per time a claim left the practice: mailed paper claim, claim the
-- user confirmed was submitted outside this application, or a submission a
-- configured clearinghouse adapter accepted. Nothing here is fabricated:
-- external_reference is whatever the user / adapter supplied.

CREATE TABLE IF NOT EXISTS claim_submissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    claim_id UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,

    submission_method VARCHAR(20) NOT NULL
        CHECK (submission_method IN ('electronic', 'paper', 'external')),
    resubmission_type VARCHAR(10) NOT NULL
        CHECK (resubmission_type IN ('new', 'amended', 'void')),
    payer_claim_control_number VARCHAR(50),

    submitted_on DATE NOT NULL,
    external_reference VARCHAR(100),
    clearinghouse VARCHAR(50),
    comment TEXT,

    created_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS claim_submissions_claim_idx ON claim_submissions (claim_id, created_at);
