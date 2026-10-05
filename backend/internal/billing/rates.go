package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// =========================================================
// RATE & BILLING-METHOD RESOLUTION (single source of truth)
// =========================================================

const (
	RateSourcePatientCash   = "patient_cash_rate"
	RateSourcePayerSchedule = "payer_rate_schedule"
	RateSourceStandard      = "standard_rate"
)

var errNoRateConfigured = errors.New("no rate is configured for this service code")

type RateResult struct {
	RatePerUnit    int64
	Source         string
	RateScheduleID *string
}

// rateCandidates are the inputs gathered from the database; chooseRate
// applies the precedence rules without touching the database.
type rateCandidates struct {
	Direct bool

	Standard *int64
	CashRate *int64

	ScheduleID          *string
	ScheduleUseStandard bool
	ScheduleCustomRate  *int64
}

// chooseRate applies the rate precedence:
//
//	Direct:    patient cash rate → service code standard rate
//	Insurance: payer schedule custom rate → service code standard rate
func chooseRate(c rateCandidates) (RateResult, error) {
	if c.Direct {
		if c.CashRate != nil {
			return RateResult{RatePerUnit: *c.CashRate, Source: RateSourcePatientCash}, nil
		}
	} else if c.ScheduleID != nil && !c.ScheduleUseStandard && c.ScheduleCustomRate != nil {
		return RateResult{
			RatePerUnit:    *c.ScheduleCustomRate,
			Source:         RateSourcePayerSchedule,
			RateScheduleID: c.ScheduleID,
		}, nil
	}

	if c.Standard == nil {
		return RateResult{}, errNoRateConfigured
	}

	result := RateResult{RatePerUnit: *c.Standard, Source: RateSourceStandard}

	// Keep the schedule reference when a schedule applied but deferred to
	// standard rates, so the snapshot explains why.
	if !c.Direct && c.ScheduleID != nil {
		result.RateScheduleID = c.ScheduleID
	}

	return result, nil
}

type rateInput struct {
	PatientID     string
	ServiceCodeID string
	ClinicianID   string
	// PayerID empty means direct (patient) billing.
	PayerID string
}

func scanNullableMoney(text *string) *int64 {
	if text == nil {
		return nil
	}

	cents := mustCents(*text)
	return &cents
}

// resolveRate is the only place that determines a charge's rate.
//
// Payer schedule selection: the clinician's assigned (active) schedule for
// the payer; otherwise, if the payer has exactly one active schedule, that
// one; otherwise none (standard rates).
func resolveRate(ctx context.Context, q queryRower, in rateInput) (RateResult, error) {
	c := rateCandidates{Direct: in.PayerID == ""}

	var standard *string

	err := q.QueryRow(
		ctx,
		`SELECT standard_rate::text FROM service_codes WHERE id = $1`,
		in.ServiceCodeID,
	).Scan(&standard)
	if err != nil {
		return RateResult{}, err
	}

	c.Standard = scanNullableMoney(standard)

	if c.Direct {
		var cash *string

		err := q.QueryRow(
			ctx,
			`
			SELECT rate::text
			FROM patient_cash_rates
			WHERE patient_id = $1 AND service_code_id = $2
			`,
			in.PatientID,
			in.ServiceCodeID,
		).Scan(&cash)

		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return RateResult{}, err
		}

		c.CashRate = scanNullableMoney(cash)

		return chooseRate(c)
	}

	var scheduleID *string
	var useStandard bool
	var custom *string

	err = q.QueryRow(
		ctx,
		`
		WITH chosen AS (
			SELECT s.id, s.use_standard_practice_rates
			FROM payer_clinician_rate_schedules a
			JOIN payer_rate_schedules s ON s.id = a.rate_schedule_id
			WHERE a.payer_id = $1
			  AND a.clinician_id = $2
			  AND s.is_active

			UNION ALL

			SELECT s.id, s.use_standard_practice_rates
			FROM payer_rate_schedules s
			WHERE s.payer_id = $1
			  AND s.is_active
			  AND NOT EXISTS (
				SELECT 1
				FROM payer_clinician_rate_schedules a
				JOIN payer_rate_schedules s2 ON s2.id = a.rate_schedule_id
				WHERE a.payer_id = $1 AND a.clinician_id = $2 AND s2.is_active
			  )
			  AND (
				SELECT COUNT(*) FROM payer_rate_schedules
				WHERE payer_id = $1 AND is_active
			  ) = 1
		)
		SELECT
			c.id::text,
			c.use_standard_practice_rates,
			i.custom_rate::text
		FROM chosen c
		LEFT JOIN payer_rate_schedule_items i
			ON i.rate_schedule_id = c.id AND i.service_code_id = $3
		LIMIT 1
		`,
		in.PayerID,
		in.ClinicianID,
		in.ServiceCodeID,
	).Scan(&scheduleID, &useStandard, &custom)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return RateResult{}, err
	}

	c.ScheduleID = scheduleID
	c.ScheduleUseStandard = useStandard
	c.ScheduleCustomRate = scanNullableMoney(custom)

	return chooseRate(c)
}

