package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Patient statements are generated from the balance engine and stored as an
// immutable snapshot plus the PDF built from it.
//
// Rules (also documented in docs/BILLING_ARCHITECTURE.md):
//
//   - Only patient-side amounts appear. Insurance balances are never shown
//     as patient due.
//   - Open balance statement: every active service with a patient balance
//     greater than zero right now, with its current patient responsibility,
//     patient payments and adjustments.
//   - Date range statement: patient-side ledger events dated within
//     [start, end], plus the balance carried in from before start. Events
//     are dated by date of service (charges), payment date (payments) and
//     the day an adjustment / responsibility transfer was recorded. The
//     balance due is the patient balance as of end date.
//   - A generated statement never changes. A later payment shows up on the
//     next statement; "regenerating" creates a new statement record.

type StatementLine struct {
	Date           string `json:"date"`
	Description    string `json:"description"`
	Charge         string `json:"charge"`
	Responsibility string `json:"responsibility"`
	Payments       string `json:"payments"`
	Adjustments    string `json:"adjustments"`
	Balance        string `json:"balance"`
}

type StatementPractice struct {
	Name    string          `json:"name"`
	Address SnapshotAddress `json:"address"`
	Phone   string          `json:"phone"`
}

type StatementPatient struct {
	Name          string          `json:"name"`
	AccountNumber string          `json:"account_number"`
	Address       SnapshotAddress `json:"address"`
}

type StatementSnapshot struct {
	StatementNumber string            `json:"statement_number"`
	Type            string            `json:"type"`
	StatementDate   string            `json:"statement_date"`
	StartDate       string            `json:"start_date"`
	EndDate         string            `json:"end_date"`
	Practice        StatementPractice `json:"practice"`
	Patient         StatementPatient  `json:"patient"`
	Lines           []StatementLine   `json:"lines"`

	PreviousBalance  string `json:"previous_balance"`
	TotalCharges     string `json:"total_charges"`
	TotalPayments    string `json:"total_payments"`
	TotalAdjustments string `json:"total_adjustments"`
	BalanceDue       string `json:"balance_due"`
	CreditOnAccount  string `json:"credit_on_account"`
	AmountDue        string `json:"amount_due"`
	Comment          string `json:"comment"`
}

type PatientStatement struct {
	ID              string `json:"id"`
	StatementNumber string `json:"statement_number"`
	PatientID       string `json:"patient_id"`
	PatientName     string `json:"patient_name"`
	StatementType   string `json:"statement_type"`
	StatementDate   string `json:"statement_date"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	BalanceDue      string `json:"balance_due"`
	CreditOnAccount string `json:"credit_on_account"`
	AmountDue       string `json:"amount_due"`
	Comment         string `json:"comment"`
	BatchID         string `json:"batch_id"`
	GeneratedBy     string `json:"generated_by"`
	CreatedAt       string `json:"created_at"`

	Snapshot *StatementSnapshot `json:"snapshot,omitempty"`
}

type StatementRequest struct {
	Type      string `json:"statement_type"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Comment   string `json:"comment"`
}

var errNothingToBill = errors.New("nothing to bill")

// =========================================================
// PURE RULES
// =========================================================

func validateStatementRequest(req *StatementRequest, today time.Time) string {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.StartDate = strings.TrimSpace(req.StartDate)
	req.EndDate = strings.TrimSpace(req.EndDate)
	req.Comment = strings.TrimSpace(req.Comment)

	if !validEnum(req.Type, "open_balance", "date_range") {
		return "statement type must be open_balance or date_range"
	}

	if len(req.Comment) > 500 {
		return "the statement comment must be 500 characters or fewer"
	}

	if req.Type == "open_balance" {
		req.StartDate, req.EndDate = "", ""
		return ""
	}

	start, ok := parseDate(req.StartDate)
	if !ok {
		return "start date must be a valid date"
	}

	end, ok := parseDate(req.EndDate)
	if !ok {
		return "end date must be a valid date"
	}

	if end.Before(start) {
		return "end date cannot be before the start date"
	}

	if isFutureDate(end, today) {
		return "end date cannot be in the future"
	}

	if start.Year() < 2000 || end.Sub(start) > 5*366*24*time.Hour {
		return "a statement period can span at most five years"
	}

	return ""
}

// statementEvent is a patient-side ledger event, reduced to what a statement
// needs.
type statementEvent struct {
	OccurredOn    string
	EventType     string
	Effect        int64
	Amount        int64
	Detail        string
	DateOfService string
	ServiceCode   string
	Description   string
}

