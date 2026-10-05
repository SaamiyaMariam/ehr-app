package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Multi-payer claim sequencing. A claim bills one payer at one sequence
// (primary, secondary, tertiary, quaternary). When a payer has finished
// adjudicating a service and insurance responsibility remains, the biller
// may explicitly create the claim for the next-priority policy. Nothing is
// created or transmitted automatically; the server decides the next
// sequence, the policy, the services and the eligible amounts.

// nextSequence is the one place sequence ordering lives ("" after the last).
func nextSequence(sequence string) string {
	for i, s := range claimSequences {
		if s == sequence && i+1 < len(claimSequences) {
			return claimSequences[i+1]
		}
	}

	return ""
}

// =========================================================
// ELIGIBILITY (pure rules)
// =========================================================

type NextPolicy struct {
	ID            string `json:"id"`
	PayerID       string `json:"payer_id"`
	PayerName     string `json:"payer_name"`
	MemberID      string `json:"member_id"`
	PlanName      string `json:"plan_name"`
	CoverageStart string `json:"coverage_start"`
	CoverageEnd   string `json:"coverage_end"`
}

// covers reports whether the policy's coverage dates include a service date.
func (p NextPolicy) covers(dateOfService string) bool {
	return (p.CoverageStart == "" || dateOfService >= p.CoverageStart) &&
		(p.CoverageEnd == "" || dateOfService <= p.CoverageEnd)
}

// nextLineInput is what the database says about one line of the claim being
// followed.
type nextLineInput struct {
	ChargeID      string
	ClaimLineID   string
	LineNumber    int
	ServiceCode   string
	DateOfService string
	ChargeActive  bool
	Adjudicated   bool
	// Eligible is the charge's insurance balance left after the payer's
	// payments, adjustments and transfers to the patient.
	Eligible int64
	// ExistingClaim is a later-sequence claim already billing this service.
	ExistingClaim string
}

type NextLineEval struct {
	ChargeID       string `json:"charge_id"`
	ClaimLineID    string `json:"claim_line_id"`
	LineNumber     int    `json:"line_number"`
	ServiceCode    string `json:"service_code"`
	DateOfService  string `json:"date_of_service"`
	Eligible       bool   `json:"eligible"`
	EligibleAmount string `json:"eligible_amount"`
	Reason         string `json:"reason"`
}

type ClaimRef struct {
	ID          string `json:"id"`
	ClaimNumber string `json:"claim_number"`
	Sequence    string `json:"sequence"`
	Status      string `json:"status"`
}

type NextSequenceEval struct {
	PreviousClaimID     string         `json:"previous_claim_id"`
	PreviousClaimNumber string         `json:"previous_claim_number"`
	PreviousSequence    string         `json:"previous_sequence"`
	NextSequence        string         `json:"next_sequence"`
	CanCreate           bool           `json:"can_create"`
	Reason              string         `json:"reason"`
	NeedsPolicyChoice   bool           `json:"needs_policy_choice"`
	Policy              *NextPolicy    `json:"policy"`
	CandidatePolicies   []NextPolicy   `json:"candidate_policies"`
	EligibleAmount      string         `json:"eligible_amount"`
	Lines               []NextLineEval `json:"lines"`
	ExistingClaims      []ClaimRef     `json:"existing_claims"`
}

func capitalize(value string) string {
	if value == "" {
		return value
	}

	return strings.ToUpper(value[:1]) + value[1:]
}

