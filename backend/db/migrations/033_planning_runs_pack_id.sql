-- Phase 3B PR-1: add context_pack_id to planning_runs.
-- Populated at run-creation time with a UUID that correlates the run to its
-- planning_context_snapshots row. Empty string until a snapshot is written.
ALTER TABLE planning_runs ADD COLUMN context_pack_id TEXT NOT NULL DEFAULT '';
