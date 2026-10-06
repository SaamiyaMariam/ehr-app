package billing

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

// =========================================================
// CLEARINGHOUSE / ERA / CARD BOUNDARIES
// =========================================================
//
// This application does not ship a real EDI clearinghouse, ERA retrieval or
// card-processing integration. These interfaces define where one plugs in.
// With nothing configured, electronic submission returns
// CLEARINGHOUSE_NOT_CONFIGURED; it never pretends a claim was sent.

// ClearinghouseProvider is implemented by a real clearinghouse adapter.
type ClearinghouseProvider interface {
	Name() string
	// Validate returns provider-specific problems with the payload.
	Validate(payload ClearinghousePayload) []string
	// Submit transmits the claim. It must only report Accepted when the
	// clearinghouse has actually acknowledged the submission.
	Submit(ctx context.Context, payload ClearinghousePayload) (ClearinghouseResult, error)
	// QueryStatus fetches the latest payer / clearinghouse status.
	QueryStatus(ctx context.Context, externalID string) (ClearinghouseStatus, error)
}

type ClearinghouseResult struct {
	Accepted   bool   // acknowledged for submission
	Queued     bool   // accepted for later transmission
	ExternalID string // clearinghouse tracking id
	Message    string
}

type ClearinghouseStatus struct {
	Status  string // sent | rejected | paid
	Message string
}

// ERASource is where an electronic remittance (835) importer would plug in.
// No implementation is provided: insurance payments are posted manually.
type ERASource interface {
	Name() string
	FetchRemittances(ctx context.Context) ([]byte, error)
}

const ClearinghouseNotConfigured = "CLEARINGHOUSE_NOT_CONFIGURED"

var errClearinghouseNotConfigured = errors.New(ClearinghouseNotConfigured)

// clearinghouseFromEnv returns the configured provider. No providers are
// bundled, so any configured name is reported and ignored.
func clearinghouseFromEnv() ClearinghouseProvider {
	name := strings.TrimSpace(os.Getenv("CLEARINGHOUSE_PROVIDER"))

	if name != "" {
		log.Printf("CLEARINGHOUSE_PROVIDER=%q is not a bundled provider; electronic submission stays disabled", name)
	}

	return nil
}

// ClearinghousePayload is a structured, provider-neutral representation of
// a claim, built only from the claim snapshot. It is NOT an X12 837 file; a
// provider adapter is responsible for producing the wire format.
type ClearinghousePayload struct {
	Format             string                   `json:"format"`
	ClaimNumber        string                   `json:"claim_number"`
	FrequencyCode      string                   `json:"frequency_code"`
	OriginalRef        string                   `json:"payer_claim_control_number"`
	Sequence           string                   `json:"sequence"`
	BillingProvider    SnapshotPractice         `json:"billing_provider"`
	Subscriber         SnapshotInsured          `json:"subscriber"`
	Patient            SnapshotPerson           `json:"patient"`
	Payer              SnapshotPayer            `json:"payer"`
	OtherInsurance     []SnapshotOtherInsurance `json:"other_insurance"`
	Diagnoses          []string                 `json:"diagnoses"`
	PriorAuthorization string                   `json:"prior_authorization"`
	TotalCharge        string                   `json:"total_charge"`
	ServiceLines       []ClearinghouseLine      `json:"service_lines"`
}

type ClearinghouseLine struct {
	LineNumber        int      `json:"line_number"`
	DateOfService     string   `json:"date_of_service"`
	ProcedureCode     string   `json:"procedure_code"`
	Modifiers         []string `json:"modifiers"`
	Units             int      `json:"units"`
	Charge            string   `json:"charge"`
	PlaceOfService    string   `json:"place_of_service"`
	DiagnosisPointers string   `json:"diagnosis_pointers"`
	RenderingNPI      string   `json:"rendering_npi"`
	RenderingName     string   `json:"rendering_name"`
}

func buildClearinghousePayload(c Claim) ClearinghousePayload {
	p := ClearinghousePayload{
		Format:          "ehr-claim-json/v1 (not X12)",
		ClaimNumber:     c.ClaimNumber,
		FrequencyCode:   frequencyCodeFor(c.ResubmissionType),
		OriginalRef:     c.PayerClaimControlNumber,
		Sequence:        c.Sequence,
		BillingProvider: c.Snapshot.Practice,
		Subscriber:      c.Snapshot.Insured,
		Patient:         c.Snapshot.Patient,
		Payer:           c.Snapshot.Payer,
		OtherInsurance:  c.Snapshot.OtherInsurance,
		TotalCharge:     c.TotalBilled,
		Diagnoses:       []string{},
		ServiceLines:    []ClearinghouseLine{},
	}

	if p.OtherInsurance == nil {
		p.OtherInsurance = []SnapshotOtherInsurance{}
	}

	for _, d := range c.Diagnoses {
		p.Diagnoses = append(p.Diagnoses, d.ICD10Code)
	}

	for _, l := range c.Lines {
		if !l.IsCurrent {
			continue
		}

		if p.PriorAuthorization == "" {
			p.PriorAuthorization = l.PriorAuthorizationCode
		}

		p.ServiceLines = append(p.ServiceLines, ClearinghouseLine{
			LineNumber: l.LineNumber, DateOfService: l.DateOfService, ProcedureCode: l.ServiceCode,
			Modifiers: l.Modifiers, Units: l.Units, Charge: l.LineTotal, PlaceOfService: l.PlaceOfService,
			DiagnosisPointers: l.DiagnosisPointers, RenderingNPI: l.RenderingNPI, RenderingName: l.RenderingName,
		})
	}

	return p
}

// GetIntegrations reports which external integrations are configured, so
// the UI can explain unavailable actions honestly.
func (h *Handler) GetIntegrations(w http.ResponseWriter, r *http.Request) {
	clearinghouse := map[string]any{"configured": false, "name": "", "code": ClearinghouseNotConfigured}
	if h.clearinghouse != nil {
		clearinghouse = map[string]any{"configured": true, "name": h.clearinghouse.Name()}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"clearinghouse": clearinghouse,
		"era":           map[string]any{"configured": false, "note": "Insurance payments are posted manually; no ERA source is configured."},
		"card_processor": map[string]any{
			"configured": false,
			"note":       "Card payments processed outside this application can be recorded as external payments. Card data is never stored.",
		},
	})
}
