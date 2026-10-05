package billing

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Work queues: the single definition of which claims need attention. The
// claims list filters by it and the dashboard counts with it, so a card's
// number always equals the list it links to.
var claimQueues = map[string]string{
	// Not yet sent anywhere.
	"pending": "cm.status IN ('draft', 'validation_error', 'ready', 'pending_submission')",
	// Failed validation and need correcting.
	"validation_errors": "cm.status = 'validation_error'",
	// Rejected by the payer / clearinghouse.
	"rejected": "cm.status IN ('rejected_new', 'rejected')",
	// Paper claims still to generate or mail.
	"paper_pending": "cm.submission_method = 'paper' AND cm.status IN ('ready', 'paper_generated')",
	// CMS-1500 produced but not yet marked as mailed.
	"paper_generated": "cm.status = 'paper_generated'",
	// Ready claims the practice will submit outside this application.
	"external_pending": "cm.submission_method = 'external' AND cm.status = 'ready'",
	// Prepared electronic claims (with no clearinghouse they cannot be sent).
	"electronic_ready": "cm.submission_method = 'electronic' AND cm.status = 'ready'",
}

// =========================================================
// DASHBOARD
// =========================================================

type DashboardAmount struct {
	Count  int    `json:"count"`
	Amount string `json:"amount"`
}

type BillingDashboard struct {
	Claims                     map[string]int  `json:"claims"`
	ClearinghouseConfigured    bool            `json:"clearinghouse_configured"`
	PatientBalance             string          `json:"patient_balance"`
	InsuranceBalance           string          `json:"insurance_balance"`
	UnallocatedPatientCredit   string          `json:"unallocated_patient_credit"`
	PatientsWithBalance        int             `json:"patients_with_balance"`
	UnbilledServices           DashboardAmount `json:"unbilled_services"`
	UnappliedInsurancePayments DashboardAmount `json:"unapplied_insurance_payments"`
}

// GetBillingDashboard returns the counts behind the Practice Biller
// workspace. Only metrics the schema really supports are included.
func (h *Handler) GetBillingDashboard(w http.ResponseWriter, r *http.Request) {
	d := BillingDashboard{Claims: map[string]int{}, ClearinghouseConfigured: h.clearinghouse != nil}

	keys := make([]string, 0, len(claimQueues))
	selects := make([]string, 0, len(claimQueues))

	for key, clause := range claimQueues {
		keys = append(keys, key)
		selects = append(selects, "COUNT(*) FILTER (WHERE "+clause+")")
	}

	counts := make([]int, len(keys))
	dest := make([]any, len(keys))
	for i := range counts {
		dest[i] = &counts[i]
	}

	if err := h.db.QueryRow(r.Context(), "SELECT "+strings.Join(selects, ", ")+" FROM claims cm").Scan(dest...); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the billing dashboard")
		return
	}

	for i, key := range keys {
		d.Claims[key] = counts[i]
	}

	if err := h.db.QueryRow(
		r.Context(),
		`
		SELECT COALESCE(SUM(patient_balance), 0)::numeric(12,2)::text,
			COALESCE(SUM(insurance_balance), 0)::numeric(12,2)::text,
			COALESCE(SUM(unallocated_credit), 0)::numeric(12,2)::text,
			COUNT(*) FILTER (WHERE patient_balance > 0)
		FROM billing_patient_balances
		`,
	).Scan(&d.PatientBalance, &d.InsuranceBalance, &d.UnallocatedPatientCredit, &d.PatientsWithBalance); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the billing dashboard")
		return
	}

	// Insurance services still on no claim.
	if err := h.db.QueryRow(
		r.Context(),
		`
		SELECT COUNT(*), COALESCE(SUM(b.insurance_balance), 0)::numeric(12,2)::text
		FROM billing_charges c
		JOIN billing_charge_balances b ON b.charge_id = c.id
		WHERE c.status = 'active' AND c.billing_method <> 'direct' AND b.insurance_balance > 0
		  AND NOT EXISTS (
			SELECT 1 FROM claim_lines cl JOIN claims cm ON cm.id = cl.claim_id
			WHERE cl.charge_id = c.id AND cl.is_current AND cm.status <> 'voided')
		`,
	).Scan(&d.UnbilledServices.Count, &d.UnbilledServices.Amount); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the billing dashboard")
		return
	}

	if err := h.db.QueryRow(
		r.Context(),
		`
		SELECT COUNT(*), COALESCE(SUM(unapplied), 0)::numeric(12,2)::text
		FROM (
			SELECT p.amount - COALESCE(SUM(a.amount_paid) FILTER (WHERE a.status = 'active'), 0) AS unapplied
			FROM insurance_payments p
			LEFT JOIN insurance_payment_allocations a ON a.payment_id = p.id
			WHERE p.status = 'posted'
			GROUP BY p.id, p.amount
		) u
		WHERE unapplied > 0
		`,
	).Scan(&d.UnappliedInsurancePayments.Count, &d.UnappliedInsurancePayments.Amount); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the billing dashboard")
		return
	}

	writeJSON(w, http.StatusOK, d)
}

