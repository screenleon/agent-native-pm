-- Migration 018: add per-run adapter type and model override to planning_runs

ALTER TABLE planning_runs ADD COLUMN IF NOT EXISTS adapter_type   TEXT;
ALTER TABLE planning_runs ADD COLUMN IF NOT EXISTS model_override TEXT;