func statementEventText(e statementEvent) string {
	service := e.ServiceCode
	if e.Description != "" {
		service += " - " + e.Description
	}

	switch e.EventType {
	case "charge":
		return service + " (service date " + e.DateOfService + ")"
	case "patient_payment":
		return "Payment received - " + service
	case "adjustment":
		return "Adjustment (" + titleWords(e.Detail) + ") - " + service
	case "transfer_in":
		return "Patient responsibility: " + titleWords(e.Detail) + " - " + service
	case "transfer_out":
		return "Moved to insurance - " + service
	}

	return service
}

func titleWords(value string) string {
	parts := strings.Split(strings.ReplaceAll(value, "_", " "), " ")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}

	return strings.Join(parts, " ")
}

// rangeStatementLines turns ordered events into statement lines: the
// balance carried in from before start, the activity in the period with a
// running balance, and the totals. Pure (no database).
func rangeStatementLines(events []statementEvent, start, end string) (lines []StatementLine, previous, charges, payments, adjustments, balance int64) {
	for _, e := range events {
		signed := e.Effect * e.Amount

		if e.OccurredOn < start {
			previous += signed
			continue
		}

		if e.OccurredOn > end {
			continue
		}

		line := StatementLine{Date: e.OccurredOn, Description: statementEventText(e)}

		switch e.EventType {
		case "charge", "transfer_in":
			line.Charge = formatMoney(e.Amount)
			charges += e.Amount
		case "patient_payment":
			line.Payments = formatMoney(e.Amount)
			payments += e.Amount
		default: // adjustment, transfer_out
			line.Adjustments = formatMoney(e.Amount)
			adjustments += e.Amount
		}

		lines = append(lines, line)
	}

	running := previous
	for i, e := range eventsInRange(events, start, end) {
		running += e.Effect * e.Amount
		lines[i].Balance = formatMoney(running)
	}

	return lines, previous, charges, payments, adjustments, previous + charges - payments - adjustments
}

func eventsInRange(events []statementEvent, start, end string) []statementEvent {
	var out []statementEvent
	for _, e := range events {
		if e.OccurredOn >= start && e.OccurredOn <= end {
			out = append(out, e)
		}
	}

	return out
}

// =========================================================
// SNAPSHOT CONSTRUCTION
// =========================================================

func loadStatementHeader(ctx context.Context, q queryRower, patientID string) (StatementPractice, StatementPatient, error) {
	var practice StatementPractice
	var patient StatementPatient

	err := q.QueryRow(
		ctx,
		`
		SELECT COALESCE(practice_name, ''), COALESCE(address_1, ''), COALESCE(address_2, ''),
			COALESCE(city, ''), COALESCE(state, ''), COALESCE(zip, ''), COALESCE(phone, '')
		FROM practice_billing_profile
		`,
	).Scan(
		&practice.Name, &practice.Address.Address1, &practice.Address.Address2,
		&practice.Address.City, &practice.Address.State, &practice.Address.Zip, &practice.Phone,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return practice, patient, err
	}

	err = q.QueryRow(
		ctx,
		`
		SELECT TRIM(COALESCE(first_name, '') || ' ' || COALESCE(middle_name, '') || ' ' || last_name),
			COALESCE(account_number, ''), COALESCE(address_1, ''), COALESCE(address_2, ''),
			COALESCE(city, ''), COALESCE(state, ''), COALESCE(zip, '')
		FROM patients WHERE id = $1
		`,
		patientID,
	).Scan(
		&patient.Name, &patient.AccountNumber, &patient.Address.Address1, &patient.Address.Address2,
		&patient.Address.City, &patient.Address.State, &patient.Address.Zip,
	)

	patient.Name = strings.Join(strings.Fields(patient.Name), " ")

	return practice, patient, err
}

