package patients

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type Patient struct {
	ID string `json:"id"`

	PatientComments string `json:"patient_comments"`

	FirstName     string `json:"first_name"`
	MiddleName    string `json:"middle_name"`
	LastName      string `json:"last_name"`
	Suffix        string `json:"suffix"`
	PreferredName string `json:"preferred_name"`
	Pronouns      string `json:"pronouns"`

	DateOfBirth   string `json:"date_of_birth"`
	AccountNumber string `json:"account_number"`

	Address1 string `json:"address_1"`
	Address2 string `json:"address_2"`
	Zip      string `json:"zip"`
	City     string `json:"city"`
	State    string `json:"state"`
	TimeZone string `json:"time_zone"`

	MobilePhone             string `json:"mobile_phone"`
	MobileMessagePreference string `json:"mobile_message_preference"`

	HomePhone             string `json:"home_phone"`
	HomeMessagePreference string `json:"home_message_preference"`

	WorkPhone             string `json:"work_phone"`
	WorkMessagePreference string `json:"work_message_preference"`

	OtherPhone             string `json:"other_phone"`
	OtherMessagePreference string `json:"other_message_preference"`

	Email                      string `json:"email"`
	AppointmentReminderSetting string `json:"appointment_reminder_setting"`

	AdministrativeSex string `json:"administrative_sex"`
	GenderIdentity    string `json:"gender_identity"`

	HIPAANPPOnFile bool   `json:"hipaa_npp_on_file"`
	PCPRelease     string `json:"pcp_release"`

	PADAcknowledged     bool   `json:"pad_acknowledged"`
	PADAcknowledgedDate string `json:"pad_acknowledged_date"`

	AssignedClinicianID string `json:"assigned_clinician_id"`
}

