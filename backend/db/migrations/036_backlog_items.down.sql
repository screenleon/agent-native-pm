DROP INDEX IF EXISTS idx_task_lineage_backlog_item_id;
ALTER TABLE task_lineage DROP COLUMN backlog_item_id;

DROP INDEX IF EXISTS idx_backlog_items_task_unique;
DROP INDEX IF EXISTS idx_backlog_items_candidate_unique;
DROP INDEX IF EXISTS idx_backlog_items_project_updated;
DROP INDEX IF EXISTS idx_backlog_items_project_priority;
DROP INDEX IF EXISTS idx_backlog_items_project_status_rank;

DROP TABLE IF EXISTS backlog_items;