// evaluateNextSequence applies the eligibility rules. A service is eligible
// for the next payer only when the earlier payer has finally adjudicated it,
// insurance responsibility remains, an active next-priority policy covers
// the date of service, and no later claim already bills it.
func evaluateNextSequence(previousStatus, previousSequence string, lines []nextLineInput, policies []NextPolicy, requestedPolicy string) NextSequenceEval {
	next := nextSequence(previousSequence)

	ev := NextSequenceEval{
		PreviousSequence: previousSequence, NextSequence: next,
		CandidatePolicies: []NextPolicy{}, Lines: []NextLineEval{}, ExistingClaims: []ClaimRef{},
		EligibleAmount: "0.00",
	}

	if next == "" {
		ev.Reason = "This is the last insurance sequence; the remaining balance can only be moved to the patient."
		for _, l := range lines {
			ev.Lines = append(ev.Lines, NextLineEval{ChargeID: l.ChargeID, ClaimLineID: l.ClaimLineID, LineNumber: l.LineNumber, ServiceCode: l.ServiceCode, DateOfService: l.DateOfService, EligibleAmount: "0.00", Reason: ev.Reason})
		}
		return ev
	}

	if !validEnum(previousStatus, postableClaimStatuses...) {
		ev.Reason = capitalize(previousSequence) + " claim has not been adjudicated: it must be submitted and paid or denied first."
	}

	// Step 1: reasons that do not depend on the policy.
	type pre struct {
		in     nextLineInput
		reason string
	}

	pres := make([]pre, 0, len(lines))

	for _, l := range lines {
		p := pre{in: l}

		switch {
		case !l.ChargeActive:
			p.reason = "Service is voided"
		case l.ExistingClaim != "":
			p.reason = capitalize(next) + " claim " + l.ExistingClaim + " already exists"
		case !l.Adjudicated:
			p.reason = capitalize(previousSequence) + " has not finished adjudicating this service"
		case l.Eligible <= 0:
			p.reason = "No remaining insurance responsibility"
		}

		pres = append(pres, p)
	}

	passing := func(p pre) bool { return p.reason == "" && ev.Reason == "" }

	// Step 2: choose the policy (only when some service still needs one;
	// otherwise the services' own reasons explain why nothing is eligible).
	var chosen *NextPolicy

	anyPassing := false
	for _, p := range pres {
		anyPassing = anyPassing || passing(p)
	}

	if !anyPassing {
		// Nothing to bill: skip policy selection.
	} else if len(policies) == 0 {
		for i := range pres {
			if passing(pres[i]) {
				pres[i].reason = "No " + next + " insurance policy"
			}
		}
		if ev.Reason == "" {
			ev.Reason = "No " + next + " insurance policy is active for this patient."
		}
	} else {
		var candidates []NextPolicy

		for _, pol := range policies {
			for _, p := range pres {
				if passing(p) && pol.covers(p.in.DateOfService) {
					candidates = append(candidates, pol)
					break
				}
			}
		}

		switch {
		case requestedPolicy != "":
			for i := range policies {
				if policies[i].ID == requestedPolicy {
					chosen = &policies[i]
				}
			}
			if chosen == nil {
				ev.Reason = "The selected insurance policy is not an active " + next + " policy for this patient."
			}
		case len(candidates) == 1:
			chosen = &candidates[0]
		case len(candidates) > 1:
			ev.NeedsPolicyChoice = true
			ev.CandidatePolicies = candidates
			ev.Reason = fmt.Sprintf("More than one %s policy applies; choose which to bill.", next)
		}

		if chosen != nil {
			ev.Policy = chosen
		} else if len(candidates) == 0 && ev.Reason == "" {
			for i := range pres {
				if passing(pres[i]) {
					pres[i].reason = "Coverage inactive for the service date"
				}
			}
			ev.Reason = "Coverage inactive for the service date: no active " + next + " policy covers it."
		}
	}

	// Step 3: per-line result under the chosen policy.
	var eligibleCents int64
	eligibleCount := 0

	for _, p := range pres {
		line := NextLineEval{
			ChargeID: p.in.ChargeID, ClaimLineID: p.in.ClaimLineID, LineNumber: p.in.LineNumber,
			ServiceCode: p.in.ServiceCode, DateOfService: p.in.DateOfService, EligibleAmount: "0.00", Reason: p.reason,
		}

		if p.reason == "" && ev.Reason == "" && ev.Policy != nil {
			if !ev.Policy.covers(p.in.DateOfService) {
				line.Reason = "Coverage inactive for the service date"
			} else {
				line.Eligible = true
				line.EligibleAmount = formatMoney(p.in.Eligible)
				eligibleCents += p.in.Eligible
				eligibleCount++
			}
		} else if p.reason == "" && ev.Reason != "" {
			line.Reason = ev.Reason
		}

		if p.in.ExistingClaim != "" {
			known := false
			for _, c := range ev.ExistingClaims {
				known = known || c.ClaimNumber == p.in.ExistingClaim
			}
			if !known {
				ev.ExistingClaims = append(ev.ExistingClaims, ClaimRef{ClaimNumber: p.in.ExistingClaim, Sequence: next})
			}
		}

		ev.Lines = append(ev.Lines, line)
	}

	ev.EligibleAmount = formatMoney(eligibleCents)
	ev.CanCreate = eligibleCount > 0 && ev.Policy != nil && ev.Reason == ""

	if !ev.CanCreate && ev.Reason == "" {
		// Nothing eligible: summarise with the first line's reason.
		for _, l := range ev.Lines {
			if l.Reason != "" {
				ev.Reason = l.Reason
				break
			}
		}
		if ev.Reason == "" {
			ev.Reason = "No services are eligible."
		}
	}

	return ev
}