// Charge billing methods combine payer/direct, network and submission.
var validChargeBillingMethods = []string{
	"direct",
	"insurance_in_network_electronic",
	"insurance_in_network_paper",
	"insurance_in_network_external",
	"insurance_out_of_network_electronic",
	"insurance_out_of_network_paper",
	"insurance_out_of_network_external",
}

// resolveSubmissionMethod applies: line override → payer override →
// practice default.
func resolveSubmissionMethod(lineOverride, payerMethod, practiceDefault string) string {
	if lineOverride != "" {
		return lineOverride
	}

	if payerMethod != "" {
		return payerMethod
	}

	return practiceDefault
}

func insuranceBillingMethod(inNetwork bool, submission string) string {
	if inNetwork {
		return "insurance_in_network_" + submission
	}

	return "insurance_out_of_network_" + submission
}

// submissionMethodOf extracts electronic/paper/external from a charge
// billing method ("" for direct).
func submissionMethodOf(billingMethod string) string {
	for _, s := range []string{"electronic", "paper", "external"} {
		if strings.HasSuffix(billingMethod, "_"+s) {
			return s
		}
	}

	return ""
}

// defaultInsuranceBillingMethod resolves the billing method for a charge
// billed to a payer when the user has not chosen one explicitly.
func defaultInsuranceBillingMethod(ctx context.Context, q queryRower, payerID string) (string, error) {
	var inNetwork bool
	var payerMethod string
	var inDefault, outDefault string

	err := q.QueryRow(
		ctx,
		`
		SELECT
			p.in_network,
			COALESCE(p.billing_method, ''),
			s.default_in_network_billing_method,
			s.default_out_of_network_billing_method
		FROM payers p
		CROSS JOIN practice_billing_settings s
		WHERE p.id = $1
		`,
		payerID,
	).Scan(&inNetwork, &payerMethod, &inDefault, &outDefault)
	if err != nil {
		return "", err
	}

	practiceDefault := outDefault
	if inNetwork {
		practiceDefault = inDefault
	}

	return insuranceBillingMethod(
		inNetwork,
		resolveSubmissionMethod("", payerMethod, practiceDefault),
	), nil
}

// =========================================================
// PRACTICE BILLING SETTINGS
// =========================================================

type PracticeBillingSettings struct {
	DefaultInNetworkBillingMethod    string `json:"default_in_network_billing_method"`
	DefaultOutOfNetworkBillingMethod string `json:"default_out_of_network_billing_method"`
}

var submissionMethods = []string{"electronic", "paper", "external"}

