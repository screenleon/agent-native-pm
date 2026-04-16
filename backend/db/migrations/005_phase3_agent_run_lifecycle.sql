-- Phase 3: Agent run lifecycle fields

ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'running';
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS error_message TEXT NOT NULL DEFAULT '';

-- Backfill existing historical rows to completed so old events do not appear as active runs.
UPDATE agent_runs
SET status = 'completed',
    started_at = COALESCE(started_at, created_at),
    completed_at = COALESCE(completed_at, created_at)
WHERE status = 'running';

CREATE INDEX IF NOT EXISTS idx_agent_runs_project_status_created_at ON agent_runs(project_id, status, created_at DESC);
