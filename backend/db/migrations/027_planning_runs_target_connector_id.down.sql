-- Down: drop target_connector_id (and its index) from planning_runs.

DROP INDEX IF EXISTS idx_planning_runs_target_connector;
ALTER TABLE planning_runs DROP COLUMN target_connector_id;
