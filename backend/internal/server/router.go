// Package server wires HTTP routes to handlers and role checks.
//
// Every route is registered through one of three helpers (public, authed,
// roles) so the access class of each endpoint is explicit, recorded in
// Routes, and covered by the route-security tests.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ehr-backend/internal/auth"
	"ehr-backend/internal/billing"
	"ehr-backend/internal/patients"
	"ehr-backend/internal/payers"
	"ehr-backend/internal/roles"
	"ehr-backend/internal/servicecodes"
	"ehr-backend/internal/users"
)

// Access classes of a route.
const (
	AccessPublic = "public"
	AccessAuth   = "authenticated"
	AccessRoles  = "roles"
)

// RouteInfo documents how one route is protected.
type RouteInfo struct {
	Pattern string
	Access  string
	Roles   []string
}

// Router owns the mux and the registry of protected routes.
type Router struct {
	mux    *http.ServeMux
	auth   *auth.Handler
	Routes []RouteInfo
}

func (r *Router) public(pattern string, h http.HandlerFunc) {
	r.Routes = append(r.Routes, RouteInfo{Pattern: pattern, Access: AccessPublic})
	r.mux.HandleFunc(pattern, h)
}

// authed requires a valid token but no particular role.
func (r *Router) authed(pattern string, h http.HandlerFunc) {
	r.Routes = append(r.Routes, RouteInfo{Pattern: pattern, Access: AccessAuth})
	r.mux.HandleFunc(pattern, r.auth.RequireAuth(h))
}

// roles requires a valid token and at least one of the listed roles.
func (r *Router) roles(pattern string, h http.HandlerFunc, roleKeys ...string) {
	r.Routes = append(r.Routes, RouteInfo{Pattern: pattern, Access: AccessRoles, Roles: roleKeys})
	r.mux.HandleFunc(pattern, r.auth.RequireAnyRole(h, roleKeys...))
}

// Role groups used across billing routes.
var (
	billerRoles = []string{"practice_biller"}

	serviceCodeManagerRoles = []string{
		"practice_administrator",
		"practice_biller",
		"clinical_administrator",
	}

	// Practice-wide billing configuration (defaults, practice profile).
	billingAdminRoles = []string{
		"practice_administrator",
		"practice_biller",
	}

	// Clinical + billing staff may maintain diagnoses.
	diagnosisRoles = []string{
		"practice_biller",
		"clinician",
		"clinical_administrator",
	}
)

