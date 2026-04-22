# Decisions — Archive

Archived decisions that are still architecturally in force but no longer
load-bearing for ongoing work (i.e. they are now baseline context reflected
in `ARCHITECTURE.md`, rules, and code rather than active constraints agents
need to re-evaluate on every task).

Move an entry here when:

- The decision has been fully absorbed into code, rules, or architecture docs.
- No current work is re-evaluating it.
- Its supersession chain (if any) is already summarised in the active
  `DECISIONS.md`.

Keep chronological order. Do not delete entries from this file — the point is
traceability.

---

## Archived on 2026-04-22

The following Phase-1 implementation decisions were archived as part of the
v1.post hardening PR. They continue to describe the system accurately and are
reflected in `ARCHITECTURE.md` / `project/project-manifest.md` / the codebase.

### 2026-04-14: Modular monolith architecture

- **Context**: Microservices would add operational complexity (multiple containers, service discovery, inter-service communication) without proportional benefit for a small-team tool.
- **Decision**: Single Go binary with internal module boundaries. Background jobs run as embedded goroutines.
- **Alternatives considered**: Separate API and worker containers — rejected for Phase 1 to minimize resource usage.
- **Constraints introduced**: Module boundaries must be enforced through Go package structure. No circular imports between top-level modules.

### 2026-04-14: Static frontend (React + Vite)

- **Context**: Next.js SSR adds a Node.js runtime in production, consuming memory for server-side rendering that this project does not need.
- **Decision**: Use React + Vite to produce a static SPA. Serve from the Go binary or a lightweight file server.
- **Alternatives considered**: Next.js — rejected due to runtime memory overhead. HTMX — rejected because the team is more productive with React and the dashboard requires rich client-side interactivity.
- **Constraints introduced**: No server-side rendering. All dynamic data comes from the JSON API.

### 2026-04-14: Drift detection as a core feature

- **Context**: The primary value proposition is knowing when documentation is out of sync with code, not just tracking tasks.
- **Decision**: Drift detection is a first-class module, not an afterthought. Every code change should be compared against the document registry.
- **Alternatives considered**: Manual doc update reminders — rejected because the whole point is automation.
- **Constraints introduced**: The `documents` table must track last-updated timestamps. The `drift` module must be able to correlate file paths from git changes to registered documents.

### 2026-04-14: Agent API uses the same HTTP endpoints as the frontend

- **Context**: Maintaining separate API surfaces for humans and agents doubles the contract surface and increases drift risk.
- **Decision**: Agents and the frontend use the same REST API. Agents authenticate via `X-API-Key` header; the frontend uses session cookies.
- **Alternatives considered**: Separate `/agent/` API namespace — rejected to avoid duplication.
- **Constraints introduced**: All API endpoints must return structured JSON. No HTML-rendering endpoints in the API router.

### 2026-04-14: Go Chi router for HTTP routing

- **Context**: Project manifest listed Chi/Echo as TBD for the HTTP framework.
- **Decision**: Use `go-chi/chi/v5` for HTTP routing.
- **Alternatives considered**: Echo — rejected because Chi is closer to the standard library (`net/http` compatible handlers) and has lower overhead.
- **Constraints introduced**: All handlers use `http.HandlerFunc` signature patterning for compatibility.

### 2026-04-14: Pure-Go SQLite driver (modernc.org/sqlite)

- **Context**: Need a SQLite driver for Go. `mattn/go-sqlite3` requires CGO and a C compiler.
- **Decision**: Use `modernc.org/sqlite` (pure Go, no CGO required).
- **Alternatives considered**: `mattn/go-sqlite3` — rejected because it complicates cross-compilation and Docker builds.
- **Constraints introduced**: CGO disabled in build (`CGO_ENABLED=0`). Some SQLite extensions may not be available.

### 2026-04-14: Unified auth context via middleware chain

- **Context**: Phase 4 introduces session auth for humans and Phase 3 introduces API key auth for agents; handlers need a single way to read caller identity.
- **Decision**: Apply session middleware first, API key middleware second, and store authenticated principal in request context under the shared `user` key.
- **Alternatives considered**: Separate route trees for human vs agent auth — rejected because it duplicates route wiring and increases drift risk.
- **Constraints introduced**: Protected API routes must rely on context identity (`RequireAuth`, `RequireAdmin`) rather than endpoint-specific credential parsing.

### 2026-04-14: Optional route registration for phased handlers

- **Context**: Existing Phase 1 handler tests construct router dependencies without Phase 2-4 handlers.
- **Decision**: Register Phase 2-4 routes conditionally when corresponding handlers are non-nil.
- **Alternatives considered**: Force tests to instantiate every new handler — rejected because it couples Phase 1 tests to unrelated subsystems.
- **Constraints introduced**: Router must guard route registration for optional handlers to avoid nil dereference during startup and tests.

### 2026-04-14: In-app document preview for registered project docs

- **Context**: Users need to inspect document content directly while managing tasks and drift, without leaving the PM system.
- **Decision**: Add `GET /api/documents/:id/content` and UI document preview modal in project detail.
- **Alternatives considered**: External editor-only workflow — rejected because it breaks PM flow context.
- **Constraints introduced**: `file_path` must remain repo-relative and content access must be constrained to the project repo root.
- **Source**: [agent:application-implementer]

### 2026-04-15: Managed repo cache for Docker-based sync

- **Context**: Docker Compose deployments previously required manual host volume mounts for every repository that needed git scanning, which blocked practical automation and forced operators to expose host paths into the app container.
- **Decision**: Add optional `repo_url` to projects and support managed clone/fetch behavior into a container-owned repo cache under `REPO_ROOT` (default `/app/data/repos`). Keep `repo_path` as a manual override and backward-compatible fallback.
- **Alternatives considered**: Continue requiring per-repo host mounts — rejected because it prevents self-service automation and increases host-path exposure.
- **Constraints introduced**: Managed clone mode currently relies on git-accessible remote URLs and does not provide first-class secret management for private repos. Private/manual cases may continue using direct `repo_path`.
- **Source**: [agent:application-implementer]

### 2026-04-15: Mirror-based multi-repo mappings for local-first sync

- **Context**: Managed clone mode does not reflect unpushed local working tree changes, and some projects need multiple repositories mounted into the app container at the same time.
- **Decision**: Add `project_repo_mappings` as a first-class project attachment model. Projects can bind one primary repo and multiple secondary mirror repos mounted read-only under `/mirrors/*`. Sync scans every mapped repo, and secondary repo paths are surfaced with alias prefixes such as `shared/pkg/helper.go`.
- **Alternatives considered**: Keep only `repo_url` managed clones — rejected because they hide local changes. Keep only a single `repo_path` — rejected because projects may span multiple repos.
- **Constraints introduced**: Non-primary mappings must use stable aliases. Documents and document links that target secondary repos must store alias-prefixed paths. `repo_url` managed clone mode remains as a fallback, but mirror mappings are the preferred Docker/local workflow.
- **Source**: [agent:documentation-architect]
