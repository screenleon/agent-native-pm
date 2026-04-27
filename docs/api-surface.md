# API Surface - Agent Native PM

This file is the canonical API contract reference for the current deployed backend. Update this file whenever endpoints are added, removed, or behavior changes.

## Conventions

- Base path: `/api`
- Content type: `application/json`
- Response envelope: `{ "data": <payload>, "error": <string|null>, "meta": <object|null>, "warnings"?: [...] }`
- Error responses: `{ "data": null, "error": "<message>", "meta": null }`
- Warnings (Path B Slice S2): some 2xx endpoints attach an optional `warnings` array of advisory codes that do not fail the request. Each entry is `{ code, message?, details? }`. Documented codes today: `connector_outdated` (planning-run create with a CLI binding when the user's connector reports `protocol_version < 1`), `stale_cli_health` (planning-run create when the picked binding's last health probe is more than 2× the probe interval old; reserved for S5b — not emitted yet in S2). The field is `omitempty`, so endpoints that do not produce warnings remain byte-identical to the pre-S2 envelope.
- Pagination: `?page=1&per_page=20` and `meta = { "page": 1, "per_page": 20, "total": 42 }`
- IDs: UUID v4 strings
- Timestamps: ISO 8601 UTC

## Authentication And Authorization

- Public bootstrap/auth routes:
  - `GET /api/auth/needs-setup`
  - `POST /api/auth/register`
  - `POST /api/auth/login`
  - `POST /api/auth/logout`
  - `GET /api/auth/me` returns `401` when not authenticated
- Human-facing application routes require authenticated user context when auth middleware is enabled. Session cookie and Bearer token are both accepted by the current auth flow.
- Admin-only routes:
  - `GET /api/users`
  - `PATCH /api/users/:id`
- API key protected routes:
  - `POST /api/agent-runs`
  - `PATCH /api/agent-runs/:id`
  - `POST /api/documents/:id/refresh-summary`
- Project-scoped API keys are limited to the allowed project. Human-authenticated requests use the same JSON endpoints as agent requests.

## CORS

- Allowed origins are driven by the `CORS_ALLOWED_ORIGINS` environment variable (comma-separated). Default for unset is the developer set: `http://localhost:5173, http://localhost:18765, http://127.0.0.1:5173, http://127.0.0.1:18765`.
- Production deployments MUST set `CORS_ALLOWED_ORIGINS` to the canonical UI host(s); leaving it unset is unsafe outside local development.
- A literal `*` in the allowlist disables credentialed CORS (`AllowCredentials: false`) because browsers reject the wildcard + credentials combination. Provide explicit origins when cookies/Authorization headers are required.
- Allowed methods: `GET, POST, PATCH, PUT, DELETE, OPTIONS`. Allowed headers include `Authorization`, `Content-Type`, `X-API-Key`. See DECISIONS 2026-04-21.

## Current Endpoint Inventory

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check. Returns `{ "status": "ok" }`. |

### Auth

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/auth/needs-setup` | Returns whether bootstrap registration is still allowed |
| POST | `/api/auth/register` | Bootstrap-only registration. First user becomes admin |
| POST | `/api/auth/login` | Create session and return `{ token, user }` |
| POST | `/api/auth/logout` | Delete current session / bearer token |
| GET | `/api/auth/me` | Return current authenticated user |

#### Register request

```json
{
  "username": "screenleon",
  "email": "owner@example.com",
  "password": "strong-password",
  "role": "member"
}
```

Notes:

- `role` defaults to `member`, but the first user is automatically promoted to `admin`.
- Open registration is blocked after setup is complete.
- Password must be at least 8 characters.

#### Login response

```json
{
  "data": {
    "token": "session-token",
    "user": {
      "id": "uuid",
      "username": "screenleon",
      "email": "owner@example.com",
      "role": "admin",
      "is_active": true,
      "created_at": "2026-04-16T12:00:00Z",
      "updated_at": "2026-04-16T12:00:00Z"
    }
  },
  "error": null,
  "meta": null
}
```

### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/users` | Admin-only list of users |
| PATCH | `/api/users/:id` | Admin-only update of email, role, or active state |

### Projects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects` | List all projects |
| POST | `/api/projects` | Create a project |
| GET | `/api/projects/:id` | Get a project by ID |
| PATCH | `/api/projects/:id` | Update a project |
| DELETE | `/api/projects/:id` | Delete a project and associated data |

#### Create project request

```json
{
  "name": "Agent Native PM",
  "description": "Planning workspace",
  "repo_url": "https://github.com/example/agent-native-pm.git",
  "repo_path": "/workspace/agent-native-pm",
  "default_branch": "",
  "initial_repo_mapping": {
    "alias": "app",
    "repo_path": "/mirrors/agent-native-pm",
    "default_branch": "",
    "is_primary": true
  }
}
```

Notes:

- `name` is required.
- `default_branch` may be an empty string. Empty means fall back to detected default branch during sync.
- `initial_repo_mapping` is optional. When provided, it is normalized as the primary mapping during project creation.

#### Project response

```json
{
  "data": {
    "id": "uuid",
    "name": "Agent Native PM",
    "description": "Planning workspace",
    "repo_url": "https://github.com/example/agent-native-pm.git",
    "repo_path": "/mirrors/agent-native-pm",
    "default_branch": "",
    "last_sync_at": null,
    "created_at": "2026-04-16T12:00:00Z",
    "updated_at": "2026-04-16T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

### Project Repo Mappings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/repo-mappings/discover` | Discover mounted mirror repos. Optional `project_id` query |
| GET | `/api/projects/:id/repo-mappings` | List repo mappings for a project |
| POST | `/api/projects/:id/repo-mappings` | Create a repo mapping |
| PATCH | `/api/repo-mappings/:id` | Update repo mapping branch override |
| DELETE | `/api/repo-mappings/:id` | Delete a repo mapping |

