# Phase 6a plan — Workspace onboarding UX + connector-owned CLI configs

**Status**: approved · 2026-04-24 · `[agent:feature-planner]`
**Gates**: owner approval satisfied. Scope is split across TWO PRs. Part 1 (this PR) ships the CLI-confusion cleanup — UX-B1 + UX-B2 + UX-B3 + UX-A7. Part 2 (follow-up) ships the workspace onboarding UX — UX-A1 + UX-A2 + UX-A3 + UX-A4 + UX-A5 + UX-A6. The split is deliberate: the backend/UX redesign around cli_configs reshapes the planning-launcher data flow, and landing that surface cleanly before layering the one-line-entry UX keeps each review focused.
**Precondition**: Phase 5 (PR #22) is merged on `main`. The three-phase pre-PR gate from `docs/operating-rules.md` applies.

---

## 1. Problem statement

Two overlapping UX pains surfaced after Phase 5 shipped:

**Gap A — First-time operators cannot start without knowing the vocabulary.**
Opening a project today lands on an empty Workspace with a "No requirements yet" placeholder. To produce any output the operator must: know what a "requirement" is, create one, understand execution_mode (deterministic / server_provider / local_connector), pick a CLI binding when in connector mode, pick a model, wait for candidates, approve, apply. Six different mental models before the first backlog appears. A non-technical user's drop-off point is somewhere around step three.

**Gap B — CLI configuration lives at the wrong level.**
Phase 3 Path B stashed CLI configs in `account_bindings` where `provider_id LIKE 'cli:*'` — a user-level list. Phase 4 P4-2 labelled that UI section "Server-side CLI Bindings (local-mode only)" but the label is misleading: those same rows are also used when a connector on the user's machine runs the CLI. The real mental model is "this connector on this machine has these CLIs available with these models" — config belongs to the connector, not to the user globally. A user with two paired machines (Laptop with Claude, Workstation with Codex) cannot correctly express that today; the server just picks "any" online connector and hopes the CLI works.

**Operator questions without answers today:**
- "I want to build X — where do I start?" (Gap A)
- "Why do I have to pick execution_mode? What does it mean?" (Gap A)
- "My Laptop has Claude, my Workstation has Codex — how do I configure that?" (Gap B)
- "What's the difference between 'Server-side CLI Bindings' and 'My Connector'?" (Gap B)

---

## 2. Current state inventory

### Workspace entry

| File | Current state |
|---|---|
| `frontend/src/pages/ProjectDetail/PlanningTab.tsx` | Renders RequirementQueue + PlanningLauncher + CandidateReviewPanel. Empty state = "No requirements yet" text with a create-requirement form. |
| `frontend/src/pages/ProjectDetail/planning/PlanningLauncher.tsx` | Exposes execution_mode radio, CLI binding select, model override input, all always-visible. |
| `frontend/src/pages/ProjectDetail/planning/hooks/usePlanningWorkspaceData.ts` | Loads `cliBindings` only when `execution_mode === 'local_connector'`; filters `account_bindings` to `cli:*`. |

### CLI config location

| Current storage | Where |
|---|---|
| Per-user cli:* bindings | `account_bindings` table, `provider_id IN ('cli:claude', 'cli:codex')` |
| Connector-side runtime health | `local_connectors.metadata.cli_health.<binding_id>` (S5b) and `local_connectors.metadata.cli_probe_results.<probe_id>` (P4-4) |
| Connector-specific CLI configs | **does not exist** |

### Backlog prompt

| File | Variables consumed today |
|---|---|
| `backend/internal/prompts/backlog.md` | `PROJECT_NAME`, `PROJECT_DESCRIPTION_LINE`, `REQUIREMENT`, `MAX_CANDIDATES`, `CONTEXT`, `SCHEMA_VERSION` |

Parity golden: `adapters/testdata/backlog_render_golden.txt`. Any variable addition must ship a refreshed golden.

---

## 3. End state

### Tier A — Workspace onboarding UX

**UX-A1: One-line Workspace entry + What's Next button**

Empty Workspace renders a single large input:

```
┌──────────── Workspace ────────────┐
│  What are you working on?         │
│  ┌─────────────────────────────┐  │
│  │ Describe it in one line…    │  │
│  └─────────────────────────────┘  │
│  [ Start planning →         ]     │
│                                    │
│  ─────── or ───────                │
│  Don't know where to start?       │
│  [ What should I focus on next? ] │
└────────────────────────────────────┘
```

Clicking "Start planning":
1. Creates a `Requirement` with `title = input`, `source = "onboarding"`
2. Resolves the default provider via existing `GET /api/projects/:id/planning-provider-options` (already returns `default_selection` + `resolved_binding_source`)
3. Fires `POST /api/requirements/:id/planning-runs` with the resolved selection — no execution_mode / binding picker dialog
4. Scrolls to the run's status + eventual candidates

"What should I focus on next?" fires the same resolution + `adapter_type="whatsnext"` planning run against a synthetic "scan current state" requirement (or against `null` if whatsnext supports that — we'll surface whichever works with zero additional clicks).

If the provider resolution returns "no usable configuration", the button is disabled and a CTA links to `/settings/models-hub`.

**UX-A2: Progressive disclosure in PlanningLauncher**

The existing launcher (shown once a requirement is selected) collapses its `execution_mode` radio, CLI binding select, and model override under a single `[ Advanced ▾ ]` toggle. Default state: collapsed. No layout jump when expanded.

**UX-A3: What / Who / Success wizard**

On the one-line input, an optional `[ Refine (audience + success) ]` link opens a modal:

- **What** — pre-filled from the input
- **Who** — "Who's this for?" (single-line)
- **Success** — "How do we know it worked?" (multi-line)

Saving the modal merges the three into one `Requirement.description`, then triggers planning as in A1. Server-side contract: `backlog.md` gains `{{AUDIENCE_LINE}}` and `{{SUCCESS_LINE}}` (both pre-computed blank when absent). Parity golden updated; old callers (expert path, no wizard) render identically to Phase 5.

**UX-A4: Demo seed for empty projects**

When a user opens a project that has **zero requirements AND zero planning runs AND zero tasks**, the Workspace renders a dismissible banner:

```
New here? Try the demo: we'll drop a sample requirement + approved
backlog into this project so you can see the full loop.  [ Show me ] [ Not now ]
```

"Show me" creates:
- One `Requirement` titled "Demo: Build a task-tracker SaaS"
- One deterministic `PlanningRun` with 3 pre-seeded `BacklogCandidate` rows (status = draft, evidence empty, content hand-written) so the candidate review panel has something real to render
- A `planning-runs/:id/status` = `completed` so the UI lands on review mode

"Not now" dismisses via localStorage `anpm_demo_dismissed_<project_id>`.

**UX-A5: Jargon tooltips**

Seven terms get inline `<abbr title="…">`-style tooltips across the app:
- `requirement` → "A short description of something you want to build or fix"
- `backlog candidate` → "A concrete task the agent proposes; you approve before it becomes a real task"
- `execution mode` → "How the planning run is carried out — by the server, by a deterministic planner, or by a CLI on your machine"
- `primary binding` → "The default provider used when you don't explicitly pick one"
- `CLI config` → "A specific CLI + model combination installed on one of your connectors"
- `connector` → "A paired machine that runs planning work locally"
- `dispatch` → "Sending a planning run to a connector for execution"

Implementation: a small shared `<Jargon term="…">` component that looks up the table and renders `abbr` with `title` + dotted underline. Zero schema change.

**UX-A6: Primary binding visual in Model Settings Hub**

`ModelSettingsHub.tsx` shows each binding with a "Primary" badge when `is_primary`, and every non-primary binding gains a "Make primary" button (wraps existing `PATCH /api/me/account-bindings/:id { is_primary: true }`). Previously the Set-Primary affordance lived only on the CLI binding card inside AccountBindings; this brings it to the hub.

**UX-A7: Legacy CLI bindings section rename + banner**

`AccountBindings.tsx` H2 "Server-side CLI Bindings (local-mode only)" → "CLI Bindings (legacy, pre-Phase 6)". Adds an above-section banner:

```
CLI configuration has moved. Add your CLI + model directly on the
connector that runs it — open My Connector.  [ Take me there ]
```

No deletion of existing bindings; they continue to work for planning runs that reference them by id. New bindings should not be created here; the "+ Add CLI Binding" button is hidden (the create flow is removed, edit and delete of existing rows stays).

### Tier B — Connector-owned CLI configs (no migration)

**UX-B1: `cli_configs[]` in connector metadata**

New key `local_connectors.metadata.cli_configs` — a JSON array of:

```jsonc
{
  "id": "uuid",              // stable id, server-generated
  "provider_id": "cli:claude" | "cli:codex",
  "cli_command": "/usr/bin/claude",   // optional, empty = PATH lookup
  "model_id": "claude-sonnet-4-6",
  "label": "My Claude",
  "is_primary": true,                  // exactly one can be primary per connector
  "created_at": "…",
  "updated_at": "…"
}
```

Zero migration — the `metadata` JSONB column has existed since migration 025. The server treats an unset `cli_configs` key as empty.

New store methods on `LocalConnectorStore`:

- `ListCliConfigs(connectorID, userID) []CliConfig`
- `AddCliConfig(connectorID, userID, req) (CliConfig, error)`  — generates id, enforces shape, optionally auto-sets `is_primary=true` when it's the first config
- `UpdateCliConfig(connectorID, userID, configID, req) (CliConfig, error)`
- `DeleteCliConfig(connectorID, userID, configID) error`
- `SetPrimaryCliConfig(connectorID, userID, configID) error`  — atomically demotes other configs within the same connector

All five paths run inside the existing `beginWriteTx` helper so metadata RMW stays race-free (same pattern as Phase 4 P4-4 probe-state methods).

**UX-B2: MyConnector page CLI configs UI**

Each connector card gains a new section:

```
┌─ Laptop (online) ─────────────────────┐
│ …existing readiness + stats…          │
│                                        │
│ CLIs on this machine                  │
│ ┌──────────────────────────────────┐  │
│ │ claude ● primary                 │  │
│ │ /usr/bin/claude · claude-sonnet… │  │
│ │ [ Edit ] [ Delete ]              │  │
│ ├──────────────────────────────────┤  │
│ │ codex                            │  │
│ │ /opt/homebrew/bin/codex · codex… │  │
│ │ [ Set primary ] [ Edit ] [ Del ] │  │
│ └──────────────────────────────────┘  │
│ [ + Add CLI ]                          │
└────────────────────────────────────────┘
```

"+ Add CLI" opens an inline form:
- Preset buttons: Claude / Codex (pre-fills `provider_id` + suggested `cli_command` + default `model_id`)
- Manual: `label`, `cli_command`, `model_id`

Health dot comes from the existing Phase 3 S5b `cli_last_healthy_at` timestamp (coarse "this machine ran a CLI OK recently") — the per-config health signal is deferred to Phase 6b.

**UX-B3: Planning run integration with cli_config**

`CreatePlanningRunRequest` (models + API) gains two optional fields:

```go
ConnectorID   *string `json:"connector_id,omitempty"`
CliConfigID   *string `json:"cli_config_id,omitempty"`
```

Resolution precedence at planning-run create time:
1. If `cli_config_id` is provided → must match a config on the named `connector_id` owned by the user. Server snapshots it onto `PlanningRun.connector_cli_info.binding_snapshot` (reusing the existing snapshot shape).
2. Else if `account_binding_id` is provided → existing Phase 3 / Phase 5 path (legacy).
3. Else if `execution_mode == "local_connector"` and exactly one config across the user's connectors is primary → auto-resolve.
4. Else → 400 with a clear "specify a CLI config" message.

The connector-side `claim-next-run` response already returns `PlanningRunCliBindingPayload { id, provider_id, model_id, cli_command, label }`. We reuse that shape unchanged — the connector does not need to know whether the snapshot came from a legacy `account_bindings` row or from a new `cli_config`.

### Tier C — Deferred to Phase 6b (roadmap record only)

- Execution layer + isolation (Docker-based sandbox / project-scoped tmpdir / network scope)
- Per-config health probe results (today's `cli_probe_results` map is probe-id-keyed; Phase 6b will also key by config_id)
- Role dispatch actually firing (Phase 5 shipped the breadcrumb; Phase 6b wires the runner)

---

## 4. Non-goals

- **No migration of existing cli:* `account_bindings`.** They keep working as a fallback (legacy resolution path in UX-B3). New users start fresh on the per-connector model; existing users reconfigure manually under My Connector when they want per-connector control. Explicit owner decision.
- **No deletion of the `account_bindings` cli:* rows.** The store and data stay. Only the UI encourages migration.
- **No change to `execution_mode` semantics.** `manual` / `role_dispatch` stay as Phase 5 defined them. UX-A2 just hides the selector.
- **No role-catalog enforcement on cli_config.** Today `cli_command` is a free-form string; validation is the same as for account_bindings (regex sanity check, no allowlist).
- **No change to the Phase 4 ProjectDetail tab layout.** The Workspace tab gets a different empty state, nothing else moves.
- **No shared-between-connectors CLI config.** If you want Claude on both Laptop and Workstation, you add it on each. This is deliberate — the whole point is per-connector clarity.

---

## 5. Slice plan

### Slice P6a-B1 — `cli_configs[]` store + handlers

**Scope**
1. `models.CliConfig` struct + `CreateCliConfigRequest` / `UpdateCliConfigRequest`.
2. Store methods listed in §3 Tier B UX-B1.
3. Handlers:
   - `GET /api/me/local-connectors/:id/cli-configs` — list
   - `POST /api/me/local-connectors/:id/cli-configs` — create
   - `PATCH /api/me/local-connectors/:id/cli-configs/:config_id` — update
   - `DELETE /api/me/local-connectors/:id/cli-configs/:config_id` — delete
   - `POST /api/me/local-connectors/:id/cli-configs/:config_id/primary` — set primary
4. All routes authenticated + enforce `connector.user_id == caller.user.id`.
5. Validation: `provider_id` must be one of `AllowedCLIProviders`; `cli_command` sanity regex same as account_bindings; exactly one `is_primary=true` per connector enforced atomically in `SetPrimaryCliConfig` (same pattern as `is_primary` demotion in account_bindings).

**Definition of Done**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-B1-1 | POST creates with valid body | 201 + persisted |
| T-6a-B1-2 | POST with duplicate `(provider_id, label)` on same connector | 409 |
| T-6a-B1-3 | POST first config on a connector auto-sets `is_primary=true` | Yes |
| T-6a-B1-4 | POST subsequent config does NOT auto-set primary | `is_primary=false` unless request says otherwise |
| T-6a-B1-5 | PATCH with `is_primary: true` demotes other configs on same connector | Atomic |
| T-6a-B1-6 | PATCH / DELETE / primary endpoints enforce connector.user_id | 404 on cross-user |
| T-6a-B1-7 | DELETE removes entry from metadata | `List` no longer returns it |
| T-6a-B1-8 | Concurrent POST + heartbeat on same connector | Both commit; no lost update (uses beginWriteTx) |
| T-6a-B1-9 | Invalid cli_command (e.g. `; rm -rf /`) | 400 (regex sanity) |

**Size**: M. ~180 LOC Go + 120 LOC tests.

### Slice P6a-B2 — MyConnector UI

**Scope**
1. `frontend/src/api/client.ts` adds: `listConnectorCliConfigs`, `createConnectorCliConfig`, `updateConnectorCliConfig`, `deleteConnectorCliConfig`, `setPrimaryConnectorCliConfig`.
2. `MyConnector.tsx` renders a new "CLIs on this machine" section per connector (see §3 mock).
3. Inline Add form with preset buttons Claude / Codex (pre-fills `provider_id` + `cli_command` placeholder + default `model_id` from `cliBindingPresets`).
4. Per-row Edit / Delete / Set-Primary.
5. Loading + empty states.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-B2-1 | Empty connector → "+ Add CLI" button + instruction text | Render |
| T-6a-B2-2 | Connector with 2 configs → both listed, primary badge on one | Render |
| T-6a-B2-3 | Click "+ Add CLI" → inline form with Claude / Codex preset buttons | Render |
| T-6a-B2-4 | Fill + Save → POST; row appears without reload | Optimistic or reload OK |
| T-6a-B2-5 | Click Delete → confirm dialog → DELETE → row removed | Happy path |
| T-6a-B2-6 | Click Set Primary on a non-primary row → PATCH → badge moves | Render |

**Size**: M. ~200 LOC TSX + 60 LOC tests.

### Slice P6a-B3 — Planning run wiring

**Scope**
1. `CreatePlanningRunRequest` gains `ConnectorID *string` + `CliConfigID *string`.
2. Server resolution logic per §3 Tier B UX-B3.
3. Resolution error paths:
   - `cli_config_id` points at a non-existent config → 400
   - `connector_id` set but not owned by user → 404
   - `execution_mode == "local_connector"` + no primary config + no explicit config_id + no legacy account_binding → 400 with a resolver hint
4. Snapshot re-uses `PlanningRun.connector_cli_info.binding_snapshot` — no new field.
5. Frontend PlanningLauncher: when `execution_mode === "local_connector"`, the CLI binding picker now shows `cli_configs` (grouped by connector) instead of user-level `account_bindings`. Legacy `cli:*` account_bindings still listed under a "Legacy" group with a hint.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-B3-1 | Create with `(connector_id, cli_config_id)` matching a real config | 201 + binding_snapshot populated from config |
| T-6a-B3-2 | Create with mismatched pair (config belongs to different connector) | 400 |
| T-6a-B3-3 | Create with only `cli_config_id` and no `connector_id` | 400 (we require both for disambiguation) |
| T-6a-B3-4 | Create with `account_binding_id` pointing at a legacy cli:* row | still works (back-compat) |
| T-6a-B3-5 | Auto-resolution: one primary cli_config across user's connectors | picks it |
| T-6a-B3-6 | Auto-resolution: zero primary, non-zero legacy account_bindings | falls back to primary legacy binding (UX-A7 banner's promise) |

**Size**: M. ~100 LOC Go + ~80 LOC TSX + 60 LOC tests.

### Slice P6a-A1 — One-line Workspace entry

**Scope**
1. New component `WorkspaceOnboardingPanel.tsx` rendered when `requirements.length === 0`.
2. One-line input + Start button + "What should I focus on next?" secondary button.
3. On Start:
   - POST `/api/projects/:id/requirements` with `title`, `source: "onboarding"`
   - Read `planning-provider-options` for the project (already cached in hook)
   - POST `/api/requirements/:id/planning-runs` with the resolved default selection
   - No launcher dialog
4. On "What next":
   - POST `/api/projects/:id/planning-runs` (new endpoint if not already present — actually use a synthetic requirement or a project-level endpoint if it exists; to be confirmed during impl)
5. Error path: no usable provider → disabled button + link to `/settings/models-hub`.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A1-1 | Empty project → panel renders with input + CTAs | Smoke |
| T-6a-A1-2 | Type "Build login" + Start → requirement created + planning run queued | Integration |
| T-6a-A1-3 | No provider configured → Start button disabled + link visible | Render assertion |
| T-6a-A1-4 | What's Next button fires a planning run with `adapter_type="whatsnext"` | Smoke |
| T-6a-A1-5 | After start, panel hidden once requirements.length > 0 | Render |

**Size**: M. ~150 LOC TSX + 60 LOC tests.

### Slice P6a-A2 — Progressive disclosure

**Scope**
1. `PlanningLauncher.tsx`: wrap the execution_mode radio + binding picker + model override in a `<details>` or stateful toggle `Advanced ▾`.
2. Default collapsed.
3. Toggle state persists via localStorage `anpm_launcher_advanced_open`.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A2-1 | Default render → advanced hidden | Render |
| T-6a-A2-2 | Click Advanced → fields visible; localStorage set | Click + check |
| T-6a-A2-3 | Reload → preference remembered | Smoke |

**Size**: S. ~40 LOC TSX + 20 LOC tests.

### Slice P6a-A3 — Wizard + prompt variables

**Scope**
1. `backlog.md` adds `{{AUDIENCE_LINE}}` and `{{SUCCESS_LINE}}` — both pre-computed to `""` when the caller provides no value.
2. Golden fixture refreshed.
3. `builtin_adapter.go` + `backlog_adapter.py` pass the two new vars (both default `""`).
4. New `RequirementWizardModal.tsx` with 3 steps (What / Who / Success).
5. Server-side: `CreateRequirementRequest` gains optional `audience`, `success_criteria`. On create, if present, these are folded into `Requirement.description` (or stored in a new nullable column; to be decided during impl — smallest path is just prepending structured text to `description`).

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A3-1 | Wizard with all three fields → requirement description contains all three | Server assertion |
| T-6a-A3-2 | Prompt render with AUDIENCE + SUCCESS filled → backlog.md body has both | Go + Python golden |
| T-6a-A3-3 | Prompt render with blank AUDIENCE + SUCCESS → byte-identical to Phase 5 output | Golden regression |

**Size**: M. ~120 LOC TSX + ~40 LOC Go + 30 LOC Python + 30 LOC tests.

### Slice P6a-A4 — Demo seed

**Scope**
1. `WorkspaceOnboardingPanel.tsx` renders a "Try the demo" banner when `requirements.length === 0 && tasks.length === 0 && planningRuns.length === 0` AND `localStorage.anpm_demo_dismissed_<pid>` is unset.
2. "Show me" button calls a new server endpoint `POST /api/projects/:id/demo-seed` which:
   - Creates one hand-written demo requirement
   - Creates one completed planning run attached to it
   - Creates 3 hand-written backlog candidates (status=draft)
3. "Not now" sets localStorage and hides the banner.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A4-1 | Completely empty project → banner renders | Render |
| T-6a-A4-2 | Project with any content → banner hidden | Render |
| T-6a-A4-3 | Click "Show me" → seed endpoint fires; Workspace now shows 1 requirement + 1 run + 3 candidates | Integration |
| T-6a-A4-4 | Click "Not now" → banner hidden; localStorage dismissed | Smoke |
| T-6a-A4-5 | POST demo-seed on a non-empty project → 409 (no clobber) | Error case |

**Size**: M. ~80 LOC Go + ~80 LOC TSX + 40 LOC tests.

### Slice P6a-A5 — Tooltips

**Scope**
1. `frontend/src/utils/jargon.ts` — term → plain-language dict.
2. `frontend/src/components/Jargon.tsx` — `<Jargon term="…">label</Jargon>` wraps `abbr` with `title` + dotted-underline class.
3. Apply to 7 high-traffic sites: PlanningLauncher execution mode label, CandidateReviewPanel "backlog candidate" header, Workspace "requirement", MyConnector "connector" title, AccountBindings "primary" badge, apply dialog "dispatch" label, MyConnector "CLI config" section header.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A5-1 | Hover over "requirement" → browser tooltip appears | Smoke (title attr presence) |
| T-6a-A5-2 | 7 jargon sites audited | Grep + snapshot |

**Size**: XS. ~60 LOC TSX + 20 LOC tests.

### Slice P6a-A6 — Primary binding visual

**Scope**
1. `ModelSettingsHub.tsx`: per path card, list the matching binding name + "Primary" badge / "Make primary" button.
2. Cross-link: "Make primary" calls `PATCH /api/me/account-bindings/:id { is_primary: true }` and re-fetches.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A6-1 | User with 2 API-key bindings, one primary → hub shows badge on that card | Render |
| T-6a-A6-2 | Click Make Primary → PATCH fires; badge moves | Integration |

**Size**: S. ~50 LOC TSX + 20 LOC tests.

### Slice P6a-A7 — Legacy CLI rename + banner

**Scope**
1. AccountBindings.tsx H2 renamed + banner added.
2. Create flow hidden (only Edit / Delete existing rows).
3. Links to `/settings/connector`.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-6a-A7-1 | Open AccountBindings → "CLI Bindings (legacy)" heading + banner | Render |
| T-6a-A7-2 | "+ Add CLI Binding" button not rendered | Negative assertion |
| T-6a-A7-3 | Existing rows still have Edit + Delete + Set Primary buttons | Back-compat |

**Size**: S. ~30 LOC TSX + 20 LOC tests.

---

## 6. Implementation order

```
Day 1–2   P6a-B1  cli_configs store + handlers + tests
Day 3     P6a-B2  MyConnector UI
Day 4     P6a-B3  Planning run wiring
Day 5     P6a-A7 + P6a-A2  (legacy rename + progressive disclosure)
Day 6     P6a-A6 + P6a-A5  (primary visual + tooltips)
Day 7     P6a-A1  (one-line entry)
Day 8     P6a-A3  (wizard + prompt vars)
Day 9     P6a-A4  (demo seed)
Day 10    Docs sync + make pre-pr + critic + /security-review + risk-reviewer
Day 11    Fix findings + open PR
```

---

## 7. Risk assessment

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | `cli_configs[]` metadata growth unbounded | Low | Low | Per-connector cap of 16 configs (same as account_bindings.configured_models); reject 17th with 409 |
| R2 | Auto-resolver picks a config on a revoked connector | Medium | Medium | Resolver must filter `connector.status != 'revoked'`; test T-6a-B3-5 covers primary pick; add revoked-filter assertion |
| R3 | Wizard `description` merge format changes break existing callers | Low | Medium | New wizard data goes into a structured prefix (`## Audience\n…\n\n## Success\n…\n`); if callers parse description as raw text they see the prefix gracefully |
| R4 | Demo seed produces candidates that conflict with real project conventions | Low | Low | Demo candidates are hand-written generic text; only land in truly empty projects |
| R5 | `backlog.md` variable addition breaks existing golden | Low | Low | Pre-computed lines default to `""`; the Phase 5 golden remains byte-identical when the wizard vars are empty |
| R6 | MyConnector UI shows legacy cli:* bindings next to new cli_configs — double-counted | Medium | Low | MyConnector shows only cli_configs; legacy bindings remain under AccountBindings only |
| R7 | Frontend test for the demo-seed 409 path flakes because seed is non-idempotent | Low | Low | Server-side: demo-seed returns 409 if ANY of (requirements, planning_runs, tasks) is non-empty on the project |
| R8 | Tooltip component bundle bloat | Low | Low | Zero-dep `<abbr>` wrapper; negligible |

---

## 8. Open questions

1. **Demo-seed endpoint auth**: local-mode only? Or any authenticated user? Current plan: any authenticated user, scoped to their project.
2. **"What should I focus on next?" button target**: does whatsnext require a requirement? Current planner code takes a requirement — we can either synthesise a "state-of-project" requirement or add a project-scoped whatsnext. Decision during impl.
3. **Wizard data persistence**: store as structured columns on `requirements` (new `audience`, `success_criteria`) or just fold into `description`? Current plan: fold into `description` with a `## Audience` / `## Success` markdown prefix — zero schema change, parseable if future UI wants to.
4. **Legacy cli:* in PlanningLauncher picker**: still show them or hide? Current plan: show under a "Legacy" group with a hint, never as the default.
5. **Per-connector cap**: 16 vs 32 vs unlimited with UI warning? Current plan: 16 (matches `MaxAccountBindingConfiguredModels`).

---

## 9. Status tracking

| Slice | Status | PR |
|---|---|---|
| P6a-B1 — cli_configs store + handlers | **implemented** | Part 1 |
| P6a-B2 — MyConnector CLI UI | **implemented** | Part 1 |
| P6a-B3 — Planning run integration | **implemented** | Part 1 |
| P6a-A7 — Legacy CLI rename | **implemented** | Part 1 |
| P6a-A1 — One-line Workspace entry | pending | Part 2 |
| P6a-A2 — Progressive disclosure | pending | Part 2 |
| P6a-A3 — Wizard + prompt vars | pending | Part 2 |
| P6a-A4 — Demo seed | pending | Part 2 |
| P6a-A5 — Tooltips | pending | Part 2 |
| P6a-A6 — Primary binding visual | pending | Part 2 |
| Docs sync + pre-PR gate | implemented (Part 1) | Part 1 |

**Phase 6a complete** when all 10 slices + docs sync are merged AND the three-phase pre-PR gate is green.

---

Source: `[agent:feature-planner]`. References `docs/phase5-plan.md`, `docs/phase4-plan.md`, `docs/operating-rules.md` § "Pre-PR verification". No code lands until the Phase 6a PR passes the three-phase gate.
