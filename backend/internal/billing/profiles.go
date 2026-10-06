package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var taxonomyPattern = regexp.MustCompile(`^[0-9A-Z]{9}X$`)

// =========================================================
// PRACTICE BILLING PROFILE
// =========================================================

type PracticeBillingProfile struct {
	PracticeName        string `json:"practice_name"`
	NPI                 string `json:"npi"`
	TaxID               string `json:"tax_id"`
	TaxIDType           string `json:"tax_id_type"`
	TaxonomyCode        string `json:"taxonomy_code"`
	Address1            string `json:"address_1"`
	Address2            string `json:"address_2"`
	City                string `json:"city"`
	State               string `json:"state"`
	Zip                 string `json:"zip"`
	Phone               string `json:"phone"`
	BillingContactName  string `json:"billing_contact_name"`
	BillingContactEmail string `json:"billing_contact_email"`
}

func digitsOnly(value string) string {
	var b strings.Builder

	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func cleanPracticeProfile(p *PracticeBillingProfile) {
	p.PracticeName = strings.TrimSpace(p.PracticeName)
	p.NPI = digitsOnly(p.NPI)
	p.TaxID = digitsOnly(p.TaxID)
	p.TaxIDType = strings.ToLower(strings.TrimSpace(p.TaxIDType))
	p.TaxonomyCode = strings.ToUpper(strings.TrimSpace(p.TaxonomyCode))
	p.Address1 = strings.TrimSpace(p.Address1)
	p.Address2 = strings.TrimSpace(p.Address2)
	p.City = strings.TrimSpace(p.City)
	p.State = strings.ToUpper(strings.TrimSpace(p.State))
	p.Zip = strings.TrimSpace(p.Zip)
	p.Phone = strings.TrimSpace(p.Phone)
	p.BillingContactName = strings.TrimSpace(p.BillingContactName)
	p.BillingContactEmail = strings.TrimSpace(p.BillingContactEmail)

	if p.TaxIDType == "" {
		p.TaxIDType = "ein"
	}
}

// validatePracticeProfile allows a partial profile (claims report what is
// missing) but rejects malformed values.
func validatePracticeProfile(p *PracticeBillingProfile) string {
	if len(p.PracticeName) > 150 {
		return "practice name must be 150 characters or fewer"
	}

	if p.NPI != "" && !validNPI(p.NPI) {
		return "NPI must be a valid 10-digit National Provider Identifier"
	}

	if p.TaxID != "" && !taxIDPattern.MatchString(p.TaxID) {
		return "tax ID must be 9 digits"
	}

	if !validEnum(p.TaxIDType, "ein", "ssn") {
		return "tax ID type must be EIN or SSN"
	}

	if p.TaxonomyCode != "" && !taxonomyPattern.MatchString(p.TaxonomyCode) {
		return "taxonomy code must be 10 characters ending in X"
	}

	if p.State != "" && !statePattern.MatchString(p.State) {
		return "state must be a 2-letter code"
	}

	if p.Zip != "" && !zipPattern.MatchString(p.Zip) {
		return "ZIP must be 5 or 9 digits"
	}

	for _, field := range []string{p.Address1, p.Address2, p.BillingContactEmail} {
		if len(field) > 255 {
			return "address and email fields must be 255 characters or fewer"
		}
	}

	if len(p.City) > 100 || len(p.Phone) > 30 || len(p.BillingContactName) > 150 {
		return "one or more fields are too long"
	}

	return ""
}

const selectPracticeProfile = `
	SELECT
		COALESCE(practice_name, ''), COALESCE(npi, ''), COALESCE(tax_id, ''), tax_id_type,
		COALESCE(taxonomy_code, ''), COALESCE(address_1, ''), COALESCE(address_2, ''),
		COALESCE(city, ''), COALESCE(state, ''), COALESCE(zip, ''), COALESCE(phone, ''),
		COALESCE(billing_contact_name, ''), COALESCE(billing_contact_email, '')
	FROM practice_billing_profile
`

func scanPracticeProfile(row pgx.Row, p *PracticeBillingProfile) error {
	return row.Scan(
		&p.PracticeName, &p.NPI, &p.TaxID, &p.TaxIDType, &p.TaxonomyCode,
		&p.Address1, &p.Address2, &p.City, &p.State, &p.Zip, &p.Phone,
		&p.BillingContactName, &p.BillingContactEmail,
	)
}

func (h *Handler) GetPracticeProfile(w http.ResponseWriter, r *http.Request) {
	var p PracticeBillingProfile

	if err := scanPracticeProfile(h.db.QueryRow(r.Context(), selectPracticeProfile), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load practice billing profile")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) UpdatePracticeProfile(w http.ResponseWriter, r *http.Request) {
	var p PracticeBillingProfile

	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPracticeProfile(&p)

	if message := validatePracticeProfile(&p); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	_, err := h.db.Exec(
		r.Context(),
		`
		UPDATE practice_billing_profile SET
			practice_name = NULLIF($1, ''), npi = NULLIF($2, ''), tax_id = NULLIF($3, ''),
			tax_id_type = $4, taxonomy_code = NULLIF($5, ''),
			address_1 = NULLIF($6, ''), address_2 = NULLIF($7, ''), city = NULLIF($8, ''),
			state = NULLIF($9, ''), zip = NULLIF($10, ''), phone = NULLIF($11, ''),
			billing_contact_name = NULLIF($12, ''), billing_contact_email = NULLIF($13, ''),
			updated_at = NOW()
		`,
		p.PracticeName, p.NPI, p.TaxID, p.TaxIDType, p.TaxonomyCode,
		p.Address1, p.Address2, p.City, p.State, p.Zip, p.Phone,
		p.BillingContactName, p.BillingContactEmail,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not save practice billing profile")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// =========================================================
// CLINICIAN BILLING PROFILE
// =========================================================

type ClinicianBillingProfile struct {
	UserID        string `json:"user_id"`
	NPI           string `json:"npi"`
	TaxonomyCode  string `json:"taxonomy_code"`
	LicenseNumber string `json:"license_number"`
}

func (h *Handler) GetClinicianProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	if !isUUID(userID) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	p := ClinicianBillingProfile{UserID: userID}
	var exists bool

	if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load billing profile")
		return
	}

	if !exists {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT COALESCE(npi, ''), COALESCE(taxonomy_code, ''), COALESCE(license_number, '')
		FROM clinician_billing_profiles WHERE user_id = $1
		`,
		userID,
	).Scan(&p.NPI, &p.TaxonomyCode, &p.LicenseNumber)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not load billing profile")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) UpdateClinicianProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	if !isUUID(userID) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var p ClinicianBillingProfile

	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.UserID = userID
	p.NPI = digitsOnly(p.NPI)
	p.TaxonomyCode = strings.ToUpper(strings.TrimSpace(p.TaxonomyCode))
	p.LicenseNumber = strings.TrimSpace(p.LicenseNumber)

	if p.NPI != "" && !validNPI(p.NPI) {
		writeError(w, http.StatusBadRequest, "NPI must be a valid 10-digit National Provider Identifier")
		return
	}

	if p.TaxonomyCode != "" && !taxonomyPattern.MatchString(p.TaxonomyCode) {
		writeError(w, http.StatusBadRequest, "taxonomy code must be 10 characters ending in X")
		return
	}

	if len(p.LicenseNumber) > 50 {
		writeError(w, http.StatusBadRequest, "license number must be 50 characters or fewer")
		return
	}

	tag, err := h.db.Exec(
		r.Context(),
		`
		INSERT INTO clinician_billing_profiles (user_id, npi, taxonomy_code, license_number)
		SELECT id, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, '')
		FROM users WHERE id = $1
		ON CONFLICT (user_id) DO UPDATE SET
			npi = EXCLUDED.npi,
			taxonomy_code = EXCLUDED.taxonomy_code,
			license_number = EXCLUDED.license_number,
			updated_at = NOW()
		`,
		userID, p.NPI, p.TaxonomyCode, p.LicenseNumber,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not save billing profile")
		return
	}

	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, p)
}
