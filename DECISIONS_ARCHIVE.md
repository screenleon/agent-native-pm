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

---

## Archived on 2026-04-27

## 2026-04-21: Four UI progressive-disclosure improvements [agent:application-implementer]

- **Context**: `SyncStatusPanel` always rendered as a full card regardless of whether attention was needed. The Settings tab rendered outside the rail layout, visually misaligned. The Planning intake form was always visible even when requirements already existed. The Drift Document Preview always rendered inline, adding noise to the detail panel.
- **Decision**:
  - (1) `SyncStatusPanel` is now collapsible. It auto-expands when action is needed (`!hasRepoSource || !latestSyncRun || latestSyncRun.status === 'failed' || canApplyDetectedBranchAndRerun`) and otherwise renders as a compact bar with a status badge, relative time, error hint, drift badge, Sync Now button, and a Details button. The expanded view retains all original content plus a Collapse button.
  - (2) The Settings tab content (Repo Mappings card) was relocated from above the rail layout to inside the rail content area, after the Agents tab block. It now renders correctly within the rail flow.
  - (3) The Planning Requirement Intake form uses sequential disclosure when requirements already exist: the form is hidden behind a `+ New Requirement` button (`showRequirementIntake` state, default false when requirements exist). After a successful create the form auto-collapses. When no requirements exist the form remains fully visible.
  - (4) The Drift detail Document Preview section now shows a toggle button (`Show Document Preview` / `Hide Preview`) instead of always rendering the `<pre>` block. The preview collapses automatically when the user selects a different drift signal.
- **Constraints introduced**: `showDriftPreview` and `showRequirementIntake` are local state in `ProjectDetail.tsx`; no state was lifted to parent/child components. `SyncStatusPanel` initializes `expanded` via a `useState` initializer function (runs once on mount, not reactively). If the conditions that would auto-expand change after mount (e.g., sync fails while the panel is already collapsed), the panel does not auto-re-expand — the user must click Details. This is intentional; re-expansion on state change would be disruptive.

## 2026-04-21: Server-side LLM provider must apply wire sanitizer + request body cap

- **Context**: `OpenAICompatibleProvider` built its outbound prompt directly from the internal `PlanningContext` via `compactX` helpers. `AgentRun.Summary` and `SyncRun.ErrorMessage` were truncated only by char count; `wire.RedactSecrets` (used on the local-connector path) was not applied. There was also no upper bound on the marshalled request body, so a pathological project with very large summaries could egress unbounded bytes to the configured remote endpoint. The local connector path (`BuildContextV1` → wire sanitizer → `ReduceSources` 256 KiB cap) was strictly safer than the server path that called the same model — an asymmetry that violated the "context.v1 is the single sanitization contract" intent of the 2026-04-20 sanitizer decision.
- **Decision**: (a) Export `wire.RedactSecrets` and `wire.TruncateRunes` so non-wire callers can apply the same v1 redaction without owning the regex set. (b) `OpenAICompatibleProvider.compactSyncRunForPrompt` and `compactAgentRunsForPrompt` now redact and truncate using those helpers (caps `wire.MaxSyncRunErrorChars` / `wire.MaxAgentRunSummaryChars`). (c) `OpenAICompatibleProvider.Generate` enforces `defaultOpenAICompatibleMaxRequestBytes = 256 KiB` on the marshalled request body and returns a typed error instead of egressing the over-cap payload. The cap mirrors `wire.DefaultMaxSourcesBytes` so server- and connector-path egress budgets stay aligned. (d) `ProjectContextBuilder.Build` no longer silently swallows store errors for documents/drift/sync/agent-runs; it logs and accumulates a per-source warning string. `BuildContextV1` propagates those warnings into `wire.PlanningContextMeta.Warnings`, giving adapters a deterministic degraded-mode signal. (e) Router CORS replaced the `AllowedOrigins:["*"] + AllowCredentials:true` combination (which browsers reject) with an env-driven allowlist (`CORS_ALLOWED_ORIGINS`) and safe localhost defaults; a literal `*` allowlist now disables credentialed CORS instead of silently breaking auth.
- **Alternatives considered**: (1) Promote `wire.PlanningContextV1` as the only input to providers (full Tier-2 refactor that would also let `Provider.Generate` accept `context.Context`) — deferred. The interface change rippled through 4 implementations and 5 tests for marginal additional safety on top of (a)–(c); it is recorded as the next step rather than blocking these fixes. (2) Leave silent error swallowing in the context builder and rely on logs alone — rejected; adapters need a structured signal to mark a recommendation as evidence-degraded.
- **Constraints introduced**: All new server-side LLM providers MUST sanitize free-form fields with `wire.RedactSecrets` before egress and MUST enforce a request body cap no larger than `wire.DefaultMaxSourcesBytes`. `wire.PlanningContextMeta.Warnings` is now part of the wire contract — adapters MUST tolerate the field but MAY ignore it. Production deployments MUST set `CORS_ALLOWED_ORIGINS` to the canonical UI host(s); leaving it unset preserves localhost-only behavior, which is unsafe for any non-development deployment. Reference adapters (`adapters/*.py`) are now committed with executable permission bits to avoid the exit-126 failure mode that surfaced when the connector serve loop tried to spawn them on a fresh checkout.

