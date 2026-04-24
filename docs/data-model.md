# Data Model - Agent Native PM

This file is the canonical schema reference for the current backend database.

## Runtime Baseline

- Runtime database: PostgreSQL
- SQL semantics: PostgreSQL placeholders, `TIMESTAMPTZ`, `BOOLEAN`, `JSONB`, partial indexes, and GIN full-text indexes
- Migrations: forward-only numbered SQL files in `backend/db/migrations/`
- Migration set currently applied through `026_backlog_candidate_execution_role.sql`
- Minimum SQLite version: **3.35** (March 2021). Required by migration 026's `.down.sql` which uses `ALTER TABLE ... DROP COLUMN`. Older SQLite versions apply the forward migration fine but rollback fails with `near "DROP": syntax error`.

## Current Entity Relationships

```text
users 1---* sessions
users 1---* notifications
users 1---* account_bindings
users *---* projects (via project_members)

projects 1---* tasks
projects 1---* requirements
projects 1---* documents
projects 1---* summary_snapshots
projects 1---* sync_runs
projects 1---* agent_runs
projects 1---* drift_signals
projects 1---* project_repo_mappings
projects 1---* api_keys
planning_settings 1---0 system-wide planning configuration

requirements 1---* planning_runs
requirements 1---* backlog_candidates
requirements 1---* task_lineage
planning_runs 1---* backlog_candidates
planning_runs 1---* task_lineage
backlog_candidates 1---* task_lineage
tasks 1---* task_lineage

documents 1---* document_links
documents 1---* drift_signals
```

## Core Tables

### Table: `projects`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `name` | TEXT | NOT NULL | Project display name |
| `description` | TEXT | DEFAULT '' | Free-text description |
| `repo_path` | TEXT | DEFAULT '' | Local repo path or synced primary repo path |
| `default_branch` | TEXT | DEFAULT 'main' | Project fallback branch; application may store empty string for auto-detect |
| `last_sync_at` | TIMESTAMPTZ | | Timestamp of last successful sync stamp |
| `repo_url` | TEXT | DEFAULT '' | Optional remote URL for managed clone mode |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `project_repo_mappings`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `alias` | TEXT | NOT NULL | Stable prefix for repo-relative alias paths |
| `repo_path` | TEXT | NOT NULL | Mounted mirror repo path |
| `default_branch` | TEXT | DEFAULT '' | Mapping-level branch override |
| `is_primary` | BOOLEAN | NOT NULL DEFAULT FALSE | Primary repo for unprefixed paths |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- At most one mapping per project may have `is_primary = TRUE`.
- `(project_id, alias)` is unique.
- Mapping branch override takes precedence over `projects.default_branch` in sync.

### Table: `tasks`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `title` | TEXT | NOT NULL | Task title |
| `description` | TEXT | DEFAULT '' | Task details |
| `status` | TEXT | NOT NULL DEFAULT 'todo' | `todo`, `in_progress`, `done`, `cancelled` |
| `priority` | TEXT | DEFAULT 'medium' | `low`, `medium`, `high` |
| `assignee` | TEXT | DEFAULT '' | Human name or agent identifier |
| `source` | TEXT | DEFAULT '' | `human` or `agent:<name>` |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

## Planning Foundation Tables

### Table: `planning_settings`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | Singleton row, currently `global` |
| `provider_id` | TEXT | NOT NULL | Central provider used for new planning runs |
| `model_id` | TEXT | NOT NULL | Central default model used for new planning runs |
| `base_url` | TEXT | NOT NULL DEFAULT '' | Remote OpenAI-compatible endpoint base URL when applicable |
| `configured_models` | JSONB | NOT NULL DEFAULT '[]' | Model IDs exposed to planning provider options and settings UI |
| `api_key_ciphertext` | TEXT | NOT NULL DEFAULT '' | Encrypted provider credential at rest; never returned by API |
| `api_key_configured` | BOOLEAN | NOT NULL DEFAULT FALSE | Whether a provider API key is currently stored |
| `credential_mode` | TEXT | NOT NULL DEFAULT 'shared' | `shared`, `personal_preferred`, or `personal_required` |
| `updated_by` | TEXT | NOT NULL DEFAULT '' | Username of the last admin who changed the settings |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- This is a singleton configuration row rather than a per-project override in v1.
- API keys are encrypted at rest with `APP_SETTINGS_MASTER_KEY`; the plaintext secret is never returned by `GET /api/settings/planning`.