#### Create repo mapping request

```json
{
  "alias": "docs-repo",
  "repo_path": "/mirrors/agent-native-pm-docs",
  "default_branch": "main",
  "is_primary": false
}
```

#### Update repo mapping request

```json
{
  "default_branch": "review/risk-git-fixes"
}
```

Notes:

- Sending `"default_branch": ""` clears the mapping-level override so sync falls back to project branch or branch auto-detect.
- Primary repo mapping branch wins over the project fallback branch during sync.

#### Discover mirror repos response

```json
{
  "data": {
    "mirror_root": "/mirrors",
    "repos": [
      {
        "repo_name": "agent-native-pm",
        "repo_path": "/mirrors/agent-native-pm",
        "suggested_alias": "agent-native-pm",
        "detected_default_branch": "review/risk-git-fixes",
        "is_mapped_to_project": true,
        "is_primary_for_project": true
      }
    ]
  },
  "error": null,
  "meta": null
}
```

### Requirements

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/requirements` | List requirements for a project |
| POST | `/api/projects/:id/requirements` | Create a requirement under a project |
| GET | `/api/requirements/:id` | Get a requirement by ID |

#### Create requirement request

```json
{
  "title": "Add planning intake foundation",
  "summary": "Capture requirements before creating tasks",
  "description": "Users should be able to submit a requirement in Project Detail before the system decomposes it into candidate backlog items.",
  "source": "human",
  "audience": "Solo devs",
  "success_criteria": "Backlog candidates are generated within 60 seconds."
}
```

Notes:

- `title` is required and is trimmed before validation.
- `status` is always initialized to `draft` on create.
- `source` defaults to `human` when omitted or blank.
- `audience` and `success_criteria` are optional free-text fields (Phase 6a A3). When non-empty they are injected into the backlog planner prompt as `AUDIENCE_LINE` and `SUCCESS_LINE` template vars.
- Requirement intake is additive only in this slice: there is no update, delete, or archive endpoint yet.

#### Requirement response

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "title": "Add planning intake foundation",
    "summary": "Capture requirements before creating tasks",
    "description": "Users should be able to submit a requirement in Project Detail before the system decomposes it into candidate backlog items.",
    "status": "draft",
    "source": "human",
    "audience": "Solo devs",
    "success_criteria": "Backlog candidates are generated within 60 seconds.",
    "created_at": "2026-04-16T15:00:00Z",
    "updated_at": "2026-04-16T15:00:00Z"
  },
  "error": null,
  "meta": null
}
```

Notes:
- `audience` and `success_criteria` are omitted from the JSON response when empty (`omitempty`).

#### List requirements query parameters

- `page`, `per_page`: pagination

Behavior:

- Default order is newest-first by `created_at`.
- Empty results return `[]`, not `null`.
- List and create routes are project-scoped and live in the same authenticated route group as projects and tasks.

### Planning Runs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/planning-provider-options` | Resolve the current caller's planning execution preview and available models |
| POST | `/api/projects/:id/demo-seed` | Seed an empty project with one demo requirement + planning run + three draft candidates |
| POST | `/api/requirements/:id/planning-runs` | Start a planning run for a requirement |
| GET | `/api/requirements/:id/planning-runs` | List planning runs for a requirement |
| GET | `/api/planning-runs/:id` | Get a planning run by ID |
| GET | `/api/planning-runs/:id/context-snapshot` | Get the V2 context snapshot for a planning run (Phase 3B PR-2) |
| GET | `/api/planning-runs/:id/backlog-candidates` | List persisted draft backlog candidates for a planning run |
| PATCH | `/api/backlog-candidates/:id` | Review and update a persisted backlog candidate |
| POST | `/api/backlog-candidates/:id/apply` | Apply one approved backlog candidate into the task workflow |

Behavior:

- `GET /api/planning-runs/:id/context-snapshot` (Phase 3B PR-2): returns `{ available, pack_id, planning_run_id, schema_version, sources_bytes, dropped_counts, open_task_count, document_count, drift_count, agent_run_count, has_sync_run, role, intent_mode, task_scale, source_of_truth }`. When no snapshot exists (run predates Phase 3B or is not a local_connector run), returns `{ available: false }` with HTTP 200. Nonexistent run returns 404. Query param `?raw=1` returns the raw `PlanningContextV2` JSON blob as `data`. Snapshots are saved fire-and-forget on `ClaimNextRun` after a successful `BuildContextV1` call.
- `PATCH /api/backlog-candidates/:id` accepts `title`, `description`, `status`, and `execution_role`. **Phase 6c PR-2 enforcement**: `execution_role` non-empty values MUST match a role in `roles.IsKnown` (catalog enforcement); empty string clears the column (NULL in DB). Unknown role returns 400. Case-sensitive (e.g. `"ui-scaffolder"`, not `"UI-Scaffolder"`). Every change to `execution_role` writes a row to `actor_audit` in the same transaction; the actor is derived from the request (session user or API-key id).
- Candidate responses include `execution_role` (nullable string) and **Phase 6c PR-2** `execution_role_authoring` (object or null) — `{ actor_kind: "user"|"api_key"|"router"|"system"|"connector", actor_id?, rationale?, confidence? (router-only), set_at }`. Pre-Phase-6c rows have no audit history and surface `null`.
- `POST /api/backlog-candidates/:id/apply` accepts JSON body `{ "execution_mode": "manual" | "role_dispatch", "execution_role"?: string }`. Missing body resolves to `"manual"`. **Phase 6c PR-2 contract change**: `mode=role_dispatch` REQUIRES `execution_role` to be present and in the catalog — empty or unknown returns 400. The chosen role becomes the `task.source` suffix (`role_dispatch:<role>`), is persisted on the candidate row, and is audited. `mode=manual` ignores `execution_role`. The Phase 5 behaviour (silently producing a bare `"role_dispatch"` source from `candidate.execution_role`) is removed.
- `mode=role_dispatch_auto` is reserved for Phase 6d (router auto-apply); 6c rejects it with 400 `invalid execution_mode`.
- `GET /api/roles` (Phase 6c PR-2, public, no auth): returns the catalog filtered to `category="role"` — `[{id, title, version, use_case, default_timeout_sec, category}]`. Meta-roles (e.g. `dispatcher` from PR-3) are filtered out. Used by the apply panel + `CandidateRoleEditor` for typed dropdowns and by the frontend drift test.