## 2026-04-20: Local connector planning runs emit in-app notifications; FE auto-refreshes the badge

- **Context**: The notification model, store, REST endpoints, and bell-badge UI were fully implemented, but no caller in the planning flow ever invoked `NotificationStore.Create`, and `App.tsx` only fetched the unread count once at bootstrap. End users running planning via a paired local connector therefore had no signal that a run finished unless they were already on the project page.
- **Decision**: When a local-connector planning run reaches a terminal state inside `LocalConnectorHandler.SubmitPlanningRunResult`, emit a best-effort notification scoped to the run's `requested_by_user_id` (falling back to the connector owner). Success uses `kind=info` with the candidate count and a deep link to `/projects/{project_id}`; failure uses `kind=error` with a truncated error message. Notification delivery never blocks run finalization — failures are logged and swallowed. On the frontend, `App.tsx` polls `getUnreadCount` every 20 s while the user is signed in, refreshes immediately on `visibilitychange`, and exposes a `anpm:refresh-notifications` window event that `ProjectDetail.tsx` dispatches the moment a watched run flips from active to terminal. The same transition surfaces a one-shot success/failure flash banner on the run card.
- **Alternatives considered**: (a) Server-Sent Events / WebSockets for push-based notifications — deferred; polling is sufficient for MVP and avoids a new transport layer. (b) Emit notifications inside `PlanningRunStore.CompleteLocalConnectorRun` to also cover server-provider runs uniformly — rejected for now; coupling persistence to side effects fights the layering and the server-provider path can be revisited when a parity gap actually shows up.
- **Constraints introduced**: Notification kind must remain in the `info | warning | error | drift | agent` enum; the helper currently uses `info`/`error`. The frontend custom event name `anpm:refresh-notifications` is a stable contract — any other page that wants to bump the unread badge must dispatch the same event.

## 2026-04-20: Local connector is user-scoped, serves all of a user's projects

- **Context**: Users asked whether a paired connector handles one project or many, and how to run concurrent planning runs across projects. The claim endpoint also previously dropped `planning_context` on its way into the adapter (a service-layer regression in `RunOnce`).
- **Decision**: A paired local connector is scoped to the owning user, not to a project. `LeaseNextLocalConnectorRun` already selects the oldest queued run across the user's entire account; this is affirmed as intentional. The `claim-next-run` response now also carries the owning `Project` (id, name, description) so adapters and connector logs can identify which project the current run belongs to. `Service.RunOnce` forwards both `Project` and `PlanningContext` into `ExecJSONInput`, fixing a latent bug that dropped the planning context.
- **Alternatives considered**: (a) Introduce per-connector project allowlists — rejected for MVP; adds schema + UX surface with no concrete use case yet. (b) Make the connector multiplex parallel runs — rejected; single-threaded FIFO keeps resource usage predictable on a developer laptop. Parallelism is achieved by pairing additional devices.
- **Constraints introduced**: Concurrent planning across projects on a single device is serialized (FIFO). Operators who need real parallelism must pair multiple devices, each running its own `bin/anpm-connector serve`. Docker-compose is supported for the server but the connector intentionally runs on the host where the agent CLI is authenticated (e.g. where `claude login` has stored credentials).

## 2026-04-20: Ship reference `adapters/backlog_adapter.py` for local connector

