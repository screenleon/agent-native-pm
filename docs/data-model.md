# Data Model — Agent Native PM

This file is the canonical data model reference. Update this file whenever the database schema changes.

## Phase 1 schema

### Entity relationship

```text
projects 1───* tasks
projects 1───* documents
projects 1───* project_repo_mappings
projects 1───* summary_snapshots
```

### Table: `projects`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `name` | TEXT | NOT NULL | Project display name |
| `description` | TEXT | | Free-text project description |
| `repo_url` | TEXT | | Remote git URL used for managed clone mode |
| `repo_path` | TEXT | | Local filesystem path to git repo |
| `default_branch` | TEXT | DEFAULT 'main' | Branch to scan |
| `last_sync_at` | DATETIME | | Timestamp of last repo sync (Phase 2) |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |

### Table: `project_repo_mappings`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK → projects.id | Parent project |
| `alias` | TEXT | NOT NULL | Stable path prefix for this repo mapping |
| `repo_path` | TEXT | NOT NULL | Mounted local mirror path (for example `/mirrors/agent-native-pm`) |
| `default_branch` | TEXT | | Branch to scan for this mapping; falls back to project default |
| `is_primary` | BOOLEAN | NOT NULL DEFAULT FALSE | Whether this mapping is the default repo root for non-prefixed files |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |

### Table: `tasks`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK → projects.id | Parent project |
| `title` | TEXT | NOT NULL | Task title |
| `description` | TEXT | | Task details |
| `status` | TEXT | NOT NULL DEFAULT 'todo' | One of: `todo`, `in_progress`, `done`, `cancelled` |
| `priority` | TEXT | DEFAULT 'medium' | One of: `low`, `medium`, `high` |
| `assignee` | TEXT | | Human name or agent identifier |
| `source` | TEXT | | Who created this: `human` or `agent:<name>` |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |

### Table: `documents`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK → projects.id | Parent project |
| `title` | TEXT | NOT NULL | Document display name |
| `file_path` | TEXT | | Relative path within the repo |
| `doc_type` | TEXT | DEFAULT 'general' | One of: `api`, `architecture`, `guide`, `adr`, `general` |
| `last_updated_at` | DATETIME | | When the document content was last modified |
| `staleness_days` | INTEGER | DEFAULT 0 | Days since last update (computed) |
| `is_stale` | BOOLEAN | DEFAULT 0 | Whether the document is considered stale |
| `source` | TEXT | | Who last updated: `human` or `agent:<name>` |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |

### Table: `summary_snapshots`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK → projects.id | Parent project |
| `snapshot_date` | DATE | NOT NULL | Date of the snapshot |
| `total_tasks` | INTEGER | DEFAULT 0 | |
| `tasks_todo` | INTEGER | DEFAULT 0 | |
| `tasks_in_progress` | INTEGER | DEFAULT 0 | |
| `tasks_done` | INTEGER | DEFAULT 0 | |
| `tasks_cancelled` | INTEGER | DEFAULT 0 | |
| `total_documents` | INTEGER | DEFAULT 0 | |
| `stale_documents` | INTEGER | DEFAULT 0 | |
| `health_score` | REAL | | 0.0 to 1.0 computed health metric |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | |

## Phase 2 additions (planned, not yet implemented)

### Table: `sync_runs`

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | UUID v4 |
| `project_id` | TEXT | FK → projects.id |
| `started_at` | DATETIME | When the sync started |
| `completed_at` | DATETIME | When the sync finished |
| `status` | TEXT | `running`, `completed`, `failed` |
| `commits_scanned` | INTEGER | Number of commits processed |
| `files_changed` | INTEGER | Number of changed files detected |
| `error_message` | TEXT | Error details if failed |

### Table: `agent_runs`

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | UUID v4 |
| `project_id` | TEXT | FK → projects.id |
| `agent_name` | TEXT | Identifier of the agent |
| `action_type` | TEXT | `create`, `update`, `review`, `sync` |
| `status` | TEXT | `running`, `completed`, `failed` |
| `summary` | TEXT | What the agent did |
| `files_affected` | TEXT | JSON array of file paths |
| `needs_human_review` | BOOLEAN | Whether a human should review |
| `started_at` | DATETIME | Lifecycle start timestamp |
| `completed_at` | DATETIME | Set when status becomes `completed` or `failed` |
| `error_message` | TEXT | Failure details when status is `failed` |
| `idempotency_key` | TEXT | UNIQUE — prevents duplicate runs |
| `created_at` | DATETIME | |

### Table: `drift_signals`

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | UUID v4 |
| `project_id` | TEXT | FK → projects.id |
| `document_id` | TEXT | FK → documents.id |
| `trigger_type` | TEXT | `code_change`, `time_decay`, `manual` |
| `trigger_detail` | TEXT | Description of what triggered the signal |
| `status` | TEXT | `open`, `resolved`, `dismissed` |
| `resolved_by` | TEXT | Human name or agent identifier |
| `resolved_at` | DATETIME | |
| `created_at` | DATETIME | |

### Table: `document_links`

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | UUID v4 |
| `document_id` | TEXT | FK → documents.id |
| `code_path` | TEXT | File path pattern the document covers |
| `link_type` | TEXT | `covers`, `references`, `depends_on` |
| `created_at` | DATETIME | |

## Migration strategy

- Phase 1: Forward-only numbered migrations (`001_init.sql`, `002_add_index.sql`, etc.)
- Migrations stored in `backend/db/migrations/`
- Applied automatically on startup
- No rollback migrations in Phase 1 (recreate from scratch if needed)
- Phase 4: Consider a proper migration tool (e.g., `goose` or `golang-migrate`) when PostgreSQL support is added

## Indexes

```sql
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_documents_project_id ON documents(project_id);
CREATE INDEX idx_documents_is_stale ON documents(is_stale);
CREATE UNIQUE INDEX idx_project_repo_mappings_project_alias ON project_repo_mappings(project_id, alias);
CREATE UNIQUE INDEX idx_project_repo_mappings_primary ON project_repo_mappings(project_id) WHERE is_primary = TRUE;
CREATE INDEX idx_summary_snapshots_project_date ON summary_snapshots(project_id, snapshot_date);
```

## SQLite configuration

```sql
PRAGMA journal_mode = WAL;          -- Write-ahead logging for better concurrency
PRAGMA busy_timeout = 5000;         -- Wait up to 5s for locks
PRAGMA foreign_keys = ON;           -- Enforce foreign key constraints
PRAGMA synchronous = NORMAL;        -- Balance durability and performance
```

## Conventions

- All primary keys are UUID v4 strings (not auto-increment integers)
- Timestamps use ISO 8601 format via SQLite `DATETIME` type
- Soft deletes are not used in Phase 1; rows are hard-deleted
- Boolean values use INTEGER (0/1) per SQLite convention
- JSON fields (e.g., `files_affected`) are stored as TEXT containing valid JSON
- Secondary repositories are addressed through alias-prefixed document and link paths (for example `docs-repo/docs/guide.md`).
- Source: `[agent:documentation-architect]`
