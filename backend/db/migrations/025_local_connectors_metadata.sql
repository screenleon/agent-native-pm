-- Migration 025: Add metadata JSONB column to local_connectors (Path B S5b).
-- Stores connector-level operational data such as the last CLI health probe timestamp
-- (local_connectors.metadata.cli_last_healthy_at). All keys are namespaced by feature.
-- The heartbeat handler merges new keys non-destructively so unrelated keys are preserved.

ALTER TABLE local_connectors ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}';
