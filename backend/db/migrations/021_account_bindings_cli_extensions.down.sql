-- Down migration for 021 (development-only rollback per design §13).
-- Production never runs down migrations; this file exists so dev/test can
-- exercise the rollback path under both SQLite and PostgreSQL.

DROP INDEX IF EXISTS idx_account_bindings_primary_unique;
ALTER TABLE account_bindings DROP COLUMN is_primary;
ALTER TABLE account_bindings DROP COLUMN cli_command;
