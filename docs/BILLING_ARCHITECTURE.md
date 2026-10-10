# Billing architecture

This document describes what is **actually implemented** in the billing module
of this EHR / practice-management application. Where something is a boundary
rather than a feature (clearinghouse, ERA, card processing) it says so.

- Backend: Go, `net/http`, pgx / PostgreSQL, JWT, bcrypt (`backend/`)
- Frontend: Next.js 16, React 19, TypeScript, Tailwind (`frontend/`)
- Browser checks: Playwright driving an installed Chrome (`e2e/`)
- Money: `NUMERIC(…, 2)` in PostgreSQL, **integer cents** in Go, decimal strings
  in JSON. The browser formats and previews but never decides an amount.

Related files: [`ROUTE_SECURITY_MATRIX.md`](ROUTE_SECURITY_MATRIX.md) (generated),
[`../scripts/check_billing_reconciliation.sql`](../scripts/check_billing_reconciliation.sql),
[`../scripts/cleanup_e2e_data.sql`](../scripts/cleanup_e2e_data.sql).

---

## 1. Entity relationships

```
patients ──< insurance_policies ──< prior_authorizations ──< prior_authorization_service_codes >── service_codes
   │              │  (priority: primary…quaternary)             └─< prior_authorization_usage (one row per authorization + charge)
   │              └── payers ──< payer_rate_schedules ──< payer_rate_schedule_items >── service_codes
   │                       └──< payer_clinician_rate_schedules >── users (clinicians)
   ├──< patient_diagnoses
   ├──< patient_cash_rates >── service_codes
   ├──< billing_charges ──< billing_charge_diagnoses
   │         │  ├── insurance_policy_id / payer_id / prior_authorization_id / rate_schedule_id (snapshot of how it was priced)
   │         ├──< claim_lines >── claims ──< claim_history, claim_comments, claim_documents (CMS-1500 PDFs), claim_submissions
   │         │                      └── previous_claim_id → claims (secondary follows primary …)
   │         ├──< patient_payment_allocations >── patient_payments ──< patient_payment_refunds
   │         ├──< insurance_payment_allocations >── insurance_payments (payer)  [also → claim_lines]
   │         ├──< billing_adjustments           (party: patient | insurance)
   │         └──< responsibility_transfers      (patient ⇄ insurance)
   ├──< patient_statements (immutable snapshot + PDF)
   └──< superbills ──< superbill_charges
```

Object-level integrity is enforced twice: in handlers (clear 400/404 messages)
and in the database with **composite foreign keys**, e.g.
`(charge_id, patient_id) → billing_charges(id, patient_id)`,
`(claim_id, payer_id) → claims(id, payer_id)` and
`(claim_line_id, claim_id, charge_id) → claim_lines(...)`. A request that
substitutes another patient's or payer's id cannot be stored even if a handler
check were missed.

## 2. Insurance policy model

`insurance_policies`: patient, payer, `priority` (primary / secondary /
tertiary / quaternary), member id, group, plan, coverage dates, copay,
deductible, policy-holder details, signature on file, `is_active`.
`(patient_id, priority)` is deliberately **not** unique (history, future
policies). Policies may be saved incomplete; completeness is checked by claim
validation. Policies are disabled, not deleted. **Disabling a policy disables
its prior authorizations**; an authorization cannot be enabled while its policy
is disabled, and re-enabling the policy does not silently re-enable it.

## 3. Prior authorizations

An authorization belongs to one policy and applies to any service code or a
listed set. It has optional dates and `uses_allowed` / `uses_remaining`, and a
`usage_setting` (`once_per_service` or `per_unit`). A charge may reference an
authorization of **its own policy** that covers the service code and date.

Usage is consumed when a claim **leaves the practice** (electronic submission,
CMS-1500 generation, or "mark submitted externally"), once per
(authorization, charge) — `prior_authorization_usage` has a unique key, so
resubmissions, repeated clicks and concurrent requests never consume it twice
(the decrement itself is a guarded `UPDATE … WHERE uses_remaining >= n`).
Authorizations belong to the primary policy; secondary and later claims never
carry or consume them.

## 4. Service codes and rate resolution

