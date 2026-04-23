# Path B design — Subscription CLI Bridge

**Status**: draft · 2026-04-23 · `[agent:feature-planner]`
**Gates**: no Path B implementation lands until this doc is approved.
**Supersedes**: nothing (extends the 2026-04-17 `subscription-connector-mvp.md` MVP).
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
| CLI selection | Host env var `ANPM_ADAPTER_AGENT` | Per-binding config in `account_bindings` (or new `cli_bindings`) |
| CLI visibility in UI | None | "CLI bindings" section in My Bindings page |
| CLI health check | None | Periodic `claude --version` / `codex --version` + auth probe via connector heartbeat |
| Per-run CLI choice | All runs use whatever the daemon was started with | Run request can specify `cli_binding_id`, defaults to user's primary |
| Failure messages | Raw subprocess stderr truncated to 240 chars | Typed failure (session-expired / rate-limit / model-not-found / generic) with operator action hint |
| Multi-CLI per user | Possible only by running multiple connectors | Single connector can route to any healthy CLI binding the user owns |

### 3.2 What stays exactly the same

- `wire.PlanningContextV1` schema and 256 KiB cap
- `claim-next-run` / `submit-result` HTTP contract (`CliInfo` field is already there but underused)
- `Provider.Generate` interface
- The 10-minute lease window
- The single-process per-connector serial execution model (parallelism still requires multiple paired devices per the 2026-04-20 ADR)
- Pairing-code one-use + connector-token separation

## 4. Non-goals

Items explicitly **not** in Path B (each kept for a separate future design):

1. **Copilot CLI / VS Code session extraction.** Same reason as the 2026-04-17 MVP exclusion: the project cannot programmatically reuse a subscription session that lives only inside an editor extension.
2. **WebSocket / push-based CLI health.** Heartbeat-piggyback is enough; introducing a second push channel for CLI health is over-engineering.
3. **Capability-aware routing across connectors.** "Pick the connector with the lowest queue depth" is a multi-device problem the owner does not have today.
4. **Adapter sandbox / containerization.** The CLIs run on the user's own machine with the user's own credentials. Sandboxing would block the entire point.
5. **OAuth-style "log in via Claude" inside the web app.** The CLIs own auth; we delegate.
6. **Cost / token accounting.** Useful but separate. Subscriptions are typically flat-rate from the user's perspective; surfacing tokens-per-run UX adds non-trivial UI work.
7. **Auto-retry of failed runs.** A failed run stays failed; the operator decides whether to re-queue. Auto-retry can mask real configuration problems.

## 5. Information architecture

### 5.1 Where the CLI binding lives in the data model

**Decision (proposed): extend `account_bindings`**, do **not** introduce a new table.

Rationale:
- The shape is 90% the same: `(user_id, provider_id, label, model_id, configured_models, is_active)`.
- The *only* fields that don't apply: `base_url` (CLI doesn't have one), `api_key_ciphertext` / `api_key_configured` (CLI owns auth).
- A new `cli_bindings` table would duplicate the per-user uniqueness logic, the CRUD endpoints, and the credential-mode resolution code in `settings_backed_planner.go`.

Concrete change:
- Accept new `provider_id` values: `"cli:claude"`, `"cli:codex"`. (Other CLIs are out of scope for Path B; the `cli:` namespace is reserved.)
- For `provider_id LIKE 'cli:%'`:
  - `base_url` MUST be empty.
  - `api_key_ciphertext` MUST be empty (the field is encrypted-empty; it never holds a CLI session token).
  - `model_id` is the CLI-side model id (e.g. `claude-sonnet-4-6`, `gpt-5.4`).
  - `configured_models` is the user-curated list of models they want to expose for that CLI.
- Add a new optional column `cli_command` for the resolved binary path (e.g. `/usr/local/bin/claude`). If empty, the connector falls back to PATH lookup.

Migration: forward-only ALTER TABLE adding `cli_command TEXT NOT NULL DEFAULT ''`.