- **Context**: The local connector speaks the `exec-json` contract, but operators had nothing concrete to plug into `--adapter-command`. Users cannot evaluate the end-to-end loop without building their own adapter.
- **Decision**: Ship `adapters/backlog_adapter.py` — a Python 3 reference adapter that reads the `exec-json` request (including `planning_context`), shells out to the Claude Code CLI (default) or Codex CLI (`ANPM_ADAPTER_AGENT=codex`), and parses ranked backlog candidates from a fenced JSON code block. User-supplied adapters remain fully supported as long as they honor the same stdin/stdout contract.
- **Alternatives considered**: (a) Ship a Go-based adapter binary — rejected; Python keeps the reference implementation easy to fork and read, and the contract is language-agnostic. (b) Build an HTTP-based adapter calling OpenAI-compatible endpoints — rejected for v1 because Claude/Codex CLIs already own auth + model selection on the operator's machine, avoiding a second credential surface.
- **Constraints introduced**: Adapter output is normalized before reaching the server: `priority_score`/`confidence` clamped to `[0,1]`, title truncated to 120 chars, evidence ids coerced to strings, errors surfaced via `error_message` with exit code 0. Frontend `ProjectDetail.tsx` auto-polls every 3 s while a planning run is `queued`/`leased`/`running` so connector results surface without manual reload.

## 2026-04-20: Adopt `context.v1` as local connector planning context contract

- **Context**: Local connector adapters (`exec-json`) currently receive only `{run, requirement}` and cannot produce high-quality backlog candidates grounded in project state. The MVP core capability "agent auto-decomposes requirements into backlog" requires structured project context.
- **Decision**: Introduce a versioned `planning_context` payload (`schema_version: "context.v1"`) attached to `POST /api/connector/claim-next-run` responses and forwarded through adapter stdin. Source of truth: `docs/local-connector-context.md`.
- **Alternatives considered**: (a) Re-use `PlanningContext` directly as the wire type — rejected because it would pull server-only planning code into the connector binary. (b) Let adapters query server APIs for context — rejected because adapters are external processes without session tokens.
- **Constraints introduced**: Wire DTOs must live in a leaf package (`backend/internal/planning/wire`) that imports only `models`; `planning` and `connector` both import `wire`, never each other. Adapters MUST ignore unknown fields and MUST treat missing `planning_context` as degraded-but-OK mode.

## 2026-04-20: Metadata-only documents in local connector context

- **Context**: Document bodies can be large, may contain sensitive content, and are not consistently useful to backlog decomposition.
- **Decision**: Phase A of `context.v1` sends documents as metadata only (title, file_path, doc_type, is_stale, staleness_days) — matching existing `compactDocumentsForPrompt` in `openai_compatible_provider.go`. Body transmission is deferred to a future opt-in design.
- **Alternatives considered**: Send full bodies with size cap — rejected; cap alone does not address sensitivity and regresses relative to current server-side provider behavior.
- **Constraints introduced**: Adapter-generated backlog candidates must rely on title + path + staleness to cite documents as evidence. File-path-based reading is the adapter's own responsibility if it has filesystem access.

## 2026-04-20: Context sanitizer v1 scope and excluded bare hex regex

- **Context**: Free-form strings in `AgentRun.Summary` and `SyncRun.ErrorMessage` occasionally contain secret-shaped substrings (API keys, bearer tokens, basic-auth URLs). Earlier plan draft included a bare `\b[A-Fa-f0-9]{32,}\b` regex that would destroy 40-char git commit SHAs and legitimate hashes in diagnostic text.
- **Decision**: Phase A sanitizer scope limited to `AgentRun.Summary` and `SyncRun.ErrorMessage`. Regex set is prefix-anchored: OpenAI `sk-…`, AWS `AKIA…`, PEM headers, `bearer <token>` (≥16 chars), basic-auth URLs, labeled secrets (`password=`, `token:`, `api_key=`), `sha256:` labeled hashes, `Authorization:` header dumps. Bare hex regex is explicitly excluded. Sanitizer version constant: `"v1"`.
- **Alternatives considered**: Aggressive entropy-based redaction — rejected for false-positive rate. No sanitizer — rejected because `AgentRun.Summary` is agent-generated and known to occasionally leak auth errors verbatim.
- **Constraints introduced**: Any change to the regex set requires a sanitizer version bump and a new DECISIONS.md entry.

## 2026-04-20: Connector context byte cap applies to `sources` only

