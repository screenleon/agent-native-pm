# Architecture Overview

## System style

Modular monolith — a single Go binary serves the HTTP API and runs background jobs. The React frontend is compiled to static assets and served by the same binary (or a lightweight reverse proxy in production).

## High-level diagram

```text
┌─────────────────────────────────────────────────┐
│                   Browser                       │
│              React + Vite (SPA)                 │
└────────────────────┬────────────────────────────┘
                     │ HTTP / JSON
┌────────────────────▼────────────────────────────┐
│                Go API Server                    │
│                                                 │
│  ┌───────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ projects  │ │  tasks   │ │  documents    │  │
│  └─────┬─────┘ └────┬─────┘ └──────┬────────┘  │
│        │             │              │           │
│  ┌─────▼─────┐ ┌────▼─────┐ ┌──────▼────────┐  │
│  │   sync    │ │agent_runs│ │    drift      │  │
│  └─────┬─────┘ └────┬─────┘ └──────┬────────┘  │
│        │             │              │           │
│  ┌─────▼─────────────▼──────────────▼────────┐  │
│  │              summaries                    │  │
│  └───────────────────┬───────────────────────┘  │
│                      │                          │
│  ┌───────────────────▼───────────────────────┐  │
│  │           SQLite (Phase 1)                │  │
│  │           PostgreSQL (Phase 4)            │  │
│  └───────────────────────────────────────────┘  │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │        Background Worker (embedded)       │  │
│  │  - repo scan                              │  │
│  │  - drift detection                        │  │
│  │  - summary generation                     │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

## Module responsibilities

| Module | Responsibility |
|--------|---------------|
| `projects` | CRUD for project entities; stores repo path, branch, last sync time |
| `project_repo_mappings` | Binds a project to one primary repo and optional secondary mirror repos via alias prefixes |
| `tasks` | Task lifecycle (create, update status, assign to human or agent) |
| `documents` | Document registry — tracks files, types, last update, staleness |
| `sync` | Scans local git repos and mirror mappings, extracts recent changes, identifies modified files |
| `agent_runs` | Logs agent activity (what agent did, which files affected, needs human review?) |
| `drift` | Compares code changes against document update timestamps; emits drift signals |
| `summaries` | Generates project/task/document summaries; stores daily snapshots |

## Data flow

1. User registers a project with a local repo path, mirror mappings, or a managed repo URL
2. `sync` module scans the primary repo and any mapped mirror repos (git log, file tree)
3. Secondary repo changes are normalized with alias-prefixed logical paths
4. `drift` module compares changed files against `documents` registry and `document_links`
5. Drift signals are created for documents that may be stale
6. Agent or human resolves drift by updating documents or dismissing signals
7. `summaries` module produces a status snapshot
8. Dashboard renders the current state

## Key design decisions

- **Embedded worker**: Background jobs run in-process using goroutines, not a separate service. This keeps deployment to a single binary.
- **SQLite first**: Reduces operational complexity. PostgreSQL is a Phase 4 upgrade for teams that need concurrent write throughput.
- **Static frontend**: React builds to `dist/` and is served as static files. No Node.js runtime in production.
- **Mirror-first local sync**: Docker deployments prefer read-only mirror mounts under `/mirrors/*` so sync can see unpushed local changes without exposing broad host paths.
- **No message queue**: Internal channels replace Redis/RabbitMQ for Phase 1-3. This is acceptable for single-instance deployment.
- **No MinIO**: File attachments are stored on local disk in Phase 1. S3-compatible storage is a Phase 4 consideration.
- **Source**: [agent:documentation-architect]

## Interfaces

- **HTTP API**: RESTful JSON API on port 18765 (see `docs/api-surface.md`)
- **Frontend**: SPA served from `/` path
- **Agent API**: Same HTTP API — agents authenticate via API key header

## Dependencies

| Dependency | Purpose | Phase |
|-----------|---------|-------|
| SQLite | Primary data store | 1 |
| PostgreSQL | Upgraded data store | 4 |
| Git CLI | Repo scanning | 2 |
| Docker | Deployment | 1 |