### 5.2 Where the CLI selection happens at run time

Decision (proposed): **run request carries `cli_binding_id`**.

- `POST /api/requirements/:id/planning-runs` accepts an optional `cli_binding_id`.
- Server validates: binding exists, belongs to the requesting user, `is_active=true`, `provider_id` starts with `cli:`.
- Server stores the resolved binding id on the planning run (new column on `planning_runs`).
- `claim-next-run` response includes the resolved binding's `provider_id`, `model_id`, `cli_command`.
- The connector picks adapter command from `cli_command` (or PATH lookup) and passes the model id via stdin in the existing `run.model_override` slot.

This **moves the CLI choice from connector-startup-time to per-run-time**. The connector becomes a generic executor; the per-run binding tells it which CLI to spawn.

### 5.3 CLI health probe

The connector heartbeat (existing endpoint) is extended to optionally include a `cli_health` payload:

```jsonc
POST /api/connector/heartbeat
{
  "cli_health": [
    {"binding_id": "uuid", "status": "healthy", "checked_at": "...", "version": "claude 1.5.0"},
    {"binding_id": "uuid", "status": "session_expired", "checked_at": "...", "hint": "run: claude login"}
  ]
}
```

The connector probes each known binding once per heartbeat cycle (default: every 60s). Probe is `<command> --version` + a tiny no-op auth probe (e.g. `claude --print "ping" --max-tokens 1` with a 5s timeout). Specific probe commands are part of the per-CLI adapter and not modelled in the server.

Server stores latest health per `(connector_id, binding_id)` pair in a new lightweight `connector_cli_health` table (or reuses `local_connectors.metadata` JSONB if we want zero new tables — see open question Q3).

### 5.4 Failure typing

`submit-result` `error_message` field today is free-text. Path B adds a typed enum on top of (not replacing) the message:

```jsonc
{
  "success": false,
  "error_message": "claude CLI failed (exit 1): authentication required",
  "error_kind": "session_expired",   // NEW
  "remediation_hint": "Run `claude login` on the connector host, then retry the planning run."  // NEW
}
```

Enum values (small, finite):
- `session_expired`
- `rate_limited`
- `model_not_available`
- `cli_not_found`
- `cli_timeout`
- `adapter_protocol_error`
- `unknown`

Adapter detects the kind from the CLI's exit code + stderr signature. When kind is `unknown`, the UI falls back to today's truncated-string display.

### 5.5 UI surfaces touched

- **`pages/AccountBindings.tsx`** (existing) — add a "CLI bindings" section beside the API-key list. New form for `Add CLI binding` with provider preset (Claude / Codex), model id field, optional command path.
- **`pages/ProjectDetail/planning/PlanningLauncher.tsx`** (existing) — when execution mode is `local_connector`, show a CLI binding picker with health icons.
- **No new pages.** No new top-level routes.

## 6. UI / structural constraints

From the 2026-04-22 ADR (`Tab/panel components migrated under pages/ProjectDetail/`) and the 2026-04-22 Tier-3 sibling rule:

- New planning-launcher subcomponents land under `frontend/src/pages/ProjectDetail/planning/`.
- New CLI-binding-form components land alongside the existing form in `frontend/src/pages/AccountBindings.tsx`. Extract to a sibling component if the file grows past ~600 LOC after the change.
- Each new component ships with a smoke test in the same PR.
- All HTTP-touching code respects the dual-runtime constraint (any new column in `planning_runs` / `account_bindings` must apply cleanly under SQLite and PostgreSQL).

## 7. Incremental slice plan

Five slices, each independently mergeable, each as its own PR. No slice depends on an unmerged slice.

### S1 — Data model: extend `account_bindings` for CLI bindings

**Scope**:
- Migration `021_account_bindings_cli_command.sql`: `ALTER TABLE account_bindings ADD COLUMN cli_command TEXT NOT NULL DEFAULT ''`.
- `models.AccountBinding` gains `CliCommand` field.
- Validation: `provider_id` starting with `cli:` requires empty `base_url` + empty `api_key_ciphertext`; allowed providers in v1 are `cli:claude`, `cli:codex`.
- Backend handler updates for create / list / patch.
- No frontend change in S1.

