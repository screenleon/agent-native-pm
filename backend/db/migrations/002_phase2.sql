-- Phase 2: Repo sync + Drift detection schema additions

CREATE TABLE IF NOT EXISTS sync_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'running',
    commits_scanned INTEGER DEFAULT 0,
    files_changed INTEGER DEFAULT 0,
    error_message TEXT DEFAULT '',
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    agent_name TEXT NOT NULL DEFAULT '',
    action_type TEXT NOT NULL DEFAULT 'update',
    summary TEXT DEFAULT '',
    files_affected TEXT DEFAULT '[]',
    needs_human_review BOOLEAN DEFAULT FALSE,
    idempotency_key TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS drift_signals (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    trigger_detail TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    resolved_by TEXT DEFAULT '',
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS document_links (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL,
    code_path TEXT NOT NULL DEFAULT '',
    link_type TEXT NOT NULL DEFAULT 'covers',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sync_runs_project_id ON sync_runs(project_id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_project_id ON agent_runs(project_id);
CREATE INDEX IF NOT EXISTS idx_drift_signals_project_id ON drift_signals(project_id);
CREATE INDEX IF NOT EXISTS idx_drift_signals_status ON drift_signals(status);
CREATE INDEX IF NOT EXISTS idx_document_links_document_id ON document_links(document_id);
