package billing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

var validUsageSettings = map[string]bool{
	"once_per_service": true,
	"per_unit":         true,
}

type PriorAuthorizationServiceCode struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
}

type PriorAuthorization struct {
	ID                string `json:"id"`
	InsurancePolicyID string `json:"insurance_policy_id"`

	AuthorizationCode string `json:"authorization_code"`

	AppliesToAnyServiceCode bool `json:"applies_to_any_service_code"`

	// ServiceCodeIDs is the write model; ServiceCodes is returned for display.
	ServiceCodeIDs []string                        `json:"service_code_ids"`
	ServiceCodes   []PriorAuthorizationServiceCode `json:"service_codes"`

	StartDate      string `json:"start_date"`
	ExpirationDate string `json:"expiration_date"`

	UsesAllowed   *int `json:"uses_allowed"`
	UsesRemaining *int `json:"uses_remaining"`

	UsageSetting string `json:"usage_setting"`

	Comments string `json:"comments"`

	IsActive bool `json:"is_active"`
}

func cleanPriorAuthorization(pa *PriorAuthorization) {
	pa.AuthorizationCode = strings.TrimSpace(pa.AuthorizationCode)
	pa.StartDate = strings.TrimSpace(pa.StartDate)
	pa.ExpirationDate = strings.TrimSpace(pa.ExpirationDate)
	pa.UsageSetting = strings.ToLower(strings.TrimSpace(pa.UsageSetting))
	pa.Comments = strings.TrimSpace(pa.Comments)

	ids := make([]string, 0, len(pa.ServiceCodeIDs))
	seen := make(map[string]bool, len(pa.ServiceCodeIDs))

	for _, id := range pa.ServiceCodeIDs {
		id = strings.ToLower(strings.TrimSpace(id))

		if id == "" || seen[id] {
			continue
		}

		seen[id] = true
		ids = append(ids, id)
	}

	pa.ServiceCodeIDs = ids
}

// validatePriorAuthorization checks the request on its own (no database).
// Like insurance policies, dates and use counts are optional but validated
// when present. It applies defaults and returns a user-facing message, or ""
// when valid.
func validatePriorAuthorization(pa *PriorAuthorization) string {
	if pa.AuthorizationCode == "" {
		return "authorization code is required"
	}

	if len(pa.AuthorizationCode) > 100 {
		return "authorization code must be 100 characters or fewer"
	}

	if pa.UsageSetting == "" {
		pa.UsageSetting = "once_per_service"
	}

	if !validUsageSettings[pa.UsageSetting] {
		return "usage setting must be once_per_service or per_unit"
	}

	if pa.StartDate != "" {
		if _, ok := parseDate(pa.StartDate); !ok {
			return "start date must be a valid date (YYYY-MM-DD)"
		}
	}

	if pa.ExpirationDate != "" {
		if _, ok := parseDate(pa.ExpirationDate); !ok {
			return "expiration date must be a valid date (YYYY-MM-DD)"
		}
	}

	if pa.StartDate != "" && pa.ExpirationDate != "" {
		start, _ := parseDate(pa.StartDate)
		end, _ := parseDate(pa.ExpirationDate)

		if end.Before(start) {
			return "expiration date cannot be before start date"
		}
	}

	if pa.UsesAllowed != nil &&
		(*pa.UsesAllowed < 0 || *pa.UsesAllowed > math.MaxInt32) {
		return "uses allowed must be zero or more"
	}

	if pa.UsesRemaining != nil &&
		(*pa.UsesRemaining < 0 || *pa.UsesRemaining > math.MaxInt32) {
		return "uses remaining must be zero or more"
	}

	if pa.UsesAllowed != nil && pa.UsesRemaining != nil &&
		*pa.UsesRemaining > *pa.UsesAllowed {
		return "uses remaining cannot be greater than uses allowed"
	}

	if pa.AppliesToAnyServiceCode {
		// "Any" replaces specific mappings; don't keep stale ones. Empty (not
		// nil) so the mapping cleanup's ANY($2) gets '{}' rather than NULL.
		pa.ServiceCodeIDs = []string{}
		return ""
	}

	if len(pa.ServiceCodeIDs) == 0 {
		return "select at least one service code, or choose any service code"
	}

	for _, id := range pa.ServiceCodeIDs {
		if !isUUID(id) {
			return "service code not found"
		}
	}

	return ""
}

// checkServiceCodeSelection validates requested service codes against the
// database. found maps each existing requested ID to its is_active flag;
// existing holds the authorization's current mappings. Disabled codes may
// stay mapped (history) but cannot be newly assigned.
func checkServiceCodeSelection(
	requested []string,
	found map[string]bool,
	existing map[string]bool,
) string {
	for _, id := range requested {
		active, ok := found[id]

		if !ok {
			return "service code not found"
		}

		if !active && !existing[id] {
			return "a selected service code is disabled"
		}
	}

	return ""
}

