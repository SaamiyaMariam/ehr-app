package billing

import (
	"context"
	"fmt"
	"strings"
)

// Aging basis: DATE OF SERVICE. The age of a balance is today minus the date
// of service of the charge it belongs to (view billing_charge_aging).
// Buckets are 0-30, 31-60, 61-90 and 91+ days.

var agingBuckets = []string{"0-30", "31-60", "61-90", "91+"}

// PatientAgingRow is one patient's patient-side balance split by age.
type PatientAgingRow struct {
	PatientID     string `json:"patient_id"`
	PatientName   string `json:"patient_name"`
	Bucket0To30   string `json:"bucket_0_30"`
	Bucket31To60  string `json:"bucket_31_60"`
	Bucket61To90  string `json:"bucket_61_90"`
	Bucket91Plus  string `json:"bucket_91_plus"`
	Total         string `json:"total"`
	Credit        string `json:"unallocated_credit"`
	LastStatement string `json:"last_statement_date"`
	OldestService string `json:"oldest_service_date"`
}

// AgingTotals are the bucket totals over every matching row (not just the
// page that was returned).
type AgingTotals struct {
	Bucket0To30  string `json:"bucket_0_30"`
	Bucket31To60 string `json:"bucket_31_60"`
	Bucket61To90 string `json:"bucket_61_90"`
	Bucket91Plus string `json:"bucket_91_plus"`
	Total        string `json:"total"`
}

// PatientAgingFilter selects which patients / charges feed the aging query.
type PatientAgingFilter struct {
	ClinicianID string
	PatientID   string
	PatientName string
	Bucket      string // only patients with a balance in this bucket
	MinBalance  int64  // cents; patients below it are left out
	Limit       int
	Offset      int
}

// queryPatientAging is the single patient-aging query, shared by statement
// candidates and the patient aging report. Only patient-side balances count
// (insurance-only balances are never patient due).
func queryPatientAging(ctx context.Context, q queryRower, f PatientAgingFilter) ([]PatientAgingRow, int, AgingTotals, error) {
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where := []string{"a.patient_balance > 0"}
	having := []string{}

	if f.ClinicianID != "" {
		where = append(where, "a.clinician_id = "+next(f.ClinicianID))
	}
	if f.PatientID != "" {
		where = append(where, "a.patient_id = "+next(f.PatientID))
	}
	if f.PatientName != "" {
		where = append(where, "(COALESCE(pt.first_name || ' ', '') || pt.last_name) ILIKE '%' || "+next(f.PatientName)+" || '%'")
	}
	if f.Bucket != "" {
		having = append(having, "COALESCE(SUM(a.patient_balance) FILTER (WHERE a.bucket = "+next(f.Bucket)+"), 0) > 0")
	}

	having = append(having, "SUM(a.patient_balance) >= "+next(formatMoney(max(f.MinBalance, 1)))+"::numeric")

	base := `
		FROM billing_charge_aging a
		JOIN patients pt ON pt.id = a.patient_id
		LEFT JOIN (
			SELECT patient_id, SUM(unapplied) AS credit FROM patient_payment_credits GROUP BY patient_id
		) cr ON cr.patient_id = a.patient_id
		LEFT JOIN (
			SELECT patient_id, MAX(statement_date) AS last_date FROM patient_statements GROUP BY patient_id
		) st ON st.patient_id = a.patient_id
		WHERE ` + strings.Join(where, " AND ") + `
		GROUP BY pt.id, pt.first_name, pt.last_name, cr.credit, st.last_date
		HAVING ` + strings.Join(having, " AND ")

	var totals AgingTotals
	var total int

	if err := q.QueryRow(
		ctx,
		`
		SELECT COUNT(*),
			COALESCE(SUM(b1), 0)::numeric(12,2)::text, COALESCE(SUM(b2), 0)::numeric(12,2)::text,
			COALESCE(SUM(b3), 0)::numeric(12,2)::text, COALESCE(SUM(b4), 0)::numeric(12,2)::text,
			COALESCE(SUM(bt), 0)::numeric(12,2)::text
		FROM (
			SELECT
				SUM(a.patient_balance) FILTER (WHERE a.bucket = '0-30') AS b1,
				SUM(a.patient_balance) FILTER (WHERE a.bucket = '31-60') AS b2,
				SUM(a.patient_balance) FILTER (WHERE a.bucket = '61-90') AS b3,
				SUM(a.patient_balance) FILTER (WHERE a.bucket = '91+') AS b4,
				SUM(a.patient_balance) AS bt
			`+base+`
		) grouped
		`,
		args...,
	).Scan(&total, &totals.Bucket0To30, &totals.Bucket31To60, &totals.Bucket61To90, &totals.Bucket91Plus, &totals.Total); err != nil {
		return nil, 0, totals, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	paged := append(args, limit, f.Offset)

	rows, err := q.Query(
		ctx,
		`
		SELECT pt.id, COALESCE(pt.first_name || ' ', '') || pt.last_name,
			COALESCE(SUM(a.patient_balance) FILTER (WHERE a.bucket = '0-30'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.patient_balance) FILTER (WHERE a.bucket = '31-60'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.patient_balance) FILTER (WHERE a.bucket = '61-90'), 0)::numeric(12,2)::text,
			COALESCE(SUM(a.patient_balance) FILTER (WHERE a.bucket = '91+'), 0)::numeric(12,2)::text,
			SUM(a.patient_balance)::numeric(12,2)::text,
			COALESCE(cr.credit, 0)::numeric(12,2)::text,
			COALESCE(TO_CHAR(st.last_date, 'YYYY-MM-DD'), ''),
			TO_CHAR(MIN(a.date_of_service), 'YYYY-MM-DD')
		`+base+`
		ORDER BY pt.last_name, pt.first_name, pt.id
		LIMIT $`+fmt.Sprint(len(paged)-1)+` OFFSET $`+fmt.Sprint(len(paged)),
		paged...,
	)
	if err != nil {
		return nil, 0, totals, err
	}
	defer rows.Close()

	result := []PatientAgingRow{}

	for rows.Next() {
		var r PatientAgingRow
		if err := rows.Scan(
			&r.PatientID, &r.PatientName, &r.Bucket0To30, &r.Bucket31To60, &r.Bucket61To90, &r.Bucket91Plus,
			&r.Total, &r.Credit, &r.LastStatement, &r.OldestService,
		); err != nil {
			return nil, 0, totals, err
		}
		result = append(result, r)
	}

	return result, total, totals, rows.Err()
}
