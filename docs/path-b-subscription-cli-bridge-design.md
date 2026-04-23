# Path B design — Subscription CLI Bridge

**Status**: design v1 · 2026-04-23 · `[agent:feature-planner]`
**Gates**: implementation slices S1–S5 are blocked until this doc is approved by the owner.
**Supersedes**: nothing — extends the 2026-04-17 `subscription-connector-mvp.md` MVP.
**Inputs**: 2026-04-17 `subscription-connector-mvp.md`, 2026-04-17 `credential-binding-design.md`, 2026-04-20 `local-connector-context.md`, current adapter at `adapters/backlog_adapter.py`, current connector at `backend/internal/connector/`.

---

## 1. Problem statement

The `subscription-connector-mvp.md` MVP shipped between 2026-04-17 and 2026-04-22:

- `local_connectors` + `connector_pairing_sessions` tables exist
- `anpm-connector pair / serve / doctor` daemon works end-to-end
- `claim-next-run` returns sanitized `wire.PlanningContextV1`
- `submit-result` writes back candidates and emits SSE notification
- `adapters/backlog_adapter.py` shells out to the user's local `claude` or `codex` CLI

But the **subscription path is still operationally invisible**. The user must:

1. Know there is an env var `ANPM_ADAPTER_AGENT=codex` to switch from Claude to Codex
2. Pass `--adapter-command /path/to/adapter` and `--adapter-arg ...` flags to `anpm-connector serve` correctly
3. Diagnose adapter failures by reading subprocess stderr (CLI session expired? rate limit? wrong model id?) — there is no UI surface for this
4. Have already authenticated `claude` / `codex` on the host before pairing

The **operator question that has no UI answer today**:
> "Which CLI is my connector going to use for the next planning run, and is it healthy?"

There is also no UI feedback when a planning run fails because the underlying CLI session expired — the user sees a generic "planning failed" notification with a truncated error string.

The **scope creep risk**: the project owner only has Claude Code CLI + Mistral API key + (claimed) Codex CLI. Building support for Copilot CLI / VS Code subscription extraction is a separate, much larger problem (the 2026-04-17 ADR explicitly excluded this from MVP and that exclusion still stands).

## 2. Current state inventory

Compiled from `adapters/backlog_adapter.py`, `backend/internal/connector/`, `backend/internal/handlers/local_connectors.go`, `backend/internal/models/`, and `backend/internal/planning/`.

### 2.1 Adapter contract (`exec-json`)

- **stdin** — `{run, requirement, requested_max_candidates, planning_context}` per `adapters/backlog_adapter.py:8–24`
- **stdout** — `{candidates: [...], error_message: ""}` per `adapters/backlog_adapter.py:26–42`
- **env vars** — `ANPM_ADAPTER_AGENT` (`claude` | `codex`), `ANPM_ADAPTER_MODEL`, `ANPM_ADAPTER_TIMEOUT`, `ANPM_ADAPTER_DEBUG` (`adapters/backlog_adapter.py:44–48`)
- **failure mode** — exit 0 + `error_message` populated; non-zero exit reserved for hard process errors; CLI invocation errors prefixed `"claude CLI failed (exit N): ..."` / `"codex CLI timed out after Ns"` (`adapters/backlog_adapter.py:101–110, 350`)

### 2.2 Connector daemon

- `backend/cmd/connector/main.go` → `connector.Run(os.Args[1:])`
- subcommands: `pair`, `serve`, `doctor` (`backend/internal/connector/app.go:41–75`)
- adapter flags persisted into state on `pair` / `doctor` / `serve`: `--adapter-command`, `--adapter-arg` (repeatable), `--adapter-working-dir`, `--adapter-timeout`, `--adapter-max-output-bytes` (`backend/internal/connector/app.go:82–87, 265–280`)
- state file precedence: `--state-path` > `$ANPM_CONNECTOR_STATE_PATH` > `$XDG_CONFIG_HOME/agent-native-pm/connector.json`, mode `0600`

### 2.3 Server dispatch / lease

- `claim-next-run` returns `{Run, Requirement, Project, PlanningContext}` (`backend/internal/handlers/local_connectors.go:144–179`)
- lease duration: 10 minutes
- run is selected as oldest queued across the connector owner's account (not per-project)
- `submit-result` accepts `{Success, ErrorMessage, Candidates, CliInfo}` (`backend/internal/models/local_connector.go`)
- failure path: `FailLocalConnectorRun` → notification kind `error` + truncated message + UI flash banner

### 2.4 Account bindings

- `account_bindings` schema (`backend/db/migrations/014_account_bindings.sql`):
  ```
  (id, user_id, provider_id, label, base_url, model_id,
   configured_models, api_key_ciphertext, api_key_configured,
   is_active, created_at, updated_at)
  UNIQUE(user_id, provider_id, label)
  ```
- current `provider_id` value in use: `"openai-compatible"` only
- there is no enum constraint on `provider_id` at the DB layer
- there is no field for "command path" / "CLI binary" / "CLI auth status"

### 2.5 Planning provider registry

- `Provider` interface: `Generate(ctx, requirement, planningContext, selection) ([]BacklogCandidateDraft, error)` (`backend/internal/planning/provider.go:36–38`)
- `RegisteredProvider` = `{Descriptor, Implementation}` (`backend/internal/planning/provider.go:50–53`)
- execution modes (`backend/internal/models/requirement.go`):
  - `deterministic` — built-in scoring
  - `server_provider` — server calls remote LLM directly
  - `local_connector` — server queues, connector claims and executes
- `local_connector` requires at least one non-revoked paired connector for the requesting user

### 2.6 Citable prior art

- DECISIONS 2026-04-17: subscription path starts with local connector pairing and registry
- DECISIONS 2026-04-17: personal account bindings alongside shared planning settings
- DECISIONS 2026-04-20: local connector is user-scoped, serves all of a user's projects
- DECISIONS 2026-04-20: ship reference `adapters/backlog_adapter.py`
- DECISIONS 2026-04-20: adopt `context.v1` as connector planning context contract
- DECISIONS 2026-04-22: `Provider.Generate` takes `context.Context`

