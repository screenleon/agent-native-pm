-- Phase 6c PR-4: connector activity tracking columns.
-- current_activity_json holds the latest ConnectorActivity snapshot as JSON
-- (empty string = no activity recorded).
-- current_activity_at is the server timestamp of the last activity update
-- (NULL until the connector first reports activity).
ALTER TABLE local_connectors ADD COLUMN current_activity_json TEXT NOT NULL DEFAULT '';
ALTER TABLE local_connectors ADD COLUMN current_activity_at TIMESTAMP;
