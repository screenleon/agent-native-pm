# Architecture Overview

## System Style

Agent Native PM is a modular monolith.

- Backend: single Go HTTP server
- Frontend: React + Vite static SPA
- Runtime database: PostgreSQL
- Background work: embedded goroutines in the same process

The system is optimized for local-first operation, Docker-based development, and repo-aware PM workflows rather than multi-service orchestration.

## High-Level Diagram

```text
+---------------------------+
| Browser / SPA             |
| React + Vite              |
+-------------+-------------+
              |
              | HTTP / JSON
              v
+-------------+-------------+
| Go API Server             |
|                           |
| projects                  |
| requirements              |
| tasks                     |
| documents                 |
| summaries                 |
| sync                      |
| drift                     |
| agent_runs                |
| planning lineage          |
| auth / users / sessions   |
| notifications             |
| search                    |
+-------------+-------------+
              |
              v
+-------------+-------------+
| PostgreSQL                |
| application tables        |
| full-text indexes         |
| summary snapshots         |
+---------------------------+

Repo inputs used by sync:

  1. project.repo_path
  2. project.repo_url -> managed clone cache
  3. project_repo_mappings -> primary + secondary mirrors
```

## Runtime Modes For Repository Access

The application supports three repo source patterns:

1. Direct local repo path via `projects.repo_path`
2. Managed clone / fetch via `projects.repo_url`
3. Mirror-based multi-repo mode via `project_repo_mappings`

Current preferred local workflow is mirror-based mapping because it allows sync to see unpushed local changes from mounted repos while keeping the app container isolated from arbitrary host paths.

### Branch Resolution Rules

Sync resolves branch in this order:

1. Primary repo mapping `default_branch`
2. Project `default_branch`
3. Detected repo default branch
4. Fallback to repository `HEAD` when detection is necessary

This is why branch repair UX now needs both project-level branch editing and repo-mapping branch editing.

## Main Modules And Responsibilities

| Module | Responsibility |
|--------|---------------|
| `projects` | CRUD for project metadata and repo source pointers |
| `requirements` | Requirement intake and draft planning inputs |
| `project_repo_mappings` | Primary / secondary repo binding, alias normalization, branch override support |
| `tasks` | Worklist CRUD, filtering, sorting, bulk update |
| `documents` | Document registry, preview, stale tracking |
| `document_links` | Maps documents to covered code paths |
| `sync` | Scans git history, records sync runs, surfaces branch resolution failures |
| `drift` | Creates and triages documentation drift signals |
| `summaries` | Computes project summary and dashboard-ready aggregates |
| `agent_runs` | Audit trail for agent work and idempotent run tracking |
| `local_connectors` / `connector_pairing_sessions` | User-owned local execution registration, pairing, and presence tracking |
| `planning_runs` / `backlog_candidates` / `task_lineage` | Planning orchestration state, candidate persistence, and traceability to created tasks |
| `auth` | Bootstrap registration, login, session issuance, current-user resolution |
| `api_keys` | Agent/API-key access for automation routes |
| `users` / `project_members` | Global and project-scoped access management |
| `notifications` | In-app notifications and unread counts |
| `search` | PostgreSQL full-text search across tasks and documents |

## Request / Auth Flow

1. Global middleware attaches session identity and API key identity to request context.
2. Protected routes require authenticated user context when auth middleware is enabled.
3. A small subset of automation routes require API key auth explicitly.
4. Admin-only routes use `RequireAdmin`.
5. Project-scoped API key routes validate project access before mutating data.

This preserves a single JSON API surface for both humans and agents.

## Main Product Flows

### Project Status / PM Loop

1. User creates or updates project metadata
2. User manages requirements, tasks, documents, and repo mappings
3. Dashboard summary aggregates task, sync, drift, and agent activity
4. Risk Inbox and project detail surfaces prioritize work

### Requirement Intake Loop

1. User creates a project-scoped requirement as a draft planning asset
2. Requirement becomes a stable record in PostgreSQL before any task is created
3. Later planning runs and backlog candidates can trace back to that requirement
4. Task creation from planning artifacts will use lineage records rather than direct free-form status text

### Local Connector Pairing Loop

1. Authenticated user creates one short-lived connector pairing session in the web app
2. A local connector claims that pairing code and receives a connector token
3. The connector uses heartbeat to refresh presence without becoming a general-purpose user session
4. Future execution slices can lease planning work to that connector without moving subscription credentials onto the server

### Sync And Drift Loop

1. Sync scans primary repo and any mapped mirror repos
2. Changed files are normalized into project-relative or alias-prefixed paths
3. Drift compares changed files against documents and document links
4. Drift signals are created or triaged
5. Summary / dashboard recomputes current project health

### Repo Repair Loop

1. Sync fails because configured branch cannot be resolved
2. Error message surfaces detected default branch hint
3. UI can apply the detected branch to the actual effective branch source
4. Sync reruns immediately from the failure card

## Data And Query Strategy

- PostgreSQL is the source of truth for runtime state
- Full-text search is implemented with PostgreSQL GIN indexes
- Drift metadata is stored in structured `JSONB`
- Summary snapshots are persisted for historical views, while dashboard summary is computed for current-state rendering
- Agent run files are stored as JSON text in the database and decoded in the model layer

## Frontend Architecture Notes

- The frontend is a static SPA built with Vite
- `ProjectList` acts as portfolio / inbox / roadmap command center
- `ProjectDetail` is the main workspace for tasks, documents, drift, sync, repo mappings, and dashboard cards
- Frontend relies on dashboard-summary rather than stitching many independent cards by hand where possible

## Current Boundaries

- Planning foundation now exists at the data and API layer via requirement intake and traceability tables
- Planning Workspace shell is now live inside `ProjectDetail`, with requirement intake, requirement queue, and a reserved planning-run slot
- Planning run orchestration records and draft candidate persistence are now live for selected requirements, each run emits a correlated internal `agent_runs` audit entry, candidate review happens in-place, and approved candidates now materialize into traceable tasks through candidate-scoped apply
- Local connector registration now exists as a separate control-plane module for future subscription-first execution, but planning-run dispatch to connectors is not implemented yet

## Near-Term Architectural Direction

The next architectural expansion is not another infrastructure tier. It is the UI and orchestration layer on top of the planning domain foundation and existing context sources:

- dashboard summary
- open tasks
- documents and document links
- sync status and sync runs
- drift signals
- recent agent activity

That Planning Workspace should remain draft-first, traceable, and human-approved before creating tasks.

Source: `[agent:documentation-architect]`, `[agent:backend-architect]`
