-- Phase 1 initial schema
-- Agent Native PM

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    repo_path TEXT DEFAULT '',
    default_branch TEXT DEFAULT 'main',
    last_sync_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo',
    priority TEXT DEFAULT 'medium',
    assignee TEXT DEFAULT '',
    source TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS documents (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    file_path TEXT DEFAULT '',
    doc_type TEXT DEFAULT 'general',
    last_updated_at TIMESTAMPTZ,
    staleness_days INTEGER DEFAULT 0,
    is_stale BOOLEAN DEFAULT FALSE,
    source TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS summary_snapshots (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    snapshot_date DATE NOT NULL,
    total_tasks INTEGER DEFAULT 0,
    tasks_todo INTEGER DEFAULT 0,
    tasks_in_progress INTEGER DEFAULT 0,
    tasks_done INTEGER DEFAULT 0,
    tasks_cancelled INTEGER DEFAULT 0,
    total_documents INTEGER DEFAULT 0,
    stale_documents INTEGER DEFAULT 0,
    health_score REAL DEFAULT 0.0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_tasks_project_id ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_documents_project_id ON documents(project_id);
CREATE INDEX IF NOT EXISTS idx_documents_is_stale ON documents(is_stale);
CREATE INDEX IF NOT EXISTS idx_summary_snapshots_project_date ON summary_snapshots(project_id, snapshot_date);
