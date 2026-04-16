# API Surface — Agent Native PM

This file is the canonical API contract reference. Update this file whenever endpoints are added or modified.

## Conventions

- Base path: `/api`
- Content type: `application/json`
- Response envelope: `{ "data": <payload>, "error": <string|null>, "meta": <object|null> }`
- Error responses: `{ "data": null, "error": "<message>", "meta": null }` with appropriate HTTP status
- Pagination: `?page=1&per_page=20` — meta includes `{ "page": 1, "per_page": 20, "total": 42 }`
- IDs: UUID v4 strings
- Timestamps: ISO 8601 format

## Authentication

- Phase 1-2: No authentication (single-user local deployment)
- Phase 3: Agent authentication via `X-API-Key` header
- Phase 4: Session-based authentication for human users

## Phase 1 endpoints

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check — returns `{ "status": "ok" }` |

### Projects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects` | List all projects |
| POST | `/api/projects` | Create a project |
| GET | `/api/projects/:id` | Get a project by ID |
| PATCH | `/api/projects/:id` | Update a project |
| DELETE | `/api/projects/:id` | Delete a project and its associated data |

#### Create project request

```json
{
  "name": "My Project",
  "description": "A sample project",
  "repo_url": "https://github.com/example/my-project.git",
  "repo_path": "/path/to/repo",
  "default_branch": "main"
}
```

#### Project response

```json
{
  "data": {
    "id": "uuid",
    "name": "My Project",
    "description": "A sample project",
    "repo_url": "https://github.com/example/my-project.git",
    "repo_path": "/path/to/repo",
    "default_branch": "main",
    "last_sync_at": null,
    "created_at": "2026-04-14T12:00:00Z",
    "updated_at": "2026-04-14T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

### Project Repo Mappings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/repo-mappings` | List mirror repo mappings for a project |
| POST | `/api/projects/:id/repo-mappings` | Add a mirror repo mapping |
| DELETE | `/api/repo-mappings/:id` | Delete a mirror repo mapping |

#### Create repo mapping request

```json
{
  "alias": "docs-repo",
  "repo_path": "/mirrors/agent-native-pm-docs",
  "default_branch": "main",
  "is_primary": false
}
```

#### Repo mapping response

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "alias": "docs-repo",
    "repo_path": "/mirrors/agent-native-pm-docs",
    "default_branch": "main",
    "is_primary": false,
    "created_at": "2026-04-15T12:00:00Z",
    "updated_at": "2026-04-15T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/tasks` | List tasks for a project |
| POST | `/api/projects/:id/tasks` | Create a task under a project |
| GET | `/api/tasks/:id` | Get a task by ID |
| PATCH | `/api/tasks/:id` | Update a task |
| DELETE | `/api/tasks/:id` | Delete a task |

#### Create task request

```json
{
  "title": "Implement login page",
  "description": "Build the login form with validation",
  "status": "todo",
  "priority": "high",
  "assignee": "agent:application-implementer",
  "source": "human"
}
```

#### Task response

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "title": "Implement login page",
    "description": "Build the login form with validation",
    "status": "todo",
    "priority": "high",
    "assignee": "agent:application-implementer",
    "source": "human",
    "created_at": "2026-04-14T12:00:00Z",
    "updated_at": "2026-04-14T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

### Documents

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/documents` | List documents for a project |
| POST | `/api/projects/:id/documents` | Register a document under a project |
| GET | `/api/documents/:id` | Get a document by ID |
| GET | `/api/documents/:id/content` | Read document content for in-app preview |
| PATCH | `/api/documents/:id` | Update document metadata |
| DELETE | `/api/documents/:id` | Delete a document registration |

#### Create document request

```json
{
  "title": "API Reference",
  "file_path": "docs/api-surface.md",
  "doc_type": "api",
  "source": "human"
}
```

#### Document response

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "title": "API Reference",
    "file_path": "docs/api-surface.md",
    "doc_type": "api",
    "last_updated_at": "2026-04-14T12:00:00Z",
    "staleness_days": 0,
    "is_stale": false,
    "source": "human",
    "created_at": "2026-04-14T12:00:00Z",
    "updated_at": "2026-04-14T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

#### Document content preview response

```json
{
  "data": {
    "path": "/repos/agent-native-pm/docs/api-surface.md",
    "language": "markdown",
    "content": "# API Surface\\n...",
    "size_bytes": 2048,
    "truncated": false
  },
  "error": null,
  "meta": null
}
```

Notes:
- `file_path` must be repo-relative (path traversal blocked).
- Secondary mirror repos use alias-prefixed file paths such as `docs-repo/docs/api-surface.md`.
- Preview payload may be truncated for large files.
- Source: `[agent:documentation-architect]`

### Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/summary` | Get current project summary (computed) |
| GET | `/api/projects/:id/summary/history` | Get summary snapshot history |

#### Summary response

```json
{
  "data": {
    "project_id": "uuid",
    "snapshot_date": "2026-04-14",
    "total_tasks": 15,
    "tasks_todo": 5,
    "tasks_in_progress": 3,
    "tasks_done": 6,
    "tasks_cancelled": 1,
    "total_documents": 8,
    "stale_documents": 2,
    "health_score": 0.72
  },
  "error": null,
  "meta": null
}
```

## Phase 2 endpoints (planned)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/projects/:id/sync` | Trigger a repo sync |
| GET | `/api/projects/:id/sync-runs` | List sync run history |
| GET | `/api/projects/:id/drift-signals` | List drift signals for a project |
| PATCH | `/api/drift-signals/:id` | Resolve or dismiss a drift signal |
| POST | `/api/agent-runs` | Log an agent activity run (requires `X-API-Key`) |
| GET | `/api/projects/:id/agent-runs` | List agent activity for a project |

## Phase 3 endpoints (planned)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/documents/:id/refresh-summary` | Agent triggers document summary refresh (requires `X-API-Key`) |
| GET | `/api/agent-runs/:id` | Get agent run details |
| PATCH | `/api/agent-runs/:id` | Update agent run status (`running`/`completed`/`failed`, requires `X-API-Key`) |

Notes:

- `POST /api/agent-runs` requires `X-API-Key` and accepts project-scoped keys.
- Project-scoped keys can only read/update agent runs in their own project.

## Phase 4 search filters

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/search` | Full-text search with optional filters |

Supported query parameters:

- `q` (required): full-text query term.
- `project_id` (optional): scope results to a project.
- `type` (optional): `all` (default), `tasks`, `documents`.
- `status` (optional): task status filter (`todo`, `in_progress`, `done`, `cancelled`).
- `doc_type` (optional): document type filter (`api`, `architecture`, `guide`, `adr`, `general`).
- `staleness` (optional): `all` (default), `stale`, `fresh`.

## Error codes

| HTTP Status | Meaning |
|-------------|---------|
| 200 | Success |
| 201 | Created |
| 400 | Bad request (validation error) |
| 404 | Resource not found |
| 409 | Conflict (duplicate resource) |
| 500 | Internal server error |

## Health score computation

The health score is a value between 0.0 and 1.0 computed as:

```
health_score = (task_completion_ratio * 0.7) + (document_freshness_ratio * 0.3)

task_completion_ratio = tasks_done / max(total_tasks - tasks_cancelled, 1)
document_freshness_ratio = (total_documents - stale_documents) / max(total_documents, 1)
```

A document is considered stale when `staleness_days > 30` (configurable in Phase 3).
