-- Migration 015: Planning run binding audit and single active binding per provider

WITH ranked_active_bindings AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY user_id, provider_id ORDER BY updated_at DESC, created_at DESC, id DESC) AS row_num
    FROM account_bindings
    WHERE is_active = TRUE
)
UPDATE account_bindings
SET is_active = FALSE,
    updated_at = NOW()
WHERE id IN (
    SELECT id
    FROM ranked_active_bindings
    WHERE row_num > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_bindings_active_unique
    ON account_bindings(user_id, provider_id)
    WHERE is_active = TRUE;

ALTER TABLE planning_runs
    ADD COLUMN IF NOT EXISTS binding_source TEXT NOT NULL DEFAULT 'system';

ALTER TABLE planning_runs
    ADD COLUMN IF NOT EXISTS binding_label TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN planning_runs.binding_source IS 'system | shared | personal';
COMMENT ON COLUMN planning_runs.binding_label IS 'Personal binding label used for the run when binding_source=personal';
