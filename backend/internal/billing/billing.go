package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
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

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

// isUUID lets handlers answer 404 for malformed IDs instead of passing
// them to PostgreSQL and surfacing a cast error as a 500.
func isUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func (h *Handler) patientExists(ctx context.Context, patientID string) (bool, error) {
	var exists bool

	err := h.db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`,
		patientID,
	).Scan(&exists)

	return exists, err
}