// statusChangeError enforces that an authorization can't be enabled while
// its insurance policy is disabled.
func statusChangeError(enable bool, policyActive bool) string {
	if enable && !policyActive {
		return "cannot enable a prior authorization while its insurance policy is disabled"
	}

	return ""
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadServiceCodeStatus(ctx context.Context, q queryer, ids []string) (map[string]bool, error) {
	found := make(map[string]bool, len(ids))

	if len(ids) == 0 {
		return found, nil
	}

	rows, err := q.Query(
		ctx,
		`SELECT id::text, is_active FROM service_codes WHERE id = ANY($1::uuid[])`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var active bool

		if err := rows.Scan(&id, &active); err != nil {
			return nil, err
		}

		found[id] = active
	}

	return found, rows.Err()
}

// Service codes are aggregated in the same statement to avoid N+1 lookups.
const selectPriorAuthorization = `
	SELECT
		pa.id,
		pa.insurance_policy_id,
		pa.authorization_code,
		pa.applies_to_any_service_code,
		COALESCE(
			(
				SELECT json_agg(
					json_build_object(
						'id', s.id,
						'code', s.code,
						'description', s.description,
						'is_active', s.is_active
					)
					ORDER BY s.code, s.description
				)
				FROM prior_authorization_service_codes m
				JOIN service_codes s ON s.id = m.service_code_id
				WHERE m.prior_authorization_id = pa.id
			),
			'[]'::json
		),
		COALESCE(TO_CHAR(pa.start_date, 'YYYY-MM-DD'), ''),
		COALESCE(TO_CHAR(pa.expiration_date, 'YYYY-MM-DD'), ''),
		pa.uses_allowed,
		pa.uses_remaining,
		pa.usage_setting,
		COALESCE(pa.comments, ''),
		pa.is_active
	FROM prior_authorizations pa
`

func scanPriorAuthorization(row pgx.Row, pa *PriorAuthorization) error {
	err := row.Scan(
		&pa.ID,
		&pa.InsurancePolicyID,
		&pa.AuthorizationCode,
		&pa.AppliesToAnyServiceCode,
		&pa.ServiceCodes,
		&pa.StartDate,
		&pa.ExpirationDate,
		&pa.UsesAllowed,
		&pa.UsesRemaining,
		&pa.UsageSetting,
		&pa.Comments,
		&pa.IsActive,
	)
	if err != nil {
		return err
	}

	pa.ServiceCodeIDs = make([]string, 0, len(pa.ServiceCodes))

	for _, s := range pa.ServiceCodes {
		pa.ServiceCodeIDs = append(pa.ServiceCodeIDs, s.ID)
	}

	return nil
}

func (h *Handler) getPriorAuthorization(ctx context.Context, id string) (PriorAuthorization, error) {
	var pa PriorAuthorization

	err := scanPriorAuthorization(
		h.db.QueryRow(ctx, selectPriorAuthorization+`WHERE pa.id = $1`, id),
		&pa,
	)

	return pa, err
}

func (h *Handler) ListPriorAuthorizations(w http.ResponseWriter, r *http.Request) {
	policyID := r.PathValue("id")

	if !isUUID(policyID) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	var exists bool

	if err := h.db.QueryRow(
		r.Context(),
		`SELECT EXISTS(SELECT 1 FROM insurance_policies WHERE id = $1)`,
		policyID,
	).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorizations")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		selectPriorAuthorization+`
		WHERE pa.insurance_policy_id = $1
		ORDER BY
			pa.is_active DESC,
			pa.expiration_date DESC NULLS FIRST,
			pa.created_at DESC
		`,
		policyID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorizations")
		return
	}
	defer rows.Close()

	result := make([]PriorAuthorization, 0)

	for rows.Next() {
		var pa PriorAuthorization

		if err := scanPriorAuthorization(rows, &pa); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load prior authorizations")
			return
		}

		result = append(result, pa)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorizations")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreatePriorAuthorization(w http.ResponseWriter, r *http.Request) {
	policyID := r.PathValue("id")

	if !isUUID(policyID) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	var pa PriorAuthorization

	if err := json.NewDecoder(r.Body).Decode(&pa); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPriorAuthorization(&pa)

	if message := validatePriorAuthorization(&pa); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create prior authorization")
		return
	}
	defer tx.Rollback(r.Context())

	// Share-lock the policy so it can't be disabled mid-create.
	var policyActive bool

	err = tx.QueryRow(
		r.Context(),
		`SELECT is_active FROM insurance_policies WHERE id = $1 FOR SHARE`,
		policyID,
	).Scan(&policyActive)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "insurance policy not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create prior authorization")
		return
	}

	if !policyActive {
		writeError(w, http.StatusBadRequest, "cannot add a prior authorization to a disabled insurance policy")
		return
	}

	found, err := loadServiceCodeStatus(r.Context(), tx, pa.ServiceCodeIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create prior authorization")
		return
	}

	if message := checkServiceCodeSelection(pa.ServiceCodeIDs, found, nil); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	// The policy always comes from the URL; new authorizations start enabled.
	var id string

	err = tx.QueryRow(
		r.Context(),
		`
		INSERT INTO prior_authorizations (
			insurance_policy_id,
			authorization_code,
			applies_to_any_service_code,
			start_date,
			expiration_date,
			uses_allowed,
			uses_remaining,
			usage_setting,
			comments
		)
		VALUES (
			$1,
			$2,
			$3,
			NULLIF($4, '')::date,
			NULLIF($5, '')::date,
			$6,
			$7,
			$8,
			NULLIF($9, '')
		)
		RETURNING id
		`,
		policyID,
		pa.AuthorizationCode,
		pa.AppliesToAnyServiceCode,
		pa.StartDate,
		pa.ExpirationDate,
		pa.UsesAllowed,
		pa.UsesRemaining,
		pa.UsageSetting,
		pa.Comments,
	).Scan(&id)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create prior authorization")
		return
	}

	if len(pa.ServiceCodeIDs) > 0 {
		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO prior_authorization_service_codes (
				prior_authorization_id,
				service_code_id
			)
			SELECT $1, UNNEST($2::uuid[])
			`,
			id,
			pa.ServiceCodeIDs,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save prior authorization service codes")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create prior authorization")
		return
	}

	created, err := h.getPriorAuthorization(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorization")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) GetPriorAuthorization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	pa, err := h.getPriorAuthorization(r.Context(), id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorization")
		return
	}

	writeJSON(w, http.StatusOK, pa)
}