- **Context**: `planning_context` has scaffolding (schema_version, limits, meta) plus `sources`. Applying a single cap to the whole payload creates pathological cases where scaffolding overhead alone exceeds the cap.
- **Decision**: `max_sources_bytes` (256 KiB default) applies only to the marshaled `sources` object. Scaffolding and envelope are excluded from the cap. `meta.sources_bytes` records the final size. Reducer drops lowest-rank items from the largest-in-bytes source, re-measured each round.
- **Alternatives considered**: Cap on full payload — rejected per above. No cap — rejected because a runaway project with thousands of drift signals could produce multi-megabyte payloads and break adapter stdin.
- **Constraints introduced**: Adapters must be prepared to receive `dropped_counts` > 0 and cannot rely on "all open drift signals" being present.

## 2026-04-14: Use SQLite as Phase 1 data store

- **Context**: Need a lightweight database that avoids extra containers and keeps RAM usage low.
- **Decision**: Use SQLite for Phase 1-3. Migrate to PostgreSQL in Phase 4 if concurrent write throughput becomes a bottleneck.
- **Alternatives considered**: PostgreSQL from day one — rejected because it adds a container and ~200MB RAM for a system that initially serves a single user or small team.
- **Constraints introduced**: All SQL must be compatible with SQLite. Use `database/sql` with a driver that supports both SQLite and PostgreSQL to ease future migration.

## 2026-04-14: Move backend runtime to PostgreSQL now

- **Context**: The project already reached Phase 4 capabilities (sessions, RBAC, full-text search, agent lifecycle) and now needs production-aligned behavior for concurrent access and reliable full-text querying.
- **Decision**: Use PostgreSQL as the backend runtime database now, including local Docker Compose development. Migrations and runtime SQL use PostgreSQL semantics (`$N` placeholders, `BOOLEAN`, `TIMESTAMPTZ`, Postgres full-text search).
- **Alternatives considered**: Keep SQLite through additional phases — rejected because it complicates correctness for search and boolean handling while increasing migration risk later.
- **Supersedes**: This decision supersedes the earlier "Use SQLite as Phase 1 data store" decision for active runtime defaults.
- **Constraints introduced**: Docker runtime requires a PostgreSQL service and `DATABASE_URL`. Data reset/re-seeding is required when moving existing local SQLite state.

## 2026-04-14: Scrum-first backlog-before-implementation workflow

- **Context**: Implementation often started before clear backlog capture and prioritization, causing requirement backfill after coding.
- **Decision**: Enforce a Scrum-first execution order: discover, triage, check decisions, capture backlog, prioritize backlog, then implement.
- **Alternatives considered**: Implementation-first with post-hoc planning — rejected due to rework and unclear priorities.
- **Constraints introduced**: Tasks are not considered implementation-ready until backlog items and acceptance criteria are explicitly recorded.
- **Source**: [agent:documentation-architect]

