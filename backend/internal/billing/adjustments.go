package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Standalone write-offs / adjustments and responsibility transfers recorded
// directly on a charge (not as part of an insurance remittance). Like every
// financial record they are voided, never deleted.

type adjustmentRequest struct {
	Party          string `json:"party"`
	AdjustmentType string `json:"adjustment_type"`
	Amount         string `json:"amount"`
	Reason         string `json:"reason"`
	Reference      string `json:"reference"`
}

type transferRequest struct {
	FromParty string `json:"from_party"`
	ToParty   string `json:"to_party"`
	Amount    string `json:"amount"`
	Reason    string `json:"reason"`
	Note      string `json:"note"`
}

// lockChargeScope locks every claim currently billing the charge, then the
// charge itself (the same order remittance posting uses).
func lockChargeScope(ctx context.Context, tx pgx.Tx, chargeID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT claim_id::text FROM claim_lines WHERE charge_id = $1 AND is_current`, chargeID)
	if err != nil {
		return nil, err
	}

	var claimIDs []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		claimIDs = append(claimIDs, id)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	claimIDs = uniqueSorted(claimIDs)

	return claimIDs, lockClaimsThenCharges(ctx, tx, claimIDs, []string{chargeID})
}

func refreshClaims(ctx context.Context, tx pgx.Tx, claimIDs []string, eventType, message, userID string) error {
	for _, id := range claimIDs {
		if err := recordClaimHistory(ctx, tx, id, eventType, "", "", "user", message, userID); err != nil {
			return err
		}

		if _, err := refreshClaimResolution(ctx, tx, id, userID); err != nil {
			return err
		}
	}

	return nil
}

// chargeBalanceFor returns a charge's current balances after locking; the
// charge must be active.
func chargeBalanceFor(ctx context.Context, tx pgx.Tx, chargeID string) (patientID string, balance chargeBalance, err error) {
	var status string

	if err = tx.QueryRow(ctx, `SELECT patient_id, status FROM billing_charges WHERE id = $1`, chargeID).Scan(&patientID, &status); err != nil {
		return
	}

	if status != "active" {
		err = finConflict("this service is voided")
		return
	}

	balances, err := loadChargeBalances(ctx, tx, []string{chargeID})
	if err != nil {
		return
	}

	balance = balances[chargeID]

	return
}

func (h *Handler) CreateAdjustment(w http.ResponseWriter, r *http.Request) {
	chargeID := r.PathValue("id")

	if !isUUID(chargeID) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	var req adjustmentRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Party = strings.ToLower(strings.TrimSpace(req.Party))
	if !validEnum(req.Party, "patient", "insurance") {
		writeError(w, http.StatusBadRequest, "party must be patient or insurance")
		return
	}

	adj, message := parseAdjustment(req.AdjustmentType, req.Amount, req.Reason, req.Reference, req.Party)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if adj.Reason == "" {
		writeError(w, http.StatusBadRequest, "a reason is required")
		return
	}

	userID := currentUserID(r)

	err := h.inChargeTx(r, chargeID, "could not post adjustment", func(tx pgx.Tx, claimIDs []string) error {
		patientID, balance, err := chargeBalanceFor(r.Context(), tx, chargeID)
		if err != nil {
			return err
		}

		open := balance.Patient
		if req.Party == "insurance" {
			open = balance.Insurance
		}

		if adj.Amount > open {
			return finBadRequest("the adjustment exceeds the " + req.Party + " balance of " + formatMoney(open))
		}

		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO billing_adjustments (charge_id, patient_id, party, adjustment_type, amount, reason, reference, created_by)
			VALUES ($1, $2, $3, $4, $5::numeric, $6, NULLIF($7, ''), $8)
			`,
			chargeID, patientID, req.Party, adj.Type, formatMoney(adj.Amount), adj.Reason, adj.Reference, nullIfEmpty(userID),
		); err != nil {
			return err
		}

		if req.Party != "insurance" {
			return nil
		}

		return refreshClaims(r.Context(), tx, claimIDs, "adjustment_posted",
			"Insurance adjustment of "+formatMoney(adj.Amount)+" ("+adj.Type+") posted: "+adj.Reason, userID)
	})
	if err != nil {
		writeFinError(w, err, "could not post adjustment")
		return
	}

	h.respondCharge(w, r, chargeID, http.StatusCreated)
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	chargeID := r.PathValue("id")

	if !isUUID(chargeID) {
		writeError(w, http.StatusNotFound, "charge not found")
		return
	}

	var req transferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.FromParty = strings.ToLower(strings.TrimSpace(req.FromParty))
	req.ToParty = strings.ToLower(strings.TrimSpace(req.ToParty))
	req.Reason = strings.ToLower(strings.TrimSpace(req.Reason))
	req.Note = strings.TrimSpace(req.Note)

	if !validEnum(req.FromParty, "patient", "insurance") || !validEnum(req.ToParty, "patient", "insurance") || req.FromParty == req.ToParty {
		writeError(w, http.StatusBadRequest, "a transfer moves responsibility between patient and insurance")
		return
	}

	if !validEnum(req.Reason, transferReasons...) {
		writeError(w, http.StatusBadRequest, "transfer reason is invalid")
		return
	}

	cents, ok := parseMoney(req.Amount)
	if !ok || cents <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be greater than zero with at most 2 decimal places")
		return
	}

	if len(req.Note) > 500 {
		writeError(w, http.StatusBadRequest, "note is too long")
		return
	}

	userID := currentUserID(r)

	err := h.inChargeTx(r, chargeID, "could not post transfer", func(tx pgx.Tx, claimIDs []string) error {
		patientID, balance, err := chargeBalanceFor(r.Context(), tx, chargeID)
		if err != nil {
			return err
		}

		open := balance.Patient
		if req.FromParty == "insurance" {
			open = balance.Insurance
		}

		if cents > open {
			return finBadRequest("the transfer exceeds the " + req.FromParty + " balance of " + formatMoney(open))
		}

		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO responsibility_transfers (charge_id, patient_id, from_party, to_party, amount, reason, note, created_by)
			VALUES ($1, $2, $3, $4, $5::numeric, $6, NULLIF($7, ''), $8)
			`,
			chargeID, patientID, req.FromParty, req.ToParty, formatMoney(cents), req.Reason, req.Note, nullIfEmpty(userID),
		); err != nil {
			return err
		}

		return refreshClaims(r.Context(), tx, claimIDs, "responsibility_transferred",
			formatMoney(cents)+" of responsibility moved from "+req.FromParty+" to "+req.ToParty+" ("+req.Reason+").", userID)
	})
	if err != nil {
		writeFinError(w, err, "could not post transfer")
		return
	}

	h.respondCharge(w, r, chargeID, http.StatusCreated)
}

// inChargeTx runs fn in a transaction holding the charge's claim / charge
// locks, then commits.
func (h *Handler) inChargeTx(r *http.Request, chargeID, fallback string, fn func(tx pgx.Tx, claimIDs []string) error) error {
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())

	var exists bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM billing_charges WHERE id = $1)`, chargeID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return finNotFound("charge not found")
	}

	claimIDs, err := lockChargeScope(r.Context(), tx, chargeID)
	if err != nil {
		return err
	}

	if err := fn(tx, claimIDs); err != nil {
		return err
	}

	if err := verifyNoNegativeBalances(r.Context(), tx, []string{chargeID}, "this change would make a balance negative"); err != nil {
		return err
	}

	return tx.Commit(r.Context())
}