`service_codes` hold code, description, add-on flags and an optional standard
rate. `resolveRate` (`billing/rates.go`) is the **only** place a charge's rate
is decided:

| Billing | Order |
|---|---|
| Direct (patient pays) | patient cash rate → standard rate |
| Insurance | the clinician's assigned active schedule for the payer → the payer's only active schedule → standard rate |

A payer schedule can be "use standard practice rates" or list custom rates per
service code (a blank custom rate falls back to the standard rate). No rate
configured is an error, never a $0 charge.

## 5. Charge lifecycle

A charge (billable service) stores the **priced snapshot** — `rate_per_unit`,
`rate_source`, `rate_schedule_id`, `total_charge = units × rate` — plus the
*initial* split into `patient_responsibility` and `insurance_responsibility`
(default: the policy copay for the patient, never an invented deductible).
Later rate changes never alter an existing charge. Charges are **voided, never
deleted**, and cannot be edited or voided once they are on a claim or have any
financial activity (payments, adjustments, transfers).

Display status: `open` → `on_claim` (current non-paid claim) → `closed`
(nothing owed) or `voided`.

## 6. Claim lifecycle

A claim bills **one payer at one sequence** (`primary`, `secondary`,
`tertiary`, `quaternary`) with one submission method. It captures a JSON
**snapshot** (patient, insured, payer, practice, policy, other insurance,
earlier-payer adjudication) and snapshot **lines** (service, units, rate,
rendering clinician / NPI, diagnosis pointers). One current line per charge per
sequence is enforced by a unique index.

Statuses and the single transition table (`billing/claim_rules.go`):

```
draft ─────────► ready / validation_error ─► (resubmission / submission)
draft / validation_error / ready  : editable, re-validated automatically
ready ─► pending_submission | submitted | resubmitted | paper_generated | externally_submitted | voided
pending_submission ─► submitted | rejected_new | ready
submitted / sent / resubmitted ─► sent | rejected_new | paid | draft
paper_generated ─► submitted | resubmitted | rejected_new | paid | draft | voided
externally_submitted ─► rejected_new | paid | draft | voided
rejected_new ─► rejected (reviewed) | draft        rejected ─► draft
paid ─► (back to the status it had before) | draft           voided: terminal
```

- **Validation** (`validateClaim`) checks the data the practice controls
  (patient / insured / payer / practice / NPI / diagnoses / lines). It is **not**
  payer-specific edit checking and not X12 compliance; a payer may still reject.
- **Paper** claims: generate the CMS-1500-compatible PDF (`paper_generated`),
  then mark mailed. **External**: the biller confirms submission made outside
  the app. **Electronic**: see §14.
- **Rejection**: record the rejection (`rejected_new`), mark reviewed
  (`rejected`), start a resubmission (new / amended / void, with the payer's
  control number), which reopens the claim as a draft.
- Reopening a claim is refused while insurance payments are posted to it or a
  follow-on claim exists (editing rewrites claim lines those records point at).
- **Paid** is not "a payment arrived": a claim becomes `paid` only when **every
  current line was finally adjudicated by the payer and no insurance balance
  remains on it** (or the remainder was forwarded to a later-sequence claim).
  Voiding the payments that resolved it restores its earlier status.

## 7. Patient payments

`patient_payments` (cash, check, `external_card`, `external_other`; an
idempotency key per payment). Allocation to charges is explicit or
"oldest balance first" and **never exceeds a service's open patient balance**;
money not applied is credit. Allocations can be unapplied (voided); credit can
be refunded (`patient_payment_refunds`); a payment can be voided only if it has
no refunds. **Card data is never stored**: a processed-elsewhere card payment is
recorded with an optional processor reference, and anything that looks like a
card number in a reference / note is rejected.

## 8. Insurance payments (manual remittance posting)

`insurance_payments` (check, EFT, virtual card, other) may be **$0.00** (an EOB).
A remittance is a list of adjudicated **claim lines**; each line carries:

- amount paid, optional allowed amount, and `is_final` (the payer is finished
  with the line — paid, partially paid or denied);
