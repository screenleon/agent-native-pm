package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
	"github.com/screenleon/agent-native-pm/db/migrations"
)

// Dialect carries per-connection database variant information and exposes
// the SQL snippets that differ between PostgreSQL and SQLite. Pass one to
// every store that needs to branch its queries.
type Dialect struct {
	sqlite bool
}

// NewDialect returns a Dialect based on the DSN.
func NewDialect(dsn string) Dialect {
	return Dialect{sqlite: IsSQLiteDSN(dsn)}
}

// IsSQLite reports whether this dialect targets SQLite.
func (d Dialect) IsSQLite() bool { return d.sqlite }

// Now returns the SQL expression for the current timestamp.
func (d Dialect) Now() string {
	if d.sqlite {
		return "CURRENT_TIMESTAMP"
	}
	return "NOW()"
}

// SkipLocked returns the "SKIP LOCKED" suffix for SELECT FOR UPDATE, or an
// empty string on SQLite (which serialises writes at the engine level).
func (d Dialect) SkipLocked() string {
	if d.sqlite {
		return ""
	}
	return " SKIP LOCKED"
}

// ForUpdate returns the "FOR UPDATE" clause for row-level locking, or an
// empty string on SQLite. SQLite serialises writes via its single-writer
// model, so explicit row locks are both unsupported syntax and unnecessary.
//
// Callers typically use it as: query + " " + dialect.ForUpdate()
func (d Dialect) ForUpdate() string {
	if d.sqlite {
		return ""
	}
	return "FOR UPDATE"
}

// IsUniqueViolation reports whether err is a unique-constraint failure. The
// detection is driver-specific: PostgreSQL returns pq.Error with SQLState
// 23505, modernc.org/sqlite surfaces a text-formatted constraint error that
// starts with "constraint failed: UNIQUE constraint failed:". Callers that
// need constraint-name granularity should still inspect pq.Error.Constraint
// on the Postgres path and fall back to column-name substring matching on
// the SQLite path.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	// Postgres surface forms vary depending on whether the caller wraps the
	// pq error or stringifies it directly. Match on every encoding of the
	// 23505 SQLSTATE we have observed in the wild:
	//   - "ERROR ... (SQLSTATE 23505)" — github.com/lib/pq formatted error
	//   - "pq: duplicate key value violates unique constraint ... (23505)"
	//     — github.com/lib/pq Error.String()
	//   - bare "SQLSTATE 23505" inside a wrapped fmt.Errorf chain
	if strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "(23505)") {
		return true
	}
	return strings.Contains(msg, "duplicate key value violates unique constraint")
}

// IntervalDays returns a SQL expression subtracting N days from a timestamp.
//
//	pg:     col > NOW() - INTERVAL '7 day'
//	sqlite: col > datetime('now', '-7 days')
func (d Dialect) IntervalDays(col string, days int) string {
	if d.sqlite {
		return fmt.Sprintf("%s > datetime('now', '-%d days')", col, days)
	}
	return fmt.Sprintf("%s > NOW() - INTERVAL '%d day'", col, days)
}

// IntervalSeconds returns a SQL expression subtracting N seconds held in a
// placeholder parameter from a timestamp (SQLite vs Postgres syntax differ).
//
// placeholder is the $N positional param for the seconds value.
func (d Dialect) IntervalSeconds(col string, placeholder string) string {
	if d.sqlite {
		return fmt.Sprintf("%s > datetime('now', '-' || %s || ' seconds')", col, placeholder)
	}
	return fmt.Sprintf("%s > NOW() - (%s || ' seconds')::interval", col, placeholder)
}

// IsSQLiteDSN reports whether the DSN targets a SQLite database.
func IsSQLiteDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "sqlite://")
}

// Open opens and configures a database connection. For SQLite the connection
// is tuned for single-writer concurrent use (WAL, busy_timeout, pool cap).
func Open(dsn string) (*sql.DB, error) {
	driver := "postgres"
	connStr := dsn
	if IsSQLiteDSN(dsn) {
		driver = "sqlite"
		connStr = strings.TrimPrefix(dsn, "sqlite://")
	}

	db, err := sql.Open(driver, connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if IsSQLiteDSN(dsn) {
		configureSQLite(db)
	}

	return db, nil
}

// configureSQLite applies performance and safety pragmas and constrains the
// connection pool so SQLite's single-writer model is respected.
func configureSQLite(db *sql.DB) {
	// WAL allows concurrent readers while a writer holds the lock.
	// synchronous=NORMAL is safe with WAL and much faster than FULL.
	// busy_timeout prevents immediate SQLITE_BUSY errors under light contention.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			slog.Warn("sqlite pragma failed", "pragma", pragma, "err", err)
		}
	}
	// SQLite allows only one writer at a time; a pool > 1 causes redundant
	// contention. Set max open connections to 1 for write safety.
	db.SetMaxOpenConns(1)
}

func RunMigrations(db *sql.DB, isSQLite bool) error {
	return runMigrationsFromFS(db, migrations.Files, isSQLite)
}

