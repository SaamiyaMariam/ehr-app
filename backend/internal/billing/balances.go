package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The balance engine has one source of truth: the billing_charge_balances
// view (what is owed on a charge, from the append-only financial records)
// and the views derived from it (billing_patient_balances,
// billing_ledger_events, patient_payment_credits — migration 011). Balances
// are never stored; handlers here only read those views. Dashboards,
// reports and statements must do the same rather than re-deriving formulas.

// PatientBalanceSummary is the patient-level roll-up.
type PatientBalanceSummary struct {
	PatientID                string `json:"patient_id"`
	PatientBalance           string `json:"patient_balance"`
	InsuranceBalance         string `json:"insurance_balance"`
	TotalOutstanding         string `json:"total_outstanding"`
	UnallocatedPatientCredit string `json:"unallocated_patient_credit"`
	OpenCharges              int    `json:"open_charges"`
}

func loadPatientSummary(ctx context.Context, q queryRower, patientID string) (PatientBalanceSummary, error) {
	s := PatientBalanceSummary{PatientID: patientID}

	err := q.QueryRow(
		ctx,
		`
		SELECT patient_balance::text, insurance_balance::text, total_outstanding::text,
			unallocated_credit::text, open_charges
		FROM billing_patient_balances
		WHERE patient_id = $1
		`,
		patientID,
	).Scan(&s.PatientBalance, &s.InsuranceBalance, &s.TotalOutstanding, &s.UnallocatedPatientCredit, &s.OpenCharges)

	return s, err
}

