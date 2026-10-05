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

var paymentMethods = []string{"cash", "check", "external_card", "external_other"}

// =========================================================
// TYPES
// =========================================================

type PaymentAllocation struct {
	ID            string `json:"id"`
	ChargeID      string `json:"charge_id"`
	Amount        string `json:"amount"`
	Status        string `json:"status"`
	DateOfService string `json:"date_of_service"`
	ServiceCode   string `json:"service_code"`
	CreatedAt     string `json:"created_at"`
}

type PaymentRefund struct {
	ID              string `json:"id"`
	Amount          string `json:"amount"`
	RefundDate      string `json:"refund_date"`
	Method          string `json:"method"`
	ReferenceNumber string `json:"reference_number"`
	Reason          string `json:"reason"`
	CreatedAt       string `json:"created_at"`
}

type PatientPayment struct {
	ID              string              `json:"id"`
	PatientID       string              `json:"patient_id"`
	PatientName     string              `json:"patient_name"`
	PaymentDate     string              `json:"payment_date"`
	Amount          string              `json:"amount"`
	Method          string              `json:"method"`
	ReferenceNumber string              `json:"reference_number"`
	CheckNumber     string              `json:"check_number"`
	Notes           string              `json:"notes"`
	Status          string              `json:"status"`
	VoidReason      string              `json:"void_reason"`
	Allocated       string              `json:"allocated"`
	Refunded        string              `json:"refunded"`
	Unallocated     string              `json:"unallocated"`
	CreatedBy       string              `json:"created_by"`
	CreatedAt       string              `json:"created_at"`
	Allocations     []PaymentAllocation `json:"allocations,omitempty"`
	Refunds         []PaymentRefund     `json:"refunds,omitempty"`
}

type AllocationRequest struct {
	ChargeID string `json:"charge_id"`
	Amount   string `json:"amount"`
}

type plannedAllocation struct {
	ChargeID string
	Amount   int64
}

type openCharge struct {
	ID            string
	DateOfService string
	CreatedAt     time.Time
	Balance       int64
}

// =========================================================
// PURE ALLOCATION RULES
// =========================================================

// planExplicitAllocations validates requested allocations against what the
// payment has left and each charge's open balance (patient or insurance,
// depending on the caller). Allocations never exceed the open balance;
// money beyond it stays unapplied credit.
func planExplicitAllocations(requests []AllocationRequest, balances map[string]int64, available int64) ([]plannedAllocation, string) {
	var plan []plannedAllocation
	var total int64
	perCharge := map[string]int64{}

	for _, req := range requests {
		cents, ok := parseMoney(req.Amount)
		if !ok || cents <= 0 {
			return nil, "allocation amounts must be positive amounts with at most 2 decimal places"
		}

		balance, ok := balances[req.ChargeID]
		if !ok {
			return nil, "allocations must target this patient's open services"
		}

		perCharge[req.ChargeID] += cents
		if perCharge[req.ChargeID] > balance {
			return nil, fmt.Sprintf("allocation exceeds the open balance of %s on a service", formatMoney(balance))
		}

		total += cents
		plan = append(plan, plannedAllocation{ChargeID: req.ChargeID, Amount: cents})
	}

	if total > available {
		return nil, fmt.Sprintf("allocations total %s but only %s of the payment is unapplied", formatMoney(total), formatMoney(available))
	}

	return plan, ""
}