**Tests**: store-level tests for the new constraint; migration applies cleanly under both drivers.

**Size**: S. One migration, one model field, ~50 LOC of validation.

### S2 — Per-run CLI selection plumbing

**Scope**:
- Migration `022_planning_runs_cli_binding.sql`: `ALTER TABLE planning_runs ADD COLUMN cli_binding_id TEXT NOT NULL DEFAULT ''`.
- `POST /api/requirements/:id/planning-runs` accepts optional `cli_binding_id`. Validation: binding belongs to user, is `cli:*`, is active.
- `claim-next-run` response gains `cli_binding` block with `provider_id`, `model_id`, `cli_command`.
- Connector `serve` reads the binding from each claimed run and overrides its env vars / command resolution per-run.
- Adapter receives a new top-level field `cli_selection` in stdin and uses it instead of `ANPM_ADAPTER_AGENT` / `ANPM_ADAPTER_MODEL` env vars when present (env vars stay as a fallback).

**Tests**: end-to-end through `LeaseNextLocalConnectorRun` fixture; adapter unit test verifies stdin parsing with `cli_selection`.

**Size**: M. One migration, three handler changes, connector-side dispatch logic, adapter parser update.

### S3 — Frontend: CLI binding management UI

**Scope**:
- Extend `pages/AccountBindings.tsx` to render a "CLI bindings" section. Add-CLI-binding form with provider preset cards (Claude Code, Codex) and model-id input.
- New helper `frontend/src/utils/cliBindingPresets.ts` mirroring the structure of `planningConnectionPresets.ts` (keep the file under 100 LOC).
- Smoke test for the new form.

**Tests**: render with empty state, render with one CLI binding, submit creates expected payload.

**Size**: M. ~200 LOC of TSX + 1 utility file + 1 smoke test.

### S4 — Frontend: CLI binding picker in PlanningLauncher

