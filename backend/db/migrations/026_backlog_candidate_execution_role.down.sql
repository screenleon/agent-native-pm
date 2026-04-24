-- Reverse migration 026 — drop the execution_role column.
-- SQLite: DROP COLUMN works since 3.35 (March 2021). Postgres: supported
-- since 9.x. If a developer is on older SQLite the rollback fails with
-- `near "DROP": syntax error`. See DECISIONS.md 2026-04-24 Phase 5 entry
-- and docs/data-model.md for the declared minimum runtime.

ALTER TABLE backlog_candidates DROP COLUMN execution_role;
