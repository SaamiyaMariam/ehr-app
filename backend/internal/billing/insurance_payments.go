package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Insurance payments are manually posted remittances (EOBs). A remittance
// has a header (payer, date, amount — which may be 0), and one allocation
// per adjudicated claim line. Each line can carry insurance-side adjustments
// (contractual write-offs, ...) and responsibility transfers to the patient
// (deductible, copay, coinsurance, non-covered). Everything is append-only:
// corrections void the remittance, never delete it.

var (
	insurancePaymentTypes = []string{"check", "eft", "virtual_card", "other"}

	adjustmentTypes = []string{
		"contractual_writeoff", "payer_adjustment", "small_balance_writeoff",
		"bad_debt_writeoff", "courtesy_writeoff", "manual_adjustment",
	}

	// Adjustments that only ever apply to the insurance side.
	insuranceOnlyAdjustmentTypes = []string{"contractual_writeoff", "payer_adjustment"}

	transferReasons           = []string{"deductible", "copay", "coinsurance", "noncovered", "correction", "other"}
	remittanceTransferReasons = []string{"deductible", "copay", "coinsurance", "noncovered", "other"}

	// Claims a payer can have adjudicated: they have left the practice.
	postableClaimStatuses = []string{"submitted", "sent", "resubmitted", "paper_generated", "externally_submitted", "paid"}

	// Statuses a claim can move to "paid" from.
	resolvableClaimStatuses = []string{"submitted", "sent", "resubmitted", "paper_generated", "externally_submitted"}
)

// finError is a user-facing failure of a financial operation.
type finError struct {
	status  int
	message string
}

func (e *finError) Error() string { return e.message }

func finBadRequest(message string) error { return &finError{http.StatusBadRequest, message} }
func finConflict(message string) error   { return &finError{http.StatusConflict, message} }
func finNotFound(message string) error   { return &finError{http.StatusNotFound, message} }

// writeFinError answers with a finError's status or a generic 500; internal
// error details are never sent to the client.
func writeFinError(w http.ResponseWriter, err error, fallback string) {
	var fe *finError
	if errors.As(err, &fe) {
		writeError(w, fe.status, fe.message)
		return
	}

	writeError(w, http.StatusInternalServerError, fallback)
}

// =========================================================
// TYPES
// =========================================================

type RemittanceAdjustment struct {
	Type      string `json:"type"`
	Amount    string `json:"amount"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

type RemittanceTransfer struct {
	Reason string `json:"reason"`
	Amount string `json:"amount"`
	Note   string `json:"note"`
}

// RemittanceLine is the adjudication of one claim line.
type RemittanceLine struct {
	ClaimID       string `json:"claim_id"`
	ClaimLineID   string `json:"claim_line_id"`
	AmountPaid    string `json:"amount_paid"`
	AllowedAmount string `json:"allowed_amount"`
	// IsFinal: the payer has finished with this line. Defaults to true.
	IsFinal     *bool                  `json:"is_final"`
	Adjustments []RemittanceAdjustment `json:"adjustments"`
	Transfers   []RemittanceTransfer   `json:"transfers"`
}

type InsurancePaymentInput struct {
	PayerID         string           `json:"payer_id"`
	PaymentDate     string           `json:"payment_date"`
	Amount          string           `json:"amount"`
	PaymentType     string           `json:"payment_type"`
	ReferenceNumber string           `json:"reference_number"`
	Notes           string           `json:"notes"`
	IdempotencyKey  string           `json:"idempotency_key"`
	Lines           []RemittanceLine `json:"lines"`
}

type AllocationAdjustment struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Amount    string `json:"amount"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

type AllocationTransfer struct {
	ID     string `json:"id"`
	From   string `json:"from_party"`
	To     string `json:"to_party"`
	Reason string `json:"reason"`
	Amount string `json:"amount"`
	Note   string `json:"note"`
	Status string `json:"status"`
}

type InsuranceAllocation struct {
	ID            string                 `json:"id"`
	ClaimID       string                 `json:"claim_id"`
	ClaimNumber   string                 `json:"claim_number"`
	ClaimLineID   string                 `json:"claim_line_id"`
	ChargeID      string                 `json:"charge_id"`
	PatientID     string                 `json:"patient_id"`
	PatientName   string                 `json:"patient_name"`
	DateOfService string                 `json:"date_of_service"`
	ServiceCode   string                 `json:"service_code"`
	Billed        string                 `json:"billed"`
	AmountPaid    string                 `json:"amount_paid"`
	AllowedAmount string                 `json:"allowed_amount"`
	IsFinal       bool                   `json:"is_final"`
	Status        string                 `json:"status"`
	Adjustments   []AllocationAdjustment `json:"adjustments"`
	Transfers     []AllocationTransfer   `json:"transfers"`
}

type InsurancePayment struct {
	ID              string `json:"id"`
	PayerID         string `json:"payer_id"`
	PayerName       string `json:"payer_name"`
	PaymentDate     string `json:"payment_date"`
	Amount          string `json:"amount"`
	PaymentType     string `json:"payment_type"`
	ReferenceNumber string `json:"reference_number"`
	Notes           string `json:"notes"`
	Status          string `json:"status"`
	VoidReason      string `json:"void_reason"`
	Allocated       string `json:"allocated"`
	Unallocated     string `json:"unallocated"`
	Adjusted        string `json:"adjusted"`
	Transferred     string `json:"transferred_to_patient"`
	CreatedBy       string `json:"created_by"`
	CreatedAt       string `json:"created_at"`

	Allocations []InsuranceAllocation `json:"allocations,omitempty"`
}

// ClaimOutcome reports a claim touched by a posting and its resulting status.
type ClaimOutcome struct {
	ClaimID     string `json:"claim_id"`
	ClaimNumber string `json:"claim_number"`
	Status      string `json:"status"`
}

// =========================================================
// PURE VALIDATION / PLANNING
// =========================================================

type parsedAdjustment struct {
	Type      string
	Amount    int64
	Reason    string
	Reference string
}

type parsedTransfer struct {
	Reason string
	Amount int64
	Note   string
}

type parsedLine struct {
	ClaimID     string
	ClaimLineID string
	Paid        int64
	Allowed     *int64
	Final       bool
	Adjustments []parsedAdjustment
	Transfers   []parsedTransfer
}

// Adjusted / Transferred are the insurance responsibility removed from the
// line by adjustments and by moves to the patient.
func (l parsedLine) adjusted() (total int64) {
	for _, a := range l.Adjustments {
		total += a.Amount
	}
	return total
}

func (l parsedLine) transferred() (total int64) {
	for _, t := range l.Transfers {
		total += t.Amount
	}
	return total
}

// resolved is how much insurance balance the line accounts for.
func (l parsedLine) resolved() int64 {
	return l.Paid + l.adjusted() + l.transferred()
}

func parseOptionalMoney(value string) (*int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}

	cents, ok := parseMoney(value)
	if !ok {
		return nil, false
	}

	return &cents, true
}

