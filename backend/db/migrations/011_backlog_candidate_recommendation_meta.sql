ALTER TABLE backlog_candidates
    ADD COLUMN IF NOT EXISTS suggestion_type TEXT NOT NULL DEFAULT 'implementation',
    ADD COLUMN IF NOT EXISTS priority_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rank INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS duplicate_titles JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_backlog_candidates_planning_run_rank_score
    ON backlog_candidates(planning_run_id, rank ASC, priority_score DESC, created_at ASC);