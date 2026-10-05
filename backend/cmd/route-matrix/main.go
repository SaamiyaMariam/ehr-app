// Command route-matrix prints the API's route/security matrix as Markdown,
// straight from the router's own registry (so it cannot drift from the code).
//
//	cd backend
//	go run ./cmd/route-matrix > ../docs/ROUTE_SECURITY_MATRIX.md
package main

import (
	"fmt"

	"ehr-backend/internal/server"
)

func main() {
	// Handlers are only registered, never called, so no database is needed.
	_, router := server.New(nil, "route-matrix")

	fmt.Print(router.Markdown())
}
