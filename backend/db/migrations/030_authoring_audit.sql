-- Phase 6c PR-2: generic actor_audit table for execution_role
-- authoring lifecycle (and any future field that needs audit). This
-- table is the single source of truth for who set the field, when,
-- and with what rationale. The backlog_candidates.execution_role
-- column carries only the current value, the trail lives here.
--
-- See docs/phase6c-plan.md section 3.2.1 (v5.1) and the matching
-- DECISIONS entry constraints (a) (b) (h).
--
-- subject_kind values: backlog_candidate, task, planning_run, connector
-- field examples: execution_role, status, po_decision (caller chooses)
-- actor_kind values: user, api_key, router, system, connector
--   user      session-authenticated human operator
--   api_key   automation-authenticated request via API key
--   router    reserved for Phase 6d auto-apply (LLM router) -- NO writer in 6c
--             PR-3 suggest writes "user" after operator confirms
--   system    server-side enforcement (e.g. claim-next-task stale-role)
--   connector connector-initiated change
-- confidence: 0.0-1.0, only set when actor_kind=router
CREATE TABLE actor_audit (
    id TEXT PRIMARY KEY,
    subject_kind TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    field TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    actor_kind TEXT NOT NULL,
    actor_id TEXT,
    rationale TEXT,
    confidence REAL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_actor_audit_subject ON actor_audit(subject_kind, subject_id, created_at DESC);
CREATE INDEX idx_actor_audit_subject_field ON actor_audit(subject_kind, subject_id, field, created_at DESC);
