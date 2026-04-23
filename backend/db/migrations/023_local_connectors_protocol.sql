-- Migration 023: Local connector protocol version (Path B Slice S2).
--
-- Tags each paired connector with the wire-protocol version it understands.
-- Used by the dispatch path to refuse handing a CLI-bound run (one with
-- non-NULL account_binding_id) to a connector that doesn't yet know how
-- to read the cli_binding block in the claim-next-run response (R3
-- mitigation).
--
-- 0 = pre-Path-B connector (no cli_binding awareness, default for any old
--     pair request that doesn't send the field).
-- 1 = Path B / S2-aware (sent by anpm-connector pair after this slice).
--
-- Runner contract. schema_migrations prevents re-application, so do not
-- write IF NOT EXISTS on ALTER TABLE ADD COLUMN (modernc.org/sqlite refuses).

ALTER TABLE local_connectors ADD COLUMN protocol_version INTEGER NOT NULL DEFAULT 0;