// parseRemittanceLine validates and normalizes one adjudicated line.
func parseRemittanceLine(l RemittanceLine) (parsedLine, string) {
	out := parsedLine{
		ClaimID:     strings.ToLower(strings.TrimSpace(l.ClaimID)),
		ClaimLineID: strings.ToLower(strings.TrimSpace(l.ClaimLineID)),
		Final:       l.IsFinal == nil || *l.IsFinal,
	}

	if !isUUID(out.ClaimID) || !isUUID(out.ClaimLineID) {
		return out, "each line must reference a claim and one of its service lines"
	}

	paid, ok := parseOptionalMoney(l.AmountPaid)
	if !ok {
		return out, "paid amounts must be non-negative with at most 2 decimal places"
	}
	if paid != nil {
		out.Paid = *paid
	}

	allowed, ok := parseOptionalMoney(l.AllowedAmount)
	if !ok {
		return out, "allowed amounts must be non-negative with at most 2 decimal places"
	}
	out.Allowed = allowed

	if len(l.Adjustments) > 20 || len(l.Transfers) > 20 {
		return out, "too many adjustments or transfers on one line"
	}

	for _, a := range l.Adjustments {
		adj, message := parseAdjustment(a.Type, a.Amount, a.Reason, a.Reference, "insurance")
		if message != "" {
			return out, message
		}
		out.Adjustments = append(out.Adjustments, parsedAdjustment{adj.Type, adj.Amount, adj.Reason, adj.Reference})
	}

	for _, t := range l.Transfers {
		reason := strings.ToLower(strings.TrimSpace(t.Reason))
		if !validEnum(reason, remittanceTransferReasons...) {
			return out, "transfer reason must be deductible, copay, coinsurance, noncovered or other"
		}

		cents, ok := parseMoney(t.Amount)
		if !ok || cents <= 0 {
			return out, "transfer amounts must be greater than zero with at most 2 decimal places"
		}

		note := strings.TrimSpace(t.Note)
		if len(note) > 500 {
			return out, "transfer notes are too long"
		}

		out.Transfers = append(out.Transfers, parsedTransfer{reason, cents, note})
	}

	if out.resolved() == 0 && !out.Final {
		return out, "a line that is not final must pay, adjust or transfer something"
	}

	return out, ""
}

// parseAdjustment validates a single adjustment for the given party.
func parseAdjustment(adjType, amount, reason, reference, party string) (parsedAdjustment, string) {
	adj := parsedAdjustment{
		Type:      strings.ToLower(strings.TrimSpace(adjType)),
		Reason:    strings.TrimSpace(reason),
		Reference: strings.TrimSpace(reference),
	}

	if !validEnum(adj.Type, adjustmentTypes...) {
		return adj, "adjustment type is invalid"
	}

	if party == "patient" && validEnum(adj.Type, insuranceOnlyAdjustmentTypes...) {
		return adj, "contractual write-offs and payer adjustments apply to insurance responsibility only"
	}

	cents, ok := parseMoney(amount)
	if !ok || cents <= 0 {
		return adj, "adjustment amounts must be greater than zero with at most 2 decimal places"
	}
	adj.Amount = cents

	if len(adj.Reason) > 500 || len(adj.Reference) > 100 {
		return adj, "adjustment reason or reference is too long"
	}

	return adj, ""
}

func validateInsurancePaymentInput(in *InsurancePaymentInput, today time.Time) ([]parsedLine, string) {
	in.PayerID = strings.ToLower(strings.TrimSpace(in.PayerID))
	in.PaymentDate = strings.TrimSpace(in.PaymentDate)
	in.Amount = strings.TrimSpace(in.Amount)
	in.PaymentType = strings.ToLower(strings.TrimSpace(in.PaymentType))
	in.ReferenceNumber = strings.TrimSpace(in.ReferenceNumber)
	in.Notes = strings.TrimSpace(in.Notes)
	in.IdempotencyKey = strings.ToLower(strings.TrimSpace(in.IdempotencyKey))

	if !isUUID(in.IdempotencyKey) {
		return nil, "an idempotency key (UUID) is required"
	}

	if !isUUID(in.PayerID) {
		return nil, "a payer is required"
	}

	d, ok := parseDate(in.PaymentDate)
	if !ok {
		return nil, "payment date must be a valid date"
	}

	if isFutureDate(d, today) {
		return nil, "payment date cannot be in the future"
	}

	// Zero-dollar EOBs are valid, so only the format is checked here.
	if _, ok := parseMoney(in.Amount); !ok {
		return nil, "amount must be zero or more with at most 2 decimal places"
	}

	if !validEnum(in.PaymentType, insurancePaymentTypes...) {
		return nil, "payment type must be check, eft, virtual_card or other"
	}

	if len(in.ReferenceNumber) > 100 || len(in.Notes) > 2000 {
		return nil, "reference or notes are too long"
	}

	if looksLikeCardNumber(in.ReferenceNumber) || looksLikeCardNumber(in.Notes) {
		return nil, "do not enter card numbers; record only the payer's remittance reference"
	}

	return parseRemittanceLines(in.Lines)
}

func parseRemittanceLines(lines []RemittanceLine) ([]parsedLine, string) {
	if len(lines) > 200 {
		return nil, "too many lines in one remittance"
	}

	seen := map[string]bool{}
	parsed := make([]parsedLine, 0, len(lines))

	for _, l := range lines {
		p, message := parseRemittanceLine(l)
		if message != "" {
			return nil, message
		}

		if seen[p.ClaimLineID] {
			return nil, "each service line can appear only once per remittance"
		}
		seen[p.ClaimLineID] = true

		parsed = append(parsed, p)
	}

	return parsed, ""
}