## 3. End state

A **first-class CLI binding** concept that sits beside today's API-key-based account bindings:

```
My Bindings page (/me/bindings)
┌─────────────────────────────────────────────────────────────────┐
│ API-key bindings                                                │
│   • Mistral (mistral-cloud) · model: mistral-small-latest · ✓  │
│                                                                 │
│ CLI bindings                                                    │
│   • Claude Code CLI · model: claude-sonnet-4-6 · ✓ healthy     │
│       Last used: 2 min ago · Connector: My Mac                 │
│   • Codex CLI · model: gpt-5.4 · ⚠ session expired             │
│       [Re-login command: codex login]                           │
└─────────────────────────────────────────────────────────────────┘
```

When the operator starts a planning run, the **execution-mode picker** shows actually available CLIs with their health:

```
Execution
  ◯ Built-in fallback (deterministic)
  ◯ Mistral cloud (server provider)
  ● Local CLI:  [Claude Code (healthy)  ▾]
                  Claude Code (healthy)
                  Codex (⚠ session expired)
```

The connector daemon picks up the per-run **CLI selection** from the claim response and invokes the right adapter command, instead of relying on a host-side env var.

When the adapter fails because the CLI session expired or rate-limited, the **failure surface** shows the next concrete operator action ("run `codex login` then retry the run") instead of a raw subprocess error string.

### 3.1 What changes vs today

| Concern | Today | End state |
|---|---|---|
| CLI selection | Host env var `ANPM_ADAPTER_AGENT` | Per-binding config in `account_bindings`; per-run `cli_binding_id` |
| CLI visibility in UI | None | "CLI bindings" section in My Bindings page |
| CLI health check | None | Periodic version + auth probe via connector heartbeat |
| Per-run CLI choice | All runs use whatever the daemon was started with | Run request can specify `cli_binding_id`, defaults to user's primary |
| Failure messages | Raw subprocess stderr truncated to 240 chars | Typed `error_kind` enum + `remediation_hint` |
| Multi-CLI per user | Possible only by running multiple connectors | Single connector routes to any healthy CLI binding the user owns |
| Audit trail | Just `error_message` on failure | `agent_runs.metadata` includes `cli_binding_id` / resolved command / model / exit_code / duration |

### 3.2 What stays exactly the same

- `wire.PlanningContextV1` schema and 256 KiB cap
- `claim-next-run` / `submit-result` HTTP contract structure (additive only)
- `Provider.Generate` interface
- The 10-minute lease window
- The single-process per-connector serial execution model (parallelism still requires multiple paired devices per the 2026-04-20 ADR)
- Pairing-code one-use + connector-token separation
- `ANPM_ADAPTER_*` env vars work as a fallback (D4)

## 4. Non-goals

Items explicitly **not** in Path B (each kept for a separate future design):

1. **Copilot CLI / VS Code session extraction.** Same reason as the 2026-04-17 MVP exclusion: the project cannot programmatically reuse a subscription session that lives only inside an editor extension.
2. **WebSocket / push-based CLI health.** Heartbeat-piggyback is enough; introducing a second push channel for CLI health is over-engineering.
3. **Capability-aware routing across connectors.** "Pick the connector with the lowest queue depth" is a multi-device problem the owner does not have today.
4. **Adapter sandbox / containerization.** The CLIs run on the user's own machine with the user's own credentials. Sandboxing would block the entire point.
5. **OAuth-style "log in via Claude" inside the web app.** The CLIs own auth; we delegate.
6. **Cost / token accounting.** Useful but separate. Subscriptions are typically flat-rate from the user's perspective; surfacing tokens-per-run UX adds non-trivial UI work.
7. **Auto-retry of failed runs.** A failed run stays failed; the operator decides whether to re-queue. Auto-retry can mask real configuration problems.
8. **Per-project CLI bindings.** Bindings stay user-scoped (matches the 2026-04-20 decision that connectors are user-scoped). Per-project pinning is a future feature when teams adopt the tool.
9. **Cross-user CLI binding sharing.** Each user owns their own bindings; admin cannot configure a binding "for" another user.

## 5. Resolved design decisions

The earlier draft posed six open questions. All six are resolved by the owner per recommended values; rationale recorded for future reference.

### D1 — Extend `account_bindings`, do not introduce a new `cli_bindings` table

**Resolution**: extend.

**Rationale**:
- The shape is 90% the same: `(user_id, provider_id, label, model_id, configured_models, is_active)`.
- The only fields that don't apply: `base_url` (CLI doesn't have one), `api_key_ciphertext` / `api_key_configured` (CLI owns auth).
- A new `cli_bindings` table would duplicate per-user uniqueness logic, CRUD endpoints, credential-mode resolution code in `settings_backed_planner.go`, and the existing `/api/me/account-bindings` route surface.
- The `provider_id` namespace is already conceptually open (no DB enum constraint); reserving the `cli:` prefix is a clean discriminator.

**Constraints introduced**:
- `provider_id` MUST start with `cli:` for CLI-based bindings.
- For `provider_id LIKE 'cli:%'`: `base_url` MUST be empty AND `api_key_ciphertext` MUST be empty (validated server-side).
- Allowed `cli:*` values in v1: `cli:claude`, `cli:codex`. Adding `cli:copilot` etc. requires a new DECISIONS entry.

### D2 — Per-run CLI selection with primary-binding default

**Resolution**: per-run `cli_binding_id` on the planning-run request; absent → server picks the user's primary CLI binding (most-recently-used active `cli:*`); user has no CLI bindings → server uses connector default (current behavior, backwards compatible).

**Rationale**:
- Treating the connector as a generic executor is forward-compatible with future capability routing (out of scope but not blocked).
- Keeping a sensible default keeps the existing happy path one-click.
- Backwards compatibility: a request without `cli_binding_id` from a pre-S2 frontend works.

**Constraints introduced**:
- "Primary CLI binding" is computed: `is_active = TRUE AND provider_id LIKE 'cli:%'`, ordered by `updated_at DESC` (touched on each successful planning run).
- Selection is server-side, not connector-side, so the audit trail records the choice before the connector ever sees the run.