### Table: `account_bindings`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Owning user |
| `provider_id` | TEXT | NOT NULL | `openai-compatible`, `cli:claude`, or `cli:codex` |
| `label` | TEXT | NOT NULL DEFAULT '' | User-facing binding name |
| `base_url` | TEXT | NOT NULL DEFAULT '' | Per-user OpenAI-compatible endpoint (empty for `cli:*`) |
| `model_id` | TEXT | NOT NULL DEFAULT '' | Default model for this binding |
| `configured_models` | JSONB | NOT NULL DEFAULT '[]' | User-selectable models exposed for this binding |
| `api_key_ciphertext` | TEXT | NOT NULL DEFAULT '' | Encrypted personal API credential (empty for `cli:*`) |
| `api_key_configured` | BOOLEAN | NOT NULL DEFAULT FALSE | Whether the binding has a stored credential |
| `is_active` | BOOLEAN | NOT NULL DEFAULT TRUE | Whether this binding is the active one for the provider |
| `cli_command` | TEXT | NOT NULL DEFAULT '' | Absolute path to the CLI binary; empty = PATH lookup (migration 021) |
| `is_primary` | BOOLEAN | NOT NULL DEFAULT FALSE | Whether this binding is the primary in its namespace (migration 021) |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- `(user_id, provider_id, label)` is unique.
- At most one binding per `(user_id, provider_id)` may be active at a time (`idx_account_bindings_active_unique`, migration 015).
- At most one binding per `(user_id, namespace)` may be primary at a time (`idx_account_bindings_primary_unique`, migration 021), where namespace is `cli` for `cli:*` providers and `api` for everything else.
- Personal API keys use the same encryption-at-rest mechanism as `planning_settings`.
- `cli:*` provider ids are local-mode only (design §5 D8); the API rejects them with 403 in server mode.

### Table: `local_connectors`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Owning user |
| `label` | TEXT | NOT NULL DEFAULT '' | User-facing device label |
| `platform` | TEXT | NOT NULL DEFAULT '' | Connector platform hint such as `linux`, `macos`, or `windows` |
| `client_version` | TEXT | NOT NULL DEFAULT '' | Connector binary version |
| `status` | TEXT | NOT NULL DEFAULT 'pending' | `pending`, `online`, `offline`, `revoked` |
| `capabilities` | JSONB | NOT NULL DEFAULT '{}' | Advertised adapter/runtime metadata |
| `token_hash` | TEXT | NOT NULL, UNIQUE | Hash of the connector token; plaintext token is returned only once on pair |
| `protocol_version` | INTEGER | NOT NULL DEFAULT 0 | Wire-protocol revision reported at pair time (Path B S2; migration 023) |
| `metadata` | JSONB | NOT NULL DEFAULT '{}' | Operational signals: CLI health + Phase 4 probe pipeline (see keys below) |
| `last_seen_at` | TIMESTAMPTZ | | Latest successful heartbeat |
| `last_error` | TEXT | NOT NULL DEFAULT '' | Last connector-reported error |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Well-known keys inside `local_connectors.metadata`:

| Key | Shape | Source | Notes |
|-----|-------|--------|-------|
| `cli_last_healthy_at` | RFC3339 string | Heartbeat (Path B S5b) | Most recent successful `<cli_command> --version` probe across any binding |
| `pending_cli_probe_requests` | array of `PendingCliProbeRequest` | Server (Phase 4 P4-4) | Probes awaiting connector pickup; dedup'd by `binding_id` |
| `cli_probe_results` | map keyed by `probe_id` of `CliProbeResult` | Connector → heartbeat (Phase 4 P4-4) | 24h retention; scrubbed on binding delete |
| `cli_configs` | array of `CliConfig` | Server + user (Phase 6a UX-B1) | Per-connector CLI + model combos. Exactly one `is_primary` when non-empty. Cap 16 entries. Replaces user-level `cli:*` account_bindings as the primary authoring surface; legacy bindings still work as a fallback. |

