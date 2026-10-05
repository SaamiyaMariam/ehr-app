package billing

import (
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"ehr-backend/internal/testdb"
)

// testPool is a pool pinned to an isolated, freshly migrated schema, or nil
// when no database is reachable (DB-backed tests then skip).
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	pool, err := testdb.Open("billing")
	if err != nil {
		fmt.Fprintln(os.Stderr, "billing: DB-backed tests will be skipped:", err)
	} else {
		testPool = pool
	}

	code := m.Run()

	if testPool != nil {
		testPool.Close()
	}

	os.Exit(code)
}

func needDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testPool == nil {
		t.Skip("no test database available")
	}

	return testPool
}
