-- No-op: we cannot safely remove rows that may have been manually inserted.
-- Re-running the up migration on a clean DB is idempotent via ON CONFLICT.
SELECT 1;