// buildStatementSnapshot reads the balance engine and assembles the exact
// content of a statement. It returns errNothingToBill when there is nothing
// a patient could owe or has been billed for.
func buildStatementSnapshot(ctx context.Context, q queryRower, patientID string, req StatementRequest, statementNumber string, today time.Time) (StatementSnapshot, error) {
	s := StatementSnapshot{
		StatementNumber: statementNumber,
		Type:            req.Type,
		StatementDate:   today.Format("2006-01-02"),
		StartDate:       req.StartDate,
		EndDate:         req.EndDate,
		Lines:           []StatementLine{},
		Comment:         req.Comment,
	}

	var err error

	s.Practice, s.Patient, err = loadStatementHeader(ctx, q, patientID)
	if err != nil {
		return s, err
	}

	var credit string

	if req.Type == "open_balance" {
		rows, err := q.Query(
			ctx,
			`
			SELECT TO_CHAR(c.date_of_service, 'YYYY-MM-DD'), sc.code, sc.description, c.total_charge::text,
				b.patient_responsibility::text, b.patient_payments::text, b.patient_adjustments::text, b.patient_balance::text
			FROM billing_charges c
			JOIN billing_charge_balances b ON b.charge_id = c.id
			JOIN service_codes sc ON sc.id = c.service_code_id
			WHERE c.patient_id = $1 AND c.status = 'active' AND b.patient_balance > 0
			ORDER BY c.date_of_service, c.created_at, c.id
			`,
			patientID,
		)
		if err != nil {
			return s, err
		}

		var responsibility, payments, adjustments, balance int64

		for rows.Next() {
			var date, code, description, charge, resp, paid, adj, bal string
			if err := rows.Scan(&date, &code, &description, &charge, &resp, &paid, &adj, &bal); err != nil {
				rows.Close()
				return s, err
			}

			s.Lines = append(s.Lines, StatementLine{
				Date: date, Description: code + " - " + description,
				Charge: charge, Responsibility: resp, Payments: paid, Adjustments: adj, Balance: bal,
			})

			responsibility += mustCents(resp)
			payments += mustCents(paid)
			adjustments += mustCents(adj)
			balance += mustCents(bal)
		}
		rows.Close()

		if err := rows.Err(); err != nil {
			return s, err
		}

		if balance == 0 {
			return s, errNothingToBill
		}

		s.TotalCharges = formatMoney(responsibility)
		s.TotalPayments = formatMoney(payments)
		s.TotalAdjustments = formatMoney(adjustments)
		s.PreviousBalance = "0.00"
		s.BalanceDue = formatMoney(balance)

		if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(unapplied), 0)::numeric(12,2)::text FROM patient_payment_credits WHERE patient_id = $1`, patientID).Scan(&credit); err != nil {
			return s, err
		}
	} else {
		rows, err := q.Query(
			ctx,
			`
			SELECT TO_CHAR(e.occurred_on, 'YYYY-MM-DD'), e.event_type, e.effect, e.amount::text, e.detail,
				TO_CHAR(c.date_of_service, 'YYYY-MM-DD'), sc.code, sc.description
			FROM billing_ledger_events e
			JOIN billing_charges c ON c.id = e.charge_id
			JOIN service_codes sc ON sc.id = c.service_code_id
			WHERE e.patient_id = $1 AND e.party = 'patient' AND e.status = 'active' AND e.occurred_on <= $2::date
			ORDER BY e.occurred_on, e.recorded_at, e.source_id, e.event_type
			`,
			patientID, req.EndDate,
		)
		if err != nil {
			return s, err
		}

		var events []statementEvent

		for rows.Next() {
			var e statementEvent
			var amount string
			var effect int
			if err := rows.Scan(&e.OccurredOn, &e.EventType, &effect, &amount, &e.Detail, &e.DateOfService, &e.ServiceCode, &e.Description); err != nil {
				rows.Close()
				return s, err
			}
			e.Effect, e.Amount = int64(effect), mustCents(amount)
			events = append(events, e)
		}
		rows.Close()

		if err := rows.Err(); err != nil {
			return s, err
		}

		lines, previous, charges, payments, adjustments, balance := rangeStatementLines(events, req.StartDate, req.EndDate)

		if len(lines) == 0 && previous == 0 && balance == 0 {
			return s, errNothingToBill
		}

		s.Lines = append(s.Lines, lines...)
		s.PreviousBalance = formatMoney(previous)
		s.TotalCharges = formatMoney(charges)
		s.TotalPayments = formatMoney(payments)
		s.TotalAdjustments = formatMoney(adjustments)
		s.BalanceDue = formatMoney(balance)

		if err := q.QueryRow(
			ctx,
			`SELECT COALESCE(SUM(unapplied), 0)::numeric(12,2)::text FROM patient_payment_credits WHERE patient_id = $1 AND payment_date <= $2::date`,
			patientID, req.EndDate,
		).Scan(&credit); err != nil {
			return s, err
		}
	}

	s.CreditOnAccount = credit
	s.AmountDue = formatMoney(max(mustCents(s.BalanceDue)-mustCents(credit), 0))

	return s, nil
}

// =========================================================
// GENERATION
// =========================================================

// createStatement builds, renders and stores one statement inside tx.
func createStatement(ctx context.Context, tx pgx.Tx, patientID string, req StatementRequest, batchID, userID string, now time.Time) (string, error) {
	var number string
	if err := tx.QueryRow(ctx, `SELECT 'STM-' || LPAD(nextval('statement_number_seq')::text, 7, '0')`).Scan(&number); err != nil {
		return "", err
	}

	snapshot, err := buildStatementSnapshot(ctx, tx, patientID, req, number, now)
	if err != nil {
		return "", err
	}

	pdf, sum, err := renderStatementPDF(snapshot)
	if err != nil {
		return "", err
	}

	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}

	var id string

	err = tx.QueryRow(
		ctx,
		`
		INSERT INTO patient_statements (
			statement_number, patient_id, statement_type, statement_date, start_date, end_date,
			balance_due, credit_on_account, comment, snapshot, pdf, sha256, batch_id, generated_by
		)
		VALUES ($1, $2, $3, $4::date, NULLIF($5, '')::date, NULLIF($6, '')::date,
			$7::numeric, $8::numeric, NULLIF($9, ''), $10, $11, $12, NULLIF($13, '')::uuid, $14)
		RETURNING id
		`,
		number, patientID, req.Type, snapshot.StatementDate, req.StartDate, req.EndDate,
		snapshot.BalanceDue, snapshot.CreditOnAccount, req.Comment, snapshotJSON, pdf, sum, batchID, nullIfEmpty(userID),
	).Scan(&id)

	return id, err
}

// =========================================================
// READ MODEL
// =========================================================

const selectStatement = `
	SELECT s.id, s.statement_number, s.patient_id,
		COALESCE(pt.first_name || ' ', '') || pt.last_name,
		s.statement_type, TO_CHAR(s.statement_date, 'YYYY-MM-DD'),
		COALESCE(TO_CHAR(s.start_date, 'YYYY-MM-DD'), ''), COALESCE(TO_CHAR(s.end_date, 'YYYY-MM-DD'), ''),
		s.balance_due::text, s.credit_on_account::text, COALESCE(s.comment, ''),
		COALESCE(s.batch_id::text, ''), COALESCE(u.first_name || ' ' || u.last_name, ''),
		TO_CHAR(s.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
	FROM patient_statements s
	JOIN patients pt ON pt.id = s.patient_id
	LEFT JOIN users u ON u.id = s.generated_by
`

func scanStatement(row pgx.Row, s *PatientStatement) error {
	if err := row.Scan(
		&s.ID, &s.StatementNumber, &s.PatientID, &s.PatientName, &s.StatementType, &s.StatementDate,
		&s.StartDate, &s.EndDate, &s.BalanceDue, &s.CreditOnAccount, &s.Comment, &s.BatchID, &s.GeneratedBy, &s.CreatedAt,
	); err != nil {
		return err
	}

	s.AmountDue = formatMoney(max(mustCents(s.BalanceDue)-mustCents(s.CreditOnAccount), 0))

	return nil
}

func (h *Handler) getStatement(ctx context.Context, q queryRower, id string, withSnapshot bool) (PatientStatement, error) {
	var s PatientStatement

	if err := scanStatement(q.QueryRow(ctx, selectStatement+`WHERE s.id = $1`, id), &s); err != nil {
		return s, err
	}

	if withSnapshot {
		var raw []byte
		if err := q.QueryRow(ctx, `SELECT snapshot FROM patient_statements WHERE id = $1`, id).Scan(&raw); err != nil {
			return s, err
		}

		var snapshot StatementSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return s, err
		}
		s.Snapshot = &snapshot
	}

	return s, nil
}

// statementForRequest loads a statement and enforces the optional patient in
// the route: a statement can only be reached through its own patient.
func (h *Handler) statementForRequest(w http.ResponseWriter, r *http.Request, withSnapshot bool) (PatientStatement, bool) {
	id := r.PathValue("id")
	patientID := r.PathValue("patientId")

	if !isUUID(id) || (patientID != "" && !isUUID(patientID)) {
		writeError(w, http.StatusNotFound, "statement not found")
		return PatientStatement{}, false
	}

	s, err := h.getStatement(r.Context(), h.db, id, withSnapshot)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && patientID != "" && s.PatientID != patientID) {
		writeError(w, http.StatusNotFound, "statement not found")
		return s, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statement")
		return s, false
	}

	return s, true
}

// =========================================================
// HANDLERS
// =========================================================

func (h *Handler) ListPatientStatements(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statements")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	rows, err := h.db.Query(r.Context(), selectStatement+`WHERE s.patient_id = $1 ORDER BY s.created_at DESC, s.statement_number DESC LIMIT 500`, patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statements")
		return
	}
	defer rows.Close()

	result := []PatientStatement{}

	for rows.Next() {
		var s PatientStatement
		if err := scanStatement(rows, &s); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load statements")
			return
		}
		result = append(result, s)
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreatePatientStatement(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var req StatementRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := time.Now()

	if message := validateStatementRequest(&req, now); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate statement")
		return
	}
	defer tx.Rollback(r.Context())

	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`, patientID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate statement")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	id, err := createStatement(r.Context(), tx, patientID, req, "", currentUserID(r), now)
	if errors.Is(err, errNothingToBill) {
		writeError(w, http.StatusConflict, "there is nothing to put on this statement: no open patient balance or activity")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate statement")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate statement")
		return
	}

	s, err := h.getStatement(r.Context(), h.db, id, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statement")
		return
	}

	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) GetStatement(w http.ResponseWriter, r *http.Request) {
	if s, ok := h.statementForRequest(w, r, true); ok {
		writeJSON(w, http.StatusOK, s)
	}
}

