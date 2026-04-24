-- Migration 025: Add metadata JSONB column to local_connectors (Path B S5b).
-- Stores connector-level operational data such as CLI health probe results
-- (local_connectors.metadata.cli_health.<binding_id>) without additional tables.
-- All keys inside metadata are namespaced by feature.
-- Nullable (NULL is treated as {} by application code). The heartbeat handler
-- merges new cli_health entries non-destructively so unrelated keys are preserved.

ALTER TABLE local_connectors ADD COLUMN metadata JSONB