// remittanceTarget is what the database says about a line being adjudicated.
type remittanceTarget struct {
	LineID       string
	ClaimID      string
	ChargeID     string
	PatientID    string
	PayerID      string
	ClaimNumber  string
	ClaimStatus  string
	Sequence     string
	ChargeStatus string
	IsCurrent    bool
	LineTotal    int64
}

// checkRemittanceTargets enforces object-level integrity: every line must
// exist, belong to the stated claim, and bill the payment's payer.
func checkRemittanceTargets(lines []parsedLine, targets map[string]remittanceTarget, payerID string) string {
	for _, l := range lines {
		t, ok := targets[l.ClaimLineID]
		if !ok {
			return "a service line was not found"
		}

		if t.ClaimID != l.ClaimID {
			return "a service line does not belong to the claim it was posted against"
		}

		if t.PayerID != payerID {
			return fmt.Sprintf("claim %s was billed to a different payer than this payment", t.ClaimNumber)
		}

		if !t.IsCurrent || t.ChargeStatus != "active" {
			return fmt.Sprintf("claim %s has a service line that is no longer billable", t.ClaimNumber)
		}

		if !validEnum(t.ClaimStatus, postableClaimStatuses...) {
			return fmt.Sprintf("claim %s is %s; payments can only be posted to claims that were submitted", t.ClaimNumber, t.ClaimStatus)
		}

		if l.Allowed != nil && *l.Allowed > t.LineTotal {
			return fmt.Sprintf("the allowed amount on claim %s exceeds the billed amount of %s", t.ClaimNumber, formatMoney(t.LineTotal))
		}
	}

	return ""
}

// checkRemittanceBalances makes sure the lines never account for more than
// the payment still has unapplied, or more insurance balance than a charge
// has open.
func checkRemittanceBalances(lines []parsedLine, targets map[string]remittanceTarget, insuranceBalance map[string]int64, available int64) string {
	var paidTotal int64
	perCharge := map[string]int64{}

	for _, l := range lines {
		paidTotal += l.Paid
		perCharge[targets[l.ClaimLineID].ChargeID] += l.resolved()
	}

	if paidTotal > available {
		return fmt.Sprintf("paid amounts total %s but only %s of the payment is unapplied", formatMoney(paidTotal), formatMoney(available))
	}

	for chargeID, resolved := range perCharge {
		if balance := insuranceBalance[chargeID]; resolved > balance {
			return fmt.Sprintf(
				"payments, adjustments and transfers total %s but the service has only %s of insurance balance open",
				formatMoney(resolved), formatMoney(balance),
			)
		}
	}

	return ""
}

// lineResolution is one claim line's state when deciding whether the claim
// is fully resolved.
type lineResolution struct {
	Adjudicated      bool
	InsuranceBalance int64
	// Forwarded: a later-sequence claim now carries the remaining balance.
	Forwarded bool
}

// claimResolved: every current line was finally adjudicated by the payer and
// either has no insurance balance left or its remainder moved to the next
// payer. A partial payment alone never resolves a claim.
func claimResolved(lines []lineResolution) bool {
	if len(lines) == 0 {
		return false
	}

	for _, l := range lines {
		if !l.Adjudicated || (l.InsuranceBalance != 0 && !l.Forwarded) {
			return false
		}
	}

	return true
}

// =========================================================
// SQL FRAGMENTS (claim line cl, claim cm)
// =========================================================

const sequenceRankSQL = `ARRAY['primary', 'secondary', 'tertiary', 'quaternary']`

// lineAdjudicatedSQL: the payer finished adjudicating claim line cl.
const lineAdjudicatedSQL = `
	EXISTS (
		SELECT 1 FROM insurance_payment_allocations a
		JOIN insurance_payments p ON p.id = a.payment_id
		WHERE a.claim_line_id = cl.id AND a.status = 'active' AND p.status = 'posted' AND a.is_final
	)
`

// lineForwardedSQL: a later-sequence, non-voided claim also bills cl's charge.
const lineForwardedSQL = `
	EXISTS (
		SELECT 1 FROM claim_lines l2
		JOIN claims c2 ON c2.id = l2.claim_id
		WHERE l2.charge_id = cl.charge_id AND l2.is_current AND c2.status <> 'voided'
		  AND array_position(` + sequenceRankSQL + `, l2.sequence::text)
		    > array_position(` + sequenceRankSQL + `, cl.sequence::text)
	)
`

// =========================================================
// POSTING (transaction helpers)
// =========================================================

// lockClaimsThenCharges takes row locks in one global order (claims, then
// charges, each by id) so concurrent postings cannot deadlock or both spend
// the same balance.
func lockClaimsThenCharges(ctx context.Context, tx pgx.Tx, claimIDs, chargeIDs []string) error {
	if len(claimIDs) > 0 {
		if _, err := tx.Exec(ctx, `SELECT id FROM claims WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`, claimIDs); err != nil {
			return err
		}
	}

	if len(chargeIDs) > 0 {
		if _, err := tx.Exec(ctx, `SELECT id FROM billing_charges WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`, chargeIDs); err != nil {
			return err
		}
	}

	return nil
}

