CREATE TABLE IF NOT EXISTS project_repo_mappings (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    alias TEXT NOT NULL,
    repo_path TEXT NOT NULL,
    default_branch TEXT DEFAULT '',
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_repo_mappings_project_alias
    ON project_repo_mappings(project_id, alias);

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_repo_mappings_primary
    ON project_repo_mappings(project_id)
    WHERE is_primary = TRUE;
