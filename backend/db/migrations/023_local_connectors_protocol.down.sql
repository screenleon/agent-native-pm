-- Down migration for 023 (development-only rollback per design §13).
-- Production never runs down migrations; this file exists so dev/test can
-- exercise the rollback path under both SQLite and PostgreSQL.

ALTER TABLE local_connectors DROP COLUMN protocol_version;