func loadRemittanceTargets(ctx context.Context, tx pgx.Tx, lineIDs []string) (map[string]remittanceTarget, error) {
	rows, err := tx.Query(
		ctx,
		`
		SELECT cl.id, cl.claim_id, cl.charge_id, cl.patient_id, cl.line_total::text, cl.is_current, cl.sequence,
			cm.payer_id, cm.status, cm.claim_number, c.status
		FROM claim_lines cl
		JOIN claims cm ON cm.id = cl.claim_id
		JOIN billing_charges c ON c.id = cl.charge_id
		WHERE cl.id = ANY($1::uuid[])
		`,
		lineIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := map[string]remittanceTarget{}

	for rows.Next() {
		var t remittanceTarget
		var total string

		if err := rows.Scan(
			&t.LineID, &t.ClaimID, &t.ChargeID, &t.PatientID, &total, &t.IsCurrent, &t.Sequence,
			&t.PayerID, &t.ClaimStatus, &t.ClaimNumber, &t.ChargeStatus,
		); err != nil {
			return nil, err
		}

		t.LineTotal = mustCents(total)
		targets[t.LineID] = t
	}

	return targets, rows.Err()
}

type chargeBalance struct {
	Patient   int64
	Insurance int64
}

func loadChargeBalances(ctx context.Context, q queryRower, chargeIDs []string) (map[string]chargeBalance, error) {
	rows, err := q.Query(
		ctx,
		`SELECT charge_id, patient_balance::text, insurance_balance::text FROM billing_charge_balances WHERE charge_id = ANY($1::uuid[])`,
		chargeIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]chargeBalance{}

	for rows.Next() {
		var id, patient, insurance string
		if err := rows.Scan(&id, &patient, &insurance); err != nil {
			return nil, err
		}
		result[id] = chargeBalance{mustCents(patient), mustCents(insurance)}
	}

	return result, rows.Err()
}

// verifyNoNegativeBalances is the last line of defence: after any financial
// write, no touched charge may owe a negative amount on either side.
func verifyNoNegativeBalances(ctx context.Context, tx pgx.Tx, chargeIDs []string, onViolation string) error {
	balances, err := loadChargeBalances(ctx, tx, chargeIDs)
	if err != nil {
		return err
	}

	for _, b := range balances {
		if b.Patient < 0 || b.Insurance < 0 {
			return finConflict(onViolation)
		}
	}

	return nil
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, v := range values {
		set[v] = true
	}

	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)

	return out
}