// planAutoAllocation applies available money to the oldest open balances
// first (by date of service, then entry time).
func planAutoAllocation(available int64, charges []openCharge) []plannedAllocation {
	sorted := append([]openCharge(nil), charges...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].DateOfService != sorted[j].DateOfService {
			return sorted[i].DateOfService < sorted[j].DateOfService
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	var plan []plannedAllocation

	for _, c := range sorted {
		if available <= 0 {
			break
		}

		if c.Balance <= 0 {
			continue
		}

		amount := min(available, c.Balance)
		plan = append(plan, plannedAllocation{ChargeID: c.ID, Amount: amount})
		available -= amount
	}

	return plan
}

type PaymentInput struct {
	PaymentDate     string              `json:"payment_date"`
	Amount          string              `json:"amount"`
	Method          string              `json:"method"`
	ReferenceNumber string              `json:"reference_number"`
	CheckNumber     string              `json:"check_number"`
	Notes           string              `json:"notes"`
	IdempotencyKey  string              `json:"idempotency_key"`
	Allocations     []AllocationRequest `json:"allocations"`
	AutoAllocate    bool                `json:"auto_allocate"`
}

func validatePaymentInput(in *PaymentInput, today time.Time) string {
	in.PaymentDate = strings.TrimSpace(in.PaymentDate)
	in.Amount = strings.TrimSpace(in.Amount)
	in.Method = strings.ToLower(strings.TrimSpace(in.Method))
	in.ReferenceNumber = strings.TrimSpace(in.ReferenceNumber)
	in.CheckNumber = strings.TrimSpace(in.CheckNumber)
	in.Notes = strings.TrimSpace(in.Notes)
	in.IdempotencyKey = strings.ToLower(strings.TrimSpace(in.IdempotencyKey))

	if !isUUID(in.IdempotencyKey) {
		return "an idempotency key (UUID) is required"
	}

	d, ok := parseDate(in.PaymentDate)
	if !ok {
		return "payment date must be a valid date"
	}

	if d.After(today) {
		return "payment date cannot be in the future"
	}

	cents, ok := parseMoney(in.Amount)
	if !ok || cents <= 0 {
		return "amount must be greater than zero with at most 2 decimal places"
	}

	if !validEnum(in.Method, paymentMethods...) {
		return "method must be cash, check, external_card or external_other"
	}

	if in.Method == "check" && in.CheckNumber == "" {
		return "a check number is required for check payments"
	}

	if len(in.ReferenceNumber) > 100 || len(in.CheckNumber) > 50 || len(in.Notes) > 2000 {
		return "reference, check number or notes are too long"
	}

	// Never accept anything that looks like a full card number.
	if looksLikeCardNumber(in.ReferenceNumber) || looksLikeCardNumber(in.Notes) {
		return "do not enter card numbers; record only the processor's transaction reference"
	}

	if len(in.Allocations) > 200 {
		return "too many allocations"
	}

	if in.AutoAllocate && len(in.Allocations) > 0 {
		return "choose either explicit allocations or automatic allocation"
	}

	for i := range in.Allocations {
		in.Allocations[i].ChargeID = strings.ToLower(strings.TrimSpace(in.Allocations[i].ChargeID))
		if !isUUID(in.Allocations[i].ChargeID) {
			return "allocations must reference services"
		}
	}

	return ""
}

// looksLikeCardNumber flags 13-19 digit runs (ignoring spaces / dashes),
// which could be a primary account number.
func looksLikeCardNumber(value string) bool {
	run := 0

	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			run++
			if run >= 13 {
				return true
			}
		case r == ' ' || r == '-':
			// separators inside a card number
		default:
			run = 0
		}
	}

	return false
}

// =========================================================
// DATA ACCESS
// =========================================================

const selectPatientPayment = `
	SELECT
		p.id, p.patient_id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
		TO_CHAR(p.payment_date, 'YYYY-MM-DD'), p.amount::text, p.method,
		COALESCE(p.reference_number, ''), COALESCE(p.check_number, ''), COALESCE(p.notes, ''),
		p.status, COALESCE(p.void_reason, ''),
		COALESCE((SELECT SUM(a.amount) FROM patient_payment_allocations a WHERE a.payment_id = p.id AND a.status = 'active'), 0)::numeric(12,2)::text,
		COALESCE((SELECT SUM(f.amount) FROM patient_payment_refunds f WHERE f.payment_id = p.id), 0)::numeric(12,2)::text,
		COALESCE(u.first_name || ' ' || u.last_name, ''),
		TO_CHAR(p.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
	FROM patient_payments p
	JOIN patients pt ON pt.id = p.patient_id
	LEFT JOIN users u ON u.id = p.created_by
`

func scanPatientPayment(row pgx.Row, p *PatientPayment) error {
	err := row.Scan(
		&p.ID, &p.PatientID, &p.PatientName, &p.PaymentDate, &p.Amount, &p.Method,
		&p.ReferenceNumber, &p.CheckNumber, &p.Notes, &p.Status, &p.VoidReason,
		&p.Allocated, &p.Refunded, &p.CreatedBy, &p.CreatedAt,
	)
	if err != nil {
		return err
	}

	unallocated := int64(0)
	if p.Status == "posted" {
		unallocated = mustCents(p.Amount) - mustCents(p.Allocated) - mustCents(p.Refunded)
	}
	p.Unallocated = formatMoney(unallocated)

	return nil
}

