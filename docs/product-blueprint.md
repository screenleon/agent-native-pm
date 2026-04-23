# Product Blueprint — Agent Native PM

## Vision

A lightweight project tracker that understands your code repository and keeps documentation in sync — built for developers who work with AI agents.

## Problem statement

Existing project management tools (Jira, Plane, Linear) are designed for humans to manually create and update work items. When AI agents participate in development:

1. Documentation falls behind code changes because no system detects the drift.
2. Agents have no structured way to report what they did or update project state.
3. Status dashboards reflect what humans remembered to update, not the actual repository state.
4. Full-featured PM tools consume 4-8 GB RAM for features most small teams never use.

## Product definition (one sentence)

A tool that derives project status from git repositories and agent activity, detects documentation drift, and provides a lightweight API for agents to update project state.

## Target users

1. Solo developers or small teams (1-10 people)
2. Teams actively using AI coding agents
3. Teams that maintain documentation alongside code and want to keep it current

## MVP scope (Phase 1)

Phase 1 shipped. The original Phase 1 checklist used to live in `docs/mvp-scope.md`; that file is now a pointer to current scope sources. For the current authoritative scope, consult `DECISIONS.md`, `docs/api-surface.md`, and `docs/data-model.md`.

Summary of the original Phase 1 deliverables (for historical context):
- Project CRUD with repo path registration
- Task board with status lifecycle
- Document registry with staleness tracking
- Manual sync trigger (scan repo)
- Basic dashboard showing project health
- PostgreSQL runtime storage
- Docker Compose deployment

## Non-goals (all phases)

1. Real-time multi-user collaborative editing (Google Docs style)
2. SaaS multi-tenancy with billing
3. Plugin/marketplace ecosystem
4. Mobile native apps
5. Full Jira feature parity (custom workflows, advanced reporting, portfolio management)

## Core workflows

### Workflow 1: Register and scan a project

1. User creates a project, provides local repo path and default branch
2. User triggers sync
3. System scans git log, identifies recently changed files
4. System creates/updates document registry entries
5. Dashboard shows project summary

### Workflow 2: Detect documentation drift

1. Sync identifies code files that changed since last scan
2. Drift module checks if related documents were also updated
3. If not, a drift signal is created
4. Dashboard highlights stale documents
5. Agent or human resolves the drift

### Workflow 3: Agent updates project state

1. Agent completes a coding task
2. Agent calls `POST /api/agent-runs` with a summary of changes
3. Agent calls `PATCH /api/tasks/:id` to update task status
4. Agent calls `POST /api/documents/:id/refresh-summary` if docs were updated
5. System records the activity and recalculates project summary

### Workflow 4: Daily status check

1. User opens the dashboard
2. Sees: open tasks, recent agent activity, drift signals, project health score
3. Clicks into drift signals to review which docs need attention
4. Resolves or delegates drift items

## System architecture

See `ARCHITECTURE.md` for the full diagram.

Summary: modular monolith in Go, React static SPA, PostgreSQL, embedded background worker.

## Data model

See `docs/data-model.md` for the full schema.

Core entities: projects, tasks, documents, document_links, sync_runs, agent_runs, drift_signals, summary_snapshots.

## API surface

See `docs/api-surface.md` for the full endpoint list.

## Agent integration strategy

Agents interact with the system through the same REST API as the frontend. Key design choices:

1. **Authentication**: API key via `X-API-Key` header
2. **Idempotency**: Agent run creation uses idempotency keys to prevent duplicates
3. **Source marking**: All agent-created content includes a `source` field identifying the agent
4. **Drift resolution**: Agents can dismiss drift signals or update documents to resolve them

## Deployment

Phase 1 target:

```yaml
services:
  app:
    build: .
    ports:
      - "18765:18765"
    volumes:
      - ./data:/app/data
      - /path/to/repos:/repos   # Local repos to scan
    environment:
      - DATABASE_URL=postgres://anpm:anpm@db:5432/anpm?sslmode=disable
```

Estimated resource usage:
- RAM: 200-500 MB
- CPU: 1 core sufficient
- Disk: < 100 MB for application

## Phase roadmap

### Phase 1: Core CRUD + Dashboard (weeks 1-2)

- Project, task, document CRUD
- PostgreSQL schema and forward-only migrations
- Basic REST API
- React dashboard with project list, task board, document list
- Docker Compose setup
- Manual sync placeholder (no git scanning yet)

### Phase 2: Repo sync + Drift detection (weeks 3-4)

- Git repository scanning (git log, file tree)
- Document-to-code file linking
- Drift signal generation
- Agent activity logging API
- Dashboard: drift signals panel, agent activity feed

### Phase 3: Agent integration (weeks 5-6)

- API key authentication
- Agent run lifecycle (create, update, complete)
- Document summary refresh via API
- Rule-based drift detection (configurable thresholds)
- Summary snapshot generation

### Phase 4: Collaboration (weeks 7+)

- User authentication (session-based)
- Role-based access (admin, member, viewer)
- Search and filtering
- Notifications (in-app, optional email)
- PostgreSQL runtime hardening and operational polish
- S3-compatible file storage (optional)

## Risks and tradeoffs

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| PostgreSQL query or index regression under concurrent agents | Medium | Medium | Keep schema docs aligned, preserve indexes, and validate with integration tests |
| Git scanning performance on large repos | Low | Medium | Limit scan depth; use `--since` flag |
| Drift detection false positives | High | Low | Allow manual dismissal; tune sensitivity |
| Scope creep toward full PM system | High | High | Enforce MVP scope via `docs/mvp-scope.md`; record scope decisions in `DECISIONS.md` |
| Agent API misuse (stale or incorrect updates) | Medium | Medium | Idempotency keys; source tracking; human review flag |