// postRemittanceLines validates and writes the adjudicated lines of a
// remittance whose payment row is already locked / created by the caller.
// It returns the claims affected, after their resolution was refreshed.
func postRemittanceLines(ctx context.Context, tx pgx.Tx, paymentID, payerID, reference string, lines []parsedLine, available int64, userID string) ([]ClaimOutcome, error) {
	if len(lines) == 0 {
		return nil, nil
	}

	lineIDs := make([]string, 0, len(lines))
	for _, l := range lines {
		lineIDs = append(lineIDs, l.ClaimLineID)
	}

	// First read (unlocked) only to learn which claims / charges to lock.
	preview, err := loadRemittanceTargets(ctx, tx, lineIDs)
	if err != nil {
		return nil, err
	}

	var claimIDs, chargeIDs []string
	for _, t := range preview {
		claimIDs = append(claimIDs, t.ClaimID)
		chargeIDs = append(chargeIDs, t.ChargeID)
	}
	claimIDs, chargeIDs = uniqueSorted(claimIDs), uniqueSorted(chargeIDs)

	if err := lockClaimsThenCharges(ctx, tx, claimIDs, chargeIDs); err != nil {
		return nil, err
	}

	// Authoritative read, under the locks.
	targets, err := loadRemittanceTargets(ctx, tx, lineIDs)
	if err != nil {
		return nil, err
	}

	if message := checkRemittanceTargets(lines, targets, payerID); message != "" {
		return nil, finBadRequest(message)
	}

	balances, err := loadChargeBalances(ctx, tx, chargeIDs)
	if err != nil {
		return nil, err
	}

	insurance := map[string]int64{}
	for id, b := range balances {
		insurance[id] = b.Insurance
	}

	if message := checkRemittanceBalances(lines, targets, insurance, available); message != "" {
		return nil, finBadRequest(message)
	}

	// A payment holds at most one active allocation per line.
	var duplicate bool
	if err := tx.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM insurance_payment_allocations WHERE payment_id = $1 AND status = 'active' AND claim_line_id = ANY($2::uuid[]))`,
		paymentID, lineIDs,
	).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate {
		return nil, finConflict("a service line on this remittance was already adjudicated")
	}

	type claimTotals struct{ paid, adjusted, transferred int64 }
	perClaim := map[string]*claimTotals{}

	for _, l := range lines {
		t := targets[l.ClaimLineID]

		var allowed *string
		if l.Allowed != nil {
			s := formatMoney(*l.Allowed)
			allowed = &s
		}

		var allocationID string
		if err := tx.QueryRow(
			ctx,
			`
			INSERT INTO insurance_payment_allocations (
				payment_id, payer_id, claim_id, claim_line_id, charge_id, amount_paid, allowed_amount, is_final, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9)
			RETURNING id
			`,
			paymentID, payerID, t.ClaimID, t.LineID, t.ChargeID, formatMoney(l.Paid), allowed, l.Final, nullIfEmpty(userID),
		).Scan(&allocationID); err != nil {
			return nil, err
		}

		for _, a := range l.Adjustments {
			if _, err := tx.Exec(
				ctx,
				`
				INSERT INTO billing_adjustments (
					charge_id, patient_id, party, adjustment_type, amount, reason, reference,
					insurance_payment_id, insurance_allocation_id, created_by
				)
				VALUES ($1, $2, 'insurance', $3, $4::numeric, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9)
				`,
				t.ChargeID, t.PatientID, a.Type, formatMoney(a.Amount), a.Reason, a.Reference, paymentID, allocationID, nullIfEmpty(userID),
			); err != nil {
				return nil, err
			}
		}

		for _, x := range l.Transfers {
			if _, err := tx.Exec(
				ctx,
				`
				INSERT INTO responsibility_transfers (
					charge_id, patient_id, from_party, to_party, amount, reason, note,
					insurance_payment_id, insurance_allocation_id, created_by
				)
				VALUES ($1, $2, 'insurance', 'patient', $3::numeric, $4, NULLIF($5, ''), $6, $7, $8)
				`,
				t.ChargeID, t.PatientID, formatMoney(x.Amount), x.Reason, x.Note, paymentID, allocationID, nullIfEmpty(userID),
			); err != nil {
				return nil, err
			}
		}

		totals := perClaim[t.ClaimID]
		if totals == nil {
			totals = &claimTotals{}
			perClaim[t.ClaimID] = totals
		}
		totals.paid += l.Paid
		totals.adjusted += l.adjusted()
		totals.transferred += l.transferred()
	}

	if err := verifyNoNegativeBalances(ctx, tx, chargeIDs, "this posting would make a balance negative"); err != nil {
		return nil, err
	}

	outcomes := make([]ClaimOutcome, 0, len(claimIDs))

	for _, claimID := range claimIDs {
		totals := perClaim[claimID]

		message := fmt.Sprintf(
			"Insurance remittance posted%s: paid %s, adjusted %s, moved to patient %s.",
			referenceSuffix(reference), formatMoney(totals.paid), formatMoney(totals.adjusted), formatMoney(totals.transferred),
		)
		if err := recordClaimHistory(ctx, tx, claimID, "payment_posted", "", "", "user", message, userID); err != nil {
			return nil, err
		}

		outcome, err := refreshClaimResolution(ctx, tx, claimID, userID)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}

	return outcomes, nil
}

func referenceSuffix(reference string) string {
	if reference == "" {
		return ""
	}

	return " (" + reference + ")"
}

// refreshClaimResolution moves a (locked) claim to "paid" once it is fully
// resolved, and back to the status it had before when a void / reversal
// leaves it unresolved. Returns the claim's resulting status.
func refreshClaimResolution(ctx context.Context, tx pgx.Tx, claimID, userID string) (ClaimOutcome, error) {
	out := ClaimOutcome{ClaimID: claimID}

	var status string
	var prePaid *string

	if err := tx.QueryRow(ctx, `SELECT claim_number, status, pre_paid_status FROM claims WHERE id = $1`, claimID).Scan(&out.ClaimNumber, &status, &prePaid); err != nil {
		return out, err
	}

	rows, err := tx.Query(
		ctx,
		`
		SELECT `+lineAdjudicatedSQL+`, b.insurance_balance::text, `+lineForwardedSQL+`
		FROM claim_lines cl
		JOIN billing_charge_balances b ON b.charge_id = cl.charge_id
		WHERE cl.claim_id = $1 AND cl.is_current
		`,
		claimID,
	)
	if err != nil {
		return out, err
	}

	var lines []lineResolution

	for rows.Next() {
		var l lineResolution
		var balance string
		if err := rows.Scan(&l.Adjudicated, &balance, &l.Forwarded); err != nil {
			rows.Close()
			return out, err
		}
		l.InsuranceBalance = mustCents(balance)
		lines = append(lines, l)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return out, err
	}

	resolved := claimResolved(lines)

	switch {
	case resolved && validEnum(status, resolvableClaimStatuses...):
		if _, err := tx.Exec(ctx, `UPDATE claims SET pre_paid_status = $2 WHERE id = $1`, claimID, status); err != nil {
			return out, err
		}

		if err := transitionClaim(ctx, tx, claimID, status, "paid", "paid", "system",
			"Every service line was adjudicated and no insurance balance remains.", userID); err != nil {
			return out, err
		}

		status = "paid"

	case !resolved && status == "paid":
		restore := "submitted"
		if prePaid != nil && validEnum(*prePaid, resolvableClaimStatuses...) {
			restore = *prePaid
		}

		if _, err := tx.Exec(ctx, `UPDATE claims SET pre_paid_status = NULL WHERE id = $1`, claimID); err != nil {
			return out, err
		}

		if err := transitionClaim(ctx, tx, claimID, "paid", restore, "payment_reversed", "system",
			"A payment was reversed, so the claim is no longer fully resolved.", userID); err != nil {
			return out, err
		}

		status = restore
	}

	out.Status = status

	return out, nil
}

// =========================================================
// READ MODEL
// =========================================================

const selectInsurancePayment = `
	SELECT
		p.id, p.payer_id, py.payer_name,
		TO_CHAR(p.payment_date, 'YYYY-MM-DD'), p.amount::text, p.payment_type,
		COALESCE(p.reference_number, ''), COALESCE(p.notes, ''),
		p.status, COALESCE(p.void_reason, ''),
		COALESCE((SELECT SUM(a.amount_paid) FROM insurance_payment_allocations a WHERE a.payment_id = p.id AND a.status = 'active'), 0)::numeric(12,2)::text,
		COALESCE((SELECT SUM(ad.amount) FROM billing_adjustments ad WHERE ad.insurance_payment_id = p.id AND ad.status = 'active'), 0)::numeric(12,2)::text,
		COALESCE((SELECT SUM(t.amount) FROM responsibility_transfers t WHERE t.insurance_payment_id = p.id AND t.status = 'active' AND t.to_party = 'patient'), 0)::numeric(12,2)::text,
		COALESCE(u.first_name || ' ' || u.last_name, ''),
		TO_CHAR(p.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
	FROM insurance_payments p
	JOIN payers py ON py.id = p.payer_id
	LEFT JOIN users u ON u.id = p.created_by
`

func scanInsurancePayment(row pgx.Row, p *InsurancePayment) error {
	if err := row.Scan(
		&p.ID, &p.PayerID, &p.PayerName, &p.PaymentDate, &p.Amount, &p.PaymentType,
		&p.ReferenceNumber, &p.Notes, &p.Status, &p.VoidReason,
		&p.Allocated, &p.Adjusted, &p.Transferred, &p.CreatedBy, &p.CreatedAt,
	); err != nil {
		return err
	}

	unallocated := int64(0)
	if p.Status == "posted" {
		unallocated = mustCents(p.Amount) - mustCents(p.Allocated)
	}
	p.Unallocated = formatMoney(unallocated)

	return nil
}

func (h *Handler) getInsurancePayment(ctx context.Context, q queryRower, id string) (InsurancePayment, error) {
	var p InsurancePayment

	if err := scanInsurancePayment(q.QueryRow(ctx, selectInsurancePayment+`WHERE p.id = $1`, id), &p); err != nil {
		return p, err
	}

	rows, err := q.Query(
		ctx,
		`
		SELECT a.id, a.claim_id, cm.claim_number, a.claim_line_id, a.charge_id, cl.patient_id,
			COALESCE(pt.first_name || ' ', '') || pt.last_name,
			TO_CHAR(cl.date_of_service, 'YYYY-MM-DD'), cl.service_code, cl.line_total::text,
			a.amount_paid::text, COALESCE(a.allowed_amount::text, ''), a.is_final, a.status,
			COALESCE((
				SELECT json_agg(json_build_object(
					'id', ad.id, 'type', ad.adjustment_type, 'amount', ad.amount::text,
					'reason', COALESCE(ad.reason, ''), 'reference', COALESCE(ad.reference, ''), 'status', ad.status
				) ORDER BY ad.created_at, ad.id)
				FROM billing_adjustments ad WHERE ad.insurance_allocation_id = a.id
			), '[]'::json),
			COALESCE((
				SELECT json_agg(json_build_object(
					'id', t.id, 'from_party', t.from_party, 'to_party', t.to_party, 'reason', t.reason,
					'amount', t.amount::text, 'note', COALESCE(t.note, ''), 'status', t.status
				) ORDER BY t.created_at, t.id)
				FROM responsibility_transfers t WHERE t.insurance_allocation_id = a.id
			), '[]'::json)
		FROM insurance_payment_allocations a
		JOIN claims cm ON cm.id = a.claim_id
		JOIN claim_lines cl ON cl.id = a.claim_line_id
		JOIN patients pt ON pt.id = cl.patient_id
		WHERE a.payment_id = $1
		ORDER BY a.created_at, cm.claim_number, cl.line_number
		`,
		id,
	)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	p.Allocations = []InsuranceAllocation{}

	for rows.Next() {
		var a InsuranceAllocation
		if err := rows.Scan(
			&a.ID, &a.ClaimID, &a.ClaimNumber, &a.ClaimLineID, &a.ChargeID, &a.PatientID, &a.PatientName,
			&a.DateOfService, &a.ServiceCode, &a.Billed, &a.AmountPaid, &a.AllowedAmount, &a.IsFinal, &a.Status,
			&a.Adjustments, &a.Transfers,
		); err != nil {
			return p, err
		}

		if a.Adjustments == nil {
			a.Adjustments = []AllocationAdjustment{}
		}
		if a.Transfers == nil {
			a.Transfers = []AllocationTransfer{}
		}

		p.Allocations = append(p.Allocations, a)
	}

	return p, rows.Err()
}

// ClaimRemittance is one insurance allocation shown on a claim.
type ClaimRemittance struct {
	AllocationID     string `json:"allocation_id"`
	PaymentID        string `json:"payment_id"`
	PaymentDate      string `json:"payment_date"`
	ReferenceNumber  string `json:"reference_number"`
	PaymentStatus    string `json:"payment_status"`
	LineNumber       int    `json:"line_number"`
	ServiceCode      string `json:"service_code"`
	AmountPaid       string `json:"amount_paid"`
	AllowedAmount    string `json:"allowed_amount"`
	IsFinal          bool   `json:"is_final"`
	AllocationStatus string `json:"allocation_status"`
	Adjusted         string `json:"adjusted"`
	Transferred      string `json:"transferred_to_patient"`
}

func (h *Handler) listClaimRemittances(ctx context.Context, q queryRower, claimID string) ([]ClaimRemittance, error) {
	rows, err := q.Query(
		ctx,
		`
		SELECT a.id, p.id, TO_CHAR(p.payment_date, 'YYYY-MM-DD'), COALESCE(p.reference_number, ''), p.status,
			cl.line_number, cl.service_code, a.amount_paid::text, COALESCE(a.allowed_amount::text, ''), a.is_final, a.status,
			COALESCE((SELECT SUM(ad.amount) FROM billing_adjustments ad WHERE ad.insurance_allocation_id = a.id AND ad.status = 'active'), 0)::numeric(12,2)::text,
			COALESCE((SELECT SUM(t.amount) FROM responsibility_transfers t WHERE t.insurance_allocation_id = a.id AND t.status = 'active' AND t.to_party = 'patient'), 0)::numeric(12,2)::text
		FROM insurance_payment_allocations a
		JOIN insurance_payments p ON p.id = a.payment_id
		JOIN claim_lines cl ON cl.id = a.claim_line_id
		WHERE a.claim_id = $1
		ORDER BY p.payment_date, a.created_at, cl.line_number
		`,
		claimID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ClaimRemittance{}

	for rows.Next() {
		var c ClaimRemittance
		if err := rows.Scan(
			&c.AllocationID, &c.PaymentID, &c.PaymentDate, &c.ReferenceNumber, &c.PaymentStatus,
			&c.LineNumber, &c.ServiceCode, &c.AmountPaid, &c.AllowedAmount, &c.IsFinal, &c.AllocationStatus,
			&c.Adjusted, &c.Transferred,
		); err != nil {
			return nil, err
		}
		result = append(result, c)
	}

	return result, rows.Err()
}

// =========================================================
// HANDLERS: INSURANCE PAYMENTS
// =========================================================

func (h *Handler) GetInsurancePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}

	p, err := h.getInsurancePayment(r.Context(), h.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payment")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) SearchInsurancePayments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := &chargeFilter{}

	if v := strings.TrimSpace(q.Get("payer_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "payer_id is invalid")
			return
		}
		f.add("p.payer_id = $%d", v)
	}

	for key, clause := range map[string]string{"from": "p.payment_date >= $%d::date", "to": "p.payment_date <= $%d::date"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if _, ok := parseDate(v); !ok {
				writeError(w, http.StatusBadRequest, key+" must be a valid date")
				return
			}
			f.add(clause, v)
		}
	}

	if v := strings.TrimSpace(q.Get("status")); v != "" {
		if !validEnum(v, "posted", "voided") {
			writeError(w, http.StatusBadRequest, "status is invalid")
			return
		}
		f.add("p.status = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("reference")); v != "" {
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "reference search is too long")
			return
		}
		f.add("p.reference_number ILIKE '%%' || $%d || '%%'", v)
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(f.where) > 0 {
		where = "WHERE " + strings.Join(f.where, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM insurance_payments p `+where, f.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payments")
		return
	}

	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectInsurancePayment+where+` ORDER BY p.payment_date DESC, p.created_at DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payments")
		return
	}
	defer rows.Close()

	result := pagedResult[InsurancePayment]{Items: []InsurancePayment{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var p InsurancePayment
		if err := scanInsurancePayment(rows, &p); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load insurance payments")
			return
		}
		result.Items = append(result.Items, p)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateInsurancePayment(w http.ResponseWriter, r *http.Request) {
	var in InsurancePaymentInput

	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	lines, message := validateInsurancePaymentInput(&in, time.Now())
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment")
		return
	}
	defer tx.Rollback(r.Context())

	// Idempotency: a retried request returns the payment already posted.
	var existingID, existingPayer string
	err = tx.QueryRow(r.Context(), `SELECT id, payer_id FROM insurance_payments WHERE idempotency_key = $1`, in.IdempotencyKey).Scan(&existingID, &existingPayer)
	if err == nil {
		if existingPayer != in.PayerID {
			writeError(w, http.StatusConflict, "this idempotency key was already used for another payment")
			return
		}

		p, err := h.getInsurancePayment(r.Context(), tx, existingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load insurance payment")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"payment": p, "claims": []ClaimOutcome{}, "duplicate": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment")
		return
	}

	var payerExists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM payers WHERE id = $1)`, in.PayerID).Scan(&payerExists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment")
		return
	}
	if !payerExists {
		writeError(w, http.StatusBadRequest, "payer not found")
		return
	}

	amount, _ := parseMoney(in.Amount)

	var paymentID string
	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO insurance_payments (
			payer_id, payment_date, amount, payment_type, reference_number, notes, idempotency_key, created_by
		)
		VALUES ($1, $2::date, $3::numeric, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id
		`,
		in.PayerID, in.PaymentDate, formatMoney(amount), in.PaymentType, in.ReferenceNumber, in.Notes, in.IdempotencyKey, nullIfEmpty(userID),
	).Scan(&paymentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "this payment is already being posted; refresh to see it")
			return
		}
		writeError(w, http.StatusBadRequest, "could not post insurance payment")
		return
	}

	outcomes, err := postRemittanceLines(r.Context(), tx, paymentID, in.PayerID, in.ReferenceNumber, lines, amount, userID)
	if err != nil {
		writeFinError(w, err, "could not post insurance payment")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment")
		return
	}

	p, err := h.getInsurancePayment(r.Context(), h.db, paymentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payment")
		return
	}

	if outcomes == nil {
		outcomes = []ClaimOutcome{}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"payment": p, "claims": outcomes})
}

// lockPostedInsurancePayment locks a payment and returns its payer, status
// and unapplied amount.
func lockInsurancePayment(ctx context.Context, tx pgx.Tx, paymentID string) (payerID, status, reference string, available int64, err error) {
	var amount, allocated string

	err = tx.QueryRow(
		ctx,
		`
		SELECT p.payer_id, p.status, COALESCE(p.reference_number, ''), p.amount::text,
			COALESCE((SELECT SUM(a.amount_paid) FROM insurance_payment_allocations a WHERE a.payment_id = p.id AND a.status = 'active'), 0)::text
		FROM insurance_payments p
		WHERE p.id = $1
		FOR UPDATE OF p
		`,
		paymentID,
	).Scan(&payerID, &status, &reference, &amount, &allocated)
	if err != nil {
		return
	}

	available = mustCents(amount) - mustCents(allocated)

	return
}

// AddInsurancePaymentLines adjudicates more claim lines on a posted payment.
func (h *Handler) AddInsurancePaymentLines(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}

	var req struct {
		Lines []RemittanceLine `json:"lines"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	lines, message := parseRemittanceLines(req.Lines)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if len(lines) == 0 {
		writeError(w, http.StatusBadRequest, "provide at least one service line")
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment lines")
		return
	}
	defer tx.Rollback(r.Context())

	payerID, status, reference, available, err := lockInsurancePayment(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment lines")
		return
	}

	if status != "posted" {
		writeError(w, http.StatusConflict, "voided payments cannot be allocated")
		return
	}

	outcomes, err := postRemittanceLines(r.Context(), tx, id, payerID, reference, lines, available, userID)
	if err != nil {
		writeFinError(w, err, "could not post insurance payment lines")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not post insurance payment lines")
		return
	}

	p, err := h.getInsurancePayment(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"payment": p, "claims": outcomes})
}

