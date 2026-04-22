package testutil

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/database"
)

const defaultTestDatabaseURL = "postgres://anpm:anpm@localhost:5432/anpm?sslmode=disable"

func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()

	baseDSN := os.Getenv("TEST_DATABASE_URL")
	if baseDSN == "" {
		baseDSN = os.Getenv("DATABASE_URL")
	}
	if baseDSN == "" {
		baseDSN = defaultTestDatabaseURL
	}

	var (
		adminDB *sql.DB
		err     error
	)
	for attempt := 1; attempt <= 20; attempt++ {
		adminDB, err = database.Open(baseDSN)
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Skipf("PostgreSQL test database unavailable (%v). Set TEST_DATABASE_URL or start a reachable PostgreSQL instance.", err)
	}

	schemaName := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create schema %s: %v", schemaName, err)
	}

	testDSN := withSearchPath(baseDSN, schemaName)
	db, err := database.Open(testDSN)
	if err != nil {
		_, _ = adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
		_ = adminDB.Close()
		t.Fatalf("open test db: %v", err)
	}

	if err := database.RunMigrations(db, false); err != nil {
		_ = db.Close()
		_, _ = adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
		_ = adminDB.Close()
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		_, _ = adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
		_ = adminDB.Close()
	})

	return db
}

func withSearchPath(baseDSN, schemaName string) string {
	parsed, err := url.Parse(baseDSN)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schemaName)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	return baseDSN + separator + "search_path=" + url.QueryEscape(schemaName)
}