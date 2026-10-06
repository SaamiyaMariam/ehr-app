// Package integration holds DB-backed API tests. They drive the real router
// (JWT auth, role checks, handlers) against an isolated, freshly migrated
// schema created by internal/testdb; they never touch the developer's data.
package integration