### Table: `connector_pairing_sessions`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Owning user |
| `pairing_code_hash` | TEXT | NOT NULL, UNIQUE | Hash of the short-lived pairing code |
| `label` | TEXT | NOT NULL DEFAULT '' | Optional target device label |
| `status` | TEXT | NOT NULL DEFAULT 'pending' | `pending`, `claimed`, `expired`, `cancelled` |
| `expires_at` | TIMESTAMPTZ | NOT NULL | Pairing TTL boundary |
| `connector_id` | TEXT | FK -> local_connectors.id | Claimed connector once paired |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- Pairing-session codes are never stored in plaintext.
- Local connector dispatch now uses planning-run lease fields in `planning_runs` rather than a separate dispatch table.

### Table: `requirements`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `title` | TEXT | NOT NULL | Requirement title |
| `summary` | TEXT | NOT NULL DEFAULT '' | Short planning summary |
| `description` | TEXT | NOT NULL DEFAULT '' | Longer free-text requirement detail |
| `status` | TEXT | NOT NULL DEFAULT 'draft' | `draft`, `planned`, `archived` |
| `source` | TEXT | NOT NULL DEFAULT 'human' | `human` or `agent:<name>` |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `planning_runs`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `requirement_id` | TEXT | NOT NULL, FK -> requirements.id | Origin requirement |
| `status` | TEXT | NOT NULL DEFAULT 'queued' | `queued`, `running`, `completed`, `failed`, `cancelled` |
| `trigger_source` | TEXT | NOT NULL DEFAULT 'manual' | Planning trigger origin |
| `provider_id` | TEXT | NOT NULL DEFAULT 'deterministic' | Resolved provider implementation used for the run |
| `model_id` | TEXT | NOT NULL DEFAULT 'deterministic-v1' | Resolved model or provider profile identifier |
| `selection_source` | TEXT | NOT NULL DEFAULT 'server_default' | Whether the run used the saved central server configuration or a legacy request override |
| `binding_source` | TEXT | NOT NULL DEFAULT 'system' | `system`, `shared`, or `personal` credential resolution source |
| `binding_label` | TEXT | NOT NULL DEFAULT '' | Personal binding label when `binding_source = personal` |
| `requested_by_user_id` | TEXT | FK -> users.id | Human user who requested the run when it is scoped to a paired local connector |
| `execution_mode` | TEXT | NOT NULL DEFAULT 'server_provider' | `deterministic`, `server_provider`, or `local_connector` |
| `dispatch_status` | TEXT | NOT NULL DEFAULT 'not_required' | `not_required`, `queued`, `leased`, `returned`, or `expired` |
| `connector_id` | TEXT | FK -> local_connectors.id | Connector currently holding the dispatch lease, if any |
| `connector_label` | TEXT | NOT NULL DEFAULT '' | Connector label snapshot recorded at lease time |
| `lease_expires_at` | TIMESTAMPTZ | | Lease expiry for local connector execution |
| `dispatch_error` | TEXT | NOT NULL DEFAULT '' | Dispatch-layer error from lease expiry or connector callback |
| `error_message` | TEXT | NOT NULL DEFAULT '' | Failure detail when planning fails |
| `started_at` | TIMESTAMPTZ | | Planning start time |
| `completed_at` | TIMESTAMPTZ | | Planning completion time |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `backlog_candidates`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `requirement_id` | TEXT | NOT NULL, FK -> requirements.id | Origin requirement |
| `planning_run_id` | TEXT | NOT NULL, FK -> planning_runs.id | Planning run that produced the candidate |
| `parent_candidate_id` | TEXT | FK -> backlog_candidates.id | Optional parent candidate for hierarchical drafts |
| `suggestion_type` | TEXT | NOT NULL DEFAULT 'implementation' | Suggestion category such as `implementation`, `integration`, or `validation` |
| `title` | TEXT | NOT NULL | Candidate title |
| `description` | TEXT | NOT NULL DEFAULT '' | Candidate detail |
| `status` | TEXT | NOT NULL DEFAULT 'draft' | `draft`, `approved`, `rejected`, `applied` |
| `rationale` | TEXT | NOT NULL DEFAULT '' | Why this candidate was proposed |
| `priority_score` | DOUBLE PRECISION | NOT NULL DEFAULT 0 | Backend-computed recommendation score |
| `confidence` | DOUBLE PRECISION | NOT NULL DEFAULT 0 | Recommendation confidence percentage |
| `rank` | INTEGER | NOT NULL DEFAULT 0 | Order within a planning run |
| `evidence` | JSONB | NOT NULL DEFAULT '[]' | Structured evidence snippets shown in review UI |
| `evidence_detail` | JSONB | NOT NULL DEFAULT '{}' | Typed context evidence grouped by documents, drift, sync, agent runs, duplicates, and score breakdown |
| `duplicate_titles` | JSONB | NOT NULL DEFAULT '[]' | Exact-title duplicate signals from current open work |
| `execution_role` | TEXT | | (Phase 5 B2, migration 026) nullable hint naming the execution specialist from `backend/internal/prompts/roles/` that should run this candidate under Phase-6 auto-dispatch. Not enforced against the catalog today. |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `task_lineage`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `task_id` | TEXT | NOT NULL, FK -> tasks.id | Materialized task |
| `requirement_id` | TEXT | FK -> requirements.id | Optional requirement ancestor |
| `planning_run_id` | TEXT | FK -> planning_runs.id | Optional planning run ancestor |
| `backlog_candidate_id` | TEXT | FK -> backlog_candidates.id | Optional candidate ancestor |
| `lineage_kind` | TEXT | NOT NULL DEFAULT 'applied_candidate' | `applied_candidate`, `manual_requirement`, `merged_requirement` |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- Requirement intake, planning run orchestration, candidate review, and candidate-scoped apply-to-task reads are now active on top of this schema.
- `task_lineage` now records traceability when an approved backlog candidate is materialized into a task through `POST /api/backlog-candidates/:id/apply`.
- `planning_runs` now snapshot the resolved provider, model, and credential source so the UI and audit trail can distinguish shared versus personal execution.
- `planning_runs` also track local-dispatch ownership and lease lifecycle so only a paired connector owned by the requesting user can claim a queued `local_connector` run.
- `backlog_candidates` now persist ranked deterministic suggestion output for completed planning runs, including score, confidence, `evidence`, typed `evidence_detail`, and duplicate-title review signals before an approved candidate becomes `applied`.