**Scope**:
- `pages/ProjectDetail/planning/PlanningLauncher.tsx` gains a CLI binding picker that appears when execution mode is `local_connector`.
- Shows health badge (✓ healthy, ⚠ session_expired, ✗ unhealthy) using server-reported `cli_health`.
- Default selection: user's primary CLI binding (most-recently-used active binding).
- Picker is hidden when the user has zero CLI bindings; existing behaviour kicks in (use the connector's default adapter).

**Tests**: render with one healthy binding, render with one expired binding, submit picks the expected `cli_binding_id`.

**Size**: S. ~80 LOC of TSX + 1 smoke test.

### S5 — Failure typing + remediation

**Scope**:
- `submit-result` accepts new optional `error_kind` + `remediation_hint` fields. Server stores them on `planning_runs`. Backwards compatible (old connectors still post just `error_message`).
- Adapter (`adapters/backlog_adapter.py`) parses Claude / Codex CLI exit signatures and emits `error_kind` for the seven enum values listed in §5.4.
- `pages/ProjectDetail/planning/CandidateReviewPanel.tsx` and the failure flash banner show the remediation hint when present.
- Health probe UX: extend `pages/AccountBindings.tsx` CLI-binding row to show last-known health from connector heartbeat.

**Tests**: adapter unit tests for each enum value's signature detection; UI test that remediation hint renders when `error_kind="session_expired"`.

**Size**: M. Largest UX surface change. Adapter parsing logic + 2 frontend components + new server fields.

### Dependency graph

```
S1 (data model) ──┬── S2 (per-run plumbing) ──┬── S4 (launcher picker)
                  │                            │
                  └── S3 (binding mgmt UI) ────┘
                                                S5 (failure typing) — depends on S2 only
```

S1 must merge first. S3 and S2 can ship in parallel after S1. S4 needs both S2 and S3. S5 only needs S2.

## 8. Acceptance criteria (per slice)

Every slice PR MUST:

1. Pass all four CI jobs (governance / frontend / backend-sqlite / backend-postgres).
2. Migrations apply cleanly under both SQLite and PostgreSQL (the dual-runtime decision).
3. Include at least one smoke test per new component / handler.
4. Update this doc's §10 Status table with slice completion.
5. Not touch backend schema unless explicitly called out (S1, S2, S5 may; others must not).

Path B itself completes when:

- A user with only Claude Code CLI installed can pair their machine, register one CLI binding via the UI (no env-var editing), and run planning end-to-end.
- A user can switch between Claude and Codex per-run from the launcher (assuming both bindings exist).
- A run that fails because the CLI session expired shows a remediation hint instead of a raw exit-code message.

## 9. Open questions for owner review

These are the load-bearing decisions where the doc is intentionally ambiguous until you weigh in:

**Q1 — Extend `account_bindings` vs new `cli_bindings` table?**
Doc currently proposes extending. Argument for new table: cleaner schema, no `cli:` prefix kludge on `provider_id`. Argument against: duplicates per-user uniqueness logic, CRUD endpoints, and resolver code. Recommendation: **extend**. Decide.

**Q2 — Where does CLI selection live: per-run, or connector default + per-run override?**
Doc proposes per-run, treating the connector as a generic executor. Alternative: keep the current behavior (connector startup picks the CLI) and add per-run as an *optional* override. Alternative is more backwards-compatible but keeps the existing operational invisibility. Recommendation: **per-run with sensible default to "user's primary binding"**. Decide.

**Q3 — Where to store `cli_health`: new table, or `local_connectors.metadata` JSONB column?**
New table is cleaner for queries. JSONB is faster to ship and survives connector reboots without separate migrations. Recommendation: **JSONB for v1, separate table when we have a reason to query it (cross-connector dashboards, capability routing)**. Decide.

**Q4 — Adapter env-var fallback: keep, deprecate, or hard-remove?**
S2 proposes the new `cli_selection` stdin field overrides env vars when present. Should env vars be hard-removed in S2, hard-removed in a later slice, or stay forever as a power-user escape? Recommendation: **keep as fallback indefinitely** — they're useful for testing and don't cost anything. Decide.

**Q5 — Codex CLI scope.**
Owner mentions Codex CLI is "claimed" — i.e. they may not have it installed today. Should S3's CLI-binding form still ship Codex as a preset card, or only Claude Code in v1 with Codex added in a follow-up once tested? Recommendation: **ship both presets**; the worst case is the user picks Codex without the binary installed and gets a typed `cli_not_found` failure, which is exactly what the typed-failure work is for. Decide.

**Q6 — Multi-binding per CLI?**
Should a user be able to register two `cli:claude` bindings (e.g. "Work Claude" + "Personal Claude" pointing at different binary paths)? The DB unique constraint `(user_id, provider_id, label)` already permits this — it's a UI question whether to expose it. Recommendation: **allow but don't promote** — the form lets you set a custom label, and the launcher picker shows label + provider. Decide.

## 10. Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| S1 — data model | not started | — | — |
| S2 — per-run plumbing | not started | — | — |
| S3 — binding mgmt UI | not started | — | — |
| S4 — launcher picker | not started | — | — |
| S5 — failure typing | not started | — | — |

## 11. Out of scope references

- **Copilot CLI / VS Code session extraction** — separate design when (if) the scope changes.
- **Cross-connector capability routing** — multi-device problem, not in scope.
- **Cost / token accounting** — separate design.
- **Auto-retry of failed runs** — explicitly rejected in §4 to avoid masking config problems.
- **OAuth-style in-app CLI login** — CLIs own auth; not changing that.

---

Source: `[agent:feature-planner]`. Cites `subscription-connector-mvp.md`, `local-connector-context.md`, `credential-binding-design.md`. No code lands until §9 questions are resolved and this doc is approved.
