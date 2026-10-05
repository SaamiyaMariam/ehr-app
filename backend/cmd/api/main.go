package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"ehr-backend/internal/auth"
	"ehr-backend/internal/billing"
	"ehr-backend/internal/database"
	"ehr-backend/internal/patients"
	"ehr-backend/internal/payers"
	"ehr-backend/internal/roles"
	"ehr-backend/internal/servicecodes"
	"ehr-backend/internal/users"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	ctx := context.Background()

	db, err := database.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	log.Println("Connected to PostgreSQL")

	authHandler := auth.NewHandler(db, jwtSecret)
	patientHandler := patients.NewHandler(db)
	userHandler := users.NewHandler(db)
	roleHandler := roles.NewHandler(db)
	payerHandler := payers.NewHandler(db)
	billingHandler := billing.NewHandler(db)
	serviceCodeHandler := servicecodes.NewHandler(db)

	serviceCodeManagerRoles := []string{
		"practice_administrator",
		"practice_biller",
		"clinical_administrator",
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)

			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":   "error",
				"database": "unavailable",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"database": "connected",
		})
	})

	// Auth
	mux.HandleFunc("POST /api/auth/signup", authHandler.Signup)
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc(
		"GET /api/auth/me",
		authHandler.RequireAuth(authHandler.Me),
	)

	// Patients
	mux.HandleFunc(
		"GET /api/patients",
		authHandler.RequireAuth(patientHandler.List),
	)
	mux.HandleFunc(
		"POST /api/patients",
		authHandler.RequireAnyRole(
			patientHandler.Create,
			"clinician",
			"practice_scheduler",
		),
	)
	mux.HandleFunc(
		"GET /api/patients/{id}",
		authHandler.RequireAuth(patientHandler.Get),
	)
	mux.HandleFunc(
		"PUT /api/patients/{id}",
		authHandler.RequireAuth(patientHandler.Update),
	)

	// Users / Employees
	mux.HandleFunc(
		"GET /api/users",
		authHandler.RequireAuth(userHandler.List),
	)
	mux.HandleFunc(
		"POST /api/users",
		authHandler.RequireAuth(userHandler.Create),
	)
	mux.HandleFunc(
		"GET /api/users/{id}",
		authHandler.RequireAuth(userHandler.Get),
	)
	mux.HandleFunc(
		"PUT /api/users/{id}",
		authHandler.RequireAuth(userHandler.Update),
	)

	// Roles
	mux.HandleFunc(
		"GET /api/roles",
		authHandler.RequireAuth(roleHandler.List),
	)

	mux.HandleFunc(
		"GET /api/users/{id}/roles",
		authHandler.RequireAuth(roleHandler.GetUserRoles),
	)

	mux.HandleFunc(
		"PUT /api/users/{id}/roles",
		authHandler.RequireAuth(roleHandler.UpdateUserRoles),
	)

	// Payers
	mux.HandleFunc(
		"GET /api/payers",
		authHandler.RequireAuth(payerHandler.List),
	)

	mux.HandleFunc(
		"POST /api/payers",
		authHandler.RequireAnyRole(
			payerHandler.Create,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"GET /api/payers/{id}",
		authHandler.RequireAuth(payerHandler.Get),
	)

	mux.HandleFunc(
		"PUT /api/payers/{id}",
		authHandler.RequireAnyRole(
			payerHandler.Update,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"PATCH /api/payers/{id}/status",
		authHandler.RequireAnyRole(
			payerHandler.SetActive,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"GET /api/clinicians",
		authHandler.RequireAuth(userHandler.ListClinicians),
	)

	// Patient Billing
	mux.HandleFunc(
		"GET /api/patients/{id}/billing-settings",
		authHandler.RequireAuth(billingHandler.GetSettings),
	)

	mux.HandleFunc(
		"PUT /api/patients/{id}/billing-settings",
		authHandler.RequireAnyRole(
			billingHandler.UpdateSettings,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"GET /api/patients/{id}/insurance-policies",
		authHandler.RequireAuth(billingHandler.ListPolicies),
	)

	mux.HandleFunc(
		"POST /api/patients/{id}/insurance-policies",
		authHandler.RequireAnyRole(
			billingHandler.CreatePolicy,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"GET /api/insurance-policies/{id}",
		authHandler.RequireAuth(billingHandler.GetPolicy),
	)

	mux.HandleFunc(
		"PUT /api/insurance-policies/{id}",
		authHandler.RequireAnyRole(
			billingHandler.UpdatePolicy,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"PATCH /api/insurance-policies/{id}/status",
		authHandler.RequireAnyRole(
			billingHandler.SetPolicyActive,
			"practice_biller",
		),
	)

	// Prior Authorizations
	mux.HandleFunc(
		"GET /api/insurance-policies/{id}/prior-authorizations",
		authHandler.RequireAuth(billingHandler.ListPriorAuthorizations),
	)

	mux.HandleFunc(
		"POST /api/insurance-policies/{id}/prior-authorizations",
		authHandler.RequireAnyRole(
			billingHandler.CreatePriorAuthorization,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"GET /api/prior-authorizations/{id}",
		authHandler.RequireAuth(billingHandler.GetPriorAuthorization),
	)

	mux.HandleFunc(
		"PUT /api/prior-authorizations/{id}",
		authHandler.RequireAnyRole(
			billingHandler.UpdatePriorAuthorization,
			"practice_biller",
		),
	)

	mux.HandleFunc(
		"PATCH /api/prior-authorizations/{id}/status",
		authHandler.RequireAnyRole(
			billingHandler.SetPriorAuthorizationActive,
			"practice_biller",
		),
	)

	// Service Codes
	mux.HandleFunc(
		"GET /api/service-codes",
		authHandler.RequireAuth(serviceCodeHandler.List),
	)

	mux.HandleFunc(
		"POST /api/service-codes",
		authHandler.RequireAnyRole(
			serviceCodeHandler.Create,
			serviceCodeManagerRoles...,
		),
	)

	mux.HandleFunc(
		"GET /api/service-codes/{id}",
		authHandler.RequireAuth(serviceCodeHandler.Get),
	)

	mux.HandleFunc(
		"PUT /api/service-codes/{id}",
		authHandler.RequireAnyRole(
			serviceCodeHandler.Update,
			serviceCodeManagerRoles...,
		),
	)

	mux.HandleFunc(
		"PATCH /api/service-codes/{id}/status",
		authHandler.RequireAnyRole(
			serviceCodeHandler.SetActive,
			serviceCodeManagerRoles...,
		),
	)

	log.Println("EHR API running on http://localhost:8080")

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
