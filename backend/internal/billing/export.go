package billing

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const maxExportRows = 50000

var transactionCSVHeader = []string{
	"Patient", "Date of Service", "Clinician", "Service Code", "Payer",
	"Charge", "Patient Responsibility", "Insurance Responsibility",
	"Patient Paid", "Insurance Paid", "Adjustments",
	"Patient Balance", "Insurance Balance", "Claim Status", "Charge Status",
}

// csvText neutralises spreadsheet formula injection: a text cell that starts
// with = + - @ (or a control character) is prefixed with an apostrophe so
// Excel / Sheets show it as text instead of evaluating it. encoding/csv then
// handles quoting of commas, quotes and newlines.
func csvText(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}

	return value
}

// ExportTransactionsCSV streams the current transaction search (same
// filters as /api/billing/transactions) as CSV.
func (h *Handler) ExportTransactionsCSV(w http.ResponseWriter, r *http.Request) {
	filter, message := buildChargeFilter(r)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	where := ""
	if len(filter.where) > 0 {
		where = "WHERE " + strings.Join(filter.where, " AND ")
	}

	from := `
		FROM billing_charges c
		JOIN billing_charge_balances b ON b.charge_id = c.id
		JOIN patients p ON p.id = c.patient_id
		JOIN users u ON u.id = c.clinician_id
		JOIN service_codes sc ON sc.id = c.service_code_id
		LEFT JOIN payers py ON py.id = c.payer_id
		LEFT JOIN LATERAL (
			SELECT cm.status
			FROM claim_lines cl JOIN claims cm ON cm.id = cl.claim_id
			WHERE cl.charge_id = c.id AND cl.is_current AND cm.status <> 'voided'
			ORDER BY cm.created_at DESC
			LIMIT 1
		) cs ON TRUE
	` + where

	var count int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) `+from, filter.args...).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "could not export transactions")
		return
	}

	if count > maxExportRows {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("this search matches %d services; narrow the filters to export at most %d", count, maxExportRows))
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT COALESCE(p.first_name || ' ', '') || p.last_name,
			TO_CHAR(c.date_of_service, 'YYYY-MM-DD'),
			u.first_name || ' ' || u.last_name, sc.code, COALESCE(py.payer_name, ''),
			c.total_charge::text, b.patient_responsibility::text, b.insurance_responsibility::text,
			b.patient_payments::text, b.insurance_payments::text,
			(b.patient_adjustments + b.insurance_adjustments)::numeric(12,2)::text,
			b.patient_balance::text, b.insurance_balance::text,
			COALESCE(cs.status, ''), c.status
		`+from+`
		ORDER BY c.date_of_service DESC, c.created_at DESC, c.id
		`,
		filter.args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not export transactions")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="billing-transactions-%s.csv"`, time.Now().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	out := csv.NewWriter(w)
	_ = out.Write(transactionCSVHeader)

	scanFailed := false

	for rows.Next() {
		var c [15]string

		if err := rows.Scan(
			&c[0], &c[1], &c[2], &c[3], &c[4], &c[5], &c[6], &c[7], &c[8], &c[9], &c[10], &c[11], &c[12], &c[13], &c[14],
		); err != nil {
			scanFailed = true
			break
		}

		record := []string{
			csvText(c[0]), c[1], csvText(c[2]), csvText(c[3]), csvText(c[4]),
			c[5], c[6], c[7], c[8], c[9], c[10], c[11], c[12], c[13], c[14],
		}
		_ = out.Write(record)
	}

	// The status line is already sent, so a failure part-way cannot become an
	// error response. End the file with a marker row instead of letting a
	// truncated export pass for a complete one.
	if scanFailed || rows.Err() != nil {
		log.Printf("transaction export failed part-way: scan=%v err=%v", scanFailed, rows.Err())
		_ = out.Write([]string{"EXPORT INCOMPLETE - an error stopped this export; do not rely on it"})
	}

	out.Flush()
}
