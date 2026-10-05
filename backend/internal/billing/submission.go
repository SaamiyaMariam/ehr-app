package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var eventTypePattern = regexp.MustCompile(`^[a-z_]{1,40}$`)

type SubmissionRequest struct {
	SubmittedOn string `json:"submitted_on"`
	Reference   string `json:"reference"`
	Comment     string `json:"comment"`
}

// cleanSubmissionRequest validates optional submission details.
func cleanSubmissionRequest(req *SubmissionRequest, today time.Time) string {
	req.SubmittedOn = strings.TrimSpace(req.SubmittedOn)
	req.Reference = strings.TrimSpace(req.Reference)
	req.Comment = strings.TrimSpace(req.Comment)

	if req.SubmittedOn == "" {
		req.SubmittedOn = today.Format("2006-01-02")
	}

	d, ok := parseDate(req.SubmittedOn)
	if !ok {
		return "submission date must be a valid date"
	}

	if isFutureDate(d, today) {
		return "submission date cannot be in the future"
	}

	if len(req.Reference) > 100 {
		return "reference must be 100 characters or fewer"
	}

	if len(req.Comment) > 2000 {
		return "comment must be 2000 characters or fewer"
	}

	return ""
}

// submissionTarget decides the status a claim moves to when it leaves the
// practice. Void resubmissions end the claim; later submissions of a claim
// that was already sent are resubmissions.
func submissionTarget(method, resubmissionType string, previouslySubmitted bool) string {
	switch {
	case resubmissionType == "void":
		return "voided"
	case method == "external":
		return "externally_submitted"
	case previouslySubmitted:
		return "resubmitted"
	default:
		return "submitted"
	}
}

type submissionContext struct {
	claimID, fromStatus, method, eventType, source, message string
	clearinghouse                                           string
	req                                                     SubmissionRequest
	userID                                                  string
}

