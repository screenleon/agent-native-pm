// Package testutil provides test database helpers that dispatch between
// PostgreSQL (server-mode) and SQLite (local-mode) based on the
// TEST_DATABASE_URL environment variable.
//
// Dispatch rules:
//
//   - TEST_DATABASE_URL="sqlite" (sentinel) or any "sqlite://..." DSN →
//     per-test disposable SQLite file under t.TempDir(). The DSN path itself
//     is ignored for test isolation: every test gets its own file regardless
//     of the URL, so accidental TEST_DATABASE_URL="sqlite:///shared.db" does
//     NOT create cross-test contamination. Use the "sqlite" sentinel in CI
//     and scripts.
//   - TEST_DATABASE_URL unset or any other value → isolated schema on a
//     reachable PostgreSQL instance (temp-container friendly).
//
// All tests should call testutil.OpenTestDB(t) and let this package pick the
// driver. The 2026-04-22 Dual-runtime-mode decision requires parity: new
// migrations, SQL, and store logic MUST pass against both drivers.
package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/database"
)

// TestDialect returns the Dialect that matches the test database driver
// selected by OpenTestDB. Tests that construct stores MUST pass this instead
// of hardcoding a DSN — otherwise dialect-aware SQL branches (FOR UPDATE,
// CURRENT_TIMESTAMP vs NOW(), interval arithmetic) will be wrong under
// SQLite and hide real regressions.
func TestDialect() database.Dialect {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if database.IsSQLiteDSN(dsn) || dsn == "sqlite" {
		return database.NewDialect("sqlite://placeholder")
	}
	return database.NewDialect("postgres://placeholder")
}

// OpenTestDB returns a freshly migrated test database. It dispatches on
// TEST_DATABASE_URL (falling back to DATABASE_URL):
//
//   - "sqlite" sentinel or any "sqlite://..." DSN → disposable SQLite file at
//     t.TempDir()/test.db. The DSN path is intentionally not honoured so
//     every test gets an isolated database regardless of environment shape.
//   - any other value (or empty) → isolated PostgreSQL schema via
//     openTestPostgresDB.
//
// The returned *sql.DB is closed automatically via t.Cleanup.
func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}

	if database.IsSQLiteDSN(dsn) || dsn == "sqlite" {
		return openTestSQLiteDB(t)
	}
	return openTestPostgresDB(t)
}

// openTestSQLiteDB creates a fresh SQLite file under t.TempDir(), opens it,
// runs migrations, and registers cleanup. Each test gets its own DB so there
// is no schema-isolation dance.
func openTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	dsn := "sqlite://" + dbPath

	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}

	if err := database.RunMigrations(db, true); err != nil {
		_ = db.Close()
		t.Fatalf("run sqlite migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