## 2026-04-17: Apply approved planning output at candidate scope
- **Context**: Phase 2 planning review persists multiple backlog candidates per requirement and per planning run. A requirement-scoped apply contract would mix one-to-many planning state with a bulk side effect before the aggregate rules are settled.
- **Decision**: Apply approved planning output with `POST /api/backlog-candidates/:id/apply`. The operation creates at most one task, writes one `task_lineage` record, marks that candidate `applied`, and is idempotent for retries of the same candidate.
- **Alternatives considered**: `POST /api/requirements/:id/apply` bulk apply — rejected because it couples candidate review state to requirement-wide mutation too early. Auto-promote requirement status to `planned` on first apply — rejected because a requirement may have multiple candidates or multiple planning runs and the aggregate rule is not yet defined.
- **Constraints introduced**: Only `approved` candidates may be applied. Duplicate open tasks are blocked by normalized-title conflict detection within the project. Requirement status remains unchanged during candidate apply until a separate aggregate rule is introduced.
## 2026-04-17: Real-model planning uses one OpenAI-compatible provider seam
- **Context**: Planning provider selection already exists in the UI and backend registry, but only a deterministic in-process implementation was available. The system needs a minimal path to use a real model without hard-coding one vendor SDK per provider.
- **Decision**: Add one optional `openai-compatible` planning provider configured by environment variables (`base URL`, `API key`, model list, timeout). The remote model generates draft content only; the server still owns ranking, scores, confidence, duplicate detection, and typed evidence detail.
- **Alternatives considered**: Vendor-specific SDK integrations first — rejected because they increase surface area and coupling before the generic provider seam is proven. Let the model own ranking and evidence — rejected because it weakens reproducibility and breaks current review semantics.
- **Constraints introduced**: Startup must fail fast if `openai-compatible` is selected as the default provider but is not fully enabled. Remote calls must be bounded by timeout and response size. Planning documentation must disclose external context egress when the remote provider is used.
- **Source**: [agent:backend-architect]
## 2026-04-17: Centralize planning model configuration inside the app
- **Context**: The first real-model slice used deploy-time environment variables for planning provider configuration, but the required workflow is closer to OpenCode: an admin configures model/provider details once in the product, and later agent-backed features consume that saved configuration automatically.
- **Decision**: Move planning provider configuration into one admin-managed singleton settings record stored in PostgreSQL. New planning runs always resolve provider/model from this central saved configuration. Keep only `APP_SETTINGS_MASTER_KEY` in environment variables so provider API keys can be encrypted at rest.
- **Alternatives considered**: Keep provider configuration in env vars — rejected because it hard-codes operational details outside the product and prevents the intended setup flow. Keep per-run provider/model overrides — rejected because the desired behavior is central configuration first, then use. Add per-project model settings in v1 — rejected because it increases secret duplication and authorization complexity before the global flow is proven.
- **Constraints introduced**: `GET` and `PATCH /api/settings/planning` must be admin-only. Stored provider API keys must never be returned by API responses and must be encrypted at rest. No saved settings row means deterministic planning remains the default. If saved remote settings are invalid at runtime, the run must fail rather than silently downgrading to a different provider.
- **Supersedes**: This decision supersedes the earlier environment-variable-based planning provider configuration decision for active runtime behavior.
## 2026-04-17: Personal account bindings alongside shared planning settings

- **Context**: The centralized planning settings singleton only supports one shared API key configured by an admin. Users with personal credentials (or local providers like Ollama that need no API key) cannot bind their own configuration. The project owner has no API keys available — only subscription accounts — so testing requires a no-API-key path (Ollama).
- **Decision**: Add an `account_bindings` table for per-user credential binding. Extend `planning_settings` with a `credential_mode` column (`shared`, `personal_preferred`, `personal_required`). Credential resolution: personal binding → shared settings → deterministic fallback. Personal API keys use the same `secrets.Box` encryption. CLI bridge for subscription-only accounts (Copilot, ChatGPT desktop) is deferred to a separate design requiring client-side architecture.
- **Alternatives considered**: Separate provider types per subscription vendor — rejected because subscription logins are not programmatically accessible from a server. Per-project credentials — rejected as premature complexity before global personal bindings are proven. Only support Ollama testing — rejected because the system needs a proper multi-credential architecture regardless.
- **Constraints introduced**: Personal bindings are user-scoped; admins cannot read other users' plaintext keys. `credential_mode` is global for v1 (not per-project). The existing singleton `planning_settings` row remains the workspace default and is not replaced. Credential resolution must log which binding was used (personal vs shared) in the planning run audit trail.
- **See also**: `docs/credential-binding-design.md`
- **Source**: [agent:feature-planner]
## 2026-04-17: Subscription path starts with local connector pairing and registry
- **Context**: The project owner is single-user, self-hosting, and has subscription-based model access but no API key. Server-side provider resolution and personal account bindings are insufficient because the server still cannot directly reuse subscription sessions. A practical execution path needs a client-side control boundary before any connector-dispatched planning work can exist.
- **Decision**: Start the subscription path with a minimal local connector control-plane slice: `local_connectors`, `connector_pairing_sessions`, authenticated user-facing pairing-session creation, connector claim, connector heartbeat, and connector revoke. Use short-lived pairing codes and distinct connector tokens. Defer planning-run dispatch, lease state, and vendor-specific subscription adapters to later slices.
- **Alternatives considered**: Keep pushing users toward account bindings and local OpenAI-compatible presets only — rejected because it does not solve the subscription-only use case. Add a full connector dispatch system immediately — rejected because it expands scope before pairing and registry are proven. Reuse bearer session tokens for connectors — rejected because connector identity must remain separate from user sessions.
- **Constraints introduced**: Pairing codes are stored only as hashes and must be single-use with short TTL. Connector presence uses `X-Connector-Token`, not user bearer auth. Batch 1 does not promise subscription execution yet; it only establishes the control-plane seam required for later dispatch.
- **Source**: [agent:documentation-architect]