func (h *Handler) GetPracticeSettings(w http.ResponseWriter, r *http.Request) {
	var s PracticeBillingSettings

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT default_in_network_billing_method, default_out_of_network_billing_method
		FROM practice_billing_settings
		`,
	).Scan(&s.DefaultInNetworkBillingMethod, &s.DefaultOutOfNetworkBillingMethod)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing settings")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) UpdatePracticeSettings(w http.ResponseWriter, r *http.Request) {
	var s PracticeBillingSettings

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.DefaultInNetworkBillingMethod = strings.ToLower(strings.TrimSpace(s.DefaultInNetworkBillingMethod))
	s.DefaultOutOfNetworkBillingMethod = strings.ToLower(strings.TrimSpace(s.DefaultOutOfNetworkBillingMethod))

	if !validEnum(s.DefaultInNetworkBillingMethod, submissionMethods...) ||
		!validEnum(s.DefaultOutOfNetworkBillingMethod, submissionMethods...) {
		writeError(w, http.StatusBadRequest, "billing methods must be electronic, paper or external")
		return
	}

	_, err := h.db.Exec(
		r.Context(),
		`
		UPDATE practice_billing_settings
		SET
			default_in_network_billing_method = $1,
			default_out_of_network_billing_method = $2,
			updated_at = NOW()
		`,
		s.DefaultInNetworkBillingMethod,
		s.DefaultOutOfNetworkBillingMethod,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save billing settings")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

// =========================================================
// PAYER RATE SCHEDULES
// =========================================================

type RateScheduleItem struct {
	ServiceCodeID string `json:"service_code_id"`
	Code          string `json:"code"`
	Description   string `json:"description"`
	StandardRate  string `json:"standard_rate"`
	CustomRate    string `json:"custom_rate"`
	IsActive      bool   `json:"service_code_is_active"`
}

type RateSchedule struct {
	ID                       string             `json:"id"`
	PayerID                  string             `json:"payer_id"`
	PayerName                string             `json:"payer_name"`
	Name                     string             `json:"name"`
	UseStandardPracticeRates bool               `json:"use_standard_practice_rates"`
	IsActive                 bool               `json:"is_active"`
	ItemCount                int                `json:"item_count"`
	Items                    []RateScheduleItem `json:"items"`
}

func cleanRateSchedule(s *RateSchedule) {
	s.Name = strings.TrimSpace(s.Name)

	for i := range s.Items {
		s.Items[i].ServiceCodeID = strings.ToLower(strings.TrimSpace(s.Items[i].ServiceCodeID))
		s.Items[i].CustomRate = strings.TrimSpace(s.Items[i].CustomRate)
	}
}

// validateRateSchedule checks the request alone; returns "" when valid.
func validateRateSchedule(s *RateSchedule) string {
	if s.Name == "" {
		return "rate schedule name is required"
	}

	if len(s.Name) > 150 {
		return "rate schedule name must be 150 characters or fewer"
	}

	if len(s.Items) > 500 {
		return "too many rate schedule items"
	}

	seen := make(map[string]bool, len(s.Items))

	for _, item := range s.Items {
		if !isUUID(item.ServiceCodeID) {
			return "service code not found"
		}

		if seen[item.ServiceCodeID] {
			return "each service code may appear only once in a rate schedule"
		}

		seen[item.ServiceCodeID] = true

		if _, ok := parseMoney(item.CustomRate); !ok {
			return "custom rates must be non-negative amounts with at most 2 decimal places"
		}
	}

	return ""
}

func (h *Handler) loadRateSchedule(ctx context.Context, q queryRower, id string) (RateSchedule, error) {
	var s RateSchedule

	err := q.QueryRow(
		ctx,
		`
		SELECT s.id, s.payer_id, p.payer_name, s.name, s.use_standard_practice_rates, s.is_active
		FROM payer_rate_schedules s
		JOIN payers p ON p.id = s.payer_id
		WHERE s.id = $1
		`,
		id,
	).Scan(&s.ID, &s.PayerID, &s.PayerName, &s.Name, &s.UseStandardPracticeRates, &s.IsActive)
	if err != nil {
		return s, err
	}

	rows, err := q.Query(
		ctx,
		`
		SELECT
			c.id, c.code, c.description,
			COALESCE(c.standard_rate::text, ''),
			i.custom_rate::text,
			c.is_active
		FROM payer_rate_schedule_items i
		JOIN service_codes c ON c.id = i.service_code_id
		WHERE i.rate_schedule_id = $1
		ORDER BY c.code, c.description
		`,
		id,
	)
	if err != nil {
		return s, err
	}
	defer rows.Close()

	s.Items = make([]RateScheduleItem, 0)

	for rows.Next() {
		var item RateScheduleItem

		if err := rows.Scan(
			&item.ServiceCodeID, &item.Code, &item.Description,
			&item.StandardRate, &item.CustomRate, &item.IsActive,
		); err != nil {
			return s, err
		}

		s.Items = append(s.Items, item)
	}

	s.ItemCount = len(s.Items)

	return s, rows.Err()
}

func (h *Handler) ListRateSchedules(w http.ResponseWriter, r *http.Request) {
	payerID := r.PathValue("id")

	if !isUUID(payerID) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			s.id, s.payer_id, p.payer_name, s.name,
			s.use_standard_practice_rates, s.is_active,
			(SELECT COUNT(*) FROM payer_rate_schedule_items i WHERE i.rate_schedule_id = s.id)
		FROM payer_rate_schedules s
		JOIN payers p ON p.id = s.payer_id
		WHERE s.payer_id = $1
		ORDER BY s.is_active DESC, s.name
		`,
		payerID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load rate schedules")
		return
	}
	defer rows.Close()

	result := make([]RateSchedule, 0)

	for rows.Next() {
		var s RateSchedule

		if err := rows.Scan(
			&s.ID, &s.PayerID, &s.PayerName, &s.Name,
			&s.UseStandardPracticeRates, &s.IsActive, &s.ItemCount,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load rate schedules")
			return
		}

		s.Items = []RateScheduleItem{}
		result = append(result, s)
	}

	writeJSON(w, http.StatusOK, result)
}