- **adjustments** (`contractual_writeoff`, `payer_adjustment`, plus
  `small_balance_writeoff`, `bad_debt_writeoff`, `courtesy_writeoff`,
  `manual_adjustment`) — insurance-side only for the first two;
- **responsibility transfers** to the patient (`deductible`, `copay`,
  `coinsurance`, `noncovered`, `other`).

Posting is **one transaction**: validate every line (the line belongs to the
stated claim, the claim is billed to the payment's payer and has been submitted,
amounts have ≤ 2 decimals and are non-negative, allowed ≤ billed, paid ≤ what
the payment still holds, paid + adjusted + transferred ≤ the service's insurance
balance), write allocations / adjustments / transfers, check no balance went
negative, refresh affected claims. Any failure rolls back the **whole**
remittance. More lines can be added to a posted payment; a payment is voided as
a whole (allocations, adjustments and transfers are voided with it — nothing is
deleted) and a void that would leave a negative patient balance, or that a
follow-on claim depends on, is refused. Standalone adjustments and transfers on
a charge exist too (and can be voided individually unless they belong to a
remittance). Two guards keep them from stranding or double-billing money:

- a **self-pay** (`direct`) service has no payer, so insurance adjustments and
  transfers to / from insurance are refused on it;
- while a **later-sequence claim is still open**, the forwarded balance belongs
  to that claim's payer: a standalone insurance write-off or an insurance →
  patient transfer is refused (post the next payer's response to that claim, or
  void the claim first).

Payer overpayments / interest / ERA import are **not** modelled (see §14).

## 9. Balances (one source of truth)

Nothing is stored; everything derives from the append-only records through
views (migrations 010–011):

```
patient_responsibility   = initial patient share + transfers to patient − transfers to insurance
insurance_responsibility = initial insurance share − transfers to patient + transfers to insurance
patient_balance   = patient_responsibility   − patient payments applied   − patient adjustments
insurance_balance = insurance_responsibility − insurance payments applied − insurance adjustments
total_balance     = patient_balance + insurance_balance
```

Only active rows of posted payments count; voided charges contribute 0. Views:
`billing_charge_balances` (per charge), `billing_patient_balances` (patient /
insurance / total outstanding / unapplied credit), `patient_payment_credits`,
`billing_ledger_events` (one row per financial event, so any balance can be
explained line by line), `billing_charge_aging`. Responsibility transfers
preserve total responsibility; only payments, adjustments and write-offs
reduce it. The dashboard, reports, statements and the patient summary all read
these views.

`scripts/check_billing_reconciliation.sql` is a read-only invariant check
(negative balances, over-applied payments, responsibility / ledger mismatches,
orphaned records on voided charges or payments, paid claims that are not
resolved…). The integration suite runs it after every balance scenario and once
more at the end, and proves it detects deliberately corrupted data.

## 10. Statements

`patient_statements` stores the exact content (JSON snapshot), the PDF and its
sha256. **Open balance** statements list services with an open patient balance
(current patient responsibility, payments, adjustments, balance). **Date range**
statements list patient-side ledger events in the range — charges by date of
service, payments by payment date, adjustments / transfers by the day recorded —
with the balance carried in and the balance as of the end date. Insurance
balances are never shown as patient due; unapplied credit is shown separately.
A statement never changes (downloads return the stored bytes); a later payment
appears on the next statement. Statements are generated in one consistent
(`REPEATABLE READ`) read so their lines, totals and credit always agree. If a
period ends with the patient ahead (e.g. a deposit dated before the service it
was applied to) the stored balance is 0.00 and the overpayment is shown as
credit. Empty statements are refused. Batch generation
creates one record per patient (patients with nothing to bill are skipped) plus
an optional combined PDF rendered from the snapshots. The PDF is generated on the
server and contains no internal ids, tokens or claim data.

## 11. Aging and reports

Aging basis: **date of service**; buckets 0–30, 31–60, 61–90, 91+ days
(`billing_charge_aging`). Insurance aging attributes each balance to the claim
currently responsible for it (highest sequence) or to the service's payer while
unbilled. Patient aging counts only patient balances. The collections summary
keeps *charges (billed, not revenue)*, *patient payments*, *insurance payments*,
*refunds*, *write-offs* and *other adjustments* separate; total collections =
patient payments + insurance payments − refunds. Dating: charges by entry day,
payments by payment date, refunds by refund date, adjustments by the day
recorded. The dashboard's claim queues are defined once (`claimQueues`) and used
by both the counts and the list filters, so a card always equals the list it
opens. CSV export streams with `encoding/csv` and neutralises spreadsheet
formulas.

