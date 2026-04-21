ALTER TABLE planning_runs
    ADD COLUMN IF NOT EXISTS provider_id TEXT NOT NULL DEFAULT 'deterministic',
    ADD COLUMN IF NOT EXISTS model_id TEXT NOT NULL DEFAULT 'deterministic-v1',
    ADD COLUMN IF NOT EXISTS selection_source TEXT NOT NULL DEFAULT 'server_default';

ALTER TABLE backlog_candidates
    ADD COLUMN IF NOT EXISTS evidence_detail JSONB NOT NULL DEFAULT '{}'::jsonb;