### Personal Account Bindings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/me/account-bindings` | List the current user's personal provider bindings |
| POST | `/api/me/account-bindings` | Create a personal provider binding for the current user |
| PATCH | `/api/me/account-bindings/:id` | Update one personal provider binding |
| DELETE | `/api/me/account-bindings/:id` | Delete one personal provider binding |

Request and response shape (Path B Slice S1):

```jsonc
{
  "provider_id": "openai-compatible | cli:claude | cli:codex",
  "label": "My Mac Claude",
  "base_url": "",                   // empty for cli:* providers
  "model_id": "claude-sonnet-4-6",
  "configured_models": ["claude-sonnet-4-6", "claude-opus-4-7"],
  "api_key": "sk-...",              // omitted for cli:* providers
  "cli_command": "/usr/local/bin/claude", // optional; empty = PATH lookup
  "is_primary": true                // optional; auto-true on first binding per namespace
}
```

Response includes the persisted fields plus `is_active`, `api_key_configured`,
`is_primary`, `cli_command`, `created_at`, `updated_at`. The plaintext
`api_key` is never returned.

Validation rules enforced server-side:

- `provider_id` must be one of `openai-compatible`, `cli:claude`, `cli:codex`. Unrecognized values return 400 (the allowlist check runs before the local-mode gate, so e.g. `cli:unknown` returns 400 in any deployment mode).
- `cli:*` provider ids (when allowlisted) are local-mode only. In server mode (`config.LocalMode == false`) any create or update of an allowlisted `cli:*` binding returns 403 (design §5 D8). The frontend hides the CLI binding section in server mode and shows an info card in its place.
- For `provider_id LIKE 'cli:%'`: `base_url` MUST be empty after trim, `api_key` MUST be absent or empty after trim, `model_id` is REQUIRED non-empty after trim.
- `cli_command` is optional. If non-empty it MUST match `^/[A-Za-z0-9_./\-]+$` (server-side sanity check; the connector enforces real hardening — `realpath` resolution, allowed-roots, interpreter blocklist, setuid rejection — at probe / invocation time per design §10).
- `configured_models` is capped at 16 entries, each at most 64 characters after trim (design §6.2 rule 4).
- `is_primary=true` atomically demotes any other primary binding in the same `(user_id, namespace)` group within the same database transaction. Namespace is `cli` for `cli:*` providers and `api` for everything else. The CLI namespace is shared across all `cli:*` providers — flipping primary from `cli:claude` to `cli:codex` auto-demotes the previous primary CLI binding. When `is_primary` is omitted on the first binding of a namespace, it defaults to `true`.
- Active uniqueness — `idx_account_bindings_active_unique` (migration 015) — is preserved. The Create/Update handler **auto-demotes** any existing active binding for the same `(user_id, provider_id)` within the same transaction before inserting/updating, so a second create normally returns **201** with the prior row silently flipped to `is_active=FALSE`. Direct INSERTs that bypass the handler (e.g. data import scripts) still hit the index and surface as 409. The `account_bindings_user_id_provider_id_label_key` UNIQUE constraint from migration 014 surfaces label collisions as 409 even via the handler path (a user cannot have two bindings with the same provider+label, regardless of `is_active`).

Source: `[agent:backend-architect]`