## 12. Secondary (and later) insurance

After a payer **finally adjudicates** a service and insurance responsibility
remains, the biller can explicitly **Create Secondary Claim** on the earlier
claim. The server decides everything: the next sequence (one ordering helper),
the policy (an active next-priority policy covering the service date; an
explicit choice if several apply), the services, and the eligible amount (the
charge's remaining insurance balance — amounts moved to the patient are never
billed again). The new claim snapshots the earlier payer's adjudication
(billed, paid, allowed, adjustments, transferred to patient, eligible amount)
and lists the earlier payers as "other insurance". It works the same for
secondary → tertiary → quaternary. Double billing is prevented by locking
(earlier claim, then its charges), a unique current-line-per-sequence index,
refusing payments to an earlier payer for a forwarded service, and refusing to
reopen / void anything a follow-on claim depends on. Cancelling the follow-on
claim re-evaluates the earlier one.

## 13. Roles, security and concurrency

See [`ROUTE_SECURITY_MATRIX.md`](ROUTE_SECURITY_MATRIX.md) for every route.

| Role | Can |
|---|---|
| `practice_administrator` | create users, **assign roles**, edit any user, service codes, billing configuration |
| `practice_biller` | all billing writes: payers, policies, authorizations, rates, charges, claims, payments, statements, adjustments |
| `clinician`, `practice_scheduler` | create / edit patients; clinicians also diagnoses |
| `clinical_administrator` | service codes, diagnoses |
| any role | read access (staff-level) |

- **Authentication**: HS256 JWT (24 h), bcrypt; tokens are checked for method,
  expiry and that the account still exists and is active; failed sign-ins are
  throttled per email (in-memory, keyed by a fixed-size digest, at most 10 000
  tracked); an unknown email costs the same time as a known one and neither
  reveals whether an account exists or is deactivated (only someone who proved
  the password is told the account is inactive). `JWT_SECRET` must be at least
  32 characters or the API refuses to start.
- **Self sign-up accounts have no role and see no practice data** (403 on every
  data route) until an administrator assigns a role.
- **Role assignment is administrator-only**; the last active administrator cannot
  be removed. Users can read / edit only their own record unless administrator.
- **Seeded administrator**: migration `013_seed_full_access_admin.sql` creates
  `admin@ehr.local` (username `ehr_admin`) with initial password
  `EHR-Admin!2026-7Qx9` and every available role. This account can create users
  and assign their roles from the Users pages. The migration stores a bcrypt
  hash and can be rerun without duplicating users or roles or resetting an
  existing password. Change the initial password before deploying the app.
- **Additional administrator**: run the local operator command (database access
  required) — never an endpoint:
  ```
  cd backend
  set -a; source .env; set +a
  go run ./cmd/bootstrap-admin --email person@example.com   # person signed up first
  go run ./cmd/bootstrap-admin --list
  ```
- **Concurrency**: financial writes run in transactions with row locks taken in
  one order (payment → claims → charges, each by id). Totals that decide a limit
  are read *after* the lock in a separate statement. Tests prove concurrent
  payments, refunds, voids, transfers, submissions, authorization use and
  secondary creation cannot spend the same balance twice.
- **Deadlocks**: claims are locked in id order, but cancelling / voiding a
  follow-on claim also locks the claim before it. If two such operations ever
  collide PostgreSQL aborts one; nothing is saved and the API answers **409
  "please try again"** instead of a 500.
- **Idempotency**: payments and refunds carry client idempotency keys (a refund
  key used for a different payment is a 409, not a silent success); voids,
  submissions and follow-on claim creation are state-guarded (a repeat is a
  409, never a second effect).
- **Requests**: 1 MiB body limit, server read / write timeouts, `no-store` and
  `nosniff` headers, bounded pagination, validated UUIDs / dates / enums / money.
