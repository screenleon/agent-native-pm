-- Migration 027: Pin per-connector cli_config runs to their target connector
-- (Phase 6a Part 1 — Copilot review on PR #23).
--
-- Adds a nullable target_connector_id to planning_runs. When a run is
-- created via the (connector_id, cli_config_id) authoring surface, this
-- column captures the chosen connector so the lease query refuses to hand
-- the run to any *other* online connector belonging to the same user. The
-- chosen cli_config (cli_command, model_id) only exists on that machine,
-- so dispatching elsewhere would either fail at exec time or, worse,
-- silently run with the wrong CLI / model.
--
-- NULL on:
--  * Account-binding-authored runs (the binding is user-scoped, not
--    connector-scoped — any of the user's online connectors with that
--    cli:* binding active may claim it, same as Phase < 6a behaviour).
--  * Pre-Phase-6a rows (no migration, existing rows stay claimable as
--    before).
--
-- Runner contract: schema_migrations prevents re-application, so do NOT
-- write IF NOT EXISTS on ALTER TABLE ADD COLUMN (modernc.org/sqlite
-- refuses).

ALTER TABLE planning_runs ADD COLUMN target_connector_id TEXT;
CREATE INDEX idx_planning_runs_target_connector ON planning_runs(target_connector_id);
