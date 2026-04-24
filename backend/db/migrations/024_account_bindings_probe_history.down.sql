-- Down migration for 024 (development-only rollback per design §13).
-- Production never runs down migrations; this file exists so dev/test can
-- exercise the rollback path under both SQLite and PostgreSQL.

ALTER TABLE account_bindings DROP COLUMN last_probe_at;
ALTER TABLE account_bindings DROP COLUMN last_probe_ok;
ALTER TABLE account_bindings DROP COLUMN last_probe_ms;