### Local Connectors

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/me/local-connectors` | List the current user's registered local connectors |
| POST | `/api/me/local-connectors/pairing-sessions` | Create one short-lived pairing session for a local connector |
| DELETE | `/api/me/local-connectors/:id` | Revoke one local connector |
| POST | `/api/me/local-connectors/:id/probe-binding` | (Phase 4 P4-4) Enqueue a CLI-binding probe on the named connector |
| GET | `/api/me/local-connectors/:id/probe-binding/:probe_id` | (Phase 4 P4-4) Poll the outcome of an enqueued probe |
| GET | `/api/me/local-connectors/:id/cli-configs` | (Phase 6a UX-B1) List per-connector CLI configs (provider + model + path) |
| POST | `/api/me/local-connectors/:id/cli-configs` | (Phase 6a UX-B1) Create a CLI config on this connector |
| PATCH | `/api/me/local-connectors/:id/cli-configs/:config_id` | (Phase 6a UX-B1) Update a CLI config |
| DELETE | `/api/me/local-connectors/:id/cli-configs/:config_id` | (Phase 6a UX-B1) Delete a CLI config |
| POST | `/api/me/local-connectors/:id/cli-configs/:config_id/primary` | (Phase 6a UX-B1) Promote a CLI config to primary for this connector |
| POST | `/api/connector/pair` | Claim a pairing code and exchange it for a connector token |
| POST | `/api/connector/heartbeat` | Refresh connector presence using `X-Connector-Token` |
| POST | `/api/connector/claim-next-run` | Lease the next queued local-connector planning run for the connector owner |
| POST | `/api/connector/planning-runs/:id/result` | Return success or failure for one leased planning run |
| POST | `/api/connector/claim-next-task` | (Phase 6b) Claim the next queued role-dispatch task for the connector owner |
| POST | `/api/connector/tasks/:task_id/execution-result` | (Phase 6b) Submit execution result for a claimed task |
| POST | `/api/connector/activity` | (Phase 6c PR-4) Report the connector's current execution phase; connector-token auth |
| GET | `/api/me/local-connectors/:id/activity` | (Phase 6c PR-4) Poll the latest activity snapshot for a connector; user auth |
| GET | `/api/me/local-connectors/:id/activity-stream` | (Phase 6c PR-4) SSE stream of connector activity updates; user auth |
| GET | `/api/projects/:id/active-connectors` | (Phase 6c PR-4) List online connectors for the authenticated user with their current activity |

Behavior:

- `POST /api/me/local-connectors/pairing-sessions` is authenticated and returns both a pairing-session record and a plaintext pairing code. The server stores only the code hash.
- `POST /api/connector/pair` is public because the pairing code itself is the temporary credential. A successful claim creates one connector record and returns a plaintext connector token once.
- `POST /api/connector/pair` (Path B Slice S2) accepts an optional `protocol_version` integer. Path-B-aware connectors send `1`. The server stores it on `local_connectors.protocol_version` (migration 023). Old clients that omit the field default to `0` server-side, which the dispatcher treats as "cannot receive cli_binding."
- `POST /api/connector/heartbeat` requires `X-Connector-Token` and marks the connector `online` while refreshing `last_seen_at`.
- `POST /api/connector/claim-next-run` requires `X-Connector-Token` and only leases `execution_mode = local_connector` runs requested by the same human user who owns the connector.
- `POST /api/connector/claim-next-run` (Path B Slice S2) returns an optional `cli_binding` block on the response when the leased run was created with an `account_binding_id`. Shape: `{ id, provider_id, model_id, cli_command, label }`. Sourced from the run's `connector_cli_info.binding_snapshot` so the audit value survives even if the live binding row was deleted between create and claim (R10 mitigation, design §6.2 / §6.5). Backwards-compat dispatch rule: when the calling connector reports `protocol_version < 1` the server refuses to lease any run with non-NULL `account_binding_id` and instead returns the same shape as "queue empty"; the run stays queued for an updated connector to claim later.
- `POST /api/connector/planning-runs/:id/result` requires `X-Connector-Token`, accepts `{ success, error_message?, candidates?, cli_info?, error_kind? }`, and finalizes the leased planning run plus its correlated audit run. (Path B S5a) When `success=false`, the optional `error_kind` field is validated against a server-side allowlist (`session_expired`, `rate_limited`, `context_overflow`, `adapter_timeout`, `unknown`); anything outside the allowlist is normalised to `"unknown"`. The server looks up a `remediation_hint` from a static catalog and persists both fields in `connector_cli_info`. The updated run's `connector_cli_info` includes `error_kind` and `remediation_hint` (non-empty only for known non-`unknown` kinds).
- `POST /api/connector/heartbeat` (Phase 4 P4-4) additionally accepts `cli_probe_results: [{ probe_id, ok, latency_ms, content?, error_kind?, error_message?, completed_at }]`. Each entry removes the matching pending probe from the connector's metadata and persists the outcome under `local_connectors.metadata.cli_probe_results.<probe_id>`. Pending probe requests are delivered to the connector as `metadata.pending_cli_probe_requests[]` within the returned connector record — the connector reads the list on each heartbeat response, runs the probes serially in a worker goroutine, and reports outcomes in the subsequent heartbeat body.
- Phase 6a UX-B1 `/api/me/local-connectors/:id/cli-configs`: each CLI config has `{ id, provider_id ("cli:claude"|"cli:codex"), cli_command (optional; empty = PATH lookup), model_id, label, is_primary, created_at, updated_at }`. Exactly one `is_primary=true` per connector when the list is non-empty (auto-promoted on first add; auto-promoted to the first survivor when the primary is deleted). Cap: 16 configs per connector. All routes are user-scoped via `connector.user_id`; a cross-user request returns 404.
- `POST /api/requirements/:id/planning-runs` (Phase 6a UX-B3) accepts optional `connector_id` + `cli_config_id` in the body. Both MUST be supplied together; either alone returns 400. `cli_config_id` is only valid for `execution_mode == "local_connector"`. When supplied, the server resolves the config from the named connector's metadata, verifies ownership, and snapshots `{ provider_id, model_id, cli_command, label }` onto the run's `connector_cli_info.binding_snapshot`. Precedence: `(connector_id, cli_config_id)` > `account_binding_id` (legacy) > auto-resolve to primary `cli:*` account_binding > run without binding.
- `POST /api/me/local-connectors/:id/probe-binding` (Phase 4 P4-4) is authenticated and takes `{ binding_id }`. The referenced binding MUST belong to the same user and MUST have a `cli:*` provider_id; otherwise 400/404 is returned. The server enqueues a pending-probe entry on the named connector's `metadata.pending_cli_probe_requests[]`. If a probe for the same binding is already in-flight on that connector, the existing `probe_id` is returned (idempotent). Returns `{ probe_id }`. If the connector's pending-probe list is already at the hard cap (64 entries, see `DECISIONS.md` 2026-04-24 "P4-4 probe pipeline"), the handler returns HTTP 429.
- `GET /api/me/local-connectors/:id/probe-binding/:probe_id` (Phase 4 P4-4) returns `{ status: "pending" | "completed" | "not_found", result? }`. The `result` block is populated only when `status == "completed"` and matches the `CliProbeResult` shape. Stored results are retained for 24 hours; callers that poll past that window receive `not_found`.
- Connector tokens are distinct from session tokens and API keys.
- `POST /api/connector/claim-next-task` (Phase 6b) requires `X-Connector-Token`. Claims one task with `dispatch_status = 'queued'` whose project has the connector's owner as a `project_members` row. Returns `{ task, requirement }` where `task` is null when the queue is empty. Sets the task's `dispatch_status = 'running'` atomically.
- `POST /api/connector/tasks/:task_id/execution-result` (Phase 6b) requires `X-Connector-Token`. Accepts `{ success, result?, error_message?, error_kind? }`. The task MUST already be in `dispatch_status = 'running'` (owned by the connector's user); otherwise 400. On success: `dispatch_status = 'completed'`, `execution_result` stored as JSON. On failure: `dispatch_status = 'failed'`. `error_kind` is validated against the same allowlist as planning runs (`session_expired`, `rate_limited`, `context_overflow`, `adapter_timeout`, `unknown`); values outside the list are normalised to `"unknown"`.
- `POST /api/connector/activity` (Phase 6c PR-4) requires `X-Connector-Token`. Accepts a `ConnectorActivity` JSON body: `{ phase, subject_kind?, subject_id?, subject_title?, role_id?, step?, started_at, updated_at }`. Phase must be one of `idle | claiming_run | planning | claiming_task | dispatching | submitting`. Returns 202. Updates the in-memory activity hub and asynchronously persists to `local_connectors.current_activity_json`.
- `GET /api/me/local-connectors/:id/activity` (Phase 6c PR-4) polling endpoint. Returns `{ activity: ConnectorActivity | null, online: bool, age_seconds: int }`. `online` is true when `last_seen_at` is within 90 seconds. Prefers in-memory hub state; falls back to `current_activity_json` in DB.
- `GET /api/me/local-connectors/:id/activity-stream` (Phase 6c PR-4) SSE endpoint. Sends the current state immediately on connect then pushes updates when the hub broadcasts. Named event type is `activity`. Keepalive comment `:\n\n` every 30 seconds. Client disconnects are detected via `r.Context().Done()`. `X-Accel-Buffering: no` header is set.
- `GET /api/projects/:id/active-connectors` (Phase 6c PR-4) returns `[{ connector_id, label, activity: ConnectorActivity | null, online: bool, age_seconds: int }]` for the authenticated user's non-revoked connectors that are either online or have a recorded activity snapshot.

### Planning Settings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/settings/planning` | Get the admin-managed central planning provider configuration |
| PATCH | `/api/settings/planning` | Update the admin-managed central planning provider configuration |