func (h *Handler) GetPatientBillingSummary(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	s, err := loadPatientSummary(r.Context(), h.db, patientID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing summary")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

// LedgerEvent is one financial event on a charge. Effect is +1 when it
// raises what the party owes and -1 when it lowers it; only "active" events
// count toward balances (voided events stay visible).
type LedgerEvent struct {
	ChargeID      string `json:"charge_id"`
	DateOfService string `json:"date_of_service"`
	ServiceCode   string `json:"service_code"`
	OccurredOn    string `json:"occurred_on"`
	RecordedAt    string `json:"recorded_at"`
	EventType     string `json:"event_type"`
	Party         string `json:"party"`
	Effect        int    `json:"effect"`
	Amount        string `json:"amount"`
	Status        string `json:"status"`
	SourceID      string `json:"source_id"`
	Detail        string `json:"detail"`
	// Voidable: an active standalone adjustment / transfer that can be voided
	// on its own (remittance records are reversed by voiding the remittance).
	Voidable bool `json:"voidable"`
}

const selectLedgerEvents = `
	SELECT e.charge_id, TO_CHAR(c.date_of_service, 'YYYY-MM-DD'), sc.code,
		TO_CHAR(e.occurred_on, 'YYYY-MM-DD'), TO_CHAR(e.recorded_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'),
		e.event_type, e.party, e.effect, e.amount::text, e.status, e.source_id, e.detail,
		e.status = 'active' AND (
			(e.event_type = 'adjustment' AND EXISTS (
				SELECT 1 FROM billing_adjustments ad WHERE ad.id = e.source_id AND ad.insurance_payment_id IS NULL))
			OR (e.event_type = 'transfer_out' AND EXISTS (
				SELECT 1 FROM responsibility_transfers t WHERE t.id = e.source_id AND t.insurance_payment_id IS NULL))
		)
	FROM billing_ledger_events e
	JOIN billing_charges c ON c.id = e.charge_id
	JOIN service_codes sc ON sc.id = c.service_code_id
`

func scanLedgerEvents(rows pgx.Rows) ([]LedgerEvent, error) {
	defer rows.Close()

	events := []LedgerEvent{}

	for rows.Next() {
		var e LedgerEvent
		if err := rows.Scan(
			&e.ChargeID, &e.DateOfService, &e.ServiceCode, &e.OccurredOn, &e.RecordedAt,
			&e.EventType, &e.Party, &e.Effect, &e.Amount, &e.Status, &e.SourceID, &e.Detail, &e.Voidable,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetPatientLedger lists a patient's financial events (newest first).
// Filters: party (patient|insurance), charge_id, include_voided.
func (h *Handler) GetPatientLedger(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	q := r.URL.Query()
	f := &chargeFilter{}
	f.add("e.patient_id = $%d", patientID)

	if v := strings.TrimSpace(q.Get("party")); v != "" {
		if !validEnum(v, "patient", "insurance") {
			writeError(w, http.StatusBadRequest, "party must be patient or insurance")
			return
		}
		f.add("e.party = $%d", v)
	}

	if v := strings.TrimSpace(q.Get("charge_id")); v != "" {
		if !isUUID(v) {
			writeError(w, http.StatusBadRequest, "charge_id is invalid")
			return
		}
		f.add("e.charge_id = $%d", v)
	}

	if q.Get("include_voided") != "true" {
		f.where = append(f.where, "e.status = 'active'")
	}

	page, pageSize := pageParams(r)
	where := " WHERE " + strings.Join(f.where, " AND ")

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM billing_ledger_events e`+where, f.args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	args := append(f.args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(
		r.Context(),
		selectLedgerEvents+where+` ORDER BY e.occurred_on DESC, e.recorded_at DESC, e.source_id LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	events, err := scanLedgerEvents(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	writeJSON(w, http.StatusOK, pagedResult[LedgerEvent]{Items: events, Total: total, Page: page, PageSize: pageSize})
}

// ChargeBreakdown explains one charge's balances line by line.
type ChargeBreakdown struct {
	OriginalCharge                 string `json:"original_charge"`
	InitialPatientResponsibility   string `json:"initial_patient_responsibility"`
	InitialInsuranceResponsibility string `json:"initial_insurance_responsibility"`
	TransfersToPatient             string `json:"transfers_to_patient"`
	TransfersToInsurance           string `json:"transfers_to_insurance"`
	PatientResponsibility          string `json:"patient_responsibility"`
	InsuranceResponsibility        string `json:"insurance_responsibility"`
	PatientPaid                    string `json:"patient_paid"`
	InsurancePaid                  string `json:"insurance_paid"`
	PatientAdjustments             string `json:"patient_adjustments"`
	InsuranceAdjustments           string `json:"insurance_adjustments"`
	Writeoffs                      string `json:"writeoffs"`
	PatientBalance                 string `json:"patient_balance"`
	InsuranceBalance               string `json:"insurance_balance"`
	TotalBalance                   string `json:"total_balance"`
}

func loadChargeBreakdown(ctx context.Context, q queryRower, chargeID string) (ChargeBreakdown, string, error) {
	var b ChargeBreakdown
	var patientID string

	err := q.QueryRow(
		ctx,
		`
		SELECT c.patient_id,
			v.total_charge::text,
			c.patient_responsibility::text, c.insurance_responsibility::text,
			v.transfers_to_patient::text, v.transfers_to_insurance::text,
			v.patient_responsibility::text, v.insurance_responsibility::text,
			v.patient_payments::text, v.insurance_payments::text,
			v.patient_adjustments::text, v.insurance_adjustments::text, v.writeoffs::text,
			v.patient_balance::text, v.insurance_balance::text, v.total_balance::text
		FROM billing_charges c
		JOIN billing_charge_balances v ON v.charge_id = c.id
		WHERE c.id = $1
		`,
		chargeID,
	).Scan(
		&patientID, &b.OriginalCharge,
		&b.InitialPatientResponsibility, &b.InitialInsuranceResponsibility,
		&b.TransfersToPatient, &b.TransfersToInsurance,
		&b.PatientResponsibility, &b.InsuranceResponsibility,
		&b.PatientPaid, &b.InsurancePaid,
		&b.PatientAdjustments, &b.InsuranceAdjustments, &b.Writeoffs,
		&b.PatientBalance, &b.InsuranceBalance, &b.TotalBalance,
	)

	return b, patientID, err
}

// GetChargeLedger returns a charge's breakdown and its full event history.
func (h *Handler) GetChargeLedger(w http.ResponseWriter, r *http.Request) {
	chargeID := r.PathValue("id")

	if !isUUID(chargeID) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	breakdown, _, err := loadChargeBreakdown(r.Context(), h.db, chargeID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectLedgerEvents+` WHERE e.charge_id = $1 ORDER BY e.occurred_on, e.recorded_at, e.source_id`,
		chargeID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	events, err := scanLedgerEvents(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load ledger")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"breakdown": breakdown, "events": events})
}
