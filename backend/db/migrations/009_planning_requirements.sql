CREATE TABLE IF NOT EXISTS requirements (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    source TEXT NOT NULL DEFAULT 'human',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_requirements_project_created_at
    ON requirements(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_requirements_project_status
    ON requirements(project_id, status);

CREATE TABLE IF NOT EXISTS planning_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    requirement_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    trigger_source TEXT NOT NULL DEFAULT 'manual',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_planning_runs_project_status_created_at
    ON planning_runs(project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_planning_runs_requirement_created_at
    ON planning_runs(requirement_id, created_at DESC);

CREATE TABLE IF NOT EXISTS backlog_candidates (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    requirement_id TEXT NOT NULL,
    planning_run_id TEXT NOT NULL,
    parent_candidate_id TEXT,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    rationale TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (requirement_id) REFERENCES requirements(id) ON DELETE CASCADE,
    FOREIGN KEY (planning_run_id) REFERENCES planning_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_candidate_id) REFERENCES backlog_candidates(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_backlog_candidates_planning_run_created_at
    ON backlog_candidates(planning_run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_backlog_candidates_requirement_status
    ON backlog_candidates(requirement_id, status);

CREATE TABLE IF NOT EXISTS task_lineage (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    requirement_id TEXT,
    planning_run_id TEXT,
    backlog_candidate_id TEXT,
    lineage_kind TEXT NOT NULL DEFAULT 'applied_candidate',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (requirement_id) REFERENCES requirements(id) ON DELETE SET NULL,
    FOREIGN KEY (planning_run_id) REFERENCES planning_runs(id) ON DELETE SET NULL,
    FOREIGN KEY (backlog_candidate_id) REFERENCES backlog_candidates(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_task_lineage_task_id
    ON task_lineage(task_id);

CREATE INDEX IF NOT EXISTS idx_task_lineage_requirement_id
    ON task_lineage(requirement_id);

CREATE INDEX IF NOT EXISTS idx_task_lineage_project_created_at
    ON task_lineage(project_id, created_at DESC);