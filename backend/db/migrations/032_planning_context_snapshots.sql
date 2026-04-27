-- Phase 3B PR-1: planning context snapshots table.
-- Stores serialized PlanningContextV2 payloads for audit and replay.
-- planning_run_id FK cascades so snapshots are removed with their parent run.
CREATE TABLE planning_context_snapshots (
    id              TEXT PRIMARY KEY,
    pack_id         TEXT NOT NULL,
    planning_run_id TEXT NOT NULL REFERENCES planning_runs(id) ON DELETE CASCADE,
    schema_version  TEXT NOT NULL DEFAULT 'context.v2',
    snapshot        TEXT NOT NULL DEFAULT '',
    sources_bytes   INTEGER NOT NULL DEFAULT 0,
    dropped_counts  TEXT NOT NULL DEFAULT '{}',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_ctx_snapshots_run ON planning_context_snapshots(planning_run_id);
