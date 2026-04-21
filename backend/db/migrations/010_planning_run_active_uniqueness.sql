WITH ranked_active_runs AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY requirement_id
            ORDER BY created_at DESC, id DESC
        ) AS row_num
    FROM planning_runs
    WHERE status IN ('queued', 'running')
)
UPDATE planning_runs
SET
    status = 'failed',
    error_message = CASE
        WHEN error_message = '' THEN 'Auto-failed during migration to enforce a single active planning run per requirement.'
        ELSE error_message
    END,
    completed_at = COALESCE(completed_at, NOW()),
    updated_at = NOW()
WHERE id IN (
    SELECT id
    FROM ranked_active_runs
    WHERE row_num > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_planning_runs_requirement_active
    ON planning_runs(requirement_id)
    WHERE status IN ('queued', 'running');