Behavior:

- Both planning settings routes are admin-only.
- `GET /api/settings/planning` never returns plaintext or ciphertext for the stored provider API key. It only returns `api_key_configured` plus non-secret metadata.
- `PATCH /api/settings/planning` accepts `provider_id`, `model_id`, `base_url`, `configured_models`, optional `api_key`, optional `clear_api_key`, and optional `credential_mode`.
- `APP_SETTINGS_MASTER_KEY` must be configured before saving a non-empty provider API key. This env exists only to encrypt persisted secrets at rest; provider/model/base URL settings themselves are stored in the application database.

#### Create planning run behavior

- Request body is optional. Active fields are `trigger_source`, optional `model_id`, optional `execution_mode`, and (Path B Slice S2) optional `account_binding_id`.
- `trigger_source` still defaults to `manual`.
- `account_binding_id` (Path B S2) names a personal `account_bindings` row to dispatch this run against. Optional. When omitted AND `execution_mode == "local_connector"` AND the user has at least one active `cli:*` binding, the server resolves the user's primary CLI binding and snapshots it onto the run. When the user has zero `cli:*` bindings, the field stays `NULL` on the run row and the connector falls back to its env-var default (backwards compatible with pre-Path-B connectors). Validation per design §6.2:
  - Must reference a binding owned by the requesting user; otherwise `400`.
  - For `execution_mode == "local_connector"` the binding must have `provider_id LIKE 'cli:%'` AND `is_active = TRUE`; otherwise `400`.
  - The server snapshots `provider_id`, `model_id`, `cli_command`, `label`, `is_primary` into `connector_cli_info.binding_snapshot` (existing JSON column from migration 019). The snapshot survives later edits or deletion of the binding (R10 mitigation).
  - The 015 audit columns (`adapter_type`, `model_override`, `binding_label`, `binding_source`) are populated to mirror the snapshot so the row is self-describing without parsing the JSON envelope.