// =========================================================
// DATA ACCESS
// =========================================================

type previousClaimInfo struct {
	ID        string
	Number    string
	PatientID string
	PayerID   string
	Sequence  string
	Status    string
	Snapshot  ClaimSnapshot
}

func loadPreviousClaim(ctx context.Context, q queryRower, claimID string) (previousClaimInfo, error) {
	var p previousClaimInfo
	var raw []byte

	err := q.QueryRow(
		ctx,
		`SELECT id, claim_number, patient_id, payer_id, sequence, status, snapshot FROM claims WHERE id = $1`,
		claimID,
	).Scan(&p.ID, &p.Number, &p.PatientID, &p.PayerID, &p.Sequence, &p.Status, &raw)
	if err != nil {
		return p, err
	}

	return p, json.Unmarshal(raw, &p.Snapshot)
}

func loadNextInputs(ctx context.Context, q queryRower, prev previousClaimInfo) ([]nextLineInput, []NextPolicy, error) {
	next := nextSequence(prev.Sequence)

	rows, err := q.Query(
		ctx,
		`
		SELECT cl.id, cl.charge_id, cl.line_number, cl.service_code, TO_CHAR(cl.date_of_service, 'YYYY-MM-DD'),
			c.status = 'active', `+lineAdjudicatedSQL+`, b.insurance_balance::text,
			COALESCE((
				SELECT cm2.claim_number FROM claim_lines l2
				JOIN claims cm2 ON cm2.id = l2.claim_id
				WHERE l2.charge_id = cl.charge_id AND l2.is_current AND cm2.status <> 'voided' AND l2.sequence = $2
				LIMIT 1
			), '')
		FROM claim_lines cl
		JOIN billing_charges c ON c.id = cl.charge_id
		JOIN billing_charge_balances b ON b.charge_id = cl.charge_id
		WHERE cl.claim_id = $1 AND cl.is_current
		ORDER BY cl.line_number
		`,
		prev.ID, next,
	)
	if err != nil {
		return nil, nil, err
	}

	var lines []nextLineInput

	for rows.Next() {
		var l nextLineInput
		var balance string
		if err := rows.Scan(&l.ClaimLineID, &l.ChargeID, &l.LineNumber, &l.ServiceCode, &l.DateOfService, &l.ChargeActive, &l.Adjudicated, &balance, &l.ExistingClaim); err != nil {
			rows.Close()
			return nil, nil, err
		}
		l.Eligible = mustCents(balance)
		lines = append(lines, l)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var policies []NextPolicy

	if next != "" {
		prows, err := q.Query(
			ctx,
			`
			SELECT p.id, p.payer_id, y.payer_name, COALESCE(p.member_id, ''), COALESCE(p.plan_name, ''),
				COALESCE(TO_CHAR(p.coverage_start, 'YYYY-MM-DD'), ''), COALESCE(TO_CHAR(p.coverage_end, 'YYYY-MM-DD'), '')
			FROM insurance_policies p
			JOIN payers y ON y.id = p.payer_id
			WHERE p.patient_id = $1 AND p.priority = $2 AND p.is_active
			ORDER BY p.created_at, p.id
			`,
			prev.PatientID, next,
		)
		if err != nil {
			return nil, nil, err
		}
		defer prows.Close()

		for prows.Next() {
			var p NextPolicy
			if err := prows.Scan(&p.ID, &p.PayerID, &p.PayerName, &p.MemberID, &p.PlanName, &p.CoverageStart, &p.CoverageEnd); err != nil {
				return nil, nil, err
			}
			policies = append(policies, p)
		}

		if err := prows.Err(); err != nil {
			return nil, nil, err
		}
	}

	return lines, policies, nil
}

func (h *Handler) evaluateClaimFollowUp(ctx context.Context, q queryRower, claimID, requestedPolicy string) (NextSequenceEval, previousClaimInfo, []nextLineInput, error) {
	prev, err := loadPreviousClaim(ctx, q, claimID)
	if err != nil {
		return NextSequenceEval{}, prev, nil, err
	}

	lines, policies, err := loadNextInputs(ctx, q, prev)
	if err != nil {
		return NextSequenceEval{}, prev, nil, err
	}

	ev := evaluateNextSequence(prev.Status, prev.Sequence, lines, policies, requestedPolicy)
	ev.PreviousClaimID, ev.PreviousClaimNumber = prev.ID, prev.Number

	// Name the claims already billing these services.
	for i := range ev.ExistingClaims {
		if err := q.QueryRow(ctx, `SELECT id, status FROM claims WHERE claim_number = $1`, ev.ExistingClaims[i].ClaimNumber).Scan(&ev.ExistingClaims[i].ID, &ev.ExistingClaims[i].Status); err != nil {
			return ev, prev, lines, err
		}
	}

	return ev, prev, lines, nil
}

// =========================================================
// HANDLERS
// =========================================================

// GetNextSequence reports whether (and how) the claim can be followed by a
// claim to the next payer, with the reason when it cannot.
func (h *Handler) GetNextSequence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	requested := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("insurance_policy_id")))
	if requested != "" && !isUUID(requested) {
		writeError(w, http.StatusBadRequest, "insurance policy not found")
		return
	}

	ev, _, _, err := h.evaluateClaimFollowUp(r.Context(), h.db, id, requested)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not evaluate the next insurance")
		return
	}

	writeJSON(w, http.StatusOK, ev)
}