// DownloadStatementPDF returns the PDF stored when the statement was
// generated, byte for byte.
func (h *Handler) DownloadStatementPDF(w http.ResponseWriter, r *http.Request) {
	s, ok := h.statementForRequest(w, r, false)
	if !ok {
		return
	}

	var pdf []byte
	if err := h.db.QueryRow(r.Context(), `SELECT pdf FROM patient_statements WHERE id = $1`, s.ID).Scan(&pdf); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statement")
		return
	}

	writePDF(w, s.StatementNumber+".pdf", pdf)
}

// SearchStatements lists statements practice-wide (paged).
func (h *Handler) SearchStatements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := &chargeFilter{}

	if v := strings.TrimSpace(q.Get("batch_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "batch_id is invalid")
			return
		}
		f.add("s.batch_id = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "patient search is too long")
			return
		}
		f.add("(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%%' || $%d || '%%'", v)
	}

	for key, clause := range map[string]string{"from": "s.statement_date >= $%d::date", "to": "s.statement_date <= $%d::date"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if _, ok := parseDate(v); !ok {
				writeError(w, http.StatusBadRequest, key+" must be a valid date")
				return
			}
			f.add(clause, v)
		}
	}

	page, pageSize := pageParams(r)

	where := ""
	if len(f.where) > 0 {
		where = " WHERE " + strings.Join(f.where, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM patient_statements s JOIN patients pt ON pt.id = s.patient_id`+where, f.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statements")
		return
	}

	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectStatement+where+` ORDER BY s.created_at DESC, s.statement_number DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statements")
		return
	}
	defer rows.Close()

	result := pagedResult[PatientStatement]{Items: []PatientStatement{}, Total: total, Page: page, PageSize: pageSize}

	for rows.Next() {
		var s PatientStatement
		if err := scanStatement(rows, &s); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load statements")
			return
		}
		result.Items = append(result.Items, s)
	}

	writeJSON(w, http.StatusOK, result)
}