- The server always resolves the provider from workspace policy, then resolves credentials from personal binding or shared settings based on `credential_mode`.
- `model_id` may be sent as a one-run override, but it must belong to the resolved provider exposed by `GET /api/projects/:id/planning-provider-options`.
- `provider_id` is server-controlled in the current implementation; request bodies do not select a different provider implementation.
- `execution_mode` may be `deterministic`, `server_provider`, or `local_connector`. `local_connector` requires a signed-in user session plus at least one non-revoked paired connector for that same user.
- A successful planning run now persists ranked backlog suggestions linked to the requirement and run.
- A successful planning run also records one internal project-scoped agent run audit entry using action type `review` and an idempotency key derived from the planning run ID.
- `local_connector` runs are created in `status = queued` and `dispatch_status = queued`, then stay pending until a paired connector claims and returns the run.
- Candidate persistence remains draft-first, but approved candidates can now be applied one-by-one into tasks through the candidate-scoped apply endpoint.
- The current implementation always stores server-owned rank, score, confidence, evidence, typed `evidence_detail`, and duplicate-title signals for UI review, even when a remote model generated the draft content.
- When the saved central provider is `openai-compatible`, the server sends compact planning context metadata to the configured upstream chat-completions endpoint: requirement title/summary/description, a bounded slice of open task titles, recent document metadata, open drift metadata, latest sync summary, and recent agent-run summaries.
- If a `queued` or `running` run already exists for the requirement, create returns `409`.
- If orchestration fails after run creation, the planning run is failed, partial draft candidates are removed, and the correlated agent run is marked failed.

#### Planning provider options response

```json
{
  "data": {
    "default_selection": {
      "provider_id": "openai-compatible",
      "model_id": "llama3",
      "selection_source": "server_default"
    },
    "credential_mode": "personal_preferred",
    "resolved_binding_source": "personal",
    "resolved_binding_label": "default",
    "available_execution_modes": ["server_provider", "local_connector"],
    "paired_connector_available": true,
    "active_connector_label": "My Machine",
    "can_run": true,
    "allow_model_override": true,
    "providers": [
      {
        "id": "openai-compatible",
        "label": "OpenAI-Compatible Planner",
        "kind": "llm",
        "description": "Remote planning provider using a configured OpenAI-compatible chat completions endpoint.",
        "default_model_id": "llama3",
        "models": [
          {
            "id": "llama3",
            "label": "llama3",
            "description": "Configured model exposed by the resolved binding or workspace settings.",
            "enabled": true
          }
        ]
      }
    ]
  },
  "error": null,
  "meta": null
}
```

#### Planning settings response

```json
{
  "data": {
    "settings": {
      "provider_id": "openai-compatible",
      "model_id": "gpt-5-mini",
      "base_url": "https://api.openai.com/v1",
      "configured_models": ["gpt-5-mini", "gpt-4.1-mini"],
      "api_key_configured": true,
      "credential_mode": "personal_preferred",
      "updated_by": "admin",
      "created_at": "2026-04-17T17:00:00Z",
      "updated_at": "2026-04-17T17:05:00Z"
    },
    "secret_storage_ready": true
  },
  "error": null,
  "meta": null
}
```

Source: `[agent:documentation-architect]`