func (h *Handler) getPatientPayment(ctx context.Context, q queryRower, id string) (PatientPayment, error) {
	var p PatientPayment

	if err := scanPatientPayment(q.QueryRow(ctx, selectPatientPayment+`WHERE p.id = $1`, id), &p); err != nil {
		return p, err
	}

	rows, err := q.Query(
		ctx,
		`
		SELECT a.id, a.charge_id, a.amount::text, a.status, TO_CHAR(c.date_of_service, 'YYYY-MM-DD'),
			sc.code, TO_CHAR(a.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM patient_payment_allocations a
		JOIN billing_charges c ON c.id = a.charge_id
		JOIN service_codes sc ON sc.id = c.service_code_id
		WHERE a.payment_id = $1
		ORDER BY a.created_at, c.date_of_service
		`,
		id,
	)
	if err != nil {
		return p, err
	}

	p.Allocations = []PaymentAllocation{}

	for rows.Next() {
		var a PaymentAllocation
		if err := rows.Scan(&a.ID, &a.ChargeID, &a.Amount, &a.Status, &a.DateOfService, &a.ServiceCode, &a.CreatedAt); err != nil {
			rows.Close()
			return p, err
		}
		p.Allocations = append(p.Allocations, a)
	}
	rows.Close()

	rows, err = q.Query(
		ctx,
		`
		SELECT id, amount::text, TO_CHAR(refund_date, 'YYYY-MM-DD'), method, COALESCE(reference_number, ''),
			reason, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM patient_payment_refunds WHERE payment_id = $1 ORDER BY created_at
		`,
		id,
	)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	p.Refunds = []PaymentRefund{}

	for rows.Next() {
		var f PaymentRefund
		if err := rows.Scan(&f.ID, &f.Amount, &f.RefundDate, &f.Method, &f.ReferenceNumber, &f.Reason, &f.CreatedAt); err != nil {
			return p, err
		}
		p.Refunds = append(p.Refunds, f)
	}

	return p, rows.Err()
}