// GetStatementCandidates lists patients with an open patient balance for
// batch statements (filters: clinician, minimum balance, aging bucket,
// patient name). Insurance-only balances never make a patient a candidate.
func (h *Handler) GetStatementCandidates(w http.ResponseWriter, r *http.Request) {
	filter, message := patientAgingFilterFromRequest(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	page, pageSize := pageParams(r)
	filter.Limit, filter.Offset = pageSize, (page-1)*pageSize

	rows, total, err := queryPatientAging(r.Context(), h.db, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load patients")
		return
	}

	writeJSON(w, http.StatusOK, pagedResult[PatientAgingRow]{Items: rows, Total: total, Page: page, PageSize: pageSize})
}

func patientAgingFilterFromRequest(r *http.Request) (PatientAgingFilter, string) {
	q := r.URL.Query()
	var f PatientAgingFilter

	for key, target := range map[string]*string{"clinician_id": &f.ClinicianID, "patient_id": &f.PatientID} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if !isUUID(v) {
				return f, key + " is invalid"
			}
			*target = v
		}
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			return f, "patient search is too long"
		}
		f.PatientName = v
	}

	if v := strings.TrimSpace(q.Get("bucket")); v != "" {
		if !validEnum(v, agingBuckets...) {
			return f, "bucket must be 0-30, 31-60, 61-90 or 91+"
		}
		f.Bucket = v
	}

	if v := strings.TrimSpace(q.Get("min_balance")); v != "" {
		cents, ok := parseMoney(v)
		if !ok {
			return f, "min_balance must be a non-negative amount with at most 2 decimal places"
		}
		f.MinBalance = cents
	}

	return f, ""
}