func runMigrationsFromFS(db *sql.DB, migrationsFS fs.FS, isSQLite bool) error {
	schemaDDL := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`
	if isSQLite {
		schemaDDL = rewriteForSQLite(schemaDDL)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Skip dev-only rollback companions (e.g. 021_*.down.sql). The
		// design §13 rollback story keeps these as sibling files for hand
		// invocation; the forward-only migration runner must never apply
		// them automatically. Without this filter the runner would treat
		// "021_*.down.sql" as a brand-new migration version and execute
		// the DROP statements right after the forward migration ran.
		if strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	for _, f := range files {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", f).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", f, err)
		}
		if count > 0 {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}

		sqlStr := string(content)
		if isSQLite {
			sqlStr = rewriteForSQLite(sqlStr)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin transaction for %s: %w", f, err)
		}

		var execErr error
		if isSQLite {
			execErr = execMulti(tx, sqlStr)
		} else {
			_, execErr = tx.Exec(sqlStr)
		}
		if execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", f, execErr)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", f); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", f, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", f, err)
		}

		slog.Info("applied migration", "file", f)
	}

	return nil
}

// pgCastRe matches PostgreSQL cast suffix like ::jsonb, ::text[], ::integer.
var pgCastRe = regexp.MustCompile(`::[a-zA-Z_][\w\[\]]*`)

// rewriteForSQLite translates PostgreSQL-specific DDL/DML tokens to their
// SQLite equivalents. Applied to migration files only.
func rewriteForSQLite(sql string) string {
	// DATETIME is recognised by modernc.org/sqlite's row scanner and causes it
	// to automatically parse text timestamps into time.Time on read.
	// TEXT would return raw strings that database/sql cannot scan into *time.Time.
	sql = strings.ReplaceAll(sql, "TIMESTAMPTZ", "DATETIME")
	// CURRENT_TIMESTAMP is standard SQL; NOW() is PostgreSQL-only.
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT_TIMESTAMP")
	// JSONB is PostgreSQL-only; TEXT with json.Marshal/Unmarshal works fine.
	sql = strings.ReplaceAll(sql, "JSONB", "TEXT")
	sql = strings.ReplaceAll(sql, "jsonb", "TEXT")
	// Strip PostgreSQL cast syntax ( ::typename )
	sql = pgCastRe.ReplaceAllString(sql, "")
	// SQLite ALTER TABLE ADD COLUMN does not support IF NOT EXISTS.
	sql = strings.ReplaceAll(sql, "ADD COLUMN IF NOT EXISTS", "ADD COLUMN")
	return sql
}

// execMulti executes semicolon-separated SQL statements one by one.
// Required for SQLite: sqlite3_prepare only parses the first statement in a
// multi-statement string.
// PostgreSQL-only constructs (GIN indexes, COMMENT ON) are silently skipped.
// Multi-ADD-COLUMN ALTER TABLE statements are split before execution.
func execMulti(tx *sql.Tx, sqlContent string) error {
	for _, raw := range strings.Split(sqlContent, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		upper := strings.ToUpper(raw)
		if strings.Contains(upper, "USING GIN") {
			continue // full-text GIN indexes are PostgreSQL-only
		}
		if strings.HasPrefix(upper, "COMMENT ON") {
			continue // DDL comments are PostgreSQL-only
		}

		for _, stmt := range splitAlterTableAddColumns(raw) {
			if _, err := tx.Exec(stmt); err != nil {
				preview := stmt
				if len(preview) > 120 {
					preview = preview[:120] + "..."
				}
				return fmt.Errorf("%w\n  statement: %s", err, preview)
			}
		}
	}
	return nil
}

// splitAlterTableAddColumns splits a multi-ADD-COLUMN ALTER TABLE statement
// into individual statements. SQLite only allows one ADD COLUMN per statement.
//
// Example:
//
//	ALTER TABLE t ADD COLUMN a INT, ADD COLUMN b TEXT
//
// becomes ["ALTER TABLE t ADD COLUMN a INT", "ALTER TABLE t ADD COLUMN b TEXT"].
func splitAlterTableAddColumns(stmt string) []string {
	upper := strings.ToUpper(stmt)
	if !strings.Contains(upper, "ALTER TABLE") || strings.Count(upper, "ADD COLUMN") <= 1 {
		return []string{stmt}
	}

	tableIdx := strings.Index(upper, "ALTER TABLE")
	rest := strings.TrimSpace(stmt[tableIdx+len("ALTER TABLE"):])
	nameEnd := strings.IndexAny(rest, " \t\n\r")
	if nameEnd < 0 {
		return []string{stmt}
	}
	tableName := rest[:nameEnd]
	body := strings.TrimSpace(rest[nameEnd:])
	bodyUpper := strings.ToUpper(body)

	prefix := "ALTER TABLE " + tableName

	var clauses []string
	start := 0
	for i := 0; i < len(body); i++ {
		if body[i] != ',' {
			continue
		}
		j := i + 1
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r') {
			j++
		}
		if strings.HasPrefix(bodyUpper[j:], "ADD COLUMN") {
			if clause := strings.TrimSpace(body[start:i]); clause != "" {
				clauses = append(clauses, prefix+" "+clause)
			}
			start = j
		}
	}
	if clause := strings.TrimSpace(body[start:]); clause != "" {
		clauses = append(clauses, prefix+" "+clause)
	}

	if len(clauses) <= 1 {
		return []string{stmt}
	}
	return clauses
}

// AppliedMigrationCount returns the number of migrations that have been applied.
// Used by the health endpoint.
func AppliedMigrationCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	return count, err
}