// lockOpenPatientBalances locks a patient's active charges (in id order)
// and returns their current patient balances. Locking the charges first
// serializes concurrent allocations to the same services.
func lockOpenPatientBalances(ctx context.Context, tx pgx.Tx, patientID string) ([]openCharge, error) {
	if _, err := tx.Exec(
		ctx,
		`SELECT id FROM billing_charges WHERE patient_id = $1 AND status = 'active' ORDER BY id FOR UPDATE`,
		patientID,
	); err != nil {
		return nil, err
	}

	rows, err := tx.Query(
		ctx,
		`
		SELECT c.id, TO_CHAR(c.date_of_service, 'YYYY-MM-DD'), c.created_at, b.patient_balance::text
		FROM billing_charges c
		JOIN billing_charge_balances b ON b.charge_id = c.id
		WHERE c.patient_id = $1 AND c.status = 'active'
		`,
		patientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []openCharge

	for rows.Next() {
		var c openCharge
		var balance string
		if err := rows.Scan(&c.ID, &c.DateOfService, &c.CreatedAt, &balance); err != nil {
			return nil, err
		}
		c.Balance = mustCents(balance)
		result = append(result, c)
	}

	return result, rows.Err()
}

// applyPatientAllocations validates and writes allocations for a locked
// payment. Returns the allocations written.
func applyPatientAllocations(ctx context.Context, tx pgx.Tx, paymentID, patientID string, requests []AllocationRequest, auto bool, available int64, userID string) ([]plannedAllocation, string, error) {
	if !auto && len(requests) == 0 {
		return nil, "", nil
	}

	charges, err := lockOpenPatientBalances(ctx, tx, patientID)
	if err != nil {
		return nil, "", err
	}

	var plan []plannedAllocation
	var message string

	if auto {
		plan = planAutoAllocation(available, charges)
	} else {
		balances := map[string]int64{}
		for _, c := range charges {
			balances[c.ID] = c.Balance
		}

		plan, message = planExplicitAllocations(requests, balances, available)
		if message != "" {
			return nil, message, nil
		}
	}

	for _, a := range plan {
		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO patient_payment_allocations (payment_id, charge_id, patient_id, amount, created_by)
			VALUES ($1, $2, $3, $4::numeric, $5)
			`,
			paymentID, a.ChargeID, patientID, formatMoney(a.Amount), nullIfEmpty(userID),
		); err != nil {
			return nil, "", err
		}
	}

	return plan, "", nil
}

func distribution(plan []plannedAllocation) []map[string]string {
	out := []map[string]string{}
	for _, a := range plan {
		out = append(out, map[string]string{"charge_id": a.ChargeID, "amount": formatMoney(a.Amount)})
	}
	return out
}

// =========================================================
// HANDLERS
// =========================================================

func (h *Handler) ListPatientPayments(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(r.Context(), selectPatientPayment+`WHERE p.patient_id = $1 ORDER BY p.payment_date DESC, p.created_at DESC LIMIT 500`, patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payments")
		return
	}
	defer rows.Close()

	result := []PatientPayment{}

	for rows.Next() {
		var p PatientPayment
		if err := scanPatientPayment(rows, &p); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load payments")
			return
		}
		result = append(result, p)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetPatientPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}

	p, err := h.getPatientPayment(r.Context(), h.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payment")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) CreatePatientPayment(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var in PaymentInput

	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if message := validatePaymentInput(&in, time.Now()); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	userID := currentUserID(r)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not post payment")
		return
	}
	defer tx.Rollback(r.Context())

	// Idempotency: a retried request returns the payment already posted.
	var existingID, existingPatient string
	err = tx.QueryRow(r.Context(), `SELECT id, patient_id FROM patient_payments WHERE idempotency_key = $1`, in.IdempotencyKey).Scan(&existingID, &existingPatient)
	if err == nil {
		if existingPatient != patientID {
			writeError(w, http.StatusConflict, "this idempotency key was already used for another payment")
			return
		}

		p, err := h.getPatientPayment(r.Context(), tx, existingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load payment")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"payment": p, "distribution": []any{}, "duplicate": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not post payment")
		return
	}

	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`, patientID).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	amount, _ := parseMoney(in.Amount)

	var paymentID string
	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO patient_payments (
			patient_id, payment_date, amount, method, reference_number, check_number, notes, idempotency_key, created_by
		)
		VALUES ($1, $2::date, $3::numeric, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, $9)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id
		`,
		patientID, in.PaymentDate, formatMoney(amount), in.Method, in.ReferenceNumber, in.CheckNumber, in.Notes,
		in.IdempotencyKey, nullIfEmpty(userID),
	).Scan(&paymentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "this payment is already being posted; refresh to see it")
			return
		}
		writeError(w, http.StatusBadRequest, "could not post payment")
		return
	}

	plan, message, err := applyPatientAllocations(r.Context(), tx, paymentID, patientID, in.Allocations, in.AutoAllocate, amount, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not allocate payment")
		return
	}
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not post payment")
		return
	}

	p, err := h.getPatientPayment(r.Context(), h.db, paymentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payment")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"payment": p, "distribution": distribution(plan)})
}

// lockPostedPayment locks a payment and returns its patient and unapplied
// amount; voided payments are rejected.
func lockPostedPayment(ctx context.Context, tx pgx.Tx, paymentID string) (patientID string, available int64, status string, err error) {
	var amount, allocated, refunded string

	err = tx.QueryRow(
		ctx,
		`
		SELECT p.patient_id, p.status, p.amount::text,
			COALESCE((SELECT SUM(a.amount) FROM patient_payment_allocations a WHERE a.payment_id = p.id AND a.status = 'active'), 0)::text,
			COALESCE((SELECT SUM(f.amount) FROM patient_payment_refunds f WHERE f.payment_id = p.id), 0)::text
		FROM patient_payments p
		WHERE p.id = $1
		FOR UPDATE OF p
		`,
		paymentID,
	).Scan(&patientID, &status, &amount, &allocated, &refunded)
	if err != nil {
		return
	}

	available = mustCents(amount) - mustCents(allocated) - mustCents(refunded)
	return
}

func (h *Handler) paymentActionTx(w http.ResponseWriter, r *http.Request, fallback string, action func(tx pgx.Tx, paymentID, patientID, status string, available int64) (any, int, string, error)) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}
	defer tx.Rollback(r.Context())

	patientID, available, status, err := lockPostedPayment(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}

	extra, code, message, err := action(tx, id, patientID, status, available)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}
	if message != "" {
		writeError(w, code, message)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, fallback)
		return
	}

	p, err := h.getPatientPayment(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"payment": p, "distribution": extra})
}

func (h *Handler) AllocatePatientPayment(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.paymentActionTx(w, r, "could not allocate payment", func(tx pgx.Tx, paymentID, patientID, status string, available int64) (any, int, string, error) {
		var req struct {
			Allocations  []AllocationRequest `json:"allocations"`
			AutoAllocate bool                `json:"auto_allocate"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, http.StatusBadRequest, "invalid request body", nil
		}

		if status != "posted" {
			return nil, http.StatusConflict, "voided payments cannot be allocated", nil
		}

		if len(req.Allocations) > 200 || (req.AutoAllocate && len(req.Allocations) > 0) || (!req.AutoAllocate && len(req.Allocations) == 0) {
			return nil, http.StatusBadRequest, "provide allocations or auto_allocate", nil
		}

		for i := range req.Allocations {
			req.Allocations[i].ChargeID = strings.ToLower(strings.TrimSpace(req.Allocations[i].ChargeID))
			if !isUUID(req.Allocations[i].ChargeID) {
				return nil, http.StatusBadRequest, "allocations must reference services", nil
			}
		}

		if available <= 0 {
			return nil, http.StatusConflict, "this payment has no unapplied amount left", nil
		}

		plan, message, err := applyPatientAllocations(r.Context(), tx, paymentID, patientID, req.Allocations, req.AutoAllocate, available, userID)
		if err != nil || message != "" {
			return nil, http.StatusBadRequest, message, err
		}

		return distribution(plan), 0, "", nil
	})
}

func (h *Handler) VoidPatientPaymentAllocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "allocation not found")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not unapply payment")
		return
	}
	defer tx.Rollback(r.Context())

	var paymentID, status string
	err = tx.QueryRow(r.Context(), `SELECT payment_id, status FROM patient_payment_allocations WHERE id = $1`, id).Scan(&paymentID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "allocation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not unapply payment")
		return
	}

	// Lock the payment first (same lock order as allocation).
	if _, _, _, err := lockPostedPayment(r.Context(), tx, paymentID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unapply payment")
		return
	}

	tag, err := tx.Exec(
		r.Context(),
		`UPDATE patient_payment_allocations SET status = 'voided', voided_at = NOW(), voided_by = $2 WHERE id = $1 AND status = 'active'`,
		id, nullIfEmpty(currentUserID(r)),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not unapply payment")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "this allocation is already removed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unapply payment")
		return
	}

	p, err := h.getPatientPayment(r.Context(), h.db, paymentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"payment": p})
}

func (h *Handler) VoidPatientPayment(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.paymentActionTx(w, r, "could not void payment", func(tx pgx.Tx, paymentID, patientID, status string, available int64) (any, int, string, error) {
		var req struct {
			Reason string `json:"reason"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, http.StatusBadRequest, "invalid request body", nil
		}

		req.Reason = strings.TrimSpace(req.Reason)
		if req.Reason == "" || len(req.Reason) > 500 {
			return nil, http.StatusBadRequest, "a void reason (up to 500 characters) is required", nil
		}

		if status == "voided" {
			return nil, http.StatusConflict, "payment is already voided", nil
		}

		var refunds int
		if err := tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM patient_payment_refunds WHERE payment_id = $1`, paymentID).Scan(&refunds); err != nil {
			return nil, 0, "", err
		}

		if refunds > 0 {
			return nil, http.StatusConflict, "this payment has refunds; it cannot be voided", nil
		}

		// Voiding the payment reverses all of its allocations, restoring
		// the services' patient balances.
		if _, err := tx.Exec(
			r.Context(),
			`UPDATE patient_payment_allocations SET status = 'voided', voided_at = NOW(), voided_by = $2 WHERE payment_id = $1 AND status = 'active'`,
			paymentID, nullIfEmpty(userID),
		); err != nil {
			return nil, 0, "", err
		}

		_, err := tx.Exec(
			r.Context(),
			`UPDATE patient_payments SET status = 'voided', void_reason = $2, voided_at = NOW(), voided_by = $3 WHERE id = $1`,
			paymentID, req.Reason, nullIfEmpty(userID),
		)

		return []any{}, 0, "", err
	})
}

