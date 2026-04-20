# MVP Scope — Historical Phase 1 Baseline

> ⚠️ **HISTORICAL DOCUMENT — DO NOT USE FOR CURRENT PLANNING.**
>
> This file captures the original Phase 1 scope. Implementation has advanced
> well beyond it (planning runs, local connectors, account bindings, drift
> signals, notifications, etc. are all live).
>
> **Sources of truth for current state**: `DECISIONS.md`,
> `docs/api-surface.md`, `docs/data-model.md`, `docs/product-blueprint.md`,
> and any open backlog priority planning notes.
>
> Use this file only as historical context when reasoning about why the
> early code paths look the way they do. Do not gate new feature work on
> what is or is not listed here.

Current implementation has advanced beyond this file's original Phase 1 scope. Use `.backlog.priority-planning.md`, `DECISIONS.md`, `docs/api-surface.md`, and `docs/data-model.md` as the source of truth for current-state planning and runtime behavior.

This file is the source of truth for what is in and out of scope for Phase 1 (Core CRUD + Dashboard). Consult this file when evaluating whether a feature belongs in the current development phase.

## In scope

### Backend (Go)

- [ ] Project CRUD — create, read, update, delete projects
- [ ] Task CRUD — create, read, update status, assign, delete tasks
- [ ] Document registry CRUD — register documents, update metadata, track staleness
- [ ] Summary generation — compute project health from tasks and documents
- [ ] PostgreSQL storage with forward-only numbered migrations
- [ ] REST API with JSON envelope (`{ data, error, meta }`)
- [ ] Basic error handling and input validation
- [ ] Docker Compose deployment (single container)
- [ ] Health check endpoint (`GET /api/health`)
- [ ] Configuration via environment variables

### Frontend (React + Vite)

- [ ] Project list page — show all projects with health summary
- [ ] Project detail page — show tasks, documents, and summary for a project
- [ ] Task board — kanban or list view with status columns (todo, in_progress, done)
- [ ] Document list — show registered documents with staleness indicator
- [ ] Create/edit forms for projects, tasks, documents
- [ ] Basic navigation (router)
- [ ] API client layer (fetch wrapper)
- [ ] Loading, empty, and error states for all data views

### Infrastructure

- [ ] `Dockerfile` — multi-stage build (Go binary + frontend static assets)
- [ ] `docker-compose.yml` — single service with volume mounts
- [ ] `Makefile` — build, test, lint, dev targets
- [ ] PostgreSQL service configured through Docker Compose and `DATABASE_URL`

## Out of scope (Phase 1)

These features are planned for later phases. Do not implement them in Phase 1.

| Feature | Target phase | Reason for deferral |
|---------|-------------|-------------------|
| Git repository scanning | Phase 2 | Requires git CLI integration and file-tree diffing |
| Drift detection | Phase 2 | Depends on repo sync and document-code linking |
| Agent activity logging (`agent_runs`) | Phase 2 | Useful only after sync and drift are working |
| API key authentication | Phase 3 | Phase 1 is single-user local deployment |
| Agent run lifecycle API | Phase 3 | Depends on Phase 2 agent logging |
| Document summary refresh via API | Phase 3 | Agent-specific feature |
| User authentication (sessions) | Phase 4 | Phase 1-3 are single-user |
| Role-based access control | Phase 4 | Depends on user auth |
| Historical SQLite-to-PostgreSQL migration planning | Superseded | PostgreSQL is already the active runtime database |
| Search and filtering | Phase 4 | Nice-to-have, not core |
| Notifications | Phase 4 | Requires user auth and preferences |
| S3-compatible file storage | Phase 4 | Local disk is fine for Phase 1-3 |

## Acceptance criteria for Phase 1 completion

1. User can create a project with a name, description, and local repo path
2. User can create tasks under a project and move them through status lifecycle
3. User can register documents under a project and see staleness indicators
4. Dashboard shows project health summary (task counts by status, document freshness)
5. All CRUD operations work through the REST API
6. Frontend renders all views with proper loading/error states
7. Application runs in Docker Compose with `docker compose up --build`
8. RAM usage stays under 500 MB in idle state
9. `make test` passes with >80% coverage on business logic
10. `make lint` passes clean

## Data model reference

See `docs/data-model.md` for the Phase 1 database schema.

## API reference

See `docs/api-surface.md` for the Phase 1 endpoint list.