// completeSubmission records that a (locked, validated) claim left the
// practice: authorization usage, status transition, submission row.
func (h *Handler) completeSubmission(ctx context.Context, tx pgx.Tx, sc submissionContext) (string, error) {
	var resubmissionType, controlNumber string
	var previouslySubmitted bool

	if err := tx.QueryRow(
		ctx,
		`SELECT resubmission_type, COALESCE(payer_claim_control_number, ''), submitted_at IS NOT NULL FROM claims WHERE id = $1`,
		sc.claimID,
	).Scan(&resubmissionType, &controlNumber, &previouslySubmitted); err != nil {
		return "", err
	}

	target := submissionTarget(sc.method, resubmissionType, previouslySubmitted)

	if target != "voided" {
		// Idempotent per (authorization, service): resubmissions never
		// consume again.
		if err := consumeClaimAuthorizations(ctx, tx, sc.claimID, sc.eventType, sc.userID); err != nil {
			return "", err
		}
	}

	message := sc.message
	if resubmissionType != "new" {
		message = fmt.Sprintf("%s (%s resubmission, payer control number %s)", message, resubmissionType, controlNumber)
	}
	if sc.req.Reference != "" {
		message += " Reference: " + sc.req.Reference + "."
	}
	if sc.req.Comment != "" {
		message += " " + sc.req.Comment
	}

	if err := transitionClaim(ctx, tx, sc.claimID, sc.fromStatus, target, sc.eventType, sc.source, message, sc.userID); err != nil {
		return "", err
	}

	if target == "voided" {
		if err := releaseClaimLines(ctx, tx, sc.claimID, sc.userID); err != nil {
			return "", err
		}
	}

	if _, err := tx.Exec(
		ctx,
		`
		UPDATE claims
		SET submitted_at = NOW(), external_reference = COALESCE(NULLIF($2, ''), external_reference), updated_at = NOW()
		WHERE id = $1
		`,
		sc.claimID, sc.req.Reference,
	); err != nil {
		return "", err
	}

	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO claim_submissions (
			claim_id, submission_method, resubmission_type, payer_claim_control_number,
			submitted_on, external_reference, clearinghouse, comment, created_by
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::date, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9)
		`,
		sc.claimID, sc.method, resubmissionType, controlNumber,
		sc.req.SubmittedOn, sc.req.Reference, sc.clearinghouse, sc.req.Comment, nullIfEmpty(sc.userID),
	); err != nil {
		return "", err
	}

	return target, nil
}

// claimActionTx runs a claim workflow action inside a transaction with the
// claim row locked, then responds with the updated claim.
func (h *Handler) claimActionTx(
	w http.ResponseWriter,
	r *http.Request,
	fallback string,
	action func(tx pgx.Tx, claimID, status, method string) error,
) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}
	defer tx.Rollback(r.Context())

	status, err := lockClaim(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}

	var method string
	if err := tx.QueryRow(r.Context(), `SELECT submission_method FROM claims WHERE id = $1`, id).Scan(&method); err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}

	if err := action(tx, id, status, method); err != nil {
		var actionErr *claimActionError
		if errors.As(err, &actionErr) && actionErr.validation != nil {
			// Persist the validation result that blocked the action.
			_ = tx.Commit(r.Context())
		}

		if errors.Is(err, errInvalidClaimTransition) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		writeClaimActionError(w, err, fallback)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}

	claim, err := h.getClaim(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim")
		return
	}

	writeJSON(w, http.StatusOK, claim)
}

func conflict(message string) error {
	return &claimActionError{status: http.StatusConflict, message: message}
}

func badRequest(message string) error {
	return &claimActionError{status: http.StatusBadRequest, message: message}
}

func decodeSubmission(r *http.Request) (SubmissionRequest, error) {
	var req SubmissionRequest

	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, badRequest("invalid request body")
		}
	}

	if message := cleanSubmissionRequest(&req, time.Now()); message != "" {
		return req, badRequest(message)
	}

	return req, nil
}

// MarkMailed records that a generated paper claim was mailed.
func (h *Handler) MarkMailed(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not mark claim as mailed", func(tx pgx.Tx, id, status, method string) error {
		req, err := decodeSubmission(r)
		if err != nil {
			return err
		}

		if method != "paper" {
			return conflict("only paper claims are mailed")
		}

		if status != "paper_generated" {
			if claimEditable(status) {
				return conflict("generate the CMS-1500 before marking the claim as mailed")
			}
			return conflict("this claim has already been marked as mailed")
		}

		_, err = h.completeSubmission(r.Context(), tx, submissionContext{
			claimID: id, fromStatus: status, method: method, eventType: "mailed", source: "user",
			message: "Paper claim mailed to payer on " + req.SubmittedOn + ".", req: req, userID: userID,
		})

		return err
	})
}

// MarkSubmittedExternally records the user's confirmation that the claim
// was submitted outside this application. Nothing is transmitted.
func (h *Handler) MarkSubmittedExternally(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not mark claim as submitted", func(tx pgx.Tx, id, status, method string) error {
		req, err := decodeSubmission(r)
		if err != nil {
			return err
		}

		if method != "external" {
			return conflict("only external claims can be marked as submitted externally")
		}

		if !claimEditable(status) {
			return conflict("this claim has already been submitted")
		}

		newStatus, err := h.ensureSubmittable(r.Context(), tx, id, status, userID)
		if err != nil {
			return err
		}

		_, err = h.completeSubmission(r.Context(), tx, submissionContext{
			claimID: id, fromStatus: newStatus, method: method, eventType: "submitted_externally", source: "user",
			message: "User confirmed the claim was submitted outside this application on " + req.SubmittedOn + ".",
			req:     req, userID: userID,
		})

		return err
	})
}

// SubmitElectronic hands a claim to the configured clearinghouse adapter.
// Without one it returns CLEARINGHOUSE_NOT_CONFIGURED and changes nothing
// except the recorded validation result.
func (h *Handler) SubmitElectronic(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not submit claim", func(tx pgx.Tx, id, status, method string) error {
		if method != "electronic" {
			return conflict("only electronic claims can be submitted electronically")
		}

		if !claimEditable(status) {
			return conflict("this claim has already been submitted")
		}

		newStatus, err := h.ensureSubmittable(r.Context(), tx, id, status, userID)
		if err != nil {
			return err
		}

		if h.clearinghouse == nil {
			// Commit the validation result, then report the boundary.
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}

			return &claimActionError{
				status:  http.StatusServiceUnavailable,
				message: ClearinghouseNotConfigured + ": the electronic claim is prepared and valid, but no clearinghouse integration is configured. Nothing was sent.",
			}
		}

		claim, err := h.getClaim(r.Context(), tx, id)
		if err != nil {
			return err
		}

		payload := buildClearinghousePayload(claim)

		if problems := h.clearinghouse.Validate(payload); len(problems) > 0 {
			return &claimActionError{
				status: http.StatusUnprocessableEntity, message: "clearinghouse rejected the payload",
				validation: &ClaimValidation{Errors: problems, Warnings: []string{}},
			}
		}

		result, err := h.clearinghouse.Submit(r.Context(), payload)
		if err != nil {
			return &claimActionError{status: http.StatusBadGateway, message: "clearinghouse submission failed: " + err.Error()}
		}

		req := SubmissionRequest{SubmittedOn: time.Now().Format("2006-01-02"), Reference: result.ExternalID}

		switch {
		case result.Accepted:
			_, err = h.completeSubmission(r.Context(), tx, submissionContext{
				claimID: id, fromStatus: newStatus, method: method, eventType: "submitted", source: "clearinghouse",
				message:       "Accepted for submission by " + h.clearinghouse.Name() + ". " + result.Message,
				clearinghouse: h.clearinghouse.Name(), req: req, userID: userID,
			})
			return err
		case result.Queued:
			return transitionClaim(r.Context(), tx, id, newStatus, "pending_submission", "queued", "clearinghouse",
				"Queued by "+h.clearinghouse.Name()+". "+result.Message, userID)
		default:
			return &claimActionError{status: http.StatusBadGateway, message: "clearinghouse did not accept the claim: " + result.Message}
		}
	})
}

// ElectronicPayload previews the provider-neutral payload for a claim.
func (h *Handler) ElectronicPayload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}

	claim, err := h.getClaim(r.Context(), h.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build payload")
		return
	}

	writeJSON(w, http.StatusOK, buildClearinghousePayload(claim))
}

// RecordRejection records a rejection received from the payer or
// clearinghouse (entered manually by the biller).
func (h *Handler) RecordRejection(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not record rejection", func(tx pgx.Tx, id, status, method string) error {
		var req struct {
			Reason     string `json:"reason"`
			RejectedOn string `json:"rejected_on"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return badRequest("invalid request body")
		}

		req.Reason = strings.TrimSpace(req.Reason)
		if req.Reason == "" || len(req.Reason) > 2000 {
			return badRequest("a rejection reason (up to 2000 characters) is required")
		}

		if req.RejectedOn != "" {
			if _, ok := parseDate(req.RejectedOn); !ok {
				return badRequest("rejection date must be a valid date")
			}
		}

		if !validEnum(status, "submitted", "sent", "resubmitted", "externally_submitted", "pending_submission") {
			return conflict("only submitted claims can be rejected")
		}

		message := "Rejected: " + req.Reason
		if req.RejectedOn != "" {
			message = fmt.Sprintf("Rejected on %s: %s", req.RejectedOn, req.Reason)
		}

		return transitionClaim(r.Context(), tx, id, status, "rejected_new", "rejected", "payer", message, userID)
	})
}

