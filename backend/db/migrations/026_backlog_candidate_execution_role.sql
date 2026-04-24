-- Migration 026: Add execution_role to backlog_candidates (Phase 5 B2).
-- A nullable TEXT column naming the execution specialist (e.g.
-- backend-architect, ui-scaffolder) that Phase 6 auto-dispatch will
-- consume. The value is not enforced against a catalog at the DB layer
-- — the file-based role library in backend/internal/prompts/roles/ is
-- the authoritative list. Phase 5 leaves the column nullable and
-- populates it only opportunistically, Phase 6 will introduce catalog
-- enforcement.

ALTER TABLE backlog_candidates ADD COLUMN execution_role TEXT;