func (h *Handler) RefundPatientPayment(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	h.paymentActionTx(w, r, "could not record refund", func(tx pgx.Tx, paymentID, patientID, status string, available int64) (any, int, string, error) {
		var req struct {
			Amount          string `json:"amount"`
			RefundDate      string `json:"refund_date"`
			Method          string `json:"method"`
			ReferenceNumber string `json:"reference_number"`
			Reason          string `json:"reason"`
			IdempotencyKey  string `json:"idempotency_key"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, http.StatusBadRequest, "invalid request body", nil
		}

		req.Reason = strings.TrimSpace(req.Reason)
		req.Method = strings.ToLower(strings.TrimSpace(req.Method))
		req.ReferenceNumber = strings.TrimSpace(req.ReferenceNumber)
		req.IdempotencyKey = strings.ToLower(strings.TrimSpace(req.IdempotencyKey))

		if !isUUID(req.IdempotencyKey) {
			return nil, http.StatusBadRequest, "an idempotency key (UUID) is required", nil
		}

		// Retried refund requests do not refund twice.
		var already bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM patient_payment_refunds WHERE idempotency_key = $1)`, req.IdempotencyKey).Scan(&already); err != nil {
			return nil, 0, "", err
		}
		if already {
			return []any{}, 0, "", nil
		}

		cents, ok := parseMoney(req.Amount)
		if !ok || cents <= 0 {
			return nil, http.StatusBadRequest, "refund amount must be greater than zero", nil
		}

		d, ok := parseDate(req.RefundDate)
		if !ok || d.After(time.Now()) {
			return nil, http.StatusBadRequest, "refund date must be a valid date, not in the future", nil
		}

		if !validEnum(req.Method, paymentMethods...) {
			return nil, http.StatusBadRequest, "invalid refund method", nil
		}

		if req.Reason == "" || len(req.Reason) > 500 || len(req.ReferenceNumber) > 100 || looksLikeCardNumber(req.ReferenceNumber) {
			return nil, http.StatusBadRequest, "a reason (up to 500 characters) is required; references must not contain card numbers", nil
		}

		if status != "posted" {
			return nil, http.StatusConflict, "voided payments cannot be refunded", nil
		}

		// Only unapplied credit can be refunded; unapply allocations first.
		if cents > available {
			return nil, http.StatusBadRequest, fmt.Sprintf("only %s of this payment is unapplied and refundable; unapply it from services first", formatMoney(available)), nil
		}

		_, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO patient_payment_refunds (
				payment_id, patient_id, amount, refund_date, method, reference_number, reason, idempotency_key, created_by
			)
			VALUES ($1, $2, $3::numeric, $4::date, $5, NULLIF($6, ''), $7, $8, $9)
			`,
			paymentID, patientID, formatMoney(cents), req.RefundDate, req.Method, req.ReferenceNumber, req.Reason,
			req.IdempotencyKey, nullIfEmpty(userID),
		)

		return []any{}, 0, "", err
	})
}

// SearchPatientPayments lists payments practice-wide (paged).
func (h *Handler) SearchPatientPayments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := &chargeFilter{}

	if v := strings.TrimSpace(q.Get("patient_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "patient_id is invalid")
			return
		}
		f.add("p.patient_id = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "patient search is too long")
			return
		}
		f.add("(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%%' || $%d || '%%'", v)
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

	if v := strings.TrimSpace(q.Get("method")); v != "" {
		if !validEnum(v, paymentMethods...) {
			writeError(w, http.StatusBadRequest, "method is invalid")
			return
		}
		f.add("p.method = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("status")); v != "" {
		if !validEnum(v, "posted", "voided") {
			writeError(w, http.StatusBadRequest, "status is invalid")
			return
		}
		f.add("p.status = $%d", v)
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(f.where) > 0 {
		where = "WHERE " + strings.Join(f.where, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM patient_payments p JOIN patients pt ON pt.id = p.patient_id `+where, f.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payments")
		return
	}

	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectPatientPayment+where+` ORDER BY p.payment_date DESC, p.created_at DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payments")
		return
	}
	defer rows.Close()

	result := pagedResult[PatientPayment]{Items: []PatientPayment{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var p PatientPayment
		if err := scanPatientPayment(rows, &p); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load payments")
			return
		}
		result.Items = append(result.Items, p)
	}

	writeJSON(w, http.StatusOK, result)
}