// VoidInsurancePayment reverses a whole remittance: its allocations,
// adjustments and transfers are voided together (nothing is deleted), the
// charges' balances are restored, and affected claims are re-evaluated.
func (h *Handler) VoidInsurancePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len(req.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "a void reason (up to 500 characters) is required")
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}
	defer tx.Rollback(r.Context())

	_, status, reference, _, err := lockInsurancePayment(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}

	if status == "voided" {
		writeError(w, http.StatusConflict, "payment is already voided")
		return
	}

	claimIDs, chargeIDs, err := remittanceScope(r.Context(), tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}

	if err := lockClaimsThenCharges(r.Context(), tx, claimIDs, chargeIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}

	// A later-sequence claim built on this remittance must be dealt with first.
	var dependent *string
	if err := tx.QueryRow(
		r.Context(),
		`
		SELECT cm2.claim_number
		FROM insurance_payment_allocations a
		JOIN claim_lines cl ON cl.id = a.claim_line_id
		JOIN claim_lines l2 ON l2.charge_id = cl.charge_id AND l2.is_current
			AND array_position(`+sequenceRankSQL+`, l2.sequence::text) > array_position(`+sequenceRankSQL+`, cl.sequence::text)
		JOIN claims cm2 ON cm2.id = l2.claim_id AND cm2.status <> 'voided'
		WHERE a.payment_id = $1 AND a.status = 'active'
		LIMIT 1
		`,
		id,
	).Scan(&dependent); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}
	if dependent != nil {
		writeError(w, http.StatusConflict, "claim "+*dependent+" was created from this remittance; cancel or void it before voiding the payment")
		return
	}

	userRef := nullIfEmpty(userID)

	for _, stmt := range []string{
		`UPDATE insurance_payment_allocations SET status = 'voided', voided_at = NOW(), voided_by = $2 WHERE payment_id = $1 AND status = 'active'`,
		`UPDATE billing_adjustments SET status = 'voided', voided_at = NOW(), voided_by = $2, void_reason = 'Remittance voided' WHERE insurance_payment_id = $1 AND status = 'active'`,
		`UPDATE responsibility_transfers SET status = 'voided', voided_at = NOW(), voided_by = $2, void_reason = 'Remittance voided' WHERE insurance_payment_id = $1 AND status = 'active'`,
	} {
		if _, err := tx.Exec(r.Context(), stmt, id, userRef); err != nil {
			writeError(w, http.StatusInternalServerError, "could not void insurance payment")
			return
		}
	}

	if _, err := tx.Exec(
		r.Context(),
		`UPDATE insurance_payments SET status = 'voided', void_reason = $2, voided_at = NOW(), voided_by = $3 WHERE id = $1`,
		id, req.Reason, userRef,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}

	// Voiding must not leave the patient owing a negative amount (they may
	// already have paid the responsibility this remittance transferred).
	if err := verifyNoNegativeBalances(r.Context(), tx, chargeIDs,
		"patient payments were applied to responsibility this remittance moved to the patient; unapply those payments first"); err != nil {
		writeFinError(w, err, "could not void insurance payment")
		return
	}

	for _, claimID := range claimIDs {
		message := "Insurance remittance" + referenceSuffix(reference) + " was voided: " + req.Reason
		if err := recordClaimHistory(r.Context(), tx, claimID, "payment_voided", "", "", "user", message, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not void insurance payment")
			return
		}

		if _, err := refreshClaimResolution(r.Context(), tx, claimID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not void insurance payment")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not void insurance payment")
		return
	}

	p, err := h.getInsurancePayment(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load insurance payment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"payment": p})
}