// =========================================================
// INSURANCE AGING
// =========================================================

// InsuranceAgingRow is one service with insurance balance, aged by date of
// service and attributed to the claim currently responsible for it (the
// highest claim sequence), or to the charge's payer when it is unbilled.
type InsuranceAgingRow struct {
	ChargeID         string `json:"charge_id"`
	PatientID        string `json:"patient_id"`
	PatientName      string `json:"patient_name"`
	DateOfService    string `json:"date_of_service"`
	ServiceCode      string `json:"service_code"`
	PayerID          string `json:"payer_id"`
	PayerName        string `json:"payer_name"`
	ClaimID          string `json:"claim_id"`
	ClaimNumber      string `json:"claim_number"`
	ClaimStatus      string `json:"claim_status"`
	ClaimSequence    string `json:"claim_sequence"`
	AgeDays          int    `json:"age_days"`
	Bucket           string `json:"bucket"`
	InsuranceBalance string `json:"insurance_balance"`
}

const insuranceAgingBase = `
	FROM billing_charge_aging a
	JOIN billing_charges c ON c.id = a.charge_id
	JOIN patients pt ON pt.id = a.patient_id
	JOIN service_codes sc ON sc.id = c.service_code_id
	LEFT JOIN (
		SELECT DISTINCT ON (cl.charge_id) cl.charge_id, cm.id AS claim_id, cm.claim_number, cm.status, cm.sequence, cm.payer_id
		FROM claim_lines cl
		JOIN claims cm ON cm.id = cl.claim_id
		WHERE cl.is_current AND cm.status <> 'voided'
		ORDER BY cl.charge_id, array_position(` + sequenceRankSQL + `, cl.sequence::text) DESC, cm.created_at DESC
	) r ON r.charge_id = a.charge_id
	JOIN payers py ON py.id = COALESCE(r.payer_id, a.payer_id)
	WHERE a.insurance_balance > 0
`