func (h *Handler) UpdatePriorAuthorization(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	var pa PriorAuthorization

	if err := json.NewDecoder(r.Body).Decode(&pa); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPriorAuthorization(&pa)

	if message := validatePriorAuthorization(&pa); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}
	defer tx.Rollback(r.Context())

	var exists bool

	err = tx.QueryRow(
		r.Context(),
		`SELECT TRUE FROM prior_authorizations WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&exists)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}

	existing := make(map[string]bool)

	rows, err := tx.Query(
		r.Context(),
		`
		SELECT service_code_id::text
		FROM prior_authorization_service_codes
		WHERE prior_authorization_id = $1
		`,
		id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}

	for rows.Next() {
		var serviceCodeID string

		if err := rows.Scan(&serviceCodeID); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "could not update prior authorization")
			return
		}

		existing[serviceCodeID] = true
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}

	found, err := loadServiceCodeStatus(r.Context(), tx, pa.ServiceCodeIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}

	if message := checkServiceCodeSelection(pa.ServiceCodeIDs, found, existing); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	// insurance_policy_id and is_active are deliberately not updatable here.
	if _, err := tx.Exec(
		r.Context(),
		`
		UPDATE prior_authorizations
		SET
			authorization_code = $1,
			applies_to_any_service_code = $2,
			start_date = NULLIF($3, '')::date,
			expiration_date = NULLIF($4, '')::date,
			uses_allowed = $5,
			uses_remaining = $6,
			usage_setting = $7,
			comments = NULLIF($8, ''),
			updated_at = NOW()
		WHERE id = $9
		`,
		pa.AuthorizationCode,
		pa.AppliesToAnyServiceCode,
		pa.StartDate,
		pa.ExpirationDate,
		pa.UsesAllowed,
		pa.UsesRemaining,
		pa.UsageSetting,
		pa.Comments,
		id,
	); err != nil {
		writeError(w, http.StatusBadRequest, "could not update prior authorization")
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`
		DELETE FROM prior_authorization_service_codes
		WHERE prior_authorization_id = $1
		  AND NOT (service_code_id = ANY($2::uuid[]))
		`,
		id,
		pa.ServiceCodeIDs,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save prior authorization service codes")
		return
	}

	if len(pa.ServiceCodeIDs) > 0 {
		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO prior_authorization_service_codes (
				prior_authorization_id,
				service_code_id
			)
			SELECT $1, UNNEST($2::uuid[])
			ON CONFLICT DO NOTHING
			`,
			id,
			pa.ServiceCodeIDs,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save prior authorization service codes")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization")
		return
	}

	updated, err := h.getPriorAuthorization(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load prior authorization")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) SetPriorAuthorizationActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeError(w, http.StatusBadRequest, "is_active is required")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization status")
		return
	}
	defer tx.Rollback(r.Context())

	// Lock the policy too, so a concurrent policy disable can't interleave.
	var policyActive bool

	err = tx.QueryRow(
		r.Context(),
		`
		SELECT p.is_active
		FROM prior_authorizations pa
		JOIN insurance_policies p ON p.id = pa.insurance_policy_id
		WHERE pa.id = $1
		FOR UPDATE OF pa
		FOR SHARE OF p
		`,
		id,
	).Scan(&policyActive)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "prior authorization not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization status")
		return
	}

	if message := statusChangeError(*req.IsActive, policyActive); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`
		UPDATE prior_authorizations
		SET
			is_active = $1,
			updated_at = NOW()
		WHERE id = $2
		`,
		*req.IsActive,
		id,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization status")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update prior authorization status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"is_active": *req.IsActive,
	})
}
