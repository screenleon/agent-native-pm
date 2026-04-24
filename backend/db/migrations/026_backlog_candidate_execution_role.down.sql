-- Reverse migration 026 — drop the execution_role column.
-- SQLite: DROP COLUMN works since 3.35. Postgres: supported since 9.x.

ALTER TABLE backlog_candidates DROP COLUMN execution_role;
