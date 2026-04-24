# Phase 4 plan — UX debt from early-operator friction

**Status**: draft · 2026-04-24 · `[agent:feature-planner]`
**Gates**: no Phase 4 coding lands until this doc is approved by the owner.
**Precondition**: Phase 3 (PR #20) is merged. All five Phase 3 slices shipped 2026-04-24.

---

## 1. Problem statement

A fresh operator opened `http://127.0.0.1:3980/projects/<id>` and the local `/settings/*` pages and ran into five distinct UX frictions within a single session. These are not bugs in shipped features; they are gaps in the **information architecture** between those features. None were on the Phase 3 punch list.

**Gap 1 — ProjectDetail has seven co-equal tabs.**
`frontend/src/pages/ProjectDetail.tsx` exposes Overview, Workspace (Planning), Tasks, Documents, Drift, Activity (Agents), and Settings as a flat row. A first-time operator cannot tell which tab is the main entry point or which ones are operational/observability surfaces. Settings in particular is rarely the thing the operator opens ProjectDetail to look at, but it competes for attention equally with Workspace.

**Gap 2 — "My Account Bindings" and "My Connector" are not visibly distinguished.**
Both pages configure "which model runs my planning jobs," but one means "the server calls a provider directly" and the other means "my machine's CLI runs the job." The account dropdown lists them as peers with no framing. AccountBindings itself hosts two sub-concepts (API-Key Bindings, CLI Bindings) whose naming collides with My Connector's CLI-focused terminology.

**Gap 3 — CLI bindings cannot be edited.**
`AccountBindings.tsx` CLI rows only expose Set Primary / Delete (`AccountBindings.tsx:655-662`). Changing the `model_id` (for example, upgrading `claude-sonnet-4-5` to `claude-sonnet-4-6`) requires deleting the binding and re-creating it. The backend `PATCH /api/me/account-bindings/:id` already supports all the patchable fields — the Update endpoint works — it is purely a UI omission.

**Gap 4 — My Connector shows adapter liveness but not model usability.**
Phase 3 S5b added `<cli_command> --version` health probes (log-only; `MyConnector.tsx:26-35` badge). That answers "is the CLI on disk and executable?" but not "can this CLI actually complete a prompt with the configured model?" There is no operator-facing way to trigger a minimal end-to-end completion against a paired machine's CLI binding and see `ok / latency`.

**Gap 5 — Phase 3 open questions left uncodified.**
`phase3-plan.md` §8 recorded three deferred decisions (probe `updated_at` bump, SQLite `by-evidence` JSON strategy, dashboard batching threshold) and §9 left the Status Tracking table stale (everything says "not started" but all five slices are merged). `DECISIONS.md` and `phase3-plan.md` need a sync pass.

**Operator questions without answers today:**
- "Where do I start when I open a project page?" (Gap 1)
- "Do I want a binding or a connector — or are those the same thing with different names?" (Gap 2)
- "How do I change the model my Claude CLI uses without deleting my binding?" (Gap 3)
- "The connector shows green — but does the model actually respond?" (Gap 4)
- "Is Phase 3 done or still in progress?" (Gap 5)

---

## 2. Current state inventory

### Frontend

| File | Current state | What is missing |
|---|---|---|
| `frontend/src/pages/ProjectDetail.tsx` | 7-tab flat rail (`:547-576`); Settings is a peer tab (`:572`) | Tab grouping / demotion / gear affordance. Deep-link compatibility (`?tab=settings`) must be preserved. |
| `frontend/src/App.tsx` | Account dropdown lists "My Connector", "API Keys", "Account Bindings", "Model Settings" as peers (`:363-368`) | No hub page that frames the three execution paths (API key, local runtime, local CLI via connector). |
| `frontend/src/pages/AccountBindings.tsx` | Page titled "My Account Bindings"; two H2 sections "API-Key Bindings" and "CLI Bindings" (`:436`, `:614`). CLI binding cards render Set-Primary + Delete only (`:655-662`) | (a) Section naming conflicts with MyConnector's CLI framing. (b) No Edit UI for CLI rows. |
| `frontend/src/pages/MyConnector.tsx` | Shows readiness badge (liveness + adapter) per paired machine (`:26-35`); CLI-probe button absent | No probe trigger for "run a minimal completion on this CLI binding from this connector." |

### Backend

| File | Current state |
|---|---|
| `backend/internal/handlers/account_bindings.go` | `Update` at `:92` accepts `label`, `model_id`, `cli_command` patches. All the backend pieces for Gap 3 already exist. |
| `backend/internal/handlers/local_connectors.go` | `Heartbeat`, `ClaimNextRun`, `SubmitPlanningRunResult`, `RunStats` — no probe-request channel server→connector. `metadata.cli_health` is populated by existing S5b heartbeat loop. |
| `backend/internal/connector/service.go` | `probeCliCommand` runs `<cmd> --version`; no "run a minimal prompt" probe. |
| `backend/internal/models/local_connector.go` | `metadata.cli_health.<binding_id>` — Phase 3 S5b. |

### Docs

| File | Current state |
|---|---|
| `docs/phase3-plan.md` §9 status table | All 5 slices marked "not started" but all are merged in PR #20 / PR #19. |
| `docs/phase3-plan.md` §8 open questions | Three open items without resolution: probe `updated_at`, JSON path strategy, pending-count batching. |
| `DECISIONS.md` | No entry yet for the Phase 3 open-question resolutions. No entry for the ProjectDetail IA reshape that P4-1 introduces. |

---

## 3. End state

### 3-A: First-session onboarding (Tier 1)

**P4-1 ProjectDetail IA slim-down**

ProjectDetail's tab rail becomes a **2-tier** structure:

- **Primary (always visible)**: Workspace · Overview · Tasks · Documents
- **Secondary (under a "More ▾" dropdown)**: Drift · Activity
- **Gear icon** (project header, right of the "Sync Now" button): opens Settings

The default landing tab is still Workspace (Phase 2 S5 decision — unchanged). `?tab=drift` / `?tab=agents` / `?tab=settings` deep links still resolve correctly; the rail just surfaces them differently. The secondary group shows an unread-count dot when Drift has open signals or Activity has recent runs, so demotion never hides actionable state.

No tab is removed; no data flow changes. The only visible behaviour change is tab placement.

**P4-2 Model Settings hub + naming cleanup**

The account dropdown gains a new top-level entry **"Model Settings"** that routes to `/settings/models-hub` (new page). The hub is a one-screen explainer:

```
How do you want planning runs to execute?

┌── Option A: Server calls a hosted API ──────────────────────┐
│  • Use your API key (OpenAI, Mistral, Anthropic, …) or     │
│    point at a local OpenAI-compatible server (Ollama,       │
│    LM Studio).                                              │
│  • Runs happen on the server that's hosting Agent Native PM.│
│  [ Configure API-key bindings → ]                           │
└─────────────────────────────────────────────────────────────┘

┌── Option B: Server runs a CLI on the same host ─────────────┐
│  (local-mode only)                                          │
│  • Use a CLI subscription (Claude Code, Codex) that is      │
│    installed on THIS machine.                               │
│  • Runs happen on the same machine as the server.           │
│  [ Configure server-side CLI bindings → ]                   │
└─────────────────────────────────────────────────────────────┘

┌── Option C: Your own machine runs the CLI ──────────────────┐
│  • Pair a laptop/workstation that has Claude Code or Codex  │
│    installed. The server dispatches planning jobs to that   │
│    machine.                                                 │
│  [ Set up My Connector → ]                                  │
└─────────────────────────────────────────────────────────────┘
```

(The existing admin-only `/settings/models` page for shared server API key is linked from Option A as the "shared key" variant; the existing user-level `/settings/account-bindings` is the "personal key" variant.)

Alongside the hub, two inline renames clarify the two CLI surfaces:
- `AccountBindings.tsx` H2 "CLI Bindings" → "Server-side CLI Bindings (local-mode only)"
- `MyConnector.tsx` subtitle gets one sentence: "My Connector runs the CLI on **your** machine, not on the server."

The existing URLs `/settings/account-bindings`, `/settings/connector`, `/settings/models` are unchanged. The hub lives at a new path.

**P4-3 CLI binding edit**

Each CLI binding card in `AccountBindings.tsx` gets an Edit button next to Set Primary / Delete. Clicking Edit reveals an inline form with three fields:
- Label
- Model ID
- CLI command (optional)

Save calls `PATCH /api/me/account-bindings/:id` (already exists). Cancel collapses the form. No schema change; no new endpoint.

### 3-B: Reliability signal closure (Tier 2)

**P4-4 Connector model probe**

A new operator affordance: from either `MyConnector` or a CLI binding card in `AccountBindings`, click **"Test on this connector"** → the server asks the connector to run the adapter with a minimal prompt against the selected binding → result comes back as `{ ok, latency_ms, content, error? }` and renders next to the existing `PersistentProbeStatus` row.

Flow:

1. `POST /api/me/local-connectors/:id/probe-binding { binding_id }` — server validates the connector belongs to the user and the binding is a CLI binding of the same user; writes a **pending probe request** into `local_connectors.metadata.pending_cli_probe_requests[]` with a fresh `probe_id`, binding snapshot (model_id, cli_command, adapter type), and `requested_at`. Returns `{ probe_id }` immediately.

2. Connector on next heartbeat receives the pending probe in the heartbeat response. Runs the existing built-in adapter (`backend/internal/connector/builtin_adapter.go`) with a minimal hardcoded prompt that asks the CLI to answer a trivial backlog prompt (one short requirement → one candidate is fine). The connector is free to use a reduced `max_candidates: 1` to keep it fast.

3. Connector reports the outcome on the next heartbeat body: `cli_probe_results: [{ probe_id, ok, latency_ms, content, error_kind?, error_message? }]`. Server stores under `local_connectors.metadata.cli_probe_results.<probe_id> = {...}` with a `completed_at` timestamp.

4. Frontend polls `GET /api/me/local-connectors` (or a new narrow endpoint `GET /api/me/local-connectors/:id/probe-binding/:probe_id`) every 2 seconds for up to 30 seconds after click; renders the result in place. On timeout, shows "probe still pending — connector may be offline or busy."

5. On binding delete (same sweep as S5b), pending + completed probe results for that `binding_id` are scrubbed.

**Scope reductions** to keep P4-4 shippable in one slice:
- Probe does NOT create a `PlanningRun` row. It is a separate ephemeral object stored only in connector metadata. This keeps planning-run history clean and avoids a follow-on migration.
- Probe prompt is hardcoded in the connector side; the server does not send prompt text. This preserves the D7 "no free-text instructions over the wire" principle.
- Only one in-flight probe per `binding_id` per connector at a time; a second click returns the existing `probe_id` until the first completes or times out.
- Results retained for 24h then GC'd by a nightly sweep (or on heartbeat if `> 24h`, whichever is cheaper — implementer chooses).

### 3-C: Doc hygiene (Tier 2)

**P4-5 Phase 3 resolutions + status sync**

- `DECISIONS.md` gains one entry per resolved open question:
  - Probe recording DOES bump `updated_at` (current behaviour stays).
  - SQLite `by-evidence` uses Go-layer JSON scan; revisit at > 500 candidates/project.
  - Dashboard pending-count uses per-project fetch when project count ≤ 20; batch endpoint is deferred until the threshold is crossed.
  - ProjectDetail primary/secondary IA split (the P4-1 decision itself).
- `phase3-plan.md` §9 status table updated to reflect merged PRs (#19 for 3-A-1; #20 for 3-A-2/3-B-1/3-B-2/3-C-1).
- `docs/api-surface.md` gains entries for the P4-4 probe endpoints.
- `docs/data-model.md` notes the new `pending_cli_probe_requests[]` / `cli_probe_results{}` keys under `local_connectors.metadata`.
- `ARCHITECTURE.md` "Near-Term Architectural Direction" section updated to reflect Phase 3 complete, Phase 4 active.

### 3-D: Deferred (roadmap record only)

- **Copilot CLI adapter**. `gh copilot` contract mismatch; revisit when Copilot ships an agent-style CLI.
- **Path B server mode** (still deferred from Phase 3 §3-D).
- **SSE horizontal scaling** (still deferred).
- **Phase 4 Collaboration in `product-blueprint.md` §127** (auth/roles/search/notifications) — intentionally out of Phase 4's UX-debt scope. A future Phase 5 will address it.

---

## 4. Non-goals

- **No new planning-run row for probes.** Probe results are ephemeral connector metadata; they are not planning history.
- **No Copilot CLI preset.** The existing `cliBindingPresets` stays at Claude + Codex.
- **No new tabs added to ProjectDetail.** Only re-grouping.
- **No rename of route URLs.** Deep links from bookmarks / docs must continue to resolve.
- **No change to the Phase 2 default-tab decision.** Workspace stays the default per the 2026-04-22 ADR.
- **No multi-CLI-per-connector probe queuing.** One in-flight probe per `(connector_id, binding_id)` at a time; subsequent clicks reuse the in-flight probe.
- **No authenticated session reuse for Copilot / ChatGPT web subscriptions.** Out of scope; same constraint as the existing QuickStart disclaimer on AccountBindings.
- **No backend changes for P4-1, P4-2, P4-3.** All three are frontend-only.

---

## 5. Slice plan

### Slice P4-1: ProjectDetail IA slim-down

**Scope**

1. `ProjectDetail.tsx` (`:547-576`): introduce three rail groups — primary buttons stay inline; secondary buttons render inside a "More ▾" popover; Settings becomes a gear icon in the project header (right of "Sync Now", line 478-480).
2. Popover logic: clicking "More ▾" toggles a small absolute-positioned menu listing Drift and Activity with their existing counts. Clicking an item activates the tab and closes the popover. The "More ▾" button itself shows an aggregated badge (sum of `openDriftCount + (agentRuns.length > 0 ? 1 : 0)` or a simple dot) so operators are not blind to demoted state.
3. Gear button: an `<button aria-label="Project settings">⚙</button>` opens the Settings tab (same state change as before).
4. The active-tab highlight logic (`tab === 'overview' ? 'is-active' : ''`) is preserved; when `tab === 'drift' | 'agents'` the "More ▾" button itself renders in active style.
5. `ProjectDetail.test.tsx` (or whichever test file drives the rail) is adjusted — both primary clicks and popover clicks are exercised.

**Definition of Done**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-P4-1-1 | Initial render: primary rail shows Workspace, Overview, Tasks, Documents | Four buttons visible |
| T-P4-1-2 | "More ▾" closed by default; Drift and Activity not in the primary rail | Render assertion |
| T-P4-1-3 | Click "More ▾" → Drift and Activity appear as menu items | Menu render |
| T-P4-1-4 | Click Drift in popover → tab switches to drift; popover closes; URL updates to `?tab=drift` | Navigation assertion |
| T-P4-1-5 | Click gear icon → tab switches to settings; URL updates to `?tab=settings` | Navigation assertion |
| T-P4-1-6 | Deep link `/projects/:id?tab=settings` → Settings tab rendered; gear icon shows as active | Deep-link compat |
| T-P4-1-7 | Deep link `/projects/:id?tab=drift` → Drift tab rendered; "More ▾" shows active style | Deep-link compat |
| T-P4-1-8 | Project has 3 open drift signals → "More ▾" shows a dot/count indicator | Demotion does not hide state |
| T-P4-1-9 | Click outside popover → popover closes | Dismiss behavior |

**Impacted modules**

```
ProjectDetail.tsx
  └ project-rail nav block (lines 547-576)  ← REORGANIZED
  └ page-header action block (lines 477-487) ← ADD gear
ProjectDetail.test.tsx  ← NEW test cases
```

**Dependencies**: none. Frontend-only.
**Size estimate**: S. ~80 LOC TSX, ~40 LOC test, ~10 LOC CSS.

---

### Slice P4-2: Model Settings hub + naming cleanup

**Scope**

1. New file `frontend/src/pages/ModelSettingsHub.tsx` — renders the three-option card layout described in §3-A P4-2.
2. `App.tsx` routing: add `<Route path="/settings/models-hub" element={<ModelSettingsHub />} />` in both local-mode and authenticated-mode route blocks.
3. `App.tsx` account dropdown (`:363-368`): add a **top-level** "Model Settings" link pointing at `/settings/models-hub`. The three existing links (My Connector, Account Bindings, admin-only Model Settings for shared key) stay but visually grouped as sub-entries under the new hub link.
4. `AccountBindings.tsx`:
   - H1 subtitle gains one line: "For CLI-subscription bindings that run on **your own machine**, see My Connector instead."
   - CLI Bindings H2 label (`:614`) → "Server-side CLI Bindings (local-mode only)".
5. `MyConnector.tsx`:
   - Subtitle gains one line: "My Connector runs the CLI on **your** machine, not on the server. For a server-side CLI binding, see Account Bindings."

**Definition of Done**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-P4-2-1 | Navigate to `/settings/models-hub` in authenticated mode → hub page renders with three cards | Render assertion |
| T-P4-2-2 | Navigate to `/settings/models-hub` in local mode → hub page renders | Render assertion |
| T-P4-2-3 | Click "Configure API-key bindings →" → router lands on `/settings/account-bindings` | Nav assertion |
| T-P4-2-4 | Click "Configure server-side CLI bindings →" → router lands on `/settings/account-bindings` (same page, scrolls to or highlights CLI section if easy; otherwise just lands) | Nav assertion |
| T-P4-2-5 | Click "Set up My Connector →" → router lands on `/settings/connector` | Nav assertion |
| T-P4-2-6 | Account dropdown shows "Model Settings" as top-level; sub-links retained | Render assertion |
| T-P4-2-7 | AccountBindings CLI H2 reads "Server-side CLI Bindings (local-mode only)" | Text assertion |
| T-P4-2-8 | MyConnector subtitle includes the disambiguating sentence | Text assertion |

**Impacted modules**

```
ModelSettingsHub.tsx       ← NEW
App.tsx                    ← 1 new route, dropdown tweak
AccountBindings.tsx        ← H2 rename, subtitle addition
MyConnector.tsx            ← subtitle addition
```

**Dependencies**: none. Frontend-only, no backend.
**Size estimate**: S. ~120 LOC TSX (hub + CSS), ~20 LOC misc copy, ~15 LOC tests.

---

### Slice P4-3: CLI Binding Edit UI

**Scope**

1. `AccountBindings.tsx` CLI binding card: add a local state `editingCliId: string | null` and a form state object for the in-flight edit.
2. New Edit button in the CLI row action group (`:654-662`), next to Set Primary / Delete. When `editingCliId === binding.id`, the card renders an inline form with three fields (label, model_id, cli_command) and Save / Cancel.
3. Save calls existing `updateAccountBinding(id, payload)` with `{ label, model_id, cli_command }` and reloads. Validation mirrors Create-form rules (non-empty model_id; cli_command optional).
4. Cancel clears `editingCliId` and discards local edits.
5. No backend change. No test-matrix for backend.

**Definition of Done**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-P4-3-1 | Click Edit on a CLI binding → inline form appears with current values pre-filled | Render assertion |
| T-P4-3-2 | Change model_id → Save → card re-renders with new model_id; PATCH called with `{model_id}` | API + render assertion |
| T-P4-3-3 | Click Cancel → form collapses; no API call | No-op assertion |
| T-P4-3-4 | Save with empty model_id → validation error shown; no PATCH | Client validation |
| T-P4-3-5 | Save with server 409 (duplicate label) → error banner shown; form stays open | Error surface |
| T-P4-3-6 | Only one binding editable at a time — opening Edit on binding B while A is in edit mode cancels A's edit | Single-editor invariant |
| T-P4-3-7 | API-key bindings do NOT get an Edit button (existing API-key flow unchanged) | Scope assertion |

**Impacted modules**

```
AccountBindings.tsx       ← editingCliId state + inline form + Edit button
AccountBindings.test.tsx  ← new cases (if the test file exists) or skip
```

**Dependencies**: none.
**Size estimate**: S. ~80 LOC TSX, ~40 LOC test.

---

### Slice P4-4: Connector model probe

**Scope**

1. **Store / models** (`backend/internal/models/local_connector.go` + `store/local_connector_store.go`):
   - `LocalConnectorHeartbeatRequest` gains optional `cli_probe_results []CliProbeResult`.
   - `LocalConnectorHeartbeatResponse` gains `pending_cli_probe_requests []PendingCliProbeRequest`.
   - New store methods: `EnqueueCliProbe(connectorID, userID string, req PendingCliProbeRequest) (probe_id string, err error)`, `RecordCliProbeResults(connectorID string, results []CliProbeResult) error`, `ScrubCliProbesForBinding(userID, bindingID string) error`, `ListPendingCliProbes(connectorID string) ([]PendingCliProbeRequest, error)` (called inside the heartbeat response).
2. **Handler** (`backend/internal/handlers/local_connectors.go`):
   - New `POST /api/me/local-connectors/:id/probe-binding` → validates ownership, looks up the CLI binding, calls `EnqueueCliProbe`, returns `{probe_id}`.
   - `Heartbeat` now reads `pending_cli_probe_requests` and embeds them in the response; processes any `cli_probe_results` from the request body.
   - New `GET /api/me/local-connectors/:id/probe-binding/:probe_id` → returns the stored result or `{status: "pending"}`.
3. **Connector** (`backend/internal/connector/service.go` + `app.go`):
   - Heartbeat cycle: when response contains `pending_cli_probe_requests`, enqueue locally; a worker goroutine processes them sequentially using the built-in adapter with a minimal prompt (one fake backlog context, `max_candidates: 1`).
   - Results pushed up on next heartbeat.
4. **Account-binding delete sweep** (`account_bindings.go` Delete handler): after successful delete, call `ScrubCliProbesForBinding(userID, bindingID)` for all connectors of that user.
5. **Frontend**:
   - `AccountBindings.tsx` CLI cards: new "Test on connector" button. Behind the scenes, picks the **first online** connector for the user (there is no per-connector primary flag; all online connectors are equivalent probe targets). A future enhancement may surface a picker when multiple are online — tracked as a follow-up, not part of this slice. Fires `probeBinding`, polls `getCliProbeResult` every 2s up to 30s, renders the result inline next to the card.
   - `MyConnector.tsx`: optional — a "test each CLI binding" button that iterates. Scope-reducible if time-constrained.
6. **API client** (`frontend/src/api/client.ts`): `probeBindingOnConnector(connectorId, bindingId)` and `getCliProbeResult(connectorId, probeId)`.

**Definition of Done**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-P4-4-1 | `POST /api/me/local-connectors/:id/probe-binding` with valid binding → 200 with `probe_id` | Happy path |
| T-P4-4-2 | Same call with unknown binding → 404 | Auth + ownership |
| T-P4-4-3 | Same call with binding belonging to another user → 404 | Cross-user isolation |
| T-P4-4-4 | Same call with API-key binding → 400 (CLI bindings only) | Input validation |
| T-P4-4-5 | Heartbeat response carries the enqueued probe request | Response embedding |
| T-P4-4-6 | Connector side: probe executes built-in adapter with `max_candidates=1`; reports back on next heartbeat | Integration (stub CLI) |
| T-P4-4-7 | `GET /api/me/local-connectors/:id/probe-binding/:probe_id` before completion → `{status: "pending"}` | Poll-not-ready |
| T-P4-4-8 | After completion → `{status: "completed", ok: true, latency_ms, content}` | Poll-ready |
| T-P4-4-9 | CLI stub exits non-zero → result `{ok: false, error_kind, error_message}` | Failure path |
| T-P4-4-10 | Deleting the binding scrubs pending + completed results for that binding | Cleanup |
| T-P4-4-11 | Second probe for same binding while one is in-flight → same `probe_id` returned | Dedup invariant |
| T-P4-4-12 | Frontend: clicking "Test on connector" shows pending spinner; resolves to green/red within 30s polling window | UI flow |
| T-P4-4-13 | Frontend: 30s timeout renders "probe still pending" message, does not error | Timeout handling |

**Impacted modules**

```
models/local_connector.go              ← new types
store/local_connector_store.go         ← 4 new methods
handlers/local_connectors.go           ← new POST + new GET + heartbeat extension
handlers/account_bindings.go           ← scrub on delete
connector/service.go                   ← heartbeat-pull + worker
connector/builtin_adapter.go           ← (reused as-is; supply minimal prompt)
api/client.ts                          ← 2 new calls
AccountBindings.tsx                    ← "Test on connector" button + polling
MyConnector.tsx                        ← optional bulk-test affordance
```

**Dependencies**: P4-3 is NOT a blocker; P4-2 adds conceptual framing that makes P4-4 easier to explain but is not a code dependency.
**Size estimate**: M. ~180 LOC Go (models + store + handlers), ~100 LOC Go (connector service), ~90 LOC TSX (button + polling), ~13 tests.

---

### Slice P4-5: Phase 3 resolutions + status sync (docs-only)

**Scope**

1. `DECISIONS.md` — append four entries (today's date, `[agent:feature-planner]` source marker):
   - Phase 3 open question: `updated_at` bump on probe — decision **keep**.
   - Phase 3 open question: `by-evidence` SQLite JSON strategy — decision **Go-layer scan until > 500 candidates/project**.
   - Phase 3 open question: Dashboard pending count batching — decision **per-project fetch up to 20 projects**.
   - ProjectDetail primary/secondary IA split (P4-1 ratification).
2. `docs/phase3-plan.md` §8 → each open question annotated with the resolution date and a pointer to the DECISIONS entry.
3. `docs/phase3-plan.md` §9 status table → all 5 slices marked `shipped` with PR #s and merge dates.
4. `docs/api-surface.md` — append probe-binding endpoint contracts (P4-4).
5. `docs/data-model.md` — append the `local_connectors.metadata.pending_cli_probe_requests[]` and `cli_probe_results{}` keys.
6. `ARCHITECTURE.md` § "Near-Term Architectural Direction" — Phase 3 marked complete, Phase 4 marked active with a pointer to this plan.

**Definition of Done**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-P4-5-1 | `DECISIONS.md` has 4 new entries, each with `[agent:feature-planner]` source marker | Presence check |
| T-P4-5-2 | `phase3-plan.md` §9 status table has no "not started" rows | Status sync |
| T-P4-5-3 | `make lint-governance` passes | Policy check |
| T-P4-5-4 | `make decisions-conflict-check TEXT="ProjectDetail IA"` reports no contradiction | Conflict check |

**Dependencies**: best landed AFTER P4-1 and P4-4 to avoid document churn. Can ship in the same PR as P4-1 or in a trailing docs PR.
**Size estimate**: XS. ~200 LOC docs.

---

## 6. Implementation order

```
Group 1 (independent, can start immediately — all frontend-only)
  P4-1  ProjectDetail IA slim-down
  P4-2  Model Settings hub + naming
  P4-3  CLI Binding Edit UI

Group 2 (after Group 1 validates — backend + frontend)
  P4-4  Connector model probe

Group 3 (after Group 1+2 land — docs only)
  P4-5  Phase 3 resolutions + status sync
```

PR strategy:
- **PR-A**: P4-1 + P4-2 + P4-3 bundled (pure frontend; reviewable together)
- **PR-B**: P4-4 standalone (backend protocol change warrants its own PR)
- **PR-C**: P4-5 standalone (doc-only)

Ordering within each slice: types/contracts first, core logic second, wiring third, tests alongside. Pre-PR critic is mandatory on PR-A and PR-B (not on PR-C per operating-rules §Pre-PR critic review exemption for doc-only PRs).

---

## 7. Risk assessment

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | Demoted tabs (Drift, Activity) hidden from operator → missed drift signals | Medium | Medium | "More ▾" shows count/dot indicator when demoted tabs have actionable state (T-P4-1-8) |
| R2 | Users with existing bookmarks to `?tab=drift` land on a disappeared tab | Low | High | Deep-link routing preserved (T-P4-1-7) |
| R3 | Hub page becomes a dead-end if the three targets feel redundant | Low | Low | Hub is additive; the three targets remain directly linkable from account dropdown |
| R4 | Probe pending-request goroutine on connector blocks heartbeat if CLI hangs | Medium | Medium | Probe runs in a worker goroutine, not the heartbeat thread; 60s hard timeout per probe |
| R5 | Probe creates cost (real LLM call); operator accidentally spams it | Medium | Low | UI debounces; dedup invariant (T-P4-4-11); prompt is minimal (`max_candidates: 1`) |
| R6 | `ScrubCliProbesForBinding` misses a connector row if the user has many connectors | Low | Low | Scrub iterates all connectors of the user; indexed by `user_id` already (Phase 3 schema) |
| R7 | Probe result grows `local_connectors.metadata` unbounded | Low | Medium | 24h GC sweep on heartbeat; result blob capped at the existing 1 MiB per-body cap |
| R8 | CLI binding Edit form allows changing `provider_id` via the UPDATE endpoint | Low | High | `UpdateAccountBindingRequest` does not expose `provider_id` — already enforced backend-side (`models/account_binding.go:91`) |
| R9 | `More ▾` popover accessibility — escape/focus-trap | Low | Low | Standard `role="menu"` + keyboard-dismiss on Esc + focus-return on close |
| R10 | Hub + rename land in a branch where AccountBindings has merge conflicts with in-flight Phase 3 hotfixes | Low | Low | No Phase 3 hotfixes expected; rebase before PR-A |

---

## 8. Open questions

1. **P4-4 probe endpoint**: should the enqueue call return a fresh `probe_id` or return 409 on duplicate? Current plan: return the existing `probe_id` (idempotent UI). Confirm before landing.
2. **P4-4 poll vs SSE**: poll is simpler; SSE would be more elegant. Current plan: poll for PR-B; consider SSE in a follow-up if polling proves chatty in practice.
3. **P4-2 hub URL**: `/settings/models-hub` or `/settings/models/overview`? Current plan: `/settings/models-hub` (avoids colliding with the admin-only `/settings/models` page).

---

## 9. Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| P4-1 — ProjectDetail IA slim-down | implemented | bundled | pending PR |
| P4-2 — Model Settings hub + naming | implemented | bundled | pending PR |
| P4-3 — CLI Binding Edit UI | implemented | bundled | pending PR |
| P4-4 — Connector model probe | implemented | bundled | pending PR |
| P4-5 — Phase 3 resolutions + status sync | implemented | bundled | pending PR |

**Phase 4 is delivered as one bundled PR** per owner request. `make lint-governance` + frontend + backend tests all green before PR creation.

---

Source: `[agent:feature-planner]`. References `docs/phase3-plan.md`, `docs/path-b-subscription-cli-bridge-design.md`, `docs/api-surface.md`, `docs/data-model.md`, `AGENTS.md`, `docs/operating-rules.md`. Reads `frontend/src/pages/ProjectDetail.tsx`, `frontend/src/pages/AccountBindings.tsx`, `frontend/src/pages/MyConnector.tsx`, `frontend/src/App.tsx`, `backend/internal/handlers/account_bindings.go`, `backend/internal/handlers/local_connectors.go`, `backend/internal/handlers/remote_models.go`, `backend/internal/connector/service.go`. No code lands until owner approval of this document.