Source: `[agent:backend-architect]`

### Table: `documents`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `title` | TEXT | NOT NULL | Display name |
| `file_path` | TEXT | DEFAULT '' | Repo-relative path; may use alias-prefixed secondary repo path |
| `doc_type` | TEXT | DEFAULT 'general' | `api`, `architecture`, `guide`, `adr`, `general` |
| `description` | TEXT | DEFAULT '' | Searchable document summary content |
| `last_updated_at` | TIMESTAMPTZ | | Last content update time |
| `staleness_days` | INTEGER | DEFAULT 0 | Computed freshness signal |
| `is_stale` | BOOLEAN | DEFAULT FALSE | Whether the doc is stale |
| `source` | TEXT | DEFAULT '' | `human` or `agent:<name>` |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `summary_snapshots`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `snapshot_date` | DATE | NOT NULL | Snapshot date |
| `total_tasks` | INTEGER | DEFAULT 0 | |
| `tasks_todo` | INTEGER | DEFAULT 0 | |
| `tasks_in_progress` | INTEGER | DEFAULT 0 | |
| `tasks_done` | INTEGER | DEFAULT 0 | |
| `tasks_cancelled` | INTEGER | DEFAULT 0 | |
| `total_documents` | INTEGER | DEFAULT 0 | |
| `stale_documents` | INTEGER | DEFAULT 0 | |
| `health_score` | REAL | DEFAULT 0.0 | Computed health value |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

## Sync And Drift Tables

### Table: `sync_runs`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `started_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Sync start time |
| `completed_at` | TIMESTAMPTZ | | Sync finish time |
| `status` | TEXT | NOT NULL DEFAULT 'running' | `running`, `completed`, `failed` |
| `commits_scanned` | INTEGER | DEFAULT 0 | Number of commits scanned |
| `files_changed` | INTEGER | DEFAULT 0 | Number of changed files detected |
| `error_message` | TEXT | DEFAULT '' | Failure reason or guidance |

