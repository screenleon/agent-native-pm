-- Phase 6b: add dispatch_status + execution_result to tasks.
-- dispatch_status tracks the connector execution lifecycle:
--   none     → task was not created via role_dispatch (default)
--   queued   → role_dispatch task waiting to be claimed
--   running  → claimed by a connector
--   completed → connector returned success result
--   failed   → connector returned failure
ALTER TABLE tasks ADD COLUMN dispatch_status TEXT NOT NULL DEFAULT 'none';
ALTER TABLE tasks ADD COLUMN execution_result JSONB;
CREATE INDEX idx_tasks_dispatch_status ON tasks(dispatch_status);
