package billing

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"ehr-backend/internal/auth"
)

// Money is handled as int64 cents in Go and NUMERIC(…, 2) in PostgreSQL.
// Floating point is never used for billing amounts.

var amountPattern = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)

// parseMoney parses a non-negative decimal string ("12", "12.3", "12.30")
// into cents.
func parseMoney(value string) (int64, bool) {
	value = strings.TrimSpace(value)

	if !amountPattern.MatchString(value) {
		return 0, false
	}

	whole, fraction, _ := strings.Cut(value, ".")

	for len(fraction) < 2 {
		fraction += "0"
	}

	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}

	f, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, false
	}

	return w*100 + f, true
}

// parseSignedMoney parses database NUMERIC text, which may be negative.
func parseSignedMoney(value string) (int64, bool) {
	value = strings.TrimSpace(value)

	if strings.HasPrefix(value, "-") {
		cents, ok := parseMoney(value[1:])
		return -cents, ok
	}

	return parseMoney(value)
}

func formatMoney(cents int64) string {
	sign := ""

	if cents < 0 {
		sign = "-"
		cents = -cents
	}

	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// mustCents converts trusted NUMERIC text from the database into cents.
func mustCents(value string) int64 {
	cents, ok := parseSignedMoney(value)
	if !ok {
		panic("invalid numeric from database: " + value)
	}

	return cents
}

// queryRower is satisfied by *pgxpool.Pool and pgx.Tx.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func currentUserID(r *http.Request) string {
	return auth.UserIDFromContext(r.Context())
}

// pageParams reads page / page_size query parameters with sane bounds.
func pageParams(r *http.Request) (page int, pageSize int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))

	if page < 1 {
		page = 1
	}

	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	return page, pageSize
}

type pagedResult[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// validEnum reports whether value is one of the allowed values.
func validEnum(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}

	return false
}