// GetInsuranceAging reports insurance balances by age (basis: date of
// service) with bucket totals and drill-down rows.
func (h *Handler) GetInsuranceAging(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := &chargeFilter{}

	for key, column := range map[string]string{
		"payer_id": "COALESCE(r.payer_id, a.payer_id)", "clinician_id": "a.clinician_id", "patient_id": "a.patient_id",
	} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if !isUUID(v) {
				writeError(w, http.StatusBadRequest, key+" is invalid")
				return
			}
			f.add(column+" = $%d", v)
		}
	}

	if v := strings.TrimSpace(q.Get("patient")); v != "" {
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "patient search is too long")
			return
		}
		f.add("(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%%' || $%d || '%%'", v)
	}

	if v := strings.TrimSpace(q.Get("sequence")); v != "" {
		if v == "unbilled" {
			f.where = append(f.where, "r.claim_id IS NULL")
		} else {
			if !validEnum(v, claimSequences...) {
				writeError(w, http.StatusBadRequest, "sequence must be primary, secondary, tertiary, quaternary or unbilled")
				return
			}
			f.add("r.sequence = $%d", v)
		}
	}

	if v := strings.TrimSpace(q.Get("bucket")); v != "" {
		if !validEnum(v, agingBuckets...) {
			writeError(w, http.StatusBadRequest, "bucket must be 0-30, 31-60, 61-90 or 91+")
			return
		}
		f.add("a.bucket = $%d", v)
	}

	where := ""
	if len(f.where) > 0 {
		where = " AND " + strings.Join(f.where, " AND ")
	}

	var totals AgingTotals

	if err := h.db.QueryRow(
		r.Context(),
		`
		SELECT COALESCE(SUM(a.insurance_balance) FILTER (WHERE a.bucket = '0-30'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.insurance_balance) FILTER (WHERE a.bucket = '31-60'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.insurance_balance) FILTER (WHERE a.bucket = '61-90'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.insurance_balance) FILTER (WHERE a.bucket = '91+'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.insurance_balance), 0)::numeric(12,2)::text
		`+insuranceAgingBase+where,
		f.args...,
	).Scan(&totals.Bucket0To30, &totals.Bucket31To60, &totals.Bucket61To90, &totals.Bucket91Plus, &totals.Total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the insurance aging report")
		return
	}

	var count int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) `+insuranceAgingBase+where, f.args...).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the insurance aging report")
		return
	}

	page, pageSize := pageParams(r)
	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT a.charge_id, a.patient_id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
			TO_CHAR(a.date_of_service, 'YYYY-MM-DD'), sc.code,
			py.id, py.payer_name,
			COALESCE(r.claim_id::text, ''), COALESCE(r.claim_number, ''), COALESCE(r.status, ''), COALESCE(r.sequence, ''),
			a.age_days, a.bucket, a.insurance_balance::text
		`+insuranceAgingBase+where+`
		ORDER BY a.date_of_service, pt.last_name, a.charge_id
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the insurance aging report")
		return
	}
	defer rows.Close()

	items := []InsuranceAgingRow{}

	for rows.Next() {
		var row InsuranceAgingRow
		if err := rows.Scan(
			&row.ChargeID, &row.PatientID, &row.PatientName, &row.DateOfService, &row.ServiceCode,
			&row.PayerID, &row.PayerName, &row.ClaimID, &row.ClaimNumber, &row.ClaimStatus, &row.ClaimSequence,
			&row.AgeDays, &row.Bucket, &row.InsuranceBalance,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load the insurance aging report")
			return
		}
		items = append(items, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"basis": "date_of_service", "totals": totals,
		"items": items, "total": count, "page": page, "page_size": pageSize,
	})
}

// =========================================================
// PATIENT AGING
// =========================================================

// GetPatientAging reports patient balances by age (basis: date of service),
// one row per patient, with bucket totals over every matching patient.
func (h *Handler) GetPatientAging(w http.ResponseWriter, r *http.Request) {
	filter, message := patientAgingFilterFromRequest(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	page, pageSize := pageParams(r)
	filter.Limit, filter.Offset = pageSize, (page-1)*pageSize

	rows, total, totals, err := queryPatientAging(r.Context(), h.db, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the patient aging report")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"basis": "date_of_service", "totals": totals,
		"items": rows, "total": total, "page": page, "page_size": pageSize,
	})
}

// =========================================================
// COLLECTIONS
// =========================================================

type CollectionsLine struct {
	Label  string `json:"label"`
	Count  int    `json:"count"`
	Amount string `json:"amount"`
}

type CollectionsReport struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Charges are billed amounts, not revenue; collections are money received.
	ChargesCreated    CollectionsLine `json:"charges_created"`
	PatientPayments   CollectionsLine `json:"patient_payments"`
	InsurancePayments CollectionsLine `json:"insurance_payments"`
	Refunds           CollectionsLine `json:"refunds"`
	TotalCollections  string          `json:"total_collections"`
	Writeoffs         CollectionsLine `json:"writeoffs"`
	Adjustments       CollectionsLine `json:"adjustments"`

	InsuranceByPayer  []CollectionsLine `json:"insurance_by_payer"`
	PatientByMethod   []CollectionsLine `json:"patient_by_method"`
	DefinitionsOfDate map[string]string `json:"definitions"`
}

// GetCollectionsReport summarises money movement in a date range. Dating
// rules: charges by the day they were entered; payments by payment date;
// refunds by refund date; adjustments and write-offs by the day recorded.
// Voided charges, payments and adjustments are excluded. Total collections
// = patient payments + insurance payments - refunds.
func (h *Handler) GetCollectionsReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	from, ok1 := parseDate(strings.TrimSpace(q.Get("from")))
	to, ok2 := parseDate(strings.TrimSpace(q.Get("to")))

	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "from and to must be valid dates")
		return
	}

	if to.Before(from) {
		writeError(w, http.StatusBadRequest, "to cannot be before from")
		return
	}

	if to.Sub(from) > 5*366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "choose a period of at most five years")
		return
	}

	fromText, toText := from.Format("2006-01-02"), to.Format("2006-01-02")

	rep := CollectionsReport{
		From: fromText, To: toText,
		InsuranceByPayer: []CollectionsLine{}, PatientByMethod: []CollectionsLine{},
		DefinitionsOfDate: map[string]string{
			"charges_created":    "Charges are dated by the day they were entered.",
			"patient_payments":   "Dated by payment date; voided payments are excluded.",
			"insurance_payments": "Dated by payment date; voided payments are excluded.",
			"refunds":            "Patient refunds, dated by refund date.",
			"writeoffs":          "Write-off adjustments (contractual, small balance, bad debt, courtesy), dated by the day recorded.",
			"adjustments":        "Other adjustments (payer / manual), dated by the day recorded. Not payments.",
			"total_collections":  "Patient payments + insurance payments - refunds.",
		},
	}

	scan := func(line *CollectionsLine, label, sql string) bool {
		line.Label = label
		if err := h.db.QueryRow(r.Context(), sql, fromText, toText).Scan(&line.Count, &line.Amount); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load the collections report")
			return false
		}
		return true
	}

	if !scan(&rep.ChargesCreated, "Charges created",
		`SELECT COUNT(*), COALESCE(SUM(total_charge), 0)::numeric(12,2)::text FROM billing_charges
		 WHERE status = 'active' AND created_at::date BETWEEN $1::date AND $2::date`) ||
		!scan(&rep.PatientPayments, "Patient payments posted",
			`SELECT COUNT(*), COALESCE(SUM(amount), 0)::numeric(12,2)::text FROM patient_payments
		 WHERE status = 'posted' AND payment_date BETWEEN $1::date AND $2::date`) ||
		!scan(&rep.InsurancePayments, "Insurance payments posted",
			`SELECT COUNT(*), COALESCE(SUM(amount), 0)::numeric(12,2)::text FROM insurance_payments
		 WHERE status = 'posted' AND payment_date BETWEEN $1::date AND $2::date`) ||
		!scan(&rep.Refunds, "Refunds",
			`SELECT COUNT(*), COALESCE(SUM(amount), 0)::numeric(12,2)::text FROM patient_payment_refunds
		 WHERE refund_date BETWEEN $1::date AND $2::date`) ||
		!scan(&rep.Writeoffs, "Write-offs",
			`SELECT COUNT(*), COALESCE(SUM(amount), 0)::numeric(12,2)::text FROM billing_adjustments
		 WHERE status = 'active' AND adjustment_type LIKE '%\_writeoff' AND created_at::date BETWEEN $1::date AND $2::date`) ||
		!scan(&rep.Adjustments, "Adjustments",
			`SELECT COUNT(*), COALESCE(SUM(amount), 0)::numeric(12,2)::text FROM billing_adjustments
		 WHERE status = 'active' AND adjustment_type NOT LIKE '%\_writeoff' AND created_at::date BETWEEN $1::date AND $2::date`) {
		return
	}

	rep.TotalCollections = formatMoney(mustCents(rep.PatientPayments.Amount) + mustCents(rep.InsurancePayments.Amount) - mustCents(rep.Refunds.Amount))

	breakdown := func(sql string) ([]CollectionsLine, bool) {
		rows, err := h.db.Query(r.Context(), sql, fromText, toText)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load the collections report")
			return nil, false
		}
		defer rows.Close()

		lines := []CollectionsLine{}
		for rows.Next() {
			var l CollectionsLine
			if err := rows.Scan(&l.Label, &l.Count, &l.Amount); err != nil {
				writeError(w, http.StatusInternalServerError, "could not load the collections report")
				return nil, false
			}
			lines = append(lines, l)
		}

		return lines, true
	}

	var ok bool

	if rep.InsuranceByPayer, ok = breakdown(`
		SELECT py.payer_name, COUNT(*), SUM(p.amount)::numeric(12,2)::text
		FROM insurance_payments p JOIN payers py ON py.id = p.payer_id
		WHERE p.status = 'posted' AND p.payment_date BETWEEN $1::date AND $2::date
		GROUP BY py.payer_name ORDER BY py.payer_name`); !ok {
		return
	}

	if rep.PatientByMethod, ok = breakdown(`
		SELECT method, COUNT(*), SUM(amount)::numeric(12,2)::text
		FROM patient_payments
		WHERE status = 'posted' AND payment_date BETWEEN $1::date AND $2::date
		GROUP BY method ORDER BY method`); !ok {
		return
	}

	writeJSON(w, http.StatusOK, rep)
}