type PatientListItem struct {
	ID            string `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	PreferredName string `json:"preferred_name"`
	DateOfBirth   string `json:"date_of_birth"`
	Email         string `json:"email"`
	MobilePhone   string `json:"mobile_phone"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func cleanPatient(p *Patient) {
	p.PatientComments = strings.TrimSpace(p.PatientComments)

	p.FirstName = strings.TrimSpace(p.FirstName)
	p.MiddleName = strings.TrimSpace(p.MiddleName)
	p.LastName = strings.TrimSpace(p.LastName)
	p.Suffix = strings.TrimSpace(p.Suffix)
	p.PreferredName = strings.TrimSpace(p.PreferredName)
	p.Pronouns = strings.TrimSpace(p.Pronouns)

	p.DateOfBirth = strings.TrimSpace(p.DateOfBirth)
	p.AccountNumber = strings.TrimSpace(p.AccountNumber)

	p.Address1 = strings.TrimSpace(p.Address1)
	p.Address2 = strings.TrimSpace(p.Address2)
	p.Zip = strings.TrimSpace(p.Zip)
	p.City = strings.TrimSpace(p.City)
	p.State = strings.TrimSpace(p.State)
	p.TimeZone = strings.TrimSpace(p.TimeZone)

	p.MobilePhone = strings.TrimSpace(p.MobilePhone)
	p.HomePhone = strings.TrimSpace(p.HomePhone)
	p.WorkPhone = strings.TrimSpace(p.WorkPhone)
	p.OtherPhone = strings.TrimSpace(p.OtherPhone)

	p.Email = strings.ToLower(strings.TrimSpace(p.Email))

	p.AssignedClinicianID = strings.TrimSpace(p.AssignedClinicianID)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var patient Patient

	if err := json.NewDecoder(r.Body).Decode(&patient); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPatient(&patient)

	if patient.LastName == "" {
		writeError(w, http.StatusBadRequest, "last name is required")
		return
	}

	err := h.db.QueryRow(
		r.Context(),
		`
		INSERT INTO patients (
			patient_comments,
			first_name,
			middle_name,
			last_name,
			suffix,
			preferred_name,
			pronouns,
			date_of_birth,
			account_number,
			address_1,
			address_2,
			zip,
			city,
			state,
			time_zone,
			mobile_phone,
			mobile_message_preference,
			home_phone,
			home_message_preference,
			work_phone,
			work_message_preference,
			other_phone,
			other_message_preference,
			email,
			appointment_reminder_setting,
			administrative_sex,
			gender_identity,
			hipaa_npp_on_file,
			pcp_release,
			pad_acknowledged,
			pad_acknowledged_date,
			assigned_clinician_id
		)
		VALUES (
			NULLIF($1, ''),
			NULLIF($2, ''),
			NULLIF($3, ''),
			$4,
			NULLIF($5, ''),
			NULLIF($6, ''),
			NULLIF($7, ''),
			NULLIF($8, '')::date,
			NULLIF($9, ''),
			NULLIF($10, ''),
			NULLIF($11, ''),
			NULLIF($12, ''),
			NULLIF($13, ''),
			NULLIF($14, ''),
			NULLIF($15, ''),
			NULLIF($16, ''),
			NULLIF($17, ''),
			NULLIF($18, ''),
			NULLIF($19, ''),
			NULLIF($20, ''),
			NULLIF($21, ''),
			NULLIF($22, ''),
			NULLIF($23, ''),
			NULLIF($24, ''),
			NULLIF($25, ''),
			NULLIF($26, ''),
			NULLIF($27, ''),
			$28,
			NULLIF($29, ''),
			$30,
			NULLIF($31, '')::date,
			NULLIF($32, '')::uuid
		)
		RETURNING id
		`,
		patient.PatientComments,
		patient.FirstName,
		patient.MiddleName,
		patient.LastName,
		patient.Suffix,
		patient.PreferredName,
		patient.Pronouns,
		patient.DateOfBirth,
		patient.AccountNumber,
		patient.Address1,
		patient.Address2,
		patient.Zip,
		patient.City,
		patient.State,
		patient.TimeZone,
		patient.MobilePhone,
		patient.MobileMessagePreference,
		patient.HomePhone,
		patient.HomeMessagePreference,
		patient.WorkPhone,
		patient.WorkMessagePreference,
		patient.OtherPhone,
		patient.OtherMessagePreference,
		patient.Email,
		patient.AppointmentReminderSetting,
		patient.AdministrativeSex,
		patient.GenderIdentity,
		patient.HIPAANPPOnFile,
		patient.PCPRelease,
		patient.PADAcknowledged,
		patient.PADAcknowledgedDate,
		patient.AssignedClinicianID,
	).Scan(&patient.ID)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not create patient")
		return
	}

	writeJSON(w, http.StatusCreated, patient)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		r.Context(),
		`
		SELECT
			id,
			COALESCE(first_name, ''),
			last_name,
			COALESCE(preferred_name, ''),
			COALESCE(TO_CHAR(date_of_birth, 'YYYY-MM-DD'), ''),
			COALESCE(email, ''),
			COALESCE(mobile_phone, '')
		FROM patients
		ORDER BY last_name, first_name
		`,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load patients")
		return
	}
	defer rows.Close()

	patients := make([]PatientListItem, 0)

	for rows.Next() {
		var patient PatientListItem

		if err := rows.Scan(
			&patient.ID,
			&patient.FirstName,
			&patient.LastName,
			&patient.PreferredName,
			&patient.DateOfBirth,
			&patient.Email,
			&patient.MobilePhone,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load patients")
			return
		}

		patients = append(patients, patient)
	}

	writeJSON(w, http.StatusOK, patients)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var patient Patient

	err := h.db.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			COALESCE(patient_comments, ''),
			COALESCE(first_name, ''),
			COALESCE(middle_name, ''),
			last_name,
			COALESCE(suffix, ''),
			COALESCE(preferred_name, ''),
			COALESCE(pronouns, ''),
			COALESCE(TO_CHAR(date_of_birth, 'YYYY-MM-DD'), ''),
			COALESCE(account_number, ''),
			COALESCE(address_1, ''),
			COALESCE(address_2, ''),
			COALESCE(zip, ''),
			COALESCE(city, ''),
			COALESCE(state, ''),
			COALESCE(time_zone, ''),
			COALESCE(mobile_phone, ''),
			COALESCE(mobile_message_preference, ''),
			COALESCE(home_phone, ''),
			COALESCE(home_message_preference, ''),
			COALESCE(work_phone, ''),
			COALESCE(work_message_preference, ''),
			COALESCE(other_phone, ''),
			COALESCE(other_message_preference, ''),
			COALESCE(email, ''),
			COALESCE(appointment_reminder_setting, ''),
			COALESCE(administrative_sex, ''),
			COALESCE(gender_identity, ''),
			hipaa_npp_on_file,
			COALESCE(pcp_release, ''),
			pad_acknowledged,
			COALESCE(TO_CHAR(pad_acknowledged_date, 'YYYY-MM-DD'), ''),
			COALESCE(assigned_clinician_id::text, '')
		FROM patients
		WHERE id = $1
		`,
		id,
	).Scan(
		&patient.ID,
		&patient.PatientComments,
		&patient.FirstName,
		&patient.MiddleName,
		&patient.LastName,
		&patient.Suffix,
		&patient.PreferredName,
		&patient.Pronouns,
		&patient.DateOfBirth,
		&patient.AccountNumber,
		&patient.Address1,
		&patient.Address2,
		&patient.Zip,
		&patient.City,
		&patient.State,
		&patient.TimeZone,
		&patient.MobilePhone,
		&patient.MobileMessagePreference,
		&patient.HomePhone,
		&patient.HomeMessagePreference,
		&patient.WorkPhone,
		&patient.WorkMessagePreference,
		&patient.OtherPhone,
		&patient.OtherMessagePreference,
		&patient.Email,
		&patient.AppointmentReminderSetting,
		&patient.AdministrativeSex,
		&patient.GenderIdentity,
		&patient.HIPAANPPOnFile,
		&patient.PCPRelease,
		&patient.PADAcknowledged,
		&patient.PADAcknowledgedDate,
		&patient.AssignedClinicianID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load patient")
		return
	}

	writeJSON(w, http.StatusOK, patient)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var patient Patient

	if err := json.NewDecoder(r.Body).Decode(&patient); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPatient(&patient)

	if patient.LastName == "" {
		writeError(w, http.StatusBadRequest, "last name is required")
		return
	}

	commandTag, err := h.db.Exec(
		r.Context(),
		`
		UPDATE patients
		SET
			patient_comments = NULLIF($1, ''),
			first_name = NULLIF($2, ''),
			middle_name = NULLIF($3, ''),
			last_name = $4,
			suffix = NULLIF($5, ''),
			preferred_name = NULLIF($6, ''),
			pronouns = NULLIF($7, ''),
			date_of_birth = NULLIF($8, '')::date,
			account_number = NULLIF($9, ''),
			address_1 = NULLIF($10, ''),
			address_2 = NULLIF($11, ''),
			zip = NULLIF($12, ''),
			city = NULLIF($13, ''),
			state = NULLIF($14, ''),
			time_zone = NULLIF($15, ''),
			mobile_phone = NULLIF($16, ''),
			mobile_message_preference = NULLIF($17, ''),
			home_phone = NULLIF($18, ''),
			home_message_preference = NULLIF($19, ''),
			work_phone = NULLIF($20, ''),
			work_message_preference = NULLIF($21, ''),
			other_phone = NULLIF($22, ''),
			other_message_preference = NULLIF($23, ''),
			email = NULLIF($24, ''),
			appointment_reminder_setting = NULLIF($25, ''),
			administrative_sex = NULLIF($26, ''),
			gender_identity = NULLIF($27, ''),
			hipaa_npp_on_file = $28,
			pcp_release = NULLIF($29, ''),
			pad_acknowledged = $30,
			pad_acknowledged_date = NULLIF($31, '')::date,
			assigned_clinician_id = NULLIF($32, '')::uuid,
			updated_at = NOW()
		WHERE id = $33
		`,
		patient.PatientComments,
		patient.FirstName,
		patient.MiddleName,
		patient.LastName,
		patient.Suffix,
		patient.PreferredName,
		patient.Pronouns,
		patient.DateOfBirth,
		patient.AccountNumber,
		patient.Address1,
		patient.Address2,
		patient.Zip,
		patient.City,
		patient.State,
		patient.TimeZone,
		patient.MobilePhone,
		patient.MobileMessagePreference,
		patient.HomePhone,
		patient.HomeMessagePreference,
		patient.WorkPhone,
		patient.WorkMessagePreference,
		patient.OtherPhone,
		patient.OtherMessagePreference,
		patient.Email,
		patient.AppointmentReminderSetting,
		patient.AdministrativeSex,
		patient.GenderIdentity,
		patient.HIPAANPPOnFile,
		patient.PCPRelease,
		patient.PADAcknowledged,
		patient.PADAcknowledgedDate,
		patient.AssignedClinicianID,
		id,
	)

	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update patient")
		return
	}

	if commandTag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "patient not found")
		return
	}

	patient.ID = id
	writeJSON(w, http.StatusOK, patient)
}