#### Planning run response

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "requirement_id": "uuid",
    "status": "completed",
    "trigger_source": "manual",
    "provider_id": "deterministic",
    "model_id": "deterministic-v1",
    "selection_source": "server_default",
    "binding_source": "system",
    "binding_label": "",
    "error_message": "",
    "started_at": "2026-04-16T16:10:00Z",
    "completed_at": "2026-04-16T16:10:00Z",
    "created_at": "2026-04-16T16:10:00Z",
    "updated_at": "2026-04-16T16:10:00Z"
  },
  "error": null,
  "meta": null
}
```

#### List planning runs query parameters

- `page`, `per_page`: pagination

Behavior:

- Planning runs are listed newest-first by `created_at`.
- Empty results return `[]`, not `null`.
- `failed` runs preserve `error_message` for frontend visibility.
- `binding_source` records whether the run used a personal binding, shared workspace credentials, or the built-in fallback.

### Planning Run Backlog Candidates

#### Backlog candidate response

```json
{
  "data": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "requirement_id": "uuid",
      "planning_run_id": "uuid",
      "parent_candidate_id": "",
      "suggestion_type": "implementation",
      "title": "Improve sync failure recovery UX",
      "description": "Expose recovery options before creating tasks\n\nUsers should be able to see a saved draft candidate before deciding whether it deserves a task.\n\nDeliver the core requirement as the first shippable backlog slice.",
      "status": "draft",
      "rationale": "Top recommendation because it is the closest implementation slice to the stated requirement and can move directly into review once copy is confirmed.",
      "priority_score": 82.6,
      "confidence": 80.5,
      "rank": 1,
      "evidence": [
        "Requirement summary: Expose recovery options before creating tasks",
        "Requirement description captured for planning context."
      ],
      "evidence_detail": {
        "summary": [
          "Requirement summary: Expose recovery options before creating tasks"
        ],
        "documents": [
          {
            "document_id": "uuid",
            "title": "Sync Recovery Guide",
            "file_path": "docs/sync-recovery.md",
            "doc_type": "guide",
            "is_stale": true,
            "staleness_days": 14,
            "matched_keywords": ["sync", "recovery"],
            "contribution_reasons": [
              "Related project context influenced ranking."
            ]
          }
        ],
        "drift_signals": [],
        "sync_run": null,
        "agent_runs": [],
        "duplicates": [],
        "score_breakdown": {
          "impact": 92,
          "urgency": 58,
          "dependency_unlock": 62,
          "risk_reduction": 50,
          "effort": 52,
          "confidence_seed": 73,
          "evidence_bonus": 3.6,
          "duplicate_penalty": 0,
          "final_priority_score": 82.6,
          "final_confidence": 80.5
        }
      },
      "duplicate_titles": [],
      "created_at": "2026-04-16T17:00:00Z",
      "updated_at": "2026-04-16T17:00:00Z"
    }
  ],
  "error": null,
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 1
  }
}
```

#### List backlog candidates query parameters

- `page`, `per_page`: pagination

Behavior:

- Results are scoped to a single planning run.
- Default order is `rank ASC`, then `priority_score DESC`, then `created_at`, so the highest-ranked suggestion becomes the default review target.
- Empty results return `[]`, not `null`.

#### Update backlog candidate request

```json
{
  "title": "Improve sync recovery UX",
  "description": "Keep this candidate in review until the team decides whether it should become a task.",
  "status": "approved"
}
```

Behavior:

- At least one mutable field must change.
- Mutable fields are `title`, `description`, and `status`.
- Valid review statuses are `draft`, `approved`, and `rejected`.
- `applied` candidates are immutable and return `400` if edited.
- Candidate updates remain project-scoped through the candidate's owning project.

#### Apply backlog candidate behavior

- No request body is required in the current slice.
- Only `approved` candidates can be applied. `draft` and `rejected` candidates return `400`.
- Apply is candidate-scoped, not requirement-scoped: each request materializes at most one task.
- Successful apply creates one task in `todo` with default `medium` priority, writes a `task_lineage` record, and marks the candidate `applied`.
- Apply is idempotent for the same candidate. Repeating the same call after success returns `200` with `already_applied: true` and the previously created task plus lineage.
- Apply uses a PostgreSQL advisory transaction lock plus title conflict detection to block concurrent duplicate open tasks with the same normalized title in the same project.
- If an open `todo` or `in_progress` task already exists with the same normalized title, apply returns `409`.

#### Apply backlog candidate response

```json
{
  "data": {
    "task": {
      "id": "uuid",
      "project_id": "uuid",
      "title": "Improve sync recovery UX",
      "description": "Keep recovery guidance in one actionable task.",
      "status": "todo",
      "priority": "medium",
      "assignee": "",
      "source": "agent:planning-orchestrator",
      "created_at": "2026-04-17T10:00:00Z",
      "updated_at": "2026-04-17T10:00:00Z"
    },
    "candidate": {
      "id": "uuid",
      "project_id": "uuid",
      "requirement_id": "uuid",
      "planning_run_id": "uuid",
      "parent_candidate_id": "",
      "title": "Improve sync recovery UX",
      "description": "Keep recovery guidance in one actionable task.",
      "status": "applied",
      "rationale": "Draft candidate synthesized from requirement intake and approved during review.",
      "created_at": "2026-04-17T09:55:00Z",
      "updated_at": "2026-04-17T10:00:00Z"
    },
    "lineage": {
      "id": "uuid",
      "project_id": "uuid",
      "task_id": "uuid",
      "requirement_id": "uuid",
      "planning_run_id": "uuid",
      "backlog_candidate_id": "uuid",
      "lineage_kind": "applied_candidate",
      "created_at": "2026-04-17T10:00:00Z"
    },
    "already_applied": false
  },
  "error": null,
  "meta": null
}
```

Source: `[agent:backend-architect]`

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/tasks` | List tasks for a project |
| POST | `/api/projects/:id/tasks` | Create a task |
| POST | `/api/projects/:id/tasks/batch-update` | Batch update multiple tasks under a project |
| GET | `/api/tasks/:id` | Get a task by ID |
| PATCH | `/api/tasks/:id` | Update a task |
| DELETE | `/api/tasks/:id` | Delete a task |

#### List tasks query parameters

- `page`, `per_page`: pagination
- `sort`, `order`: list ordering
- `status`: exact match. Allowed values: `todo`, `in_progress`, `done`, `cancelled`
- `priority`: exact match. Allowed values: `low`, `medium`, `high`
- `assignee`: exact match assignee filter

Invalid `status` or `priority` values return `400`.

#### Batch update tasks request

```json
{
  "task_ids": ["uuid-1", "uuid-2"],
  "changes": {
    "status": "done",
    "priority": "high",
    "assignee": ""
  }
}
```

Notes:

- `task_ids` must contain at least 1 task and at most 100.
- `changes` must include at least one of `status`, `priority`, `assignee`.
- Empty assignee clears the assignee.
- Batch updates are atomic inside the project scope.

### Documents

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/documents` | List documents |
| POST | `/api/projects/:id/documents` | Register a document |
| GET | `/api/documents/:id` | Get document metadata |
| GET | `/api/documents/:id/content` | Get document content preview |
| PATCH | `/api/documents/:id` | Update document metadata |
| DELETE | `/api/documents/:id` | Delete document registration |
| POST | `/api/documents/:id/refresh-summary` | API-key-only document summary refresh trigger |

#### Document content preview response

```json
{
  "data": {
    "path": "/mirrors/agent-native-pm/docs/api-surface.md",
    "language": "markdown",
    "content": "# API Surface\n...",
    "size_bytes": 2048,
    "truncated": false
  },
  "error": null,
  "meta": null
}
```

Notes:

- `file_path` must remain repo-relative. Path traversal is blocked.
- Secondary repo documents use alias-prefixed paths such as `docs-repo/docs/guide.md`.

### Document Links

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/documents/:id/links` | List document-to-code links |
| POST | `/api/documents/:id/links` | Create a document link |
| DELETE | `/api/document-links/:id` | Delete a document link |

#### Create document link request

```json
{
  "code_path": "backend/internal/router/router.go",
  "link_type": "covers"
}
```

Allowed `link_type` values: `covers`, `references`, `depends_on`.

### Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/summary` | Get current computed project summary |
| GET | `/api/projects/:id/dashboard-summary` | Get dashboard-ready aggregates |
| GET | `/api/projects/:id/summary/history` | Get summary snapshot history |

#### Dashboard summary response

```json
{
  "data": {
    "project_id": "uuid",
    "summary": {
      "project_id": "uuid",
      "snapshot_date": "2026-04-16",
      "total_tasks": 15,
      "tasks_todo": 5,
      "tasks_in_progress": 3,
      "tasks_done": 6,
      "tasks_cancelled": 1,
      "total_documents": 8,
      "stale_documents": 2,
      "health_score": 0.72
    },
    "latest_sync_run": {
      "id": "uuid",
      "project_id": "uuid",
      "started_at": "2026-04-16T12:00:00Z",
      "completed_at": "2026-04-16T12:03:00Z",
      "status": "completed",
      "commits_scanned": 12,
      "files_changed": 34,
      "error_message": ""
    },
    "open_drift_count": 4,
    "recent_agent_runs": []
  },
  "error": null,
  "meta": null
}
```

Notes:

- `latest_sync_run` may be `null`.
- `recent_agent_runs` returns the most recent 5 runs.
- This contract is the current dashboard source-of-truth for frontend overview cards and planning context preparation.

### Sync Runs

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/projects/:id/sync` | Trigger sync for a project |
| GET | `/api/projects/:id/sync-runs` | List sync history for a project |

Notes:

- Sync responses use the standard envelope and sync run payload.
- Branch resolution failures may include detected branch hints in `error_message`, for example: `detected default branch is "review/risk-git-fixes"`.

### Drift Signals

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/:id/drift-signals` | List drift signals for a project |
| POST | `/api/projects/:id/drift-signals` | Create a drift signal |
| POST | `/api/projects/:id/drift-signals/resolve-all` | Bulk resolve open drift signals |
| PATCH | `/api/drift-signals/:id` | Update a drift signal status |

#### Drift signal list query parameters

- `page`, `per_page`: pagination
- `status`: `open`, `resolved`, `dismissed`, or empty for all
- `sort_by`: `severity` or `created_at`

#### Create drift signal request

```json
{
  "document_id": "uuid-document",
  "trigger_type": "code_change",
  "trigger_detail": "router changed",
  "trigger_meta": {
    "changed_files": [
      { "path": "backend/internal/router/router.go", "change_type": "M" }
    ],
    "confidence": "high"
  },
  "severity": 3,
  "sync_run_id": "uuid-sync-run"
}
```

### Agent Runs

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/agent-runs` | API-key-only create agent run |
| GET | `/api/projects/:id/agent-runs` | List agent runs for a project |
| GET | `/api/agent-runs/:id` | Get agent run details |
| PATCH | `/api/agent-runs/:id` | API-key-only update agent run |

Notes:

- Valid `action_type`: `create`, `update`, `review`, `sync`
- Valid `status`: `running`, `completed`, `failed`
- `idempotency_key` is optional but strongly recommended for automated callers.
- Planning runs now emit an internal audit entry through this same model using `action_type: review`; this is internal correlation behavior, not a separate planning-agent API.
- Repeating `POST /api/agent-runs` with the same `idempotency_key` for the same project returns the existing run.
- Reusing an `idempotency_key` across different projects returns `409`.

### API Keys

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/keys` | List API keys visible to the authenticated user |
| POST | `/api/keys` | Create an API key |
| DELETE | `/api/keys/:id` | Revoke an API key |

#### Create API key request

```json
{
  "project_id": "uuid-project",
  "label": "planning-agent"
}
```

Notes:

- `project_id` is optional. Omit for a global key.
- Raw secret is only returned once at creation time.

### Notifications

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/notifications` | List notifications for current user |
| POST | `/api/notifications` | Create a notification |
| PATCH | `/api/notifications/:id/read` | Mark one notification as read |
| PATCH | `/api/notifications/:id/unread` | Mark one notification as unread |
| POST | `/api/notifications/read-all` | Mark all notifications as read |
| GET | `/api/notifications/unread-count` | Return unread notification count |

### Search

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/search` | Full-text search across tasks and documents |

Supported query parameters:

- `q` (required): full-text query term
- `project_id` (optional): limit to one project
- `type` (optional): `all`, `tasks`, `documents`
- `status` (optional): task status filter
- `doc_type` (optional): document type filter
- `staleness` (optional): `all`, `stale`, `fresh`

#### Search response

```json
{
  "data": {
    "tasks": [],
    "documents": []
  },
  "error": null,
  "meta": null
}
```

## Not Implemented Yet

The following planning endpoints are intentionally not present in the current codebase yet:

- `POST /api/requirements/:id/apply`

These belong to the future Planning Workspace implementation, not the current deployed API.

## Error Codes

| HTTP Status | Meaning |
|-------------|---------|
| 200 | Success |
| 201 | Created |
| 400 | Bad request / validation error |
| 401 | Authentication required or invalid credentials |
| 403 | Forbidden / setup already completed / project not allowed |
| 404 | Resource not found |
| 409 | Conflict / duplicate resource |
| 500 | Internal server error |

## Health Score Computation

The current project health score remains:

```text
health_score = (task_completion_ratio * 0.7) + (document_freshness_ratio * 0.3)

task_completion_ratio = tasks_done / max(total_tasks - tasks_cancelled, 1)
document_freshness_ratio = (total_documents - stale_documents) / max(total_documents, 1)
```

Source: `[agent:documentation-architect]`