type BatchStatementRequest struct {
	PatientIDs []string `json:"patient_ids"`
	Comment    string   `json:"comment"`
}

type BatchSkip struct {
	PatientID string `json:"patient_id"`
	Reason    string `json:"reason"`
}

// CreateStatementBatch generates one open-balance statement per selected
// patient (each its own record, in its own transaction). Patients with
// nothing to bill are skipped and reported, never given an empty statement.
func (h *Handler) CreateStatementBatch(w http.ResponseWriter, r *http.Request) {
	var req BatchStatementRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Comment = strings.TrimSpace(req.Comment)

	if len(req.Comment) > 500 {
		writeError(w, http.StatusBadRequest, "the statement comment must be 500 characters or fewer")
		return
	}

	if len(req.PatientIDs) == 0 || len(req.PatientIDs) > 200 {
		writeError(w, http.StatusBadRequest, "select between 1 and 200 patients")
		return
	}

	seen := map[string]bool{}
	ids := make([]string, 0, len(req.PatientIDs))

	for _, id := range req.PatientIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if !isUUID(id) {
			writeError(w, http.StatusBadRequest, "patient not found")
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	var batchID string
	if err := h.db.QueryRow(r.Context(), `SELECT gen_random_uuid()::text`).Scan(&batchID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate statements")
		return
	}

	userID := currentUserID(r)
	now := time.Now()

	created := []PatientStatement{}
	skipped := []BatchSkip{}

	for _, patientID := range ids {
		id, err := h.generateOne(r.Context(), patientID, StatementRequest{Type: "open_balance", Comment: req.Comment}, batchID, userID, now)

		switch {
		case errors.Is(err, errNothingToBill):
			skipped = append(skipped, BatchSkip{PatientID: patientID, Reason: "no open patient balance"})
		case errors.Is(err, pgx.ErrNoRows):
			skipped = append(skipped, BatchSkip{PatientID: patientID, Reason: "patient not found"})
		case err != nil:
			writeError(w, http.StatusInternalServerError, "could not generate statements")
			return
		default:
			s, err := h.getStatement(r.Context(), h.db, id, false)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not load statement")
				return
			}
			created = append(created, s)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"batch_id": batchID, "created": created, "skipped": skipped})
}

func (h *Handler) generateOne(ctx context.Context, patientID string, req StatementRequest, batchID, userID string, now time.Time) (string, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`, patientID).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "", pgx.ErrNoRows
	}

	id, err := createStatement(ctx, tx, patientID, req, batchID, userID, now)
	if err != nil {
		return "", err
	}

	return id, tx.Commit(ctx)
}

// DownloadCombinedStatementsPDF renders several stored statements into one
// PDF, one statement per page set, straight from their snapshots.
func (h *Handler) DownloadCombinedStatementsPDF(w http.ResponseWriter, r *http.Request) {
	raw := strings.Split(strings.TrimSpace(r.URL.Query().Get("ids")), ",")
	ids := make([]string, 0, len(raw))

	for _, id := range raw {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if !isUUID(id) {
			writeError(w, http.StatusBadRequest, "ids must be statement ids")
			return
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 || len(ids) > 100 {
		writeError(w, http.StatusBadRequest, "choose between 1 and 100 statements")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT snapshot FROM patient_statements
		WHERE id = ANY($1::uuid[])
		ORDER BY statement_number
		`,
		ids,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load statements")
		return
	}
	defer rows.Close()

	var snapshots []StatementSnapshot

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load statements")
			return
		}

		var s StatementSnapshot
		if err := json.Unmarshal(raw, &s); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load statements")
			return
		}
		snapshots = append(snapshots, s)
	}

	if len(snapshots) == 0 {
		writeError(w, http.StatusNotFound, "statements not found")
		return
	}

	pdf, _, err := renderStatementPDF(snapshots...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build the combined PDF")
		return
	}

	writePDF(w, "statements-batch.pdf", pdf)
}
