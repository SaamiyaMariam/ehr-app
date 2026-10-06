-- Billing reconciliation check (READ-ONLY).
--
-- Returns one row per violation of a financial invariant; an empty result
-- means the ledger is consistent. It never modifies data.
--
--   psql "$DATABASE_URL" -f scripts/check_billing_reconciliation.sql
--
-- The backend integration tests run this exact file against their isolated
-- schema (internal/integration), so it is exercised on every `go test`.
--
-- Columns: check_name, object_id (the offending row), detail.

SELECT check_name, object_id::text AS object_id, detail
FROM (

    -- 1. Nobody can owe a negative amount on a service.
    SELECT 'negative_patient_balance' AS check_name, charge_id AS object_id,
           'patient balance ' || patient_balance AS detail
    FROM billing_charge_balances
    WHERE patient_balance < 0

    UNION ALL
    SELECT 'negative_insurance_balance', charge_id,
           'insurance balance ' || insurance_balance
    FROM billing_charge_balances
    WHERE insurance_balance < 0

    -- 2. A payment cannot be applied / refunded beyond what it holds.
    UNION ALL
    SELECT 'patient_payment_overapplied', payment_id,
           'unapplied amount ' || unapplied
    FROM patient_payment_credits
    WHERE unapplied < 0

    UNION ALL
    SELECT 'insurance_payment_overallocated', p.id,
           'payment ' || p.amount || ' but ' || SUM(a.amount_paid) || ' allocated'
    FROM insurance_payments p
    JOIN insurance_payment_allocations a ON a.payment_id = p.id AND a.status = 'active'
    WHERE p.status = 'posted'
    GROUP BY p.id, p.amount
    HAVING SUM(a.amount_paid) > p.amount

    -- 3. Responsibility is only ever moved, never created: the two sides of
    --    a live charge always add up to its total.
    UNION ALL
    SELECT 'responsibility_mismatch', b.charge_id,
           'patient ' || b.patient_responsibility || ' + insurance ' || b.insurance_responsibility
               || ' <> total ' || b.total_charge
    FROM billing_charge_balances b
    JOIN billing_charges c ON c.id = b.charge_id
    WHERE c.status = 'active'
      AND b.patient_responsibility + b.insurance_responsibility <> b.total_charge

    -- 4. Total balance = charge - everything that reduced it.
    UNION ALL
    SELECT 'balance_not_reconciled', b.charge_id,
           'total balance ' || b.total_balance || ' <> charge ' || b.total_charge
               || ' - payments/adjustments'
    FROM billing_charge_balances b
    JOIN billing_charges c ON c.id = b.charge_id
    WHERE c.status = 'active'
      AND b.total_balance <>
          b.total_charge - b.patient_payments - b.insurance_payments
                         - b.patient_adjustments - b.insurance_adjustments

    -- 5. The event ledger explains every balance exactly.
    UNION ALL
    SELECT 'ledger_mismatch', b.charge_id,
           'patient ' || b.patient_balance || ' / insurance ' || b.insurance_balance
               || ' vs ledger ' || COALESCE(l.patient, 0) || ' / ' || COALESCE(l.insurance, 0)
    FROM billing_charge_balances b
    JOIN billing_charges c ON c.id = b.charge_id
    LEFT JOIN (
        SELECT charge_id,
               SUM(effect * amount) FILTER (WHERE party = 'patient') AS patient,
               SUM(effect * amount) FILTER (WHERE party = 'insurance') AS insurance
        FROM billing_ledger_events
        WHERE status = 'active'
        GROUP BY charge_id
    ) l ON l.charge_id = b.charge_id
    WHERE c.status = 'active'
      AND (b.patient_balance <> COALESCE(l.patient, 0) OR b.insurance_balance <> COALESCE(l.insurance, 0))

    -- 6. Orphaned financial records.
    UNION ALL
    SELECT 'active_activity_on_voided_charge', c.id,
           'voided charge still has active financial records'
    FROM billing_charges c
    WHERE c.status = 'voided'
      AND (
          EXISTS (SELECT 1 FROM patient_payment_allocations a WHERE a.charge_id = c.id AND a.status = 'active')
          OR EXISTS (SELECT 1 FROM insurance_payment_allocations a WHERE a.charge_id = c.id AND a.status = 'active')
          OR EXISTS (SELECT 1 FROM billing_adjustments a WHERE a.charge_id = c.id AND a.status = 'active')
          OR EXISTS (SELECT 1 FROM responsibility_transfers t WHERE t.charge_id = c.id AND t.status = 'active')
      )

    UNION ALL
    SELECT 'active_allocation_on_voided_patient_payment', a.id,
           'allocation active but payment ' || p.id || ' is voided'
    FROM patient_payment_allocations a
    JOIN patient_payments p ON p.id = a.payment_id
    WHERE a.status = 'active' AND p.status = 'voided'

    UNION ALL
    SELECT 'active_allocation_on_voided_insurance_payment', a.id,
           'allocation active but payment ' || p.id || ' is voided'
    FROM insurance_payment_allocations a
    JOIN insurance_payments p ON p.id = a.payment_id
    WHERE a.status = 'active' AND p.status = 'voided'

    UNION ALL
    SELECT 'active_adjustment_on_voided_insurance_payment', ad.id,
           'adjustment active but payment ' || p.id || ' is voided'
    FROM billing_adjustments ad
    JOIN insurance_payments p ON p.id = ad.insurance_payment_id
    WHERE ad.status = 'active' AND p.status = 'voided'

    UNION ALL
    SELECT 'active_transfer_on_voided_insurance_payment', t.id,
           'transfer active but payment ' || p.id || ' is voided'
    FROM responsibility_transfers t
    JOIN insurance_payments p ON p.id = t.insurance_payment_id
    WHERE t.status = 'active' AND p.status = 'voided'

    UNION ALL
    SELECT 'duplicate_active_allocation', a.claim_line_id,
           'payment ' || a.payment_id || ' adjudicated this line ' || COUNT(*) || ' times'
    FROM insurance_payment_allocations a
    WHERE a.status = 'active'
    GROUP BY a.payment_id, a.claim_line_id
    HAVING COUNT(*) > 1

    -- 7. Claim / charge consistency.
    UNION ALL
    SELECT 'current_claim_line_on_voided_charge', cl.id,
           'claim line is current but its charge is voided'
    FROM claim_lines cl
    JOIN billing_charges c ON c.id = cl.charge_id
    WHERE cl.is_current AND c.status = 'voided'

    UNION ALL
    SELECT 'paid_claim_not_resolved', cm.id,
           'claim ' || cm.claim_number || ' is paid but line ' || cl.line_number || ' is not resolved'
    FROM claims cm
    JOIN claim_lines cl ON cl.claim_id = cm.id AND cl.is_current
    JOIN billing_charge_balances b ON b.charge_id = cl.charge_id
    WHERE cm.status = 'paid'
      AND (
          NOT EXISTS (
              SELECT 1 FROM insurance_payment_allocations a
              JOIN insurance_payments p ON p.id = a.payment_id
              WHERE a.claim_line_id = cl.id AND a.status = 'active' AND p.status = 'posted' AND a.is_final
          )
          OR (
              b.insurance_balance <> 0
              AND NOT EXISTS (
                  SELECT 1 FROM claim_lines l2
                  JOIN claims c2 ON c2.id = l2.claim_id
                  WHERE l2.charge_id = cl.charge_id AND l2.is_current AND c2.status <> 'voided'
                    AND array_position(ARRAY['primary', 'secondary', 'tertiary', 'quaternary'], l2.sequence::text)
                      > array_position(ARRAY['primary', 'secondary', 'tertiary', 'quaternary'], cl.sequence::text)
              )
          )
      )

    UNION ALL
    SELECT 'allocation_payer_mismatch', a.id,
           'allocation payer differs from its claim or payment'
    FROM insurance_payment_allocations a
    JOIN claims cm ON cm.id = a.claim_id
    JOIN insurance_payments p ON p.id = a.payment_id
    WHERE cm.payer_id <> p.payer_id OR a.payer_id <> p.payer_id

    UNION ALL
    SELECT 'allocation_patient_mismatch', a.id,
           'allocation charge belongs to a different patient than its claim line'
    FROM insurance_payment_allocations a
    JOIN claim_lines cl ON cl.id = a.claim_line_id
    JOIN billing_charges c ON c.id = a.charge_id
    WHERE cl.patient_id <> c.patient_id

) violations
ORDER BY check_name, object_id;
