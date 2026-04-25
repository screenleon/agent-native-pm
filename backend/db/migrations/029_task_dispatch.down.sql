-- SQLite >= 3.35 required for DROP COLUMN (released 2021-03-12).
-- Operators on older SQLite must rebuild the table manually to roll back.
DROP INDEX IF EXISTS idx_tasks_dispatch_status;
ALTER TABLE tasks DROP COLUMN execution_result;
ALTER TABLE tasks DROP COLUMN dispatch_status;
