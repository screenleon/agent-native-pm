ALTER TABLE backlog_candidates
    ADD COLUMN IF NOT EXISTS validation_criteria TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS po_decision         TEXT NOT NULL DEFAULT '';