// remittanceScope lists the claims and charges a remittance touched.
func remittanceScope(ctx context.Context, tx pgx.Tx, paymentID string) (claimIDs, chargeIDs []string, err error) {
	rows, err := tx.Query(
		ctx,
		`
		SELECT claim_id::text, charge_id::text FROM insurance_payment_allocations WHERE payment_id = $1
		UNION
		SELECT NULL, charge_id::text FROM billing_adjustments WHERE insurance_payment_id = $1
		UNION
		SELECT NULL, charge_id::text FROM responsibility_transfers WHERE insurance_payment_id = $1
		`,
		paymentID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var claimID *string
		var chargeID string
		if err := rows.Scan(&claimID, &chargeID); err != nil {
			return nil, nil, err
		}
		if claimID != nil {
			claimIDs = append(claimIDs, *claimID)
		}
		chargeIDs = append(chargeIDs, chargeID)
	}

	return uniqueSorted(claimIDs), uniqueSorted(chargeIDs), rows.Err()
}

// =========================================================
// OUTSTANDING INSURANCE (what a remittance can be posted against)
// =========================================================

type OutstandingInsuranceLine struct {
	ClaimID                 string `json:"claim_id"`
	ClaimNumber             string `json:"claim_number"`
	ClaimStatus             string `json:"claim_status"`
	Sequence                string `json:"sequence"`
	PayerID                 string `json:"payer_id"`
	PayerName               string `json:"payer_name"`
	PatientID               string `json:"patient_id"`
	PatientName             string `json:"patient_name"`
	ClaimLineID             string `json:"claim_line_id"`
	ChargeID                string `json:"charge_id"`
	LineNumber              int    `json:"line_number"`
	DateOfService           string `json:"date_of_service"`
	ServiceCode             string `json:"service_code"`
	Billed                  string `json:"billed"`
	InsuranceResponsibility string `json:"insurance_responsibility"`
	InsurancePaid           string `json:"insurance_paid"`
	InsuranceBalance        string `json:"insurance_balance"`
	PatientBalance          string `json:"patient_balance"`
	Adjudicated             bool   `json:"adjudicated"`
}