func (h *Handler) MarkRejectionReviewed(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not update claim", func(tx pgx.Tx, id, status, method string) error {
		var req struct {
			Comment string `json:"comment"`
		}

		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				return badRequest("invalid request body")
			}
		}

		req.Comment = strings.TrimSpace(req.Comment)
		if len(req.Comment) > 2000 {
			return badRequest("comment must be 2000 characters or fewer")
		}

		if status != "rejected_new" {
			return conflict("only newly rejected claims can be marked reviewed")
		}

		return transitionClaim(r.Context(), tx, id, status, "rejected", "reviewed", "user",
			strings.TrimSpace("Rejection reviewed. "+req.Comment), userID)
	})
}

// StartResubmission reopens a sent claim as an editable draft with new /
// amended (replacement) / void resubmission details.
func (h *Handler) StartResubmission(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.claimActionTx(w, r, "could not start resubmission", func(tx pgx.Tx, id, status, method string) error {
		var req struct {
			ResubmissionType        string `json:"resubmission_type"`
			PayerClaimControlNumber string `json:"payer_claim_control_number"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return badRequest("invalid request body")
		}

		req.ResubmissionType = strings.ToLower(strings.TrimSpace(req.ResubmissionType))
		req.PayerClaimControlNumber = strings.TrimSpace(req.PayerClaimControlNumber)

		if req.ResubmissionType == "new" {
			req.PayerClaimControlNumber = ""
		}

		if message := validateResubmission(req.ResubmissionType, req.PayerClaimControlNumber); message != "" {
			return badRequest(message)
		}

		if claimEditable(status) || status == "voided" || status == "pending_submission" {
			return conflict("only claims that were sent (or generated) can be resubmitted")
		}

		var previouslySubmitted bool
		if err := tx.QueryRow(r.Context(), `SELECT submitted_at IS NOT NULL FROM claims WHERE id = $1`, id).Scan(&previouslySubmitted); err != nil {
			return err
		}

		if !previouslySubmitted && req.ResubmissionType != "new" {
			return badRequest("this claim was never sent, so it can only be corrected as a new claim")
		}

		// A claim that was followed by a claim to the next payer cannot be
		// reopened: the follow-on claim snapshots this claim's adjudication.
		var followUp *string
		if err := tx.QueryRow(
			r.Context(),
			`SELECT claim_number FROM claims WHERE previous_claim_id = $1 AND status <> 'voided' LIMIT 1`,
			id,
		).Scan(&followUp); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if followUp != nil {
			return conflict("claim " + *followUp + " was created from this claim for the next payer; cancel it before reopening this claim")
		}

		// Editing a claim rewrites its lines, which posted remittances point
		// at; reverse the payments first.
		var posted bool
		if err := tx.QueryRow(
			r.Context(),
			`SELECT EXISTS(
				SELECT 1 FROM insurance_payment_allocations a
				JOIN insurance_payments p ON p.id = a.payment_id
				WHERE a.claim_id = $1 AND a.status = 'active' AND p.status = 'posted'
			)`,
			id,
		).Scan(&posted); err != nil {
			return err
		}

		if posted {
			return conflict("insurance payments are posted to this claim; void them before reopening it for resubmission")
		}

		if _, err := tx.Exec(
			r.Context(),
			`UPDATE claims SET resubmission_type = $1, payer_claim_control_number = NULLIF($2, '') WHERE id = $3`,
			req.ResubmissionType, req.PayerClaimControlNumber, id,
		); err != nil {
			return err
		}

		message := "Claim reopened for correction as a " + req.ResubmissionType + " claim."
		if req.PayerClaimControlNumber != "" {
			message += " Payer control number " + req.PayerClaimControlNumber + "."
		}

		return transitionClaim(r.Context(), tx, id, status, "draft", "resubmission_started", "user", message, userID)
	})
}

// =========================================================
// CLAIM HISTORY FEED
// =========================================================

type ClaimHistoryItem struct {
	ClaimHistoryEvent
	ClaimNumber      string `json:"claim_number"`
	PatientID        string `json:"patient_id"`
	PatientName      string `json:"patient_name"`
	PayerName        string `json:"payer_name"`
	Sequence         string `json:"sequence"`
	ClaimStatus      string `json:"claim_status"`
	SubmissionMethod string `json:"submission_method"`
}

func (h *Handler) ListClaimHistory(w http.ResponseWriter, r *http.Request) {
	filter, message := buildClaimFilter(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	q := r.URL.Query()

	if v := strings.TrimSpace(q.Get("event_type")); v != "" {
		if !eventTypePattern.MatchString(v) {
			writeError(w, http.StatusBadRequest, "event_type is invalid")
			return
		}
		filter.add("h.event_type = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("event_from")); v != "" {
		if _, ok := parseDate(v); !ok {
			writeError(w, http.StatusBadRequest, "event_from must be a valid date")
			return
		}
		filter.add("h.created_at >= $%d::date", v)
	}

	if v := strings.TrimSpace(q.Get("event_to")); v != "" {
		if _, ok := parseDate(v); !ok {
			writeError(w, http.StatusBadRequest, "event_to must be a valid date")
			return
		}
		filter.add("h.created_at < $%d::date + 1", v)
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(filter.where) > 0 {
		where = "WHERE " + strings.Join(filter.where, " AND ")
	}

	from := `
		FROM claim_history h
		JOIN claims cm ON cm.id = h.claim_id
		JOIN patients pt ON pt.id = cm.patient_id
		JOIN payers py ON py.id = cm.payer_id
		LEFT JOIN users u ON u.id = h.created_by
	` + where

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) `+from, filter.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim history")
		return
	}

	args := append(filter.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT h.id, h.claim_id, h.event_type, COALESCE(h.from_status, ''), COALESCE(h.to_status, ''),
			h.source, COALESCE(h.message, ''), COALESCE(u.first_name || ' ' || u.last_name, ''),
			TO_CHAR(h.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.MSOF'),
			cm.claim_number, cm.patient_id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
			py.payer_name, cm.sequence, cm.status, cm.submission_method
		`+from+`
		ORDER BY h.created_at DESC, h.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load claim history")
		return
	}
	defer rows.Close()

	result := pagedResult[ClaimHistoryItem]{Items: []ClaimHistoryItem{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var item ClaimHistoryItem
		e := &item.ClaimHistoryEvent

		if err := rows.Scan(
			&e.ID, &e.ClaimID, &e.EventType, &e.FromStatus, &e.ToStatus, &e.Source, &e.Message, &e.CreatedBy, &e.CreatedAt,
			&item.ClaimNumber, &item.PatientID, &item.PatientName, &item.PayerName, &item.Sequence, &item.ClaimStatus, &item.SubmissionMethod,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load claim history")
			return
		}

		result.Items = append(result.Items, item)
	}

	writeJSON(w, http.StatusOK, result)
}