- **Logging**: passwords, tokens, member ids and financial payloads are never
  logged; user-facing errors carry no SQL or stack details.

## 14. External integration boundaries

Nothing here pretends to talk to an outside service.

- **Electronic claims**: the claim is prepared internally (validation, a
  provider-neutral JSON payload preview). With no clearinghouse configured,
  electronic submission returns **`CLEARINGHOUSE_NOT_CONFIGURED`** and the claim
  stays `ready`. `ClearinghouseProvider` (`billing/clearinghouse.go`) is the
  interface a real adapter implements; no provider is bundled and
  `CLEARINGHOUSE_PROVIDER` only logs a notice. The dashboard says "Electronic
  Claims, No Clearinghouse" instead of implying delivery.
- **ERA / 835**: not implemented. `ERASource` is only an interface. Insurance
  payments are posted manually.
- **Cards**: no processor. Card payments taken elsewhere are recorded as
  `external_card`; card numbers are rejected and never stored.

## 15. Migrations

Plain SQL files in `backend/migrations/`, applied once and in order with `psql`
(no migration table). Never edit an applied file; add a new one.

| File | Purpose |
|---|---|
| 001 initial schema | users, roles, patients, payers |
| 002 patient billing | billing settings, insurance policies |
| 003 service codes / prior authorizations | codes, authorizations, usage mappings |
| 004 billing rates | schedules, items, clinician assignment, cash rates, settings |
| 005 billing charges | diagnoses, charges, base balance view |
| 006 claims | claims, lines, diagnoses, history, comments, usage ledger |
| 007 claim documents | CMS-1500 PDFs, superbills |
| 008 claim submissions | submission records |
| 009 patient payments | payments, allocations, refunds, balance view v2 |
| 010 insurance payments | remittances, allocations, adjustments, transfers, balance view v3 |
| 011 balance engine | credits, patient balances, ledger events |
| 012 patient statements | statements, charge aging view |

```
cd backend
set -a; source .env; set +a
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/012_patient_statements.sql   # only the new file
```

## 16. Tests and the test database

`go test ./...` runs unit tests and an API-level integration suite
(`backend/internal/integration`) against the **real router, JWT auth, role checks
and handlers**. DB tests **never touch your data**: `internal/testdb` drops and
rebuilds a dedicated schema (`ehr_test_integration`) inside the development
database from `backend/migrations/` and pins every connection's `search_path` to
it. The database comes from `TEST_DATABASE_URL`, else `DATABASE_URL`, else
`backend/.env`; if none is reachable the DB tests skip with a message. No
superuser or second database is needed.

Browser checks (`e2e/`, needs the API on :8080, the frontend on :3000, Google
Chrome and the bootstrap command's Go toolchain on `PATH`):

```
cd e2e && npm install
node module9.mjs … node module13.mjs      # per-module checks
node final.mjs                            # the full workflow, records named "E2E FINAL …"
```

They create clearly named `E2E …` records in the development database;
`scripts/cleanup_e2e_data.sql` removes them (dry run by default, `-v commit=1`
to delete). It never touches records without the E2E marker.

## 17. Known limitations

- Read access is staff-level: any user with any role can read any patient's
  billing data (no per-clinician patient scoping). Tighten before real PHI.
- The login throttle and JWT handling are single-instance, token in
  `localStorage` (move to HttpOnly cookies before production). A throttle keyed
  by email can be used to lock out a known account for 15 minutes; add per-IP
  limits / alerting behind a proxy before production.
- The CSV export streams: if the database fails part-way the file ends with an
  `EXPORT INCOMPLETE` row (the 200 status has already been sent).
- No ERA import, no clearinghouse, no card processor; payer overpayments,
  insurance refunds / recoupments (use void and re-post) and interest are not
  modelled.
- Timestamps use the database session time zone; report days are server days.

### Test schema housekeeping

`go test` leaves the throwaway schema `ehr_test_integration` in the development
database (it is rebuilt on every run and holds no real data). Drop it any time:

```
psql "$DATABASE_URL" -c "DROP SCHEMA IF EXISTS ehr_test_integration CASCADE"
```