// SearchOutstandingInsurance lists current claim lines on submitted claims
// that still carry an insurance balance, for the payer / patient / claim
// being posted. A line already adjudicated whose balance moved to a later
// payer is excluded from the earlier payer's list.
func (h *Handler) SearchOutstandingInsurance(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := &chargeFilter{}

	if v := strings.TrimSpace(q.Get("payer_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "payer_id is invalid")
			return
		}
		f.add("cm.payer_id = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("patient_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "patient_id is invalid")
			return
		}
		f.add("cl.patient_id = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "patient search is too long")
			return
		}
		f.add("(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%%' || $%d || '%%'", v)
	}

	if v := strings.TrimSpace(q.Get("claim_number")); v != "" {
		if len(v) > 20 {
			writeError(w, http.StatusBadRequest, "claim number search is too long")
			return
		}
		f.add("cm.claim_number ILIKE '%%' || $%d || '%%'", v)
	}

	for key, clause := range map[string]string{"from": "cl.date_of_service >= $%d::date", "to": "cl.date_of_service <= $%d::date"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if _, ok := parseDate(v); !ok {
				writeError(w, http.StatusBadRequest, key+" must be a valid date")
				return
			}
			f.add(clause, v)
		}
	}

	page, pageSize := pageParams(r)

	statuses := "'" + strings.Join(postableClaimStatuses, "', '") + "'"

	base := `
		FROM claim_lines cl
		JOIN claims cm ON cm.id = cl.claim_id
		JOIN payers py ON py.id = cm.payer_id
		JOIN billing_charges c ON c.id = cl.charge_id
		JOIN billing_charge_balances b ON b.charge_id = cl.charge_id
		JOIN patients pt ON pt.id = cl.patient_id
		WHERE cl.is_current AND c.status = 'active'
		  AND cm.status IN (` + statuses + `)
		  AND b.insurance_balance > 0
		  AND NOT (` + lineAdjudicatedSQL + ` AND ` + lineForwardedSQL + `)
	`
	if len(f.where) > 0 {
		base += " AND " + strings.Join(f.where, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) `+base, f.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load outstanding insurance")
		return
	}

	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT cm.id, cm.claim_number, cm.status, cm.sequence, cm.payer_id, py.payer_name,
			cl.patient_id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
			cl.id, cl.charge_id, cl.line_number, TO_CHAR(cl.date_of_service, 'YYYY-MM-DD'), cl.service_code,
			cl.line_total::text, b.insurance_responsibility::text,
			`+claimLinePaymentSQL+`,
			b.patient_balance::text
		`+base+`
		ORDER BY cl.date_of_service, cm.claim_number, cl.line_number
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load outstanding insurance")
		return
	}
	defer rows.Close()

	result := pagedResult[OutstandingInsuranceLine]{Items: []OutstandingInsuranceLine{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var l OutstandingInsuranceLine
		if err := rows.Scan(
			&l.ClaimID, &l.ClaimNumber, &l.ClaimStatus, &l.Sequence, &l.PayerID, &l.PayerName,
			&l.PatientID, &l.PatientName,
			&l.ClaimLineID, &l.ChargeID, &l.LineNumber, &l.DateOfService, &l.ServiceCode,
			&l.Billed, &l.InsuranceResponsibility,
			&l.InsurancePaid, &l.InsuranceBalance, &l.Adjudicated,
			&l.PatientBalance,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load outstanding insurance")
			return
		}
		result.Items = append(result.Items, l)
	}

	writeJSON(w, http.StatusOK, result)
}