func (h *Handler) respondCharge(w http.ResponseWriter, r *http.Request, chargeID string, status int) {
	c, err := h.getCharge(r.Context(), h.db, chargeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load charge")
		return
	}

	writeJSON(w, status, c)
}

// voidFinancialRecord voids a standalone adjustment or transfer. Records that
// belong to an insurance remittance can only be reversed by voiding it.
func (h *Handler) voidFinancialRecord(w http.ResponseWriter, r *http.Request, table, label string) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, label+" not found")
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

	var chargeID string
	var paymentID *string

	err := h.db.QueryRow(r.Context(), `SELECT charge_id, insurance_payment_id FROM `+table+` WHERE id = $1`, id).Scan(&chargeID, &paymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, label+" not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not void "+label)
		return
	}

	if paymentID != nil {
		writeError(w, http.StatusConflict, "this "+label+" is part of an insurance remittance; void the remittance instead")
		return
	}

	userID := currentUserID(r)

	err = h.inChargeTx(r, chargeID, "could not void "+label, func(tx pgx.Tx, claimIDs []string) error {
		tag, err := tx.Exec(
			r.Context(),
			`UPDATE `+table+` SET status = 'voided', voided_at = NOW(), voided_by = $2, void_reason = $3 WHERE id = $1 AND status = 'active'`,
			id, nullIfEmpty(userID), req.Reason,
		)
		if err != nil {
			return err
		}

		if tag.RowsAffected() == 0 {
			return finConflict("this " + label + " is already voided")
		}

		return refreshClaims(r.Context(), tx, claimIDs, "adjustment_voided", "A "+label+" was voided: "+req.Reason, userID)
	})
	if err != nil {
		writeFinError(w, err, "could not void "+label)
		return
	}

	h.respondCharge(w, r, chargeID, http.StatusOK)
}

func (h *Handler) VoidAdjustment(w http.ResponseWriter, r *http.Request) {
	h.voidFinancialRecord(w, r, "billing_adjustments", "adjustment")
}

func (h *Handler) VoidTransfer(w http.ResponseWriter, r *http.Request) {
	h.voidFinancialRecord(w, r, "responsibility_transfers", "responsibility transfer")
}