type nextSequenceRequest struct {
	ChargeIDs         []string `json:"charge_ids"`
	InsurancePolicyID string   `json:"insurance_policy_id"`
}

// CreateNextSequenceClaim creates the follow-on claim (secondary, tertiary,
// quaternary) for the next-priority policy. The server picks the sequence,
// policy, services and amounts; the claim snapshots the earlier payer's
// adjudication so later changes to live data never rewrite it.
func (h *Handler) CreateNextSequenceClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	var req nextSequenceRequest

	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	req.InsurancePolicyID = strings.ToLower(strings.TrimSpace(req.InsurancePolicyID))
	if req.InsurancePolicyID != "" && !isUUID(req.InsurancePolicyID) {
		writeError(w, http.StatusBadRequest, "insurance policy not found")
		return
	}

	if len(req.ChargeIDs) > 50 {
		writeError(w, http.StatusBadRequest, "a claim can contain at most 50 services")
		return
	}

	wanted := map[string]bool{}
	for _, c := range req.ChargeIDs {
		c = strings.ToLower(strings.TrimSpace(c))
		if !isUUID(c) {
			writeError(w, http.StatusBadRequest, "billable service not found")
			return
		}
		wanted[c] = true
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}
	defer tx.Rollback(r.Context())

	// Lock order: earlier claim, then its charges (as remittance posting does).
	if _, err := lockClaim(r.Context(), tx, id); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	var chargeIDs []string
	crow, err := tx.Query(r.Context(), `SELECT charge_id::text FROM claim_lines WHERE claim_id = $1 AND is_current ORDER BY charge_id`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}
	for crow.Next() {
		var c string
		if err := crow.Scan(&c); err != nil {
			crow.Close()
			writeError(w, http.StatusInternalServerError, "could not create the claim")
			return
		}
		chargeIDs = append(chargeIDs, c)
	}
	crow.Close()

	locked, err := lockChargesForClaim(r.Context(), tx, chargeIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	ev, prev, _, err := h.evaluateClaimFollowUp(r.Context(), tx, id, req.InsurancePolicyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	if !ev.CanCreate {
		writeError(w, http.StatusConflict, ev.Reason)
		return
	}

	eligible := map[string]NextLineEval{}
	for _, l := range ev.Lines {
		if l.Eligible {
			eligible[l.ChargeID] = l
		}
	}

	for chargeID := range wanted {
		if _, ok := eligible[chargeID]; !ok {
			reason := "that service is not part of this claim"
			for _, l := range ev.Lines {
				if l.ChargeID == chargeID && l.Reason != "" {
					reason = l.Reason
				}
			}
			writeError(w, http.StatusConflict, "a selected service is not eligible: "+reason)
			return
		}
	}

	var chosen []claimChargeRow
	for _, c := range locked {
		if _, ok := eligible[c.ID]; !ok {
			continue
		}
		if len(wanted) > 0 && !wanted[c.ID] {
			continue
		}
		// The authorization on the charge belongs to the primary policy.
		c.PriorAuthorizationID, c.PriorAuthorizationCode = "", ""
		chosen = append(chosen, c)
	}

	if len(chosen) == 0 {
		writeError(w, http.StatusConflict, "no eligible services were selected")
		return
	}

	diagnoses, lines, message := assignClaimDiagnoses(chosen)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	snapshot, err := buildClaimSnapshot(r.Context(), tx, prev.PatientID, ev.Policy.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build the claim")
		return
	}

	adjudication, err := buildAdjudicationSnapshot(r.Context(), tx, prev, chosen, eligible)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build the claim")
		return
	}

	snapshot.OtherInsurance = append(append([]SnapshotOtherInsurance{}, prev.Snapshot.OtherInsurance...), adjudication.otherInsurance)
	snapshot.Adjudication = &adjudication.snapshot

	snapshotJSON, _ := json.Marshal(snapshot)

	billingMethod, err := defaultInsuranceBillingMethod(r.Context(), tx, ev.Policy.PayerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve the submission method")
		return
	}

	var total int64
	for _, c := range chosen {
		total += mustCents(c.Total)
	}

	var claimID string

	err = tx.QueryRow(
		r.Context(),
		`
		INSERT INTO claims (
			patient_id, insurance_policy_id, payer_id, sequence, submission_method,
			total_billed, snapshot, previous_claim_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9)
		RETURNING id
		`,
		prev.PatientID, ev.Policy.ID, ev.Policy.PayerID, ev.NextSequence, submissionMethodOf(billingMethod),
		formatMoney(total), snapshotJSON, prev.ID, nullIfEmpty(userID),
	).Scan(&claimID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create the claim")
		return
	}

	if err := writeClaimContent(r.Context(), tx, claimID, prev.PatientID, ev.NextSequence, diagnoses, lines); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a "+ev.NextSequence+" claim already bills one of these services")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not add services to the claim")
		return
	}

	if err := recordClaimHistory(r.Context(), tx, claimID, "created", "", "draft", "user",
		fmt.Sprintf("%s claim created from %s %s for %s with %s eligible.", capitalize(ev.NextSequence), prev.Sequence, prev.Number, ev.Policy.PayerName, formatMoney(sumEligible(chosen, eligible))), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	if _, err := h.revalidateClaim(r.Context(), tx, claimID, "draft", userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate the claim")
		return
	}

	if err := recordClaimHistory(r.Context(), tx, prev.ID, "forwarded", "", "", "user",
		fmt.Sprintf("Remaining insurance responsibility forwarded to %s claim.", ev.NextSequence), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	// The earlier claim is now fully handled: its remainder lives on the new claim.
	if _, err := refreshClaimResolution(r.Context(), tx, prev.ID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the claim")
		return
	}

	created, err := h.getClaim(r.Context(), h.db, claimID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the claim")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func sumEligible(chosen []claimChargeRow, eligible map[string]NextLineEval) int64 {
	var total int64
	for _, c := range chosen {
		total += mustCents(eligible[c.ID].EligibleAmount)
	}

	return total
}

// =========================================================
// ADJUDICATION SNAPSHOT
// =========================================================

type SnapshotAdjudicationLine struct {
	ChargeID             string `json:"charge_id"`
	PreviousClaimLineID  string `json:"previous_claim_line_id"`
	LineNumber           int    `json:"line_number"`
	DateOfService        string `json:"date_of_service"`
	ServiceCode          string `json:"service_code"`
	Billed               string `json:"billed"`
	PreviousPaid         string `json:"previous_paid"`
	PreviousAllowed      string `json:"previous_allowed"`
	PreviousAdjustments  string `json:"previous_adjustments"`
	TransferredToPatient string `json:"transferred_to_patient"`
	EligibleAmount       string `json:"eligible_amount"`
}

// SnapshotAdjudication freezes what the earlier payer decided when the
// follow-on claim was created.
type SnapshotAdjudication struct {
	PreviousClaimID     string                     `json:"previous_claim_id"`
	PreviousClaimNumber string                     `json:"previous_claim_number"`
	PreviousSequence    string                     `json:"previous_sequence"`
	PreviousPayerName   string                     `json:"previous_payer_name"`
	TotalEligible       string                     `json:"total_eligible"`
	Lines               []SnapshotAdjudicationLine `json:"lines"`
}

type adjudicationResult struct {
	snapshot       SnapshotAdjudication
	otherInsurance SnapshotOtherInsurance
}

func buildAdjudicationSnapshot(ctx context.Context, q queryRower, prev previousClaimInfo, chosen []claimChargeRow, eligible map[string]NextLineEval) (adjudicationResult, error) {
	res := adjudicationResult{snapshot: SnapshotAdjudication{
		PreviousClaimID: prev.ID, PreviousClaimNumber: prev.Number, PreviousSequence: prev.Sequence,
		PreviousPayerName: prev.Snapshot.Payer.Name, Lines: []SnapshotAdjudicationLine{},
	}}

	var totalEligible, totalPaid, totalAllowed int64
	allAllowed := true

	for _, c := range chosen {
		var line SnapshotAdjudicationLine

		var lineNumber int
		var lineID, billed, paid, adjusted, transferred string
		var allowed *string

		if err := q.QueryRow(
			ctx,
			`
			SELECT cl.id, cl.line_number, cl.line_total::text,
				COALESCE((SELECT SUM(a.amount_paid) FROM insurance_payment_allocations a JOIN insurance_payments p ON p.id = a.payment_id
					WHERE a.claim_line_id = cl.id AND a.status = 'active' AND p.status = 'posted'), 0)::numeric(12,2)::text,
				(SELECT a.allowed_amount FROM insurance_payment_allocations a JOIN insurance_payments p ON p.id = a.payment_id
					WHERE a.claim_line_id = cl.id AND a.status = 'active' AND p.status = 'posted' AND a.allowed_amount IS NOT NULL
					ORDER BY a.created_at DESC, a.id DESC LIMIT 1)::numeric(12,2)::text,
				COALESCE((SELECT SUM(ad.amount) FROM billing_adjustments ad JOIN insurance_payment_allocations a ON a.id = ad.insurance_allocation_id
					WHERE a.claim_line_id = cl.id AND ad.status = 'active' AND a.status = 'active'), 0)::numeric(12,2)::text,
				COALESCE((SELECT SUM(t.amount) FROM responsibility_transfers t JOIN insurance_payment_allocations a ON a.id = t.insurance_allocation_id
					WHERE a.claim_line_id = cl.id AND t.status = 'active' AND a.status = 'active' AND t.to_party = 'patient'), 0)::numeric(12,2)::text
			FROM claim_lines cl
			WHERE cl.claim_id = $1 AND cl.charge_id = $2 AND cl.is_current
			`,
			prev.ID, c.ID,
		).Scan(&lineID, &lineNumber, &billed, &paid, &allowed, &adjusted, &transferred); err != nil {
			return res, err
		}

		line = SnapshotAdjudicationLine{
			ChargeID: c.ID, PreviousClaimLineID: lineID, LineNumber: lineNumber,
			DateOfService: c.DateOfService, ServiceCode: c.ServiceCode, Billed: billed,
			PreviousPaid: paid, PreviousAdjustments: adjusted, TransferredToPatient: transferred,
			EligibleAmount: eligible[c.ID].EligibleAmount,
		}

		if allowed != nil {
			line.PreviousAllowed = *allowed
			totalAllowed += mustCents(*allowed)
		} else {
			allAllowed = false
		}

		totalEligible += mustCents(line.EligibleAmount)
		totalPaid += mustCents(paid)
		res.snapshot.Lines = append(res.snapshot.Lines, line)
	}

	res.snapshot.TotalEligible = formatMoney(totalEligible)

	insuredName := strings.TrimSpace(prev.Snapshot.Insured.FirstName + " " + prev.Snapshot.Insured.LastName)

	res.otherInsurance = SnapshotOtherInsurance{
		Sequence:    prev.Sequence,
		PayerName:   prev.Snapshot.Payer.Name,
		MemberID:    prev.Snapshot.Insured.MemberID,
		PolicyGroup: prev.Snapshot.Insured.PolicyGroup,
		InsuredName: insuredName,
		ClaimNumber: prev.Number,
		AmountPaid:  formatMoney(totalPaid),
	}

	if allAllowed && len(chosen) > 0 {
		res.otherInsurance.AdjudicatedFor = formatMoney(totalAllowed)
	}

	return res, nil
}

// =========================================================
// FOLLOW-ON CLAIM LINKS
// =========================================================

func (h *Handler) listFollowUpClaims(ctx context.Context, q queryRower, claimID string) ([]ClaimRef, error) {
	rows, err := q.Query(
		ctx,
		`SELECT id, claim_number, sequence, status FROM claims WHERE previous_claim_id = $1 ORDER BY created_at`,
		claimID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ClaimRef{}

	for rows.Next() {
		var c ClaimRef
		if err := rows.Scan(&c.ID, &c.ClaimNumber, &c.Sequence, &c.Status); err != nil {
			return nil, err
		}
		result = append(result, c)
	}

	return result, rows.Err()
}

// refreshPreviousClaim re-evaluates the earlier claim after a follow-on claim
// was voided or cancelled (its remainder is no longer forwarded).
func refreshPreviousClaim(ctx context.Context, tx pgx.Tx, claimID, userID string) error {
	var previous *string
	if err := tx.QueryRow(ctx, `SELECT previous_claim_id::text FROM claims WHERE id = $1`, claimID).Scan(&previous); err != nil {
		return err
	}

	if previous == nil {
		return nil
	}

	if _, err := lockClaim(ctx, tx, *previous); err != nil {
		return err
	}

	if err := recordClaimHistory(ctx, tx, *previous, "followup_cancelled", "", "", "system",
		"The follow-on claim was voided; the remaining insurance responsibility is no longer forwarded.", userID); err != nil {
		return err
	}

	_, err := refreshClaimResolution(ctx, tx, *previous, userID)

	return err
}