### Table: `agent_runs`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `agent_name` | TEXT | NOT NULL DEFAULT '' | Agent identifier |
| `action_type` | TEXT | NOT NULL DEFAULT 'update' | `create`, `update`, `review`, `sync` |
| `summary` | TEXT | DEFAULT '' | Human-readable action summary |
| `files_affected` | TEXT | DEFAULT '[]' | JSON array stored as text |
| `needs_human_review` | BOOLEAN | DEFAULT FALSE | Human review flag |
| `idempotency_key` | TEXT | UNIQUE | Optional duplicate prevention key |
| `status` | TEXT | NOT NULL DEFAULT 'running' | Lifecycle status |
| `started_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Start time |
| `completed_at` | TIMESTAMPTZ | | Completion time |
| `error_message` | TEXT | NOT NULL DEFAULT '' | Failure detail |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `drift_signals`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `document_id` | TEXT | NOT NULL, FK -> documents.id | Related document |
| `trigger_type` | TEXT | NOT NULL DEFAULT 'manual' | `code_change`, `time_decay`, `manual` |
| `trigger_detail` | TEXT | DEFAULT '' | Human-readable trigger summary |
| `trigger_meta` | JSONB | NOT NULL DEFAULT '{}' | Structured metadata for changed files / decay info |
| `severity` | SMALLINT | NOT NULL DEFAULT 1 | 1=low, 2=medium, 3=high |
| `sync_run_id` | TEXT | FK -> sync_runs.id | Optional originating sync run |
| `status` | TEXT | NOT NULL DEFAULT 'open' | `open`, `resolved`, `dismissed` |
| `resolved_by` | TEXT | DEFAULT '' | Human or agent identifier |
| `resolved_at` | TIMESTAMPTZ | | Resolution time |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `document_links`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `document_id` | TEXT | NOT NULL, FK -> documents.id | Parent document |
| `code_path` | TEXT | NOT NULL DEFAULT '' | Repo-relative file or path pattern |
| `link_type` | TEXT | NOT NULL DEFAULT 'covers' | `covers`, `references`, `depends_on` |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

## Access Control And Collaboration Tables

### Table: `api_keys`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | FK -> projects.id | Optional project scope; null means global key |
| `key_hash` | TEXT | NOT NULL UNIQUE | Hashed raw key |
| `label` | TEXT | NOT NULL DEFAULT '' | Human label |
| `is_active` | BOOLEAN | NOT NULL DEFAULT TRUE | Active flag |
| `last_used_at` | TIMESTAMPTZ | | Last usage time |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `users`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `username` | TEXT | NOT NULL UNIQUE | Login name |
| `email` | TEXT | NOT NULL UNIQUE | User email |
| `password_hash` | TEXT | NOT NULL | Stored password hash |
| `role` | TEXT | NOT NULL DEFAULT 'member' | `admin`, `member`, `viewer` |
| `is_active` | BOOLEAN | NOT NULL DEFAULT TRUE | Active flag |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `sessions`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | Session token |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Parent user |
| `expires_at` | TIMESTAMPTZ | NOT NULL | Expiry timestamp |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Table: `project_members`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `project_id` | TEXT | NOT NULL, FK -> projects.id | Parent project |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Parent user |
| `role` | TEXT | NOT NULL DEFAULT 'member' | Project-scoped role |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

Notes:

- `(project_id, user_id)` is unique.

### Table: `notifications`

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | UUID v4 |
| `user_id` | TEXT | NOT NULL, FK -> users.id | Recipient |
| `project_id` | TEXT | FK -> projects.id | Optional related project |
| `kind` | TEXT | NOT NULL DEFAULT 'info' | `info`, `warning`, `error`, `drift`, `agent` |
| `title` | TEXT | NOT NULL DEFAULT '' | Notification title |
| `body` | TEXT | DEFAULT '' | Notification body |
| `is_read` | BOOLEAN | NOT NULL DEFAULT FALSE | Read state |
| `link` | TEXT | DEFAULT '' | Optional in-app navigation target |
| `created_at` | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

## Search Indexes And Query Support

- `tasks` full-text search:
  - `GIN (to_tsvector('english', title || ' ' || COALESCE(description, '')))`
- `documents` full-text search:
  - `GIN (to_tsvector('english', title || ' ' || COALESCE(description, '')))`
- `drift_signals` triage indexes:
  - `idx_drift_signals_status`
  - `idx_drift_signals_severity`
  - `idx_drift_signals_sync_run`
- `agent_runs` lifecycle index:
  - `idx_agent_runs_project_status_created_at`

## Important Current Indexes

```sql
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_documents_project_id ON documents(project_id);
CREATE INDEX idx_documents_is_stale ON documents(is_stale);
CREATE UNIQUE INDEX idx_project_repo_mappings_project_alias ON project_repo_mappings(project_id, alias);
CREATE UNIQUE INDEX idx_project_repo_mappings_primary ON project_repo_mappings(project_id) WHERE is_primary = TRUE;
CREATE INDEX idx_summary_snapshots_project_date ON summary_snapshots(project_id, snapshot_date);
CREATE INDEX idx_sync_runs_project_id ON sync_runs(project_id);
CREATE INDEX idx_agent_runs_project_id ON agent_runs(project_id);
CREATE INDEX idx_agent_runs_project_status_created_at ON agent_runs(project_id, status, created_at DESC);
CREATE INDEX idx_drift_signals_project_id ON drift_signals(project_id);
CREATE INDEX idx_drift_signals_status ON drift_signals(status);
CREATE INDEX idx_drift_signals_severity ON drift_signals(severity DESC);
CREATE INDEX idx_drift_signals_sync_run ON drift_signals(sync_run_id);
```

## Planning Workflow Coverage

The planning-domain tables are part of the current schema.

- `requirements` and `planning_runs` are now exercised by active API and UI flows.
- `backlog_candidates` now persist draft planning output and are exposed through a run-scoped read API.
- `task_lineage` remains schema-ready for a later apply flow.

## Conventions

- Primary keys are UUID v4 strings stored as `TEXT`
- Timestamps use `TIMESTAMPTZ`
- Booleans use PostgreSQL `BOOLEAN`
- Structured drift metadata uses `JSONB`
- Full-text search uses PostgreSQL GIN indexes and `to_tsvector` / `plainto_tsquery`
- Rows are hard-deleted; no soft-delete convention is active
- Secondary repos are addressed with alias-prefixed logical paths such as `docs-repo/docs/api-surface.md`

## Wire Types (non-persistent)

### `context.v1` (`backend/internal/planning/wire/`)

`PlanningContextV1` is the serialized planning context delivered to local connector adapters via `POST /api/connector/claim-next-run`. It is **not** persisted — it is rebuilt on each claim from live store data.

- Source of truth: [`docs/local-connector-context.md`](local-connector-context.md)
- Schema version: `context.v1` (additive changes non-breaking; renames require v2)
- Sanitizer version: `v1` (prefix-anchored secret patterns; see §7 of source doc)
- Per-source defaults: 100 tasks, 8 documents, 6 drift signals, 6 agent runs, latest sync run
- Sources byte cap: 256 KiB (applied to `sources` only, excludes scaffolding)

#### Layering

The `wire` package is a leaf:

```text
models   — leaf (no internal deps)
wire     — leaf (std lib only)
planning ← models, wire
connector← models, wire (never planning)
handlers ← models, wire, planning
```

`models.LocalConnectorClaimNextRunResponse.PlanningContext` is `*wire.PlanningContextV1`; the `wire` package never imports `models`, avoiding an import cycle.

#### Top-level fields

| Field | Type | Notes |
| --- | --- | --- |
| `schema_version` | string | `"context.v1"` |
| `generated_at` | `time.Time` | UTC, set at claim serialization time |
| `generated_by` | string | `"server"` |
| `sanitizer_version` | string | `"v1"` |
| `limits` | `PlanningContextLimits` | per-source caps + `max_sources_bytes` |
| `sources` | `PlanningContextSources` | sanitized, byte-capped payload |
| `meta` | `PlanningContextMeta` | ranking taxonomy, drop counts, final byte size |

#### Source DTOs (metadata-only)

- `WireTask` — `id`, `title`, `status`, `priority`, `updated_at`
- `WireDocument` — `id`, `title`, `file_path`, `doc_type`, `is_stale`, `staleness_days` (no body)
- `WireDriftSignal` — `id`, `document_title`, `trigger_type`, `trigger_detail`, `severity` (`"low"|"medium"|"high"`), `opened_at`
- `WireSyncRun` — `id`, `status`, `started_at`, `completed_at?`, `error_message` (redacted + truncated ≤240 chars)
- `WireAgentRun` — `id`, `agent_name`, `action_type`, `status`, `started_at`, `summary` (redacted + truncated ≤180 chars)

Source: `[agent:feature-planner]`
