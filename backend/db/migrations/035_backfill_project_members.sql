-- Migration 035: backfill project_members from planning_runs + local-admin
--
-- project_members was created in migration 004 but nothing ever inserted
-- into it, so ClaimNextDispatchTask (which joins project_members to verify
-- ownership) always returned an empty queue.
--
-- Two sources:
--   1. planning_runs.requested_by_user_id — covers multi-user deployments.
--   2. local-admin + all projects — covers local-mode where ensureLocalProject
--      called ps.Create without an owner before this fix landed.
--
-- Uses WHERE NOT EXISTS instead of ON CONFLICT so the same SQL runs on
-- both PostgreSQL and SQLite without dialect-specific upsert syntax.
-- gen_random_uuid() is rewritten to lower(hex(randomblob(16))) for SQLite
-- by the migration runner's rewriteForSQLite pass.

INSERT INTO project_members (id, project_id, user_id, role)
SELECT
    gen_random_uuid(),
    pr.project_id,
    pr.requested_by_user_id,
    'owner'
FROM (
    SELECT DISTINCT project_id, requested_by_user_id
    FROM planning_runs
    WHERE requested_by_user_id IS NOT NULL
      AND requested_by_user_id != ''
) pr
WHERE NOT EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = pr.project_id
      AND pm.user_id = pr.requested_by_user_id
);

INSERT INTO project_members (id, project_id, user_id, role)
SELECT gen_random_uuid(), p.id, 'local-admin', 'owner'
FROM projects p
WHERE EXISTS (SELECT 1 FROM users WHERE id = 'local-admin')
  AND NOT EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = p.id
      AND pm.user_id = 'local-admin'
  );
