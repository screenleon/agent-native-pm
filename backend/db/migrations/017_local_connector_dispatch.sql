-- Migration 017: Local connector planning run dispatch lifecycle

ALTER TABLE planning_runs
    ADD COLUMN IF NOT EXISTS requested_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'server_provider',
    ADD COLUMN IF NOT EXISTS dispatch_status TEXT NOT NULL DEFAULT 'not_required',
    ADD COLUMN IF NOT EXISTS connector_id TEXT REFERENCES local_connectors(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS connector_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dispatch_error TEXT NOT NULL DEFAULT '';

UPDATE planning_runs
SET execution_mode = CASE
        WHEN provider_id = 'deterministic' THEN 'deterministic'
        ELSE 'server_provider'
    END,
    dispatch_status = 'not_required'
WHERE execution_mode = 'server_provider'
  AND dispatch_status = 'not_required';

CREATE INDEX IF NOT EXISTS idx_planning_runs_local_dispatch_queue
    ON planning_runs(requested_by_user_id, execution_mode, dispatch_status, created_at ASC)
    WHERE execution_mode = 'local_connector';

CREATE INDEX IF NOT EXISTS idx_planning_runs_connector_lease
    ON planning_runs(connector_id, dispatch_status, lease_expires_at)
    WHERE execution_mode = 'local_connector';

COMMENT ON COLUMN planning_runs.requested_by_user_id IS 'Authenticated user who requested the planning run when the run is tied to a personal local connector';
COMMENT ON COLUMN planning_runs.execution_mode IS 'deterministic | server_provider | local_connector';
COMMENT ON COLUMN planning_runs.dispatch_status IS 'not_required | queued | leased | returned | expired';
COMMENT ON COLUMN planning_runs.connector_id IS 'Connector that currently owns the local dispatch lease, if any';
COMMENT ON COLUMN planning_runs.connector_label IS 'Connector label captured at lease time for audit and UI clarity';
COMMENT ON COLUMN planning_runs.lease_expires_at IS 'Lease expiry for local connector dispatch';
COMMENT ON COLUMN planning_runs.dispatch_error IS 'Dispatch-layer error from lease or connector callback';