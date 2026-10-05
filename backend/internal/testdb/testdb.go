// Package testdb gives DB-backed tests an isolated, freshly migrated schema.
//
// Tests never touch the developer's data: every connection returned by Open
// has search_path pinned to a dedicated "ehr_test_<name>" schema (the public
// schema is not on the path), so unqualified table names can only resolve
// inside that schema. The schema is dropped and rebuilt from
// backend/migrations each time Open is called.
//
// The database is taken from TEST_DATABASE_URL, then DATABASE_URL, then
// backend/.env. When none is reachable Open returns an error and callers
// skip their DB tests.
package testdb

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var schemaPattern = regexp.MustCompile(`^ehr_test_[a-z0-9_]{1,40}$`)

// backendDir finds the backend module root (the directory holding migrations/).
func backendDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "migrations")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("backend directory not found")
		}

		dir = parent
	}
}

func databaseURL(backend string) string {
	for _, key := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}

	f, err := os.Open(filepath.Join(backend, ".env"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if v, ok := strings.CutPrefix(line, "TEST_DATABASE_URL="); ok {
			return strings.Trim(v, `"'`)
		}
		if v, ok := strings.CutPrefix(line, "DATABASE_URL="); ok {
			return strings.Trim(v, `"'`)
		}
	}

	return ""
}

// Open drops and recreates schema ehr_test_<name>, applies every migration
// to it and returns a pool pinned to that schema.
func Open(name string) (*pgxpool.Pool, error) {
	schema := "ehr_test_" + name
	if !schemaPattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid test schema name %q", schema)
	}

	backend, err := backendDir()
	if err != nil {
		return nil, err
	}

	url := databaseURL(backend)
	if url == "" {
		return nil, errors.New("no TEST_DATABASE_URL / DATABASE_URL configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
	} {
		if _, err := admin.Exec(ctx, stmt); err != nil {
			admin.Close(ctx)
			return nil, fmt.Errorf("prepare schema: %w", err)
		}
	}
	admin.Close(ctx)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 20

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(backend, "migrations", "*.sql"))
	if err != nil {
		pool.Close()
		return nil, err
	}
	sort.Strings(files)

	for _, file := range files {
		sqlText, err := os.ReadFile(file)
		if err != nil {
			pool.Close()
			return nil, err
		}

		// Migrations guard some constraints with "pg_constraint WHERE conname
		// = ...", which looks across every schema in the database. Scope
		// those checks to the test schema so they behave as on a fresh DB.
		script := strings.ReplaceAll(
			string(sqlText),
			"FROM pg_constraint WHERE conname =",
			"FROM pg_constraint WHERE connamespace = current_schema()::regnamespace AND conname =",
		)

		if _, err := pool.Exec(ctx, script); err != nil {
			pool.Close()
			return nil, fmt.Errorf("apply %s: %w", filepath.Base(file), err)
		}
	}

	// Safety: the pool must resolve tables only inside the test schema.
	var current string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil || current != schema {
		pool.Close()
		return nil, fmt.Errorf("test pool is not pinned to %s (got %q)", schema, current)
	}

	return pool, nil
}
