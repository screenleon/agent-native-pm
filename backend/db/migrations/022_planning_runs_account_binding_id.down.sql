-- Down migration for 022 (development-only rollback per design §13).
-- Production never runs down migrations; this file exists so dev/test can
-- exercise the rollback path under both SQLite and PostgreSQL.

DROP INDEX IF EXISTS idx_planning_runs_account_binding;
ALTER TABLE planning_runs DROP COLUMN account_binding_id;
