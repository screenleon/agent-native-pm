-- Migration 022: Per-run account binding reference (Path B Slice S2).
--
-- Adds a nullable account_binding_id to planning_runs so a run can record
-- which CLI / API-key binding the user picked when launching it. The column
-- is FK-less by design. When a binding is later deleted, the snapshot
-- stored inside connector_cli_info.binding_snapshot (migration 019, JSON
-- column, extended in code by S2) preserves the audit trail for the
-- in-flight run (design 6.5, R10 mitigation).
--
-- Nullable on purpose. NULL means "no binding selected" (request omitted
-- it AND user has zero active cli:* bindings, so the connector falls back
-- to env-var defaults). The default is intentionally NOT empty string,
-- which would make "no binding" indistinguishable from "explicitly cleared".
--
-- Runner contract. schema_migrations prevents re-application, so do not
-- write IF NOT EXISTS on ALTER TABLE ADD COLUMN (modernc.org/sqlite refuses).

ALTER TABLE planning_runs ADD COLUMN account_binding_id TEXT;
CREATE INDEX idx_planning_runs_account_binding ON planning_runs(account_binding_id);