### D3 — `cli_health` lives in `local_connectors.metadata` JSONB, not a new table

**Resolution**: JSONB now; new table only when a concrete query justifies it.

**Rationale**:
- The data is per-connector + per-binding; a `(connector_id, binding_id)` shape is naturally a JSONB map keyed by `binding_id`.
- No cross-connector / cross-user analytical queries are planned (and the 2026-04-20 connector ADR explicitly defers cross-device routing).
- A separate table can be introduced later via a forward-only migration if a need emerges (e.g. cross-connector dashboard).

**Constraints introduced**:
- `local_connectors.metadata->>'cli_health'` is reserved for this purpose. Other metadata keys MUST NOT collide.
- Stale entries (last `checked_at` > 5 min ago) MUST be displayed with a "?" indicator in the UI.
- The store layer adds typed read/write helpers so callers don't manipulate raw JSON paths.

### D4 — Env-var fallback (`ANPM_ADAPTER_AGENT`, `ANPM_ADAPTER_MODEL`) kept indefinitely

**Resolution**: keep as a power-user / debugging escape hatch.

**Rationale**:
- Zero ongoing maintenance cost (just don't delete the existing parsing).
- Useful for testing adapter changes without touching the database.
- Explicit precedence: stdin `cli_selection` > env var > adapter built-in default.

**Constraints introduced**:
- Behaviour MUST be documented in `adapters/backlog_adapter.py` docstring AND in this doc's §6.3.
- If both `cli_selection` and env var are present, the connector logs a one-line WARN (not ERROR) noting the env var was overridden.

### D5 — Both Claude and Codex preset cards ship in S3

**Resolution**: ship both. Owner does not need to confirm Codex is installed before merging S3.

**Rationale**:
- The typed-failure work (S5) is exactly designed for this: pick Codex without it installed → run fails with `cli_not_found` + remediation hint.
- Adding the second preset later is unnecessary churn when the data model already supports it after S1.

**Constraints introduced**:
- The Claude preset card is the default selection in the S3 form (the owner has confirmed Claude Code installed).
- The Codex preset card includes a "Untested by maintainer" note in its description until the owner has actually run it through.

### D6 — Multi-binding per CLI (`Work Claude` + `Personal Claude`) allowed but not promoted

**Resolution**: the existing DB unique constraint `(user_id, provider_id, label)` already permits this. UI lets the user set a custom `label`; the launcher picker shows `label + provider`.

**Rationale**:
- Zero schema work — the constraint already accommodates this.
- The form does not nudge users toward creating multiples; only the label field implies the option.
- Failure mode if abused: confusion. Failure mode if forbidden: power users hit a wall in S3 with no escape.

**Constraints introduced**:
- The Add CLI Binding form's label field MUST default to a sensible placeholder ("My Claude Code") rather than leave it blank.

## 6. Information architecture (concrete)

### 6.1 Schema changes

**Migration `021_account_bindings_cli_command.sql`** (forward-only; both SQLite + PostgreSQL):
```sql
ALTER TABLE account_bindings
  ADD COLUMN cli_command TEXT NOT NULL DEFAULT '';
```

**Migration `022_planning_runs_cli_binding.sql`** (forward-only; both drivers):
```sql
ALTER TABLE planning_runs
  ADD COLUMN cli_binding_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_planning_runs_cli_binding ON planning_runs(cli_binding_id);
```

No migration for `cli_health` — uses existing `local_connectors.metadata` JSONB column (D3).

### 6.2 API contracts

#### `POST /api/me/account-bindings` (extended in S1)

Request:
```jsonc
{
  "provider_id": "cli:claude",          // NEW: cli:claude | cli:codex (v1)
  "label": "My Claude Code",
  "model_id": "claude-sonnet-4-6",
  "configured_models": ["claude-sonnet-4-6", "claude-opus-4-7"],
  "cli_command": "/usr/local/bin/claude" // NEW: optional; empty = PATH lookup
}
```

Validation (server-side):
- `provider_id` MUST be in the registered set: `openai-compatible`, `cli:claude`, `cli:codex`.
- For `cli:*` providers: `base_url` MUST be empty (omit or send `""`); `api_key` MUST NOT be set; `model_id` REQUIRED.
- `cli_command`: optional. If set, MUST be an absolute path with no shell metacharacters (`;`, `|`, `&`, `$`, backticks, newlines). Validated by `regexp.MustCompile('^/[A-Za-z0-9_./\\-]+$')` — explicit allowlist, not denylist.
- `(user_id, provider_id, label)` uniqueness from existing DB constraint.

Response (201): same shape as request plus standard envelope metadata.

#### `POST /api/requirements/:id/planning-runs` (extended in S2)

Request gains optional `cli_binding_id`:
```jsonc
{
  "execution_mode": "local_connector",
  "cli_binding_id": "uuid"   // NEW: optional; must belong to user, be active, be cli:*
}
```

Validation:
- If `cli_binding_id` present: row exists AND `user_id == requesting user` AND `is_active = TRUE` AND `provider_id LIKE 'cli:%'`. Reject with 400 otherwise.
- If absent AND `execution_mode == "local_connector"` AND user has ≥1 active `cli:*` binding: server resolves to the most-recently-`updated_at` one and stores it on the run.
- If absent AND user has zero `cli:*` bindings: `cli_binding_id` stays empty on the run; connector falls back to env var / built-in default (backwards compatible with pre-S2 connectors).

#### `POST /api/connector/claim-next-run` (response extended in S2)

```jsonc
{
  "run": {...},
  "requirement": {...},
  "project": {...},
  "planning_context": {...},
  "cli_binding": {                          // NEW: optional, present iff run has cli_binding_id
    "id": "uuid",
    "provider_id": "cli:claude",
    "model_id": "claude-sonnet-4-6",
    "cli_command": "/usr/local/bin/claude"
  }
}
```

Old connectors that don't know the field ignore it (per the 2026-04-20 wire-contract discipline: unknown fields tolerated).

#### `POST /api/connector/heartbeat` (request extended in S5)

```jsonc
{
  // existing connector token in X-Connector-Token header
  "cli_health": [                           // NEW: optional, repeated
    {
      "binding_id": "uuid",
      "status": "healthy",
      "checked_at": "2026-04-23T01:00:00Z",
      "version_string": "claude 1.5.0"
    },
    {
      "binding_id": "uuid",
      "status": "session_expired",
      "checked_at": "2026-04-23T01:00:00Z",
      "remediation_hint": "Run: claude login"
    }
  ]
}
```

`status` enum: `healthy`, `session_expired`, `rate_limited`, `cli_not_found`, `unknown`.

Server stores the latest entry per `binding_id` in `local_connectors.metadata`:
```jsonc
{
  "cli_health": {
    "<binding_id>": {
      "status": "...",
      "checked_at": "...",
      "version_string": "...",
      "remediation_hint": "..."
    }
  }
}
```

#### `POST /api/connector/planning-runs/:id/result` (request extended in S5)

```jsonc
{
  "success": false,
  "error_message": "claude CLI failed (exit 1): authentication required",
  "error_kind": "session_expired",          // NEW: optional, enum
  "remediation_hint": "Run: claude login",  // NEW: optional, plain text, ≤240 chars
  "cli_info": { ... }                       // existing
}
```

`error_kind` enum: `session_expired`, `rate_limited`, `model_not_available`, `cli_not_found`, `cli_timeout`, `adapter_protocol_error`, `unknown`.

If `error_kind` is missing OR not in the enum, server stores it as empty string and UI falls back to today's truncated `error_message` display. Backwards compatible.

### 6.3 Adapter stdin (extended in S2)

```jsonc
{
  "run": {...},
  "requirement": {...},
  "requested_max_candidates": 3,
  "planning_context": {...},
  "cli_selection": {                  // NEW: optional, takes precedence over ANPM_ADAPTER_* env vars
    "provider_id": "cli:claude",
    "model_id": "claude-sonnet-4-6",
    "cli_command": "/usr/local/bin/claude"  // empty = PATH lookup
  }
}
```

Adapter precedence (D4):
1. `cli_selection.provider_id` if present.
2. Else `ANPM_ADAPTER_AGENT` env var.
3. Else built-in default (`claude`).

The same precedence applies for `model_id`. `cli_command` only comes from `cli_selection`; env var has no equivalent today.

### 6.4 Adapter stdout (extended in S5)

```jsonc
{
  "candidates": [],
  "error_message": "claude CLI failed (exit 1): authentication required",
  "error_kind": "session_expired",        // NEW
  "remediation_hint": "Run: claude login" // NEW
}
```

Adapter classification table (`adapters/backlog_adapter.py`):

| CLI signal (stderr substring or exit code) | `error_kind` | `remediation_hint` |
|---|---|---|
| `authentication required` / `not authenticated` / exit 401 | `session_expired` | `Run: <agent> login` |
| `rate limit` / `quota exceeded` / exit 429 | `rate_limited` | `Wait and retry, or switch to a less-loaded model` |
| `model not found` / `no such model` / `unknown model` | `model_not_available` | `Pick a model the CLI exposes (run: <agent> models)` |
| `command not found` / `No such file` (Python `FileNotFoundError`) | `cli_not_found` | `Install the <agent> CLI or set cli_command to its path` |
| Python `subprocess.TimeoutExpired` | `cli_timeout` | `Increase ANPM_ADAPTER_TIMEOUT or simplify the requirement` |
| stdout not parseable as JSON / missing `candidates` field | `adapter_protocol_error` | `Adapter version mismatch — update reference adapter` |
| anything else | `unknown` | `""` (UI falls back to error_message) |

The classifier is conservative: when in doubt, return `unknown`. Adding new enum values requires a DECISIONS entry.

### 6.5 Server-side resolved selection on run creation

When the server creates a `local_connector` planning run with `cli_binding_id`:

1. Validate binding exists / belongs to user / is active / is `cli:*`.
2. Snapshot `provider_id`, `model_id`, `cli_command` from the binding **at creation time** onto the run row (in `cli_binding_id` plus a JSON `cli_binding_snapshot` field on `planning_runs`).
3. Touch the binding's `updated_at` so primary-binding resolution stays consistent (D2).

Snapshotting matters because the binding can change between run creation and connector claim (e.g. user edits the model id). The run uses the snapshot for reproducibility; lineage / audit refers to the snapshot, not the live binding.

Extra column on `planning_runs` (folded into migration 022):
```sql
ALTER TABLE planning_runs
  ADD COLUMN cli_binding_snapshot TEXT NOT NULL DEFAULT '';
```
Stored as JSON-encoded string for SQLite parity (we already do this for other JSON-on-SQLite columns; PostgreSQL stores as TEXT and the model layer parses).

### 6.6 UI surfaces touched

- **`pages/AccountBindings.tsx`** (existing) — add a "CLI bindings" section beside the API-key list. New form for `Add CLI binding` with provider preset cards (Claude Code, Codex), model id field, optional command path. (S3 + S5 health row)
- **`pages/ProjectDetail/planning/PlanningLauncher.tsx`** (existing) — when execution mode is `local_connector`, show a CLI binding picker with health icons. (S4)
- **`pages/ProjectDetail/planning/CandidateReviewPanel.tsx`** (existing) — failure flash banner shows remediation hint when present. (S5)
- **No new pages.** No new top-level routes.

## 7. UI / structural constraints

From the 2026-04-22 ADR (`Tab/panel components migrated under pages/ProjectDetail/`) and the 2026-04-22 Tier-3 sibling rule:

- New planning-launcher subcomponents (e.g. `CliBindingPicker.tsx`) land under `frontend/src/pages/ProjectDetail/planning/`.
- New CLI-binding-form components land alongside the existing form in `frontend/src/pages/AccountBindings.tsx`. Extract to a sibling component if the file grows past ~600 LOC after the change.
- A new `frontend/src/utils/cliBindingPresets.ts` mirrors the structure of `planningConnectionPresets.ts`. Stays under 100 LOC; carries Claude / Codex preset metadata only.
- Each new component ships with a smoke test in the same PR.
- All HTTP-touching code respects the dual-runtime constraint (any new column in `planning_runs` / `account_bindings` MUST apply cleanly under SQLite and PostgreSQL).

## 8. Incremental slice plan

Five slices, each independently mergeable, each as its own PR. No slice depends on an unmerged slice.

### S1 — Data model: extend `account_bindings` for CLI bindings

**Scope**:
- Migration `021_account_bindings_cli_command.sql`: `ALTER TABLE account_bindings ADD COLUMN cli_command TEXT NOT NULL DEFAULT ''`.
- `models.AccountBinding` gains `CliCommand` field; JSON tag `cli_command`.
- Validation logic in `handlers/account_bindings.go`:
  - `provider_id` allowlist: `openai-compatible`, `cli:claude`, `cli:codex`.
  - `cli:*` → reject `base_url` non-empty; reject `api_key` non-empty; require `model_id`.
  - `cli_command` regex allowlist (§6.2).
- Backend handler updates for create / list / patch.
- `docs/api-surface.md` updated for the new field + validation.
- No frontend change in S1.

**Definition of Done**:
- Migration applies cleanly under both SQLite (`scripts/test-with-sqlite.sh`) and PostgreSQL.
- `POST /api/me/account-bindings` with `provider_id: "cli:claude"`, valid model_id, empty base_url, empty api_key returns 201 with `cli_command` echoed back.
- `POST` with `provider_id: "cli:claude"` and non-empty `base_url` returns 400 with descriptive error.
- `POST` with `provider_id: "cli:claude"` and non-empty `api_key` returns 400.
- `POST` with `cli_command: "rm -rf /;evil"` returns 400 (regex rejection).
- `POST` with `provider_id: "cli:invalid"` returns 400.
- 5 store-level tests for the new validation rules.
- 1 handler-level test for the happy path.

**Size**: S. ~80 LOC backend + 2 test files + 1 migration + 1 doc update. No risk to existing functionality (additive only).

**Rollback**: pre-merge revert. Post-merge: ship a forward migration that drops `cli_command` (no code change needed because the column has DEFAULT '').

### S2 — Per-run CLI selection plumbing

**Scope**:
- Migration `022_planning_runs_cli_binding.sql`:
  ```sql
  ALTER TABLE planning_runs ADD COLUMN cli_binding_id TEXT NOT NULL DEFAULT '';
  ALTER TABLE planning_runs ADD COLUMN cli_binding_snapshot TEXT NOT NULL DEFAULT '';
  CREATE INDEX idx_planning_runs_cli_binding ON planning_runs(cli_binding_id);
  ```
- `models.PlanningRun` gains `CliBindingID` + `CliBindingSnapshot` (typed struct, JSON-marshaled).
- `POST /api/requirements/:id/planning-runs` accepts optional `cli_binding_id`. Validation per §6.2. Snapshot logic per §6.5.
- Server-side primary-binding resolution: when `cli_binding_id` absent and `execution_mode == local_connector`, query for `is_active AND provider_id LIKE 'cli:%' ORDER BY updated_at DESC LIMIT 1`.
- `claim-next-run` response gains `cli_binding` block populated from snapshot.
- Connector `serve` reads `cli_binding` from each claimed run, passes it to adapter via stdin `cli_selection`.
- Adapter receives `cli_selection`, uses it with the documented precedence (§6.3); env vars stay as fallback (D4).
- Adapter logs WARN line when both stdin selection and env var are present.
- `docs/api-surface.md` updated.
- `docs/local-connector-context.md` updated for the new optional `cli_binding` field on claim response.

**Definition of Done**:
- Migration applies cleanly under both drivers.
- Run creation with `cli_binding_id` snapshots the binding fields onto the run.
- Run creation without `cli_binding_id` and one active `cli:claude` binding picks that binding.
- Run creation without `cli_binding_id` and zero CLI bindings stores empty snapshot (backwards compat).
- Connector receives `cli_binding` in claim response and forwards it to adapter stdin.
- Adapter respects stdin precedence over env var.
- Old connector (paired before S2, using the still-valid v1 wire contract) ignores `cli_binding` field and behaves as before.
- Integration test: full flow create-run → claim → submit-result with two different CLI bindings produces two distinct adapter invocations.
- 8+ unit tests across handler / store / adapter parsing.

**Size**: M. Largest backend slice. ~250 LOC backend + 50 LOC adapter + 1 migration + 2 doc updates.

**Rollback**: pre-merge revert. Post-merge: forward migration drops the two new columns; frontend pre-S4 doesn't send `cli_binding_id` so no orphan data risk.

### S3 — Frontend: CLI binding management UI

**Scope**:
- Extend `pages/AccountBindings.tsx` to render a "CLI bindings" section.
- Add-CLI-binding form with provider preset cards (Claude Code, Codex).
- New helper `frontend/src/utils/cliBindingPresets.ts` mirroring `planningConnectionPresets.ts` shape; carries Claude + Codex defaults (D5).
- Codex preset card description includes "Untested by maintainer" annotation (D5).
- Form submits to `POST /api/me/account-bindings` with `provider_id: cli:<x>`.
- Delete CLI binding works through the existing DELETE endpoint.
- Smoke tests for: new section empty state, populated state, submit, delete.

**Definition of Done**:
- AccountBindings page shows two sections: "API-key bindings" (existing) and "CLI bindings" (new).
- Add form's label field defaults to "My Claude Code" / "My Codex" (D6).
- Submitting the form creates a binding; UI refreshes; new entry visible.
- Submitting with empty model_id is blocked client-side.
- 4+ smoke tests.
- No backend change in this slice; relies on S1 endpoints.

**Size**: M. ~200 LOC of TSX + 1 utility file + 4 smoke tests.

**Rollback**: pre-merge revert removes the section; backend stays compatible.

### S4 — Frontend: CLI binding picker in PlanningLauncher

**Scope**:
- New component `frontend/src/pages/ProjectDetail/planning/CliBindingPicker.tsx`.
- `PlanningLauncher.tsx` gains a CLI binding picker that appears when `execution_mode == local_connector` AND user has ≥1 CLI binding.
- Health badge per binding using server-reported `cli_health` (queried via `GET /api/me/local-connectors` which returns merged `metadata`).
- Default selection: most-recently-`updated_at` active `cli:*` binding (matches server-side resolution in S2).
- Picker is hidden when user has zero CLI bindings (backwards compat).
- Submit picks `cli_binding_id` and includes it in the POST.
- Smoke tests: render with one healthy binding, render with one expired binding, render with zero bindings (picker absent), submit picks expected `cli_binding_id`.

**Definition of Done**:
- Picker shows when conditions met.
- Health icon reflects cli_health status (✓ healthy, ⚠ session_expired/rate_limited, ✗ cli_not_found, ? when stale >5min per D3).
- Click on the badge surfaces the remediation hint as a tooltip.
- Submit POSTs with the chosen `cli_binding_id`.
- 4+ smoke tests.

**Size**: S. ~80 LOC of TSX + 1 new sibling + 4 smoke tests.

**Rollback**: pre-merge revert removes the picker; planning runs still work via primary-binding default from S2.

### S5 — Failure typing + remediation + CLI health UX

**Scope**:
- `submit-result` accepts optional `error_kind` + `remediation_hint`. Server validates against enum, stores them on `planning_runs` (new columns: `error_kind TEXT`, `remediation_hint TEXT`). Backwards compat: missing fields stored as empty.
- Adapter (`adapters/backlog_adapter.py`) classifies CLI exit signatures using §6.4 mapping table.
- `pages/ProjectDetail/planning/CandidateReviewPanel.tsx` and the failure flash banner show the remediation hint when present.
- Health probe: connector `serve` loop runs `<cli_command> --version` + a tiny `<cli_command> --print "ping" --max-tokens 1` (Claude) or `codex chat "ping"` (Codex) on each heartbeat tick (60s default). Times out at 5s. Posts `cli_health` payload per §6.2.
- AccountBindings CLI binding rows show last-known cli_health (S5 surfaces what S5 generates).
- Adapter unit tests for each enum value's signature detection (8+ cases).
- Migration `023_planning_runs_error_kind.sql` adds the two columns.

**Definition of Done**:
- Adapter classifies the 7 enum values from realistic CLI signatures (test fixtures included).
- Server stores `error_kind` + `remediation_hint` on the run.
- UI shows remediation hint in failure flash banner.
- AccountBindings row shows health status with relative timestamp ("checked 12s ago").
- Stale (>5 min) entries show "?" badge per D3.
- 10+ tests across adapter + handler + UI.
- API surface doc updated for both new optional fields.

**Size**: M. Largest UX surface change. Adapter parsing + 2 frontend components + new server fields + 1 migration.

**Rollback**: pre-merge revert. Post-merge: forward migration drops the two columns; old runs lose their classification; UI gracefully falls back.

### Dependency graph

```
S1 (data model) ──┬── S2 (per-run plumbing) ──┬── S4 (launcher picker)
                  │                            │
                  └── S3 (binding mgmt UI) ────┘
                                                S5 (failure typing) ── needs S2
```

S1 must merge first. S3 and S2 can ship in parallel after S1. S4 needs both S2 and S3. S5 only needs S2.

## 9. Risk register

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | CLI binary path injection — user-controlled `cli_command` becomes part of subprocess invocation | Medium | High | Regex allowlist `^/[A-Za-z0-9_./\\-]+$` validated server-side AND adapter-side; subprocess invoked with `shell=False` (already enforced); per-user setting only — not a privilege escalation vector since the connector already runs as the user |
| R2 | Stale `cli_health` misleads operator (shows "healthy" but session actually expired) | High | Low | Health is best-effort by design (D3); UI marks stale (>5 min) entries with "?" indicator; ground truth is the actual run failure |
| R3 | Multi-binding per CLI causes accidental cross-pollination (operator picks wrong one) | Medium | Medium | Picker shows label + provider id prominently; recently-used binding is highlighted; D6 explicitly says "allow but don't promote"; agent_runs metadata records which binding was actually used |
| R4 | `error_kind` enum becomes wrong/incomplete as CLIs evolve | Medium | Low | Adapter classifier is conservative — defaults to `unknown` when no signature matches; UI falls back to today's truncated string for `unknown`; new enum values require a DECISIONS entry (not silent drift) |
| R5 | Adapter stdin grows past 256 KiB cap due to `cli_selection` envelope | Low | Medium | `cli_selection` is small (~200 bytes); existing 256 KiB cap on `wire.PlanningContextV1` still applies; explicit budget assertion added to S2 integration test |
| R6 | User schedules planning runs but no connector is online | Medium | Low | Existing behavior: run sits in `queued` status indefinitely; surfaced via planning-run list. No change in Path B. Future iteration may add TTL |
| R7 | Backwards-compat regression — old connector paired before S2 doesn't understand `cli_binding` field | Medium | High | `claim-next-run` `cli_binding` field is OPTIONAL; old connector ignores unknown fields per the 2026-04-20 wire contract; new connector handles missing field by falling back to env var (D4); S2 integration test exercises an old-connector simulation |
| R8 | Per-run `cli_binding_id` resolution introduces N+1 query (look up binding per claim) | Low | Low | Bindings table is small per user (<10 expected); index on `(user_id, id)` is enough. Snapshot avoids re-fetching the binding at claim time. If profile shows hot path, cache in-memory |
| R9 | Adapter health probe consumes user's CLI quota | Low | Low | Probe is `--print "ping" --max-tokens 1` — single token request, cheap. Heartbeat interval 60s = ~1500 probes/day per binding. Owner-acceptable for subscription-flat-rate; document the cost in §10 observability |
| R10 | Deleting a CLI binding while a planning run referencing it is in flight | Low | Medium | Run uses `cli_binding_snapshot` (not live binding lookup) per §6.5. Deletion of binding does NOT cascade to runs. Audit trail still resolvable. UI shows "(deleted binding)" annotation when binding is gone |
| R11 | Codex CLI interface differs from Claude in ways the adapter doesn't anticipate | Medium | Medium | Adapter unit tests for both CLIs in S5; D5 acknowledges Codex is owner-untested → `cli_not_found` failure path is the catch-all; Codex preset card carries "Untested by maintainer" note |
| R12 | New JSONB usage in `local_connectors.metadata` collides with existing keys | Low | High | All metadata writes go through typed helpers in the store layer; reserved key namespace documented inline; existing keys enumerated in S5 PR description |

## 10. Security analysis

The CLI bridge is a **subprocess invocation surface**. The connector already runs subprocesses today (the existing adapter is itself a subprocess), but Path B widens the attack surface by letting the user configure the binary path per-binding.

### Threat model (single-operator self-hosted deployment)

1. The connector runs as the operator's user. It already has full filesystem + network access of that user. The CLI binding path is **not** a privilege escalation vector — anything it can do, the connector could already do.
2. The web app is reachable from LAN + (if exposed) the internet. CLI binding CRUD MUST be authenticated (existing session + API-key middleware applies).
3. The pairing handshake uses single-use codes; the connector token is opaque and revocable (existing behavior, unchanged).

### Specific controls in Path B

- **`cli_command` validation**: regex allowlist `^/[A-Za-z0-9_./\\-]+$` (absolute path; alphanumeric, dot, slash, underscore, hyphen only). Rejects empty paths, relative paths, paths with shell metacharacters (`;`, `|`, `&`, `$`, backticks, newlines, spaces, redirects). Validation enforced server-side (S1) AND adapter-side (S2).
- **`cli_command` is per-user** — admin cannot set it for other users (the existing `/api/me/account-bindings` route is user-scoped).
- **Subprocess invocation discipline** in adapter: `subprocess.run([cli_command, ...args])` only. `shell=True` is **forbidden** (lint check added: `grep -n 'shell=True' adapters/` MUST be empty in CI).
- **`cli_selection.model_id` whitelisting**: adapter checks the resolved `model_id` against the binding's `configured_models` list before passing it to the CLI.
- **Audit trail** in `agent_runs.metadata` for every CLI invocation:
  - `cli_binding_id` + snapshot (provider, model, command)
  - `cli_exit_code`
  - `cli_duration_ms`
  - `error_kind`
  - **NEVER** the planning context body (already redacted via `wire.SanitizePlanningContextV1`)
  - **NEVER** stdout/stderr verbatim (only the typed `error_kind` + truncated message)

### Negative tests (S1 + S2)

- `cli_command` containing `;` MUST be rejected by validation → 400
- `cli_command` `"../bin/evil"` MUST be rejected (not absolute)
- `cli_command` set to a non-executable file MUST surface as `cli_not_found` error_kind at adapter invocation time
- `cli_selection.model_id` not in `configured_models` MUST be rejected by adapter with `model_not_available`
- Authenticated request to create a CLI binding for `user_id != self` MUST be rejected (existing middleware behavior; explicit test added)
- Unauthenticated request to `/api/me/account-bindings` MUST be 401 (existing; explicit test added in S1)

## 11. Observability + audit trail

For each `local_connector` planning run, the existing `agent_runs` audit entry is extended with the resolved CLI selection.

`agent_runs.summary` example:
```
"Planning via cli:claude (binding: My Mac Claude, model: claude-sonnet-4-6)"
```

`agent_runs.metadata` (JSONB) example:
```jsonc
{
  "cli_binding_id": "uuid",
  "cli_provider_id": "cli:claude",
  "cli_command_resolved": "/usr/local/bin/claude",
  "cli_model_id": "claude-sonnet-4-6",
  "cli_exit_code": 0,
  "cli_duration_ms": 4521,
  "error_kind": ""
}
```

Frontend `AgentsTab.tsx` already renders `agent_runs`; `cli_binding_id` becomes a small badge on the run card (S2 frontend follow-on; tracked but small enough to ride along with S4).

### Quota cost of health probes (R9 follow-up)

- Probe interval: 60s default (configurable via `--cli-health-interval` connector flag, range 30s–600s).
- Probe payload: 1 token in, 1 token out.
- Daily cost per binding: ~1500 tokens → flat-rate subscription noise, paid plan ~$0.001.
- **Health probes can be disabled** via `--cli-health-disabled` connector flag for users who object. UI then shows "Health checks disabled by connector" instead of a status badge.

## 12. UX wireframes (text)

### 12.1 AccountBindings page after S3 + S5

```
═══════════════════════════════════════════════════════════════
  My Bindings
═══════════════════════════════════════════════════════════════

  API-key bindings                    [+ Add API-key binding]
  ─────────────────────────────────────────────────────────────
  • My Mistral
    Provider: openai-compatible (Mistral AI)
    Model: mistral-small-latest    Status: ● active
    [Disable] [Delete]
  ─────────────────────────────────────────────────────────────

  CLI bindings                          [+ Add CLI binding]
  ─────────────────────────────────────────────────────────────
  • My Mac Claude
    Provider: cli:claude    Model: claude-sonnet-4-6
    Health: ✓ healthy (claude 1.5.0, checked 12s ago)
    Connector: My Mac    Status: ● active
    [Disable] [Delete]

  • Old Codex
    Provider: cli:codex    Model: gpt-5.4
    Health: ⚠ session expired (checked 2 min ago)
            → Run: codex login
    Connector: My Mac    Status: ● active
    [Disable] [Delete]
  ─────────────────────────────────────────────────────────────
```

### 12.2 Add-CLI-binding form (S3)

```
  ┌─ Add CLI binding ─────────────────────────────────────────┐
  │                                                            │
  │  Choose CLI:                                               │
  │  ┌───────────────────┐  ┌───────────────────────────────┐ │
  │  │ ● Claude Code     │  │ ◯ Codex                       │ │
  │  │   Anthropic CLI   │  │   OpenAI CLI                  │ │
  │  │                   │  │   (Untested by maintainer)    │ │
  │  └───────────────────┘  └───────────────────────────────┘ │
  │                                                            │
  │  Label:     [My Mac Claude                              ]  │
  │  Model id:  [claude-sonnet-4-6                          ]  │
  │  Configured models:                                        │
  │             [claude-sonnet-4-6, claude-opus-4-7         ]  │
  │  CLI command path: (optional)                              │
  │             [/usr/local/bin/claude                      ]  │
  │             Leave empty to use PATH lookup.                │
  │                                                            │
  │  [Cancel]                                       [Create]   │
  └────────────────────────────────────────────────────────────┘
```

### 12.3 PlanningLauncher CLI picker (S4)

```
  ┌─ New planning run ────────────────────────────────────────┐
  │                                                            │
  │  Requirement:  Improve sync recovery UX                    │
  │                                                            │
  │  Execution mode:                                           │
  │    ◯ Built-in fallback (deterministic)                    │
  │    ◯ Mistral (server provider)                            │
  │    ● Local CLI                                             │
  │       └─ CLI binding:                                      │
  │          [My Mac Claude (✓ healthy)            ▾]         │
  │             My Mac Claude     ✓ healthy                    │
  │             Old Codex         ⚠ session expired           │
  │                                                            │
  │  [Cancel]                                  [Run planning]  │
  └────────────────────────────────────────────────────────────┘
```

### 12.4 Failure flash banner (S5)

```
  ┌─ Planning run failed ─────────────────────────────────────┐
  │  ⚠ Run on requirement "Improve sync recovery UX" failed.  │
  │                                                            │
  │  Reason: CLI session expired                              │
  │  Hint:   Run `claude login` on the connector host, then   │
  │          retry the planning run.                          │
  │                                                            │
  │  [View details]                            [Retry run]    │
  └────────────────────────────────────────────────────────────┘
```

## 13. Migration / rollback strategy

**Forward-only migrations** per project policy:
- `021_account_bindings_cli_command.sql` — adds nullable column with DEFAULT ''.
- `022_planning_runs_cli_binding.sql` — adds two columns + index.
- `023_planning_runs_error_kind.sql` (S5) — adds two columns.

Each migration:
1. Idempotent: re-running on already-migrated DB is a no-op (the `IF NOT EXISTS` clause is added explicitly).
2. Adds columns with DEFAULT — old rows automatically populated.
3. Has a sibling `down.sql` for development-only rollback (NOT applied in production).

**Rollback story per slice** (in case a slice ships and needs to be undone):

| Slice | Pre-merge revert | Post-merge revert |
|---|---|---|
| S1 | `git revert` clean — no users yet | Forward migration drops `cli_command`; backend pre-S1 still works because column had DEFAULT '' |
| S2 | `git revert` clean | Forward migration drops `cli_binding_id` + `cli_binding_snapshot`; old connectors that ignore the field continue to work |
| S3 | `git revert` clean | Frontend revert leaves backend intact; no data cleanup |
| S4 | `git revert` clean | Frontend revert hides the picker; backend default-binding logic from S2 still picks correct CLI |
| S5 | `git revert` clean | Forward migration drops `error_kind` + `remediation_hint`; UI falls back to truncated `error_message` |

No slice introduces irreversible state changes.

## 14. Out of scope references

- **Copilot CLI / VS Code session extraction** — separate design when (if) the scope changes.
- **Cross-connector capability routing** — multi-device problem, not in scope.
- **Cost / token accounting** — separate design.
- **Auto-retry of failed runs** — explicitly rejected in §4 to avoid masking config problems.
- **OAuth-style in-app CLI login** — CLIs own auth; not changing that.
- **Per-project CLI bindings** — bindings stay user-scoped (matches 2026-04-20 connector decision).
- **Cross-user CLI binding sharing** — each user owns their bindings.
- **Sandboxed CLI execution** — defeats the point of using the user's authenticated CLI.

## 15. Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| Design (this doc) | in review | #14 | — |
| S1 — data model | not started | — | — |
| S2 — per-run plumbing | not started | — | — |
| S3 — binding mgmt UI | not started | — | — |
| S4 — launcher picker | not started | — | — |
| S5 — failure typing + health UX | not started | — | — |

## 16. Implementation checkpoints (between slices)

Between each slice merge, the owner should validate the following manually before greenlighting the next:

**After S1**:
- `curl -X POST /api/me/account-bindings -d '{"provider_id":"cli:claude","label":"test","model_id":"claude-sonnet-4-6"}'` returns 201.
- `curl` with `cli_command: "rm -rf /;evil"` returns 400.

**After S2**:
- Create a planning run via API with `cli_binding_id` → run row has snapshot populated.
- Run a paired connector against the run → adapter receives `cli_selection` in stdin.

**After S3**:
- AccountBindings page shows new "CLI bindings" section.
- Add a Claude binding via UI → it appears in the list.

**After S4**:
- Launcher shows CLI picker when execution mode is `local_connector`.
- Selecting a binding and submitting creates a run with the right `cli_binding_id`.

**After S5**:
- Trigger an intentional CLI failure (e.g. delete `~/.config/claude/credentials`) → run failure shows remediation hint.
- AccountBindings row reflects health status updates.

## 17. Glossary

- **CLI binding** — a per-user record describing one local CLI (e.g. Claude Code, Codex) with its label, model id, and optional command path. Stored as a row in `account_bindings` with `provider_id` like `cli:*`.
- **Primary CLI binding** — the user's most-recently-`updated_at` active `cli:*` binding. Used as the default when a planning run is created without an explicit `cli_binding_id`.
- **CLI selection** — the resolved `{provider_id, model_id, cli_command}` triple that the connector forwards to the adapter via stdin. Snapshotted from the binding at run-creation time.
- **`error_kind`** — typed enum classifying why an adapter run failed (`session_expired`, `rate_limited`, `model_not_available`, `cli_not_found`, `cli_timeout`, `adapter_protocol_error`, `unknown`).
- **`cli_health`** — connector-reported per-binding health status (`healthy`, `session_expired`, `rate_limited`, `cli_not_found`, `unknown`) stored under `local_connectors.metadata.cli_health.<binding_id>`.
- **Snapshot** — a JSON-encoded copy of the binding's `provider_id` / `model_id` / `cli_command` at the moment of run creation. Decouples runs from later binding edits.

---

Source: `[agent:feature-planner]`. Cites `subscription-connector-mvp.md`, `local-connector-context.md`, `credential-binding-design.md`. Six design decisions resolved per §5. No code lands until owner approval.