// saveRateScheduleItems replaces a schedule's items inside tx. Service codes
// newly added to the schedule must be active; existing ones may be kept.
func saveRateScheduleItems(ctx context.Context, tx pgx.Tx, scheduleID string, items []RateScheduleItem) string {
	existing := map[string]bool{}

	rows, err := tx.Query(
		ctx,
		`SELECT service_code_id::text FROM payer_rate_schedule_items WHERE rate_schedule_id = $1`,
		scheduleID,
	)
	if err != nil {
		return "could not save rate schedule"
	}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "could not save rate schedule"
		}
		existing[id] = true
	}
	rows.Close()

	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ServiceCodeID)
	}

	found, err := loadServiceCodeStatus(ctx, tx, ids)
	if err != nil {
		return "could not save rate schedule"
	}

	if message := checkServiceCodeSelection(ids, found, existing); message != "" {
		return message
	}

	if _, err := tx.Exec(
		ctx,
		`DELETE FROM payer_rate_schedule_items WHERE rate_schedule_id = $1`,
		scheduleID,
	); err != nil {
		return "could not save rate schedule"
	}

	for _, item := range items {
		cents, _ := parseMoney(item.CustomRate)

		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO payer_rate_schedule_items (rate_schedule_id, service_code_id, custom_rate)
			VALUES ($1, $2, $3::numeric)
			`,
			scheduleID,
			item.ServiceCodeID,
			formatMoney(cents),
		); err != nil {
			return "could not save rate schedule"
		}
	}

	return ""
}

func (h *Handler) CreateRateSchedule(w http.ResponseWriter, r *http.Request) {
	payerID := r.PathValue("id")

	if !isUUID(payerID) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	var s RateSchedule

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanRateSchedule(&s)

	if message := validateRateSchedule(&s); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create rate schedule")
		return
	}
	defer tx.Rollback(r.Context())

	found, active, err := h.checkPayer(r.Context(), payerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create rate schedule")
		return
	}

	if !found {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	if !active {
		writeError(w, http.StatusBadRequest, "payer is disabled")
		return
	}

	var id string

	if err := tx.QueryRow(
		r.Context(),
		`
		INSERT INTO payer_rate_schedules (payer_id, name, use_standard_practice_rates)
		VALUES ($1, $2, $3)
		RETURNING id
		`,
		payerID,
		s.Name,
		s.UseStandardPracticeRates,
	).Scan(&id); err != nil {
		writeError(w, http.StatusBadRequest, "could not create rate schedule")
		return
	}

	if message := saveRateScheduleItems(r.Context(), tx, id, s.Items); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create rate schedule")
		return
	}

	created, err := h.loadRateSchedule(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load rate schedule")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) GetRateSchedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	s, err := h.loadRateSchedule(r.Context(), h.db, id)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load rate schedule")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) UpdateRateSchedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	var s RateSchedule

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanRateSchedule(&s)

	if message := validateRateSchedule(&s); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update rate schedule")
		return
	}
	defer tx.Rollback(r.Context())

	// payer_id is never changed by an update.
	tag, err := tx.Exec(
		r.Context(),
		`
		UPDATE payer_rate_schedules
		SET name = $1, use_standard_practice_rates = $2, updated_at = NOW()
		WHERE id = $3
		`,
		s.Name,
		s.UseStandardPracticeRates,
		id,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update rate schedule")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	if message := saveRateScheduleItems(r.Context(), tx, id, s.Items); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update rate schedule")
		return
	}

	updated, err := h.loadRateSchedule(r.Context(), h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load rate schedule")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) SetRateScheduleActive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeError(w, http.StatusBadRequest, "is_active is required")
		return
	}

	// Disabled schedules stay assigned but are ignored by rate resolution.
	tag, err := h.db.Exec(
		r.Context(),
		`UPDATE payer_rate_schedules SET is_active = $1, updated_at = NOW() WHERE id = $2`,
		*req.IsActive,
		id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update rate schedule status")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "rate schedule not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"is_active": *req.IsActive})
}

// =========================================================
// CLINICIAN RATE SCHEDULE ASSIGNMENTS
// =========================================================

type ClinicianRateSchedule struct {
	ClinicianID      string `json:"clinician_id"`
	ClinicianName    string `json:"clinician_name"`
	RateScheduleID   string `json:"rate_schedule_id"`
	RateScheduleName string `json:"rate_schedule_name"`
	ScheduleIsActive bool   `json:"rate_schedule_is_active"`
}

func (h *Handler) listClinicianRateSchedules(ctx context.Context, payerID string) ([]ClinicianRateSchedule, error) {
	rows, err := h.db.Query(
		ctx,
		`
		SELECT
			u.id, u.first_name || ' ' || u.last_name,
			s.id, s.name, s.is_active
		FROM payer_clinician_rate_schedules a
		JOIN users u ON u.id = a.clinician_id
		JOIN payer_rate_schedules s ON s.id = a.rate_schedule_id
		WHERE a.payer_id = $1
		ORDER BY u.last_name, u.first_name
		`,
		payerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ClinicianRateSchedule, 0)

	for rows.Next() {
		var a ClinicianRateSchedule

		if err := rows.Scan(
			&a.ClinicianID, &a.ClinicianName,
			&a.RateScheduleID, &a.RateScheduleName, &a.ScheduleIsActive,
		); err != nil {
			return nil, err
		}

		result = append(result, a)
	}

	return result, rows.Err()
}

func (h *Handler) ListClinicianRateSchedules(w http.ResponseWriter, r *http.Request) {
	payerID := r.PathValue("id")

	if !isUUID(payerID) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	result, err := h.listClinicianRateSchedules(r.Context(), payerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load clinician rate schedules")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func isClinician(ctx context.Context, q queryRower, userID string) (bool, error) {
	var ok bool

	err := q.QueryRow(
		ctx,
		`
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN user_roles ur ON ur.user_id = u.id
			JOIN roles ro ON ro.id = ur.role_id
			WHERE u.id = $1 AND ro.key = 'clinician'
		)
		`,
		userID,
	).Scan(&ok)

	return ok, err
}

func (h *Handler) UpdateClinicianRateSchedules(w http.ResponseWriter, r *http.Request) {
	payerID := r.PathValue("id")

	if !isUUID(payerID) {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	var req struct {
		Assignments []ClinicianRateSchedule `json:"assignments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Assignments) > 500 {
		writeError(w, http.StatusBadRequest, "too many assignments")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
		return
	}
	defer tx.Rollback(r.Context())

	var payerExists bool

	if err := tx.QueryRow(
		r.Context(),
		`SELECT EXISTS(SELECT 1 FROM payers WHERE id = $1)`,
		payerID,
	).Scan(&payerExists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
		return
	}

	if !payerExists {
		writeError(w, http.StatusNotFound, "payer not found")
		return
	}

	seen := map[string]bool{}

	for _, a := range req.Assignments {
		a.ClinicianID = strings.ToLower(strings.TrimSpace(a.ClinicianID))
		a.RateScheduleID = strings.ToLower(strings.TrimSpace(a.RateScheduleID))

		if !isUUID(a.ClinicianID) || !isUUID(a.RateScheduleID) {
			writeError(w, http.StatusBadRequest, "clinician and rate schedule are required")
			return
		}

		if seen[a.ClinicianID] {
			writeError(w, http.StatusBadRequest, "each clinician may have only one rate schedule per payer")
			return
		}

		seen[a.ClinicianID] = true

		ok, err := isClinician(r.Context(), tx, a.ClinicianID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
			return
		}

		if !ok {
			writeError(w, http.StatusBadRequest, "only clinicians can be assigned a rate schedule")
			return
		}

		// The schedule must belong to this payer (also enforced by the
		// composite foreign key).
		var belongs bool

		if err := tx.QueryRow(
			r.Context(),
			`SELECT EXISTS(SELECT 1 FROM payer_rate_schedules WHERE id = $1 AND payer_id = $2)`,
			a.RateScheduleID,
			payerID,
		).Scan(&belongs); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
			return
		}

		if !belongs {
			writeError(w, http.StatusBadRequest, "rate schedule does not belong to this payer")
			return
		}
	}

	if _, err := tx.Exec(
		r.Context(),
		`DELETE FROM payer_clinician_rate_schedules WHERE payer_id = $1`,
		payerID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
		return
	}

	for _, a := range req.Assignments {
		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO payer_clinician_rate_schedules (payer_id, clinician_id, rate_schedule_id)
			VALUES ($1, $2, $3)
			`,
			payerID,
			strings.ToLower(strings.TrimSpace(a.ClinicianID)),
			strings.ToLower(strings.TrimSpace(a.RateScheduleID)),
		); err != nil {
			writeError(w, http.StatusBadRequest, "could not save clinician rate schedules")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save clinician rate schedules")
		return
	}

	result, err := h.listClinicianRateSchedules(r.Context(), payerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load clinician rate schedules")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================
// PATIENT CASH RATES
// =========================================================

type PatientCashRate struct {
	ServiceCodeID string `json:"service_code_id"`
	Code          string `json:"code"`
	Description   string `json:"description"`
	StandardRate  string `json:"standard_rate"`
	Rate          string `json:"rate"`
}

func (h *Handler) listCashRates(ctx context.Context, patientID string) ([]PatientCashRate, error) {
	rows, err := h.db.Query(
		ctx,
		`
		SELECT c.id, c.code, c.description, COALESCE(c.standard_rate::text, ''), r.rate::text
		FROM patient_cash_rates r
		JOIN service_codes c ON c.id = r.service_code_id
		WHERE r.patient_id = $1
		ORDER BY c.code, c.description
		`,
		patientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]PatientCashRate, 0)

	for rows.Next() {
		var c PatientCashRate

		if err := rows.Scan(&c.ServiceCodeID, &c.Code, &c.Description, &c.StandardRate, &c.Rate); err != nil {
			return nil, err
		}

		result = append(result, c)
	}

	return result, rows.Err()
}

func (h *Handler) ListCashRates(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	exists, err := h.patientExists(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load cash rates")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	result, err := h.listCashRates(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load cash rates")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateCashRates(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")

	if !isUUID(patientID) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	var req struct {
		Rates []PatientCashRate `json:"rates"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Rates) > 500 {
		writeError(w, http.StatusBadRequest, "too many cash rates")
		return
	}

	seen := map[string]bool{}
	ids := make([]string, 0, len(req.Rates))

	for i := range req.Rates {
		rate := &req.Rates[i]
		rate.ServiceCodeID = strings.ToLower(strings.TrimSpace(rate.ServiceCodeID))

		if !isUUID(rate.ServiceCodeID) {
			writeError(w, http.StatusBadRequest, "service code not found")
			return
		}

		if seen[rate.ServiceCodeID] {
			writeError(w, http.StatusBadRequest, "each service code may have only one cash rate")
			return
		}

		seen[rate.ServiceCodeID] = true
		ids = append(ids, rate.ServiceCodeID)

		if _, ok := parseMoney(rate.Rate); !ok {
			writeError(w, http.StatusBadRequest, "cash rates must be non-negative amounts with at most 2 decimal places")
			return
		}
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}
	defer tx.Rollback(r.Context())

	var exists bool

	if err := tx.QueryRow(
		r.Context(),
		`SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`,
		patientID,
	).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	existing := map[string]bool{}

	rows, err := tx.Query(
		r.Context(),
		`SELECT service_code_id::text FROM patient_cash_rates WHERE patient_id = $1`,
		patientID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "could not save cash rates")
			return
		}
		existing[id] = true
	}
	rows.Close()

	found, err := loadServiceCodeStatus(r.Context(), tx, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}

	if message := checkServiceCodeSelection(ids, found, existing); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	if _, err := tx.Exec(
		r.Context(),
		`DELETE FROM patient_cash_rates WHERE patient_id = $1`,
		patientID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}

	for _, rate := range req.Rates {
		cents, _ := parseMoney(rate.Rate)

		if _, err := tx.Exec(
			r.Context(),
			`
			INSERT INTO patient_cash_rates (patient_id, service_code_id, rate)
			VALUES ($1, $2, $3::numeric)
			`,
			patientID,
			rate.ServiceCodeID,
			formatMoney(cents),
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save cash rates")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save cash rates")
		return
	}

	result, err := h.listCashRates(r.Context(), patientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load cash rates")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =========================================================
// RATE PREVIEW
// =========================================================

type RatePreviewRequest struct {
	PatientID         string `json:"patient_id"`
	ServiceCodeID     string `json:"service_code_id"`
	ClinicianID       string `json:"clinician_id"`
	InsurancePolicyID string `json:"insurance_policy_id"`
	BillingMethod     string `json:"billing_method"`
	Units             int    `json:"units"`
}

type RatePreview struct {
	RatePerUnit    string  `json:"rate_per_unit"`
	Source         string  `json:"source"`
	RateScheduleID *string `json:"rate_schedule_id"`
	BillingMethod  string  `json:"billing_method"`
	PayerID        string  `json:"payer_id"`
	Units          int     `json:"units"`
	TotalCharge    string  `json:"total_charge"`

	// SuggestedPatientResponsibility is the default patient share: the full
	// total for direct billing, otherwise the policy copay capped at the total.
	SuggestedPatientResponsibility string `json:"suggested_patient_responsibility"`

	copay *int64
}

// chargePricing resolves billing method, payer and rate for a prospective
// charge. It is shared by the rate preview and charge creation.
func (h *Handler) chargePricing(ctx context.Context, q queryRower, req RatePreviewRequest) (RatePreview, int, string) {
	var out RatePreview

	if !isUUID(req.PatientID) || !isUUID(req.ServiceCodeID) || !isUUID(req.ClinicianID) {
		return out, http.StatusBadRequest, "patient, service code and clinician are required"
	}

	if req.Units < 1 || req.Units > 999 {
		return out, http.StatusBadRequest, "units must be between 1 and 999"
	}

	out.Units = req.Units
	method := strings.ToLower(strings.TrimSpace(req.BillingMethod))

	if req.InsurancePolicyID == "" {
		if method != "" && method != "direct" {
			return out, http.StatusBadRequest, "an insurance policy is required for insurance billing"
		}

		method = "direct"
	} else {
		if !isUUID(req.InsurancePolicyID) {
			return out, http.StatusBadRequest, "insurance policy not found"
		}

		if method == "direct" {
			return out, http.StatusBadRequest, "direct billing cannot use an insurance policy"
		}

		var policyPatient, payerID string
		var policyActive bool
		var copay *string

		err := q.QueryRow(
			ctx,
			`SELECT patient_id, payer_id, is_active, copay::text FROM insurance_policies WHERE id = $1`,
			req.InsurancePolicyID,
		).Scan(&policyPatient, &payerID, &policyActive, &copay)

		if errors.Is(err, pgx.ErrNoRows) || (err == nil && policyPatient != req.PatientID) {
			return out, http.StatusBadRequest, "insurance policy not found for this patient"
		}

		if err != nil {
			return out, http.StatusInternalServerError, "could not resolve rate"
		}

		if !policyActive {
			return out, http.StatusBadRequest, "insurance policy is disabled"
		}

		out.PayerID = payerID
		out.copay = scanNullableMoney(copay)

		if method == "" {
			method, err = defaultInsuranceBillingMethod(ctx, q, payerID)
			if err != nil {
				return out, http.StatusInternalServerError, "could not resolve billing method"
			}
		}
	}

	if !validEnum(method, validChargeBillingMethods...) {
		return out, http.StatusBadRequest, "invalid billing method"
	}

	out.BillingMethod = method

	var codeActive bool

	err := q.QueryRow(ctx, `SELECT is_active FROM service_codes WHERE id = $1`, req.ServiceCodeID).Scan(&codeActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, http.StatusBadRequest, "service code not found"
	}

	if err != nil {
		return out, http.StatusInternalServerError, "could not resolve rate"
	}

	if !codeActive {
		return out, http.StatusBadRequest, "service code is disabled"
	}

	ok, err := isClinician(ctx, q, req.ClinicianID)
	if err != nil {
		return out, http.StatusInternalServerError, "could not resolve rate"
	}

	if !ok {
		return out, http.StatusBadRequest, "selected user is not a clinician"
	}

	payerForRate := out.PayerID
	if method == "direct" {
		payerForRate = ""
	}

	rate, err := resolveRate(ctx, q, rateInput{
		PatientID:     req.PatientID,
		ServiceCodeID: req.ServiceCodeID,
		ClinicianID:   req.ClinicianID,
		PayerID:       payerForRate,
	})

	if errors.Is(err, errNoRateConfigured) {
		return out, http.StatusBadRequest, err.Error()
	}

	if err != nil {
		return out, http.StatusInternalServerError, "could not resolve rate"
	}

	out.RatePerUnit = formatMoney(rate.RatePerUnit)
	out.Source = rate.Source
	out.RateScheduleID = rate.RateScheduleID
	total := rate.RatePerUnit * int64(req.Units)
	out.TotalCharge = formatMoney(total)

	patientShare, _, _ := splitResponsibility(total, method, nil, out.copay)
	out.SuggestedPatientResponsibility = formatMoney(patientShare)

	return out, http.StatusOK, ""
}

func (h *Handler) RatePreview(w http.ResponseWriter, r *http.Request) {
	var req RatePreviewRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Units == 0 {
		req.Units = 1
	}

	preview, status, message := h.chargePricing(r.Context(), h.db, req)
	if message != "" {
		writeError(w, status, message)
		return
	}

	writeJSON(w, http.StatusOK, preview)
}