// New builds the full API handler.
func New(db *pgxpool.Pool, jwtSecret string) (http.Handler, *Router) {
	rt := &Router{
		mux:  http.NewServeMux(),
		auth: auth.NewHandler(db, jwtSecret),
	}

	patientHandler := patients.NewHandler(db)
	userHandler := users.NewHandler(db)
	roleHandler := roles.NewHandler(db)
	payerHandler := payers.NewHandler(db)
	billingHandler := billing.NewHandler(db)
	serviceCodeHandler := servicecodes.NewHandler(db)

	rt.public("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "database": "unavailable"})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "database": "connected"})
	})

	// Auth
	rt.public("POST /api/auth/signup", rt.auth.Signup)
	rt.public("POST /api/auth/login", rt.auth.Login)
	rt.authed("GET /api/auth/me", rt.auth.Me)

	// Patients
	rt.authed("GET /api/patients", patientHandler.List)
	rt.roles("POST /api/patients", patientHandler.Create, "clinician", "practice_scheduler")
	rt.authed("GET /api/patients/{id}", patientHandler.Get)
	rt.authed("PUT /api/patients/{id}", patientHandler.Update)

	// Users / Employees
	rt.authed("GET /api/users", userHandler.List)
	rt.authed("POST /api/users", userHandler.Create)
	rt.authed("GET /api/users/{id}", userHandler.Get)
	rt.authed("PUT /api/users/{id}", userHandler.Update)

	// Roles
	rt.authed("GET /api/roles", roleHandler.List)
	rt.authed("GET /api/users/{id}/roles", roleHandler.GetUserRoles)
	rt.authed("PUT /api/users/{id}/roles", roleHandler.UpdateUserRoles)

	// Payers
	rt.authed("GET /api/payers", payerHandler.List)
	rt.roles("POST /api/payers", payerHandler.Create, billerRoles...)
	rt.authed("GET /api/payers/{id}", payerHandler.Get)
	rt.roles("PUT /api/payers/{id}", payerHandler.Update, billerRoles...)
	rt.roles("PATCH /api/payers/{id}/status", payerHandler.SetActive, billerRoles...)
	rt.authed("GET /api/clinicians", userHandler.ListClinicians)

	// Patient billing
	rt.authed("GET /api/patients/{id}/billing-settings", billingHandler.GetSettings)
	rt.roles("PUT /api/patients/{id}/billing-settings", billingHandler.UpdateSettings, billerRoles...)
	rt.authed("GET /api/patients/{id}/insurance-policies", billingHandler.ListPolicies)
	rt.roles("POST /api/patients/{id}/insurance-policies", billingHandler.CreatePolicy, billerRoles...)
	rt.authed("GET /api/insurance-policies/{id}", billingHandler.GetPolicy)
	rt.roles("PUT /api/insurance-policies/{id}", billingHandler.UpdatePolicy, billerRoles...)
	rt.roles("PATCH /api/insurance-policies/{id}/status", billingHandler.SetPolicyActive, billerRoles...)

	// Prior authorizations
	rt.authed("GET /api/insurance-policies/{id}/prior-authorizations", billingHandler.ListPriorAuthorizations)
	rt.roles("POST /api/insurance-policies/{id}/prior-authorizations", billingHandler.CreatePriorAuthorization, billerRoles...)
	rt.authed("GET /api/prior-authorizations/{id}", billingHandler.GetPriorAuthorization)
	rt.roles("PUT /api/prior-authorizations/{id}", billingHandler.UpdatePriorAuthorization, billerRoles...)
	rt.roles("PATCH /api/prior-authorizations/{id}/status", billingHandler.SetPriorAuthorizationActive, billerRoles...)

	// Service codes
	rt.authed("GET /api/service-codes", serviceCodeHandler.List)
	rt.roles("POST /api/service-codes", serviceCodeHandler.Create, serviceCodeManagerRoles...)
	rt.authed("GET /api/service-codes/{id}", serviceCodeHandler.Get)
	rt.roles("PUT /api/service-codes/{id}", serviceCodeHandler.Update, serviceCodeManagerRoles...)
	rt.roles("PATCH /api/service-codes/{id}/status", serviceCodeHandler.SetActive, serviceCodeManagerRoles...)

	// Billing configuration / rates
	rt.authed("GET /api/billing/settings", billingHandler.GetPracticeSettings)
	rt.roles("PUT /api/billing/settings", billingHandler.UpdatePracticeSettings, billingAdminRoles...)
	rt.authed("GET /api/payers/{id}/rate-schedules", billingHandler.ListRateSchedules)
	rt.roles("POST /api/payers/{id}/rate-schedules", billingHandler.CreateRateSchedule, billerRoles...)
	rt.authed("GET /api/rate-schedules/{id}", billingHandler.GetRateSchedule)
	rt.roles("PUT /api/rate-schedules/{id}", billingHandler.UpdateRateSchedule, billerRoles...)
	rt.roles("PATCH /api/rate-schedules/{id}/status", billingHandler.SetRateScheduleActive, billerRoles...)
	rt.authed("GET /api/payers/{id}/clinician-rate-schedules", billingHandler.ListClinicianRateSchedules)
	rt.roles("PUT /api/payers/{id}/clinician-rate-schedules", billingHandler.UpdateClinicianRateSchedules, billerRoles...)
	rt.authed("GET /api/patients/{id}/cash-rates", billingHandler.ListCashRates)
	rt.roles("PUT /api/patients/{id}/cash-rates", billingHandler.UpdateCashRates, billerRoles...)
	rt.authed("POST /api/billing/rate-preview", billingHandler.RatePreview)

	// Diagnoses
	rt.authed("GET /api/patients/{id}/diagnoses", billingHandler.ListDiagnoses)
	rt.roles("POST /api/patients/{id}/diagnoses", billingHandler.CreateDiagnosis, diagnosisRoles...)
	rt.roles("PUT /api/diagnoses/{id}", billingHandler.UpdateDiagnosis, diagnosisRoles...)
	rt.roles("PATCH /api/diagnoses/{id}/status", billingHandler.SetDiagnosisActive, diagnosisRoles...)

	// Charges / billing transactions
	rt.authed("GET /api/patients/{id}/billing-transactions", billingHandler.ListPatientTransactions)
	rt.roles("POST /api/patients/{id}/charges", billingHandler.CreateCharge, billerRoles...)
	rt.authed("GET /api/charges/{id}", billingHandler.GetCharge)
	rt.roles("PUT /api/charges/{id}", billingHandler.UpdateCharge, billerRoles...)
	rt.roles("POST /api/charges/{id}/void", billingHandler.VoidCharge, billerRoles...)
	rt.authed("GET /api/billing/transactions", billingHandler.SearchTransactions)

	// Billing profiles (claim provider data)
	rt.authed("GET /api/billing/practice-profile", billingHandler.GetPracticeProfile)
	rt.roles("PUT /api/billing/practice-profile", billingHandler.UpdatePracticeProfile, billingAdminRoles...)
	rt.authed("GET /api/users/{id}/billing-profile", billingHandler.GetClinicianProfile)
	rt.roles("PUT /api/users/{id}/billing-profile", billingHandler.UpdateClinicianProfile, billingAdminRoles...)

	// Claims
	rt.authed("GET /api/claims", billingHandler.ListClaims)
	rt.roles("POST /api/claims", billingHandler.CreateClaim, billerRoles...)
	rt.authed("GET /api/claims/{id}", billingHandler.GetClaim)
	rt.roles("PUT /api/claims/{id}", billingHandler.UpdateClaim, billerRoles...)
	rt.roles("POST /api/claims/{id}/validate", billingHandler.ValidateClaim, billerRoles...)
	rt.roles("POST /api/claims/{id}/comments", billingHandler.AddClaimComment, billerRoles...)
	rt.roles("POST /api/claims/{id}/cancel", billingHandler.CancelClaim, billerRoles...)
	rt.authed("GET /api/patients/{id}/claimable-charges", billingHandler.ListClaimableCharges)

	// CMS-1500 / superbills
	rt.roles("POST /api/claims/{id}/cms1500", billingHandler.GenerateCMS1500, billerRoles...)
	rt.authed("GET /api/claims/{id}/cms1500", billingHandler.DownloadCMS1500)
	rt.authed("GET /api/patients/{id}/superbill-charges", billingHandler.ListSuperbillCharges)
	rt.authed("GET /api/patients/{id}/superbills", billingHandler.ListSuperbills)
	rt.roles("POST /api/patients/{id}/superbills", billingHandler.CreateSuperbill, billerRoles...)
	rt.authed("GET /api/superbills/{id}/pdf", billingHandler.DownloadSuperbill)

	// Claim submission workflows / claim history
	for path, handler := range map[string]http.HandlerFunc{
		"/mark-mailed":             billingHandler.MarkMailed,
		"/mark-external":           billingHandler.MarkSubmittedExternally,
		"/submit-electronic":       billingHandler.SubmitElectronic,
		"/record-rejection":        billingHandler.RecordRejection,
		"/mark-rejection-reviewed": billingHandler.MarkRejectionReviewed,
		"/start-resubmission":      billingHandler.StartResubmission,
	} {
		rt.roles("POST /api/claims/{id}"+path, handler, billerRoles...)
	}

	rt.authed("GET /api/claims/{id}/electronic-payload", billingHandler.ElectronicPayload)
	rt.authed("GET /api/claim-history", billingHandler.ListClaimHistory)
	rt.authed("GET /api/billing/integrations", billingHandler.GetIntegrations)

	// Patient payments
	rt.authed("GET /api/patients/{id}/payments", billingHandler.ListPatientPayments)
	rt.roles("POST /api/patients/{id}/payments", billingHandler.CreatePatientPayment, billerRoles...)
	rt.authed("GET /api/patient-payments", billingHandler.SearchPatientPayments)
	rt.authed("GET /api/patient-payments/{id}", billingHandler.GetPatientPayment)

	for path, handler := range map[string]http.HandlerFunc{
		"/allocations": billingHandler.AllocatePatientPayment,
		"/void":        billingHandler.VoidPatientPayment,
		"/refunds":     billingHandler.RefundPatientPayment,
	} {
		rt.roles("POST /api/patient-payments/{id}"+path, handler, billerRoles...)
	}

	rt.roles("POST /api/patient-payment-allocations/{id}/void", billingHandler.VoidPatientPaymentAllocation, billerRoles...)

	// Insurance payments (manually posted remittances) and adjustments
	rt.authed("GET /api/insurance-payments", billingHandler.SearchInsurancePayments)
	rt.roles("POST /api/insurance-payments", billingHandler.CreateInsurancePayment, billerRoles...)
	rt.authed("GET /api/insurance-payments/{id}", billingHandler.GetInsurancePayment)
	rt.roles("POST /api/insurance-payments/{id}/allocations", billingHandler.AddInsurancePaymentLines, billerRoles...)
	rt.roles("POST /api/insurance-payments/{id}/void", billingHandler.VoidInsurancePayment, billerRoles...)
	rt.authed("GET /api/billing/outstanding-insurance", billingHandler.SearchOutstandingInsurance)

	// Billing dashboard / reports / export
	rt.authed("GET /api/billing/dashboard", billingHandler.GetBillingDashboard)
	rt.authed("GET /api/billing/transactions/export", billingHandler.ExportTransactionsCSV)
	rt.authed("GET /api/billing/reports/insurance-aging", billingHandler.GetInsuranceAging)
	rt.authed("GET /api/billing/reports/patient-aging", billingHandler.GetPatientAging)
	rt.authed("GET /api/billing/reports/collections", billingHandler.GetCollectionsReport)

	// Patient statements (immutable snapshots + PDFs)
	rt.authed("GET /api/patients/{id}/statements", billingHandler.ListPatientStatements)
	rt.roles("POST /api/patients/{id}/statements", billingHandler.CreatePatientStatement, billerRoles...)
	rt.authed("GET /api/patients/{patientId}/statements/{id}", billingHandler.GetStatement)
	rt.authed("GET /api/patients/{patientId}/statements/{id}/pdf", billingHandler.DownloadStatementPDF)
	rt.authed("GET /api/statements/{id}", billingHandler.GetStatement)
	rt.authed("GET /api/statements/{id}/pdf", billingHandler.DownloadStatementPDF)
	rt.authed("GET /api/billing/statements", billingHandler.SearchStatements)
	rt.authed("GET /api/billing/statement-candidates", billingHandler.GetStatementCandidates)
	rt.roles("POST /api/billing/statements/batch", billingHandler.CreateStatementBatch, billerRoles...)
	rt.authed("GET /api/billing/statements/combined-pdf", billingHandler.DownloadCombinedStatementsPDF)

	// Balance engine (read-only views over the ledger)
	rt.authed("GET /api/patients/{id}/billing-summary", billingHandler.GetPatientBillingSummary)
	rt.authed("GET /api/patients/{id}/ledger", billingHandler.GetPatientLedger)
	rt.authed("GET /api/charges/{id}/ledger", billingHandler.GetChargeLedger)

	rt.roles("POST /api/charges/{id}/adjustments", billingHandler.CreateAdjustment, billerRoles...)
	rt.roles("POST /api/charges/{id}/transfers", billingHandler.CreateTransfer, billerRoles...)
	rt.roles("POST /api/billing-adjustments/{id}/void", billingHandler.VoidAdjustment, billerRoles...)
	rt.roles("POST /api/responsibility-transfers/{id}/void", billingHandler.VoidTransfer, billerRoles...)

	return rt.mux, rt
}
