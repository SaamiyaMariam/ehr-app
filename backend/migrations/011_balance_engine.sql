-- =========================================================
-- BALANCE ENGINE
-- =========================================================
--
-- billing_charge_balances (migration 010) is the single definition of what
-- is owed on a charge. Everything here is derived from it and from the
-- append-only financial records; no balance is ever stored or edited.
--
--   patient_payment_credits   unapplied patient money, per posted payment
--   billing_patient_balances  per-patient totals (patient / insurance /
--                             total outstanding / unapplied credit)
--   billing_ledger_events     one row per financial event on a charge, so
--                             any balance can be explained line by line
--
-- Dashboards, reports, statements and the patient billing summary all read
-- these views instead of re-deriving balances.

-- Money received from a patient that is not applied to a service and has not
-- been refunded.
CREATE OR REPLACE VIEW patient_payment_credits AS
SELECT
    p.id AS payment_id,
    p.patient_id,
    p.payment_date,
    (p.amount - COALESCE(a.applied, 0) - COALESCE(f.refunded, 0))::numeric(12, 2) AS unapplied
FROM patient_payments p
LEFT JOIN (
    SELECT payment_id, SUM(amount) AS applied
    FROM patient_payment_allocations
    WHERE status = 'active'
    GROUP BY payment_id
) a ON a.payment_id = p.id
LEFT JOIN (
    SELECT payment_id, SUM(amount) AS refunded
    FROM patient_payment_refunds
    GROUP BY payment_id
) f ON f.payment_id = p.id
WHERE p.status = 'posted';


CREATE OR REPLACE VIEW billing_patient_balances AS
SELECT
    pt.id AS patient_id,
    COALESCE(b.patient_balance, 0)::numeric(12, 2) AS patient_balance,
    COALESCE(b.insurance_balance, 0)::numeric(12, 2) AS insurance_balance,
    (COALESCE(b.patient_balance, 0) + COALESCE(b.insurance_balance, 0))::numeric(12, 2) AS total_outstanding,
    COALESCE(cr.credit, 0)::numeric(12, 2) AS unallocated_credit,
    COALESCE(b.open_charges, 0)::integer AS open_charges
FROM patients pt
LEFT JOIN (
    SELECT
        patient_id,
        SUM(patient_balance) AS patient_balance,
        SUM(insurance_balance) AS insurance_balance,
        COUNT(*) FILTER (WHERE total_balance <> 0) AS open_charges
    FROM billing_charge_balances
    GROUP BY patient_id
) b ON b.patient_id = pt.id
LEFT JOIN (
    SELECT patient_id, SUM(unapplied) AS credit
    FROM patient_payment_credits
    GROUP BY patient_id
) cr ON cr.patient_id = pt.id;


-- effect: +1 raises what the party owes, -1 lowers it. Only rows whose
-- status is 'active' count toward balances; voided rows stay visible so the
-- history is complete.
CREATE OR REPLACE VIEW billing_ledger_events AS

SELECT
    c.id AS charge_id, c.patient_id,
    c.date_of_service AS occurred_on, c.created_at AS recorded_at,
    'charge'::text AS event_type, 'patient'::text AS party, 1::smallint AS effect,
    c.patient_responsibility::numeric(12, 2) AS amount,
    c.status::text AS status, c.id AS source_id,
    'Patient responsibility at charge'::text AS detail
FROM billing_charges c
WHERE c.patient_responsibility > 0

UNION ALL
SELECT
    c.id, c.patient_id, c.date_of_service, c.created_at,
    'charge', 'insurance', 1::smallint,
    c.insurance_responsibility::numeric(12, 2),
    c.status::text, c.id,
    'Insurance responsibility at charge'
FROM billing_charges c
WHERE c.insurance_responsibility > 0

UNION ALL
SELECT
    a.charge_id, a.patient_id, p.payment_date, a.created_at,
    'patient_payment', 'patient', (-1)::smallint,
    a.amount::numeric(12, 2),
    (CASE WHEN a.status = 'active' AND p.status = 'posted' THEN 'active' ELSE 'voided' END)::text,
    p.id,
    'Patient payment applied'
FROM patient_payment_allocations a
JOIN patient_payments p ON p.id = a.payment_id

UNION ALL
SELECT
    a.charge_id, cl.patient_id, p.payment_date, a.created_at,
    'insurance_payment', 'insurance', (-1)::smallint,
    a.amount_paid::numeric(12, 2),
    (CASE WHEN a.status = 'active' AND p.status = 'posted' THEN 'active' ELSE 'voided' END)::text,
    p.id,
    'Insurance payment'
FROM insurance_payment_allocations a
JOIN insurance_payments p ON p.id = a.payment_id
JOIN claim_lines cl ON cl.id = a.claim_line_id
WHERE a.amount_paid > 0

UNION ALL
SELECT
    ad.charge_id, ad.patient_id, ad.created_at::date, ad.created_at,
    'adjustment', ad.party::text, (-1)::smallint,
    ad.amount::numeric(12, 2),
    ad.status::text, ad.id,
    ad.adjustment_type::text
FROM billing_adjustments ad

UNION ALL
SELECT
    t.charge_id, t.patient_id, t.created_at::date, t.created_at,
    'transfer_out', t.from_party::text, (-1)::smallint,
    t.amount::numeric(12, 2),
    t.status::text, t.id,
    t.reason::text
FROM responsibility_transfers t

UNION ALL
SELECT
    t.charge_id, t.patient_id, t.created_at::date, t.created_at,
    'transfer_in', t.to_party::text, 1::smallint,
    t.amount::numeric(12, 2),
    t.status::text, t.id,
    t.reason::text
FROM responsibility_transfers t;
