-- Migration 019: store CLI usage info returned by the local connector adapter
ALTER TABLE planning_runs ADD COLUMN IF NOT EXISTS connector_cli_info TEXT;
