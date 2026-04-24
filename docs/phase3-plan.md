# Phase 3 plan — Local mode completeness + Planning depth + Dashboard aggregation

**Status**: draft · 2026-04-24 · `[agent:feature-planner]`
**Gates**: no Phase 3 coding lands until this doc is approved by the owner.
**Precondition**: Phase 2 (Planning Workspace) is complete (all five slices merged). Path B slices S1–S5a are shipped. S5b is not yet shipped and is the last open Path B slice.

---

## 1. Problem statement

Three operator-facing gaps remain after Phase 2 and Path B S1–S5a ship:

**Gap A — Probe results disappear on refresh (Probe persistence)**
`POST /api/me/probe-model` tests a binding's connection but writes nothing to the database. Every page refresh loses the test result. The operator has no way to see "was this binding recently tested and is it still reachable?" without re-testing manually.

**Gap B — Path B S5b is unshipped (CLI health probe)**
The adapter error classifier (7 enum values), the connector `--version` health probe cycle, the heartbeat `cli_health[]` extension, and the AccountBindings health badge UI are all defined in the Path B design doc but have not been implemented. S5b is the last Path B slice and is a prerequisite for full CLI bridge operability.

**Gap C — Planning workspace evidence is unidirectional**
`CandidateReviewPanel` already shows clickable evidence links (doc and drift) from the candidate's perspective. The reverse direction is not surfaced: `DocumentsTab` has no column showing "contributed to candidates," `DriftTab` has no column showing "cited in candidates," and `TasksTab` shows no requirement → run → candidate chain for tasks that originated from planning. This was an explicit Phase 2 non-goal.

**Gap D — Dashboard project cards are counts only**
`Dashboard.tsx` shows per-project health counts but not the most actionable signal: how many candidates are currently awaiting the operator's review decision across all projects. There is no cross-project attention row.

**Operator questions without answers today:**
- "Is my Mistral binding still reachable as of an hour ago?" (Gap A)
- "Is my Claude CLI healthy right now on the connector machine?" (Gap B)
- "Which project most urgently needs my planning review attention?" (Gap D)
- "What candidates did this document contribute to?" (Gap C)

---

## 2. Current state inventory

### Backend

| File | Responsibility | Current state |
|---|---|---|
| `backend/internal/handlers/remote_models.go` | `POST /api/me/probe-model` handler | Probe executes, latency measured, result returned — but NOT persisted. `bindingStore.RecordProbe(...)` does not exist. |
| `backend/internal/store/account_binding_store.go` | CRUD + query for `account_bindings` | No probe-recording method. `ListByUser` SELECT does not include probe columns (they don't exist yet). |
| `backend/internal/models/` (inferred) | `AccountBinding` struct | No `LastProbeAt`, `LastProbeOK`, `LastProbeMS` fields |
| `backend/db/migrations/` | Schema migrations | Latest applied: `023_local_connectors_protocol.sql`. Next number available: `024`. |
| `adapters/backlog_adapter.py` | CLI adapter — invokes Claude/Codex | No `error_kind` classification in `_invoke_agent`. Current error paths return free-text strings only. |
| `backend/internal/connector/` | Connector daemon subcommands | No health probe loop. No `--cli-health-interval` / `--cli-health-disabled` flags. Heartbeat does not send `cli_health[]`. |
| `backend/internal/handlers/local_connectors.go` | Heartbeat handler, claim/submit | Heartbeat does not accept `cli_health[]`. Server does not write to `local_connectors.metadata.cli_health`. |

### Frontend

| File | Current state | What is missing |
|---|---|---|
| `frontend/src/pages/AccountBindings.tsx` | Binding cards show "Test" button; `cardProbeResults` is purely in-component state | No persistent probe display. `last_probe_at`, `last_probe_ok`, `last_probe_ms` not rendered because they don't exist in the API response. CLI binding rows have no health badge. |
| `frontend/src/pages/ProjectDetail/planning/CandidateReviewPanel.tsx` | Evidence rows are clickable links (Phase 2 S3) | No reverse cross-link in DocumentsTab or DriftTab. No failure banner for `error_kind` (S5a ships banner; this is already done). |
| `frontend/src/pages/ProjectDetail/DocumentsTab.tsx` | Lists documents; shows drift associations | No "contributed to N candidates" column or modal |
| `frontend/src/pages/ProjectDetail/DriftTab.tsx` | Lists drift signals with resolve/dismiss | No "cited in N candidates" indicator |
| `frontend/src/pages/ProjectDetail/TasksTab.tsx` | Lists tasks with filters | No lineage chip showing requirement → run → candidate chain |
| `frontend/src/pages/Dashboard.tsx` | Project cards with health counts and notifications | No per-card "N pending review" count. No cross-project attention row. |
| `frontend/src/pages/ProjectDetail/planning/AppliedLineage.tsx` | Shows lineage lane within Planning Workspace | Correct and complete; no change needed here |

### DB migration state

Migrations `021`, `022`, `023` shipped with Path B. The `account_bindings` table currently has `cli_command` and `is_primary` but no probe columns. Migration `024` is next.

---

## 3. End state

### 3-A: Local mode completeness

**3-A-1 Probe persistence**

`account_bindings` gains three nullable columns (`last_probe_at`, `last_probe_ok`, `last_probe_ms`). When the operator clicks "Test" on any binding card and the probe completes, the result is written to the database immediately. The binding list response returns these columns. The card renders:
```
Last tested: 3 min ago  ✓ 45 ms
```
or
```
Last tested: 2 min ago  ✗ Connection refused
```
Bindings never tested render no probe line (all three columns are NULL until first probe).

**3-A-2 Path B S5b (adapter classifier + health probe + UI badges)**

The `backlog_adapter.py` classifies CLI failures into 7 `error_kind` values. The connector daemon adds a health probe loop (`<cli_command> --version`, default 5-minute interval). Heartbeat sends `cli_health[]` to the server. The server stores the health map in `local_connectors.metadata.cli_health`. AccountBindings CLI rows show a health badge ("✓ healthy (claude 1.5.0, checked 12s ago)" / "? stale" / "⚠ cli_not_found").

### 3-B: Planning workspace depth

**3-B-1 Evidence cross-links (Documents + Drift reverse-view)**

Each document row in `DocumentsTab` shows a chip "N candidates" when at least one backlog candidate lists that document's id in `evidence_detail.documents`. Clicking opens a compact modal listing those candidates (title, status, planning run label, requirement title) with a link that deep-dives into the Planning Workspace with that run pre-selected. Same pattern for drift signals in `DriftTab`: each row shows "cited in N candidates" chip.

No backend schema change is required. The existing `evidence_detail` JSONB on `backlog_candidates` contains `documents[].document_id` and (implicitly) drift evidence. A new lightweight query endpoint is needed: `GET /api/projects/:id/backlog-candidates/by-evidence?document_id=X` (or drift_signal_id=Y).

**3-B-2 Task lineage full UI**

Each task row in `TasksTab` that has a `task_lineage` record of kind `applied_candidate` shows a lineage chip: "Via planning: [requirement title] → [run status] → [candidate title]". Clicking any segment deep-links into the Planning Workspace with the correct run pre-selected. The data is already in the `task_lineage` table and the `listProjectTaskLineage` API endpoint already exists (used by `AppliedLineage.tsx`). This is purely a `TasksTab` UI addition.

### 3-C: Dashboard cross-project aggregation

**3-C-1 Dashboard pending decisions**

Each project card in `Dashboard.tsx` shows a supplementary line "N candidates pending review" when `N > 0`. Candidates with `status = 'draft'` belonging to a `completed` planning run count as pending. A new endpoint `GET /api/projects/:id/pending-review-count` returns `{ count: N }`. The cross-project attention section adds a sorted list of projects with pending decisions: the project with the most pending candidates appears first, with a direct link to that project's Planning Workspace tab.

### 3-D: Deferred (roadmap record only)

- **SSE horizontal scaling** — deferred until multi-instance deployment is required.
- **Path B server mode** — needs a separate design: admin-managed binary allowlist, interpreter denylist, sandboxing. Not Path B's job per design D8.

---

## 4. Non-goals

- **No server-mode CLI bindings.** D8 gate from the Path B design remains in force. CLI bindings are local-mode only.
- **No new planning providers** beyond the existing `openai-compatible`, `deterministic`, `local_connector`.
- **No AI auto-approve of candidates.** Every candidate still requires explicit human approval (2026-04-17 decision).
- **No rewrite of DocumentsTab or DriftTab.** Both stay as siblings; only new cross-link columns are added.
- **No new top-level routes.** All changes are within existing routes and tabs.
- **No SSE horizontal scaling.** Per-process broker is sufficient for the current deployment model.
- **No multi-active bindings per CLI provider.** The existing `idx_account_bindings_active_unique` constraint is unchanged.
- **No probe history table** — only the most recent probe result is kept (three nullable columns on the binding row). A full probe-history time series is a future enhancement.
- **No task `status` filtering in the lineage chip** — the chip is shown regardless of task status; filtering is a future UX refinement.
- **No bulk-apply of candidates from the Dashboard.**

---

## 5. Slice plan

### Slice 3-A-1: Probe result persistence

**Scope**

1. Migration `024_account_bindings_probe_history.sql` — add three nullable columns to `account_bindings`:
   ```sql
   ALTER TABLE account_bindings ADD COLUMN last_probe_at  TIMESTAMPTZ;
   ALTER TABLE account_bindings ADD COLUMN last_probe_ok  BOOLEAN;
   ALTER TABLE account_bindings ADD COLUMN last_probe_ms  INTEGER;
   ```
   Sibling `024_account_bindings_probe_history.down.sql` drops the three columns.

2. `backend/internal/store/account_binding_store.go` — new method `RecordProbe(id, userID string, ok bool, latencyMS int64) error`. Uses a single `UPDATE account_bindings SET last_probe_at=$1, last_probe_ok=$2, last_probe_ms=$3, updated_at=$4 WHERE id=$5 AND user_id=$6`. The `updated_at` bump is intentional: updated_at tracks "last write" not "last user edit."

3. `backend/internal/models/` — `AccountBinding` struct gains `LastProbeAt *time.Time`, `LastProbeOK *bool`, `LastProbeMS *int`.

4. `backend/internal/store/account_binding_store.go` — `ListByUser` SELECT and `GetByID` SELECT gain the three new columns in the scan.

5. `backend/internal/handlers/remote_models.go` — `Probe` handler: after `writeSuccess`, call `h.bindingStore.RecordProbe(*req.BindingID, user.ID, result.OK, latencyMS)` when `req.BindingID != nil`. Probe-recording failure is logged but does not change the 200 response already sent.

6. `frontend/src/pages/AccountBindings.tsx` — binding cards (both API-key and CLI) render a "Last tested" row when `last_probe_at` is non-null. Format: relative time + OK/fail indicator + latency for OK probes. The existing `ProbeReport` component (rendered in `cardProbeResults`) is unchanged; the persistent row is a separate sub-component `PersistentProbeStatus` added inline or extracted to a sibling file if `AccountBindings.tsx` approaches 800 LOC.

7. `docs/api-surface.md` — update `GET /api/me/account-bindings` response to document the three new fields.
8. `docs/data-model.md` — add the three probe columns to the `account_bindings` table entry.

**Definition of Done — explicit test matrix**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-3A1-1 | Migration `024` applies under SQLite (local mode) | Clean apply; three nullable columns exist |
| T-3A1-2 | Migration `024` applies under PostgreSQL | Clean apply |
| T-3A1-3 | `024.down.sql` drops the three columns under both drivers | Clean drop; existing data unaffected |
| T-3A1-4 | `RecordProbe` with valid `(id, userID)` writes all three columns | SELECT confirms values match probe result |
| T-3A1-5 | `RecordProbe` with unknown `id` or wrong `userID` | `rows_affected == 0`; no error returned (silent; caller already sent 200) |
| T-3A1-6 | `ListByUser` returns bindings with NULL probe columns when never probed | All three fields are nil/null in JSON response |
| T-3A1-7 | `POST /api/me/probe-model` with `binding_id` → succeeds → `GET /api/me/account-bindings` → binding row has non-null `last_probe_at`, `last_probe_ok`, `last_probe_ms` | Integration: probe persisted |
| T-3A1-8 | `POST /api/me/probe-model` without `binding_id` → probe runs → `GET /api/me/account-bindings` → no binding row changes | Probe without binding ID does not write |
| T-3A1-9 | Frontend: binding card with non-null `last_probe_at` renders "Last tested: N min ago ✓ 45 ms" | Smoke test render assertion |
| T-3A1-10 | Frontend: binding card with null `last_probe_at` renders no "Last tested" row | Smoke test render assertion |
| T-3A1-11 | Frontend: binding card with `last_probe_ok=false` renders failure indicator | Smoke test render assertion |

**Impacted modules (call chain)**

```
AccountBindings.tsx (click "Test")
  → probeModel() in frontend/src/api/client.ts
  → POST /api/me/probe-model
  → remote_models.go Probe()
  → [sends HTTP probe to provider]
  → bindingStore.RecordProbe()         ← NEW
  → account_binding_store.go           ← NEW method
  → account_bindings table (024 migration)
  → writeSuccess(w, 200, result, nil)
AccountBindings.tsx (page load)
  → listAccountBindings()
  → GET /api/me/account-bindings
  → account_binding_store.go ListByUser() ← extended SELECT
  → AccountBinding struct               ← new fields
  → AccountBindings.tsx renders PersistentProbeStatus  ← NEW
```

**API contract impact**

`GET /api/me/account-bindings` response: each binding object gains three new nullable fields:
```jsonc
{
  "last_probe_at": "2026-04-24T12:00:00Z",   // null if never probed
  "last_probe_ok": true,                       // null if never probed
  "last_probe_ms": 45                          // null if never probed
}
```
No breaking change. Pre-existing clients ignore unknown fields. `docs/api-surface.md` must be updated in this PR.

**DB / migration impact**

Migration `024_account_bindings_probe_history.sql`: three nullable `ALTER TABLE ADD COLUMN`. No default values (nullable). `024_account_bindings_probe_history.down.sql` reverses with `DROP COLUMN`. No index needed (probe columns are not filtered on).

**State / navigation / UI impact**

`AccountBindings.tsx`: minor addition to the binding card render. The `ProbeReport` component (in-component state probe result) continues to work as before. The persistent probe row is additive and shown below or beside the existing Test button row.

**Size estimate**: S. One migration, ~30 LOC backend (store method), ~20 LOC struct extension, ~50 LOC frontend (new sub-component + render branch). ~11 tests.

**Dependencies**: none. S5b is independent. 3-A-1 can ship without 3-A-2.

---

### Slice 3-A-2: Path B S5b — Adapter classifier + connector health probe + UI badges

This slice is defined in full in `docs/path-b-subscription-cli-bridge-design.md` §8 S5b. The plan here records the integration into Phase 3 tracking and adds one cross-slice dependency note.

**Scope** (from design doc §8 S5b, reproduced for completeness)

1. `adapters/backlog_adapter.py` — classify CLI failures into 7 `error_kind` enum values per design §6.4 table. Output: `error_kind` field in adapter stdout JSON. No `remediation_hint` in adapter output (D7).
2. Connector `serve` command — add health probe loop: `<cli_command> --version` at configurable interval (default 300 s). New flags: `--cli-health-interval=<seconds>`, `--cli-health-disabled`. Probe result classified as `healthy`, `cli_not_found`, or `unknown`. Connector-side hardening: `realpath`, allowed-roots check, interpreter blocklist, setuid rejection.
3. `POST /api/connector/heartbeat` — request body extended to accept `cli_health[]` array. Server stores latest per-binding entry in `local_connectors.metadata.cli_health.<binding_id>`.
4. Frontend `AccountBindings.tsx` — CLI binding rows show health badge with status and relative timestamp. Source: `GET /api/me/local-connectors` response, which already returns the `metadata` JSONB.
5. On binding delete — server scrubs the corresponding `cli_health` key from `local_connectors.metadata` for all user connectors (D3 cleanup discipline, R12).

**Definition of Done** (test matrix IDs from design doc §8 S5b, reproduced verbatim):

| ID | Scenario | Expected outcome |
|---|---|---|
| T-S5b-1 | Adapter unit tests for each of 7 `error_kind` classifications (English signatures) | All 7 enum values exercised |
| T-S5b-2 | Adapter receives non-English stderr | Returns `unknown` |
| T-S5b-3 | Probe with `cli_command` pointing at a real binary | Returns `version_string` |
| T-S5b-4 | Probe with `cli_command` pointing at non-existent path | `status: cli_not_found` |
| T-S5b-5 | Probe with `cli_command` resolving to interpreter binary (e.g. /bin/bash) | Connector refuses; status `cli_not_found`, `probe_error_message: "interpreter binary not allowed"` |
| T-S5b-6 | Heartbeat persists `cli_health`; subsequent GET reflects it | Server metadata updated |
| T-S5b-7 | Binding deleted → server scrubs corresponding `cli_health` entry across all user connectors | Metadata entry removed |
| T-S5b-8 | AccountBindings UI shows "checked Ns ago" relative timestamp; renders "?" when >10 min | Smoke test render assertion |

**Dependency on 3-A-2**: The probe badge in AccountBindings reads `local_connectors.metadata.cli_health`. This has no dependency on the probe-persistence feature (3-A-1). The two slices are independent.

**Size estimate**: M. ~150 LOC adapter Python + ~120 LOC Go connector + ~80 LOC frontend TSX + 8 tests.

**Gate**: 3-A-2 depends on S5a being merged (already shipped per status tracking).

---

### Slice 3-B-1: Evidence cross-links (Documents + Drift reverse-view)

**Scope**

1. New backend endpoint: `GET /api/projects/:id/backlog-candidates/by-evidence`
   - Query params: `?document_id=<uuid>` OR `?drift_signal_id=<uuid>` (exactly one required)
   - Returns list of candidate summaries (id, title, status, planning_run_id, requirement_id) where the candidate's `evidence_detail` JSONB contains a matching document or drift reference.
   - SQLite-compatible JSON path query: `WHERE evidence_detail->'documents' @> '[{"document_id": "X"}]'` in Postgres; for SQLite use `json_each` + filter. Alternatively, scan at the Go layer (fewer candidates per project typically) — decide during implementation based on expected row count.
   - Auth: project-member middleware (same as other project-scoped reads).
   - Pagination: `page`, `per_page` standard envelope.

2. `DocumentsTab.tsx` — each document row gains a "N candidates" chip when the count is non-zero. Chip is lazy-loaded (fires `GET /api/projects/:id/backlog-candidates/by-evidence?document_id=X` when the documents list is first rendered). Clicking opens a small inline panel or modal listing matching candidates with: title, status badge, "In run: [run id short]", "Requirement: [req title]", and a "View in workspace" link that navigates to the Planning Workspace tab with the run pre-selected via the existing `onSelectLineage`-style mechanism.

3. `DriftTab.tsx` — each drift signal row gains a "cited in N candidates" chip. Same lazy-load + modal pattern. The modal lists candidates with the same shape as the document modal.

4. No changes to `CandidateReviewPanel.tsx` or `AppliedLineage.tsx`.

5. `docs/api-surface.md` — document new endpoint.

**Definition of Done — explicit test matrix**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-3B1-1 | `GET /api/projects/:id/backlog-candidates/by-evidence?document_id=X` returns candidates that list doc X in `evidence_detail.documents` | Correct candidates returned |
| T-3B1-2 | Same endpoint with `drift_signal_id=Y` | Candidates whose evidence includes drift Y returned |
| T-3B1-3 | Document referenced by zero candidates | Endpoint returns empty list; UI shows no chip |
| T-3B1-4 | Document referenced by three candidates | UI chip shows "3 candidates"; modal lists all three |
| T-3B1-5 | Both `document_id` and `drift_signal_id` in same request | 400 — exactly one required |
| T-3B1-6 | Neither param | 400 |
| T-3B1-7 | Cross-project request (document in project A, requesting project B) | 404 or empty (project scoping enforced) |
| T-3B1-8 | Unauthenticated request | 401 |
| T-3B1-9 | DocumentsTab smoke test: render with fixture doc having 2 candidates → chip "2 candidates" visible | Render assertion |
| T-3B1-10 | DriftTab smoke test: drift signal with 1 candidate → chip "cited in 1 candidate" visible | Render assertion |
| T-3B1-11 | Modal content: clicking chip shows candidate list with "View in workspace" link | Click + modal assertion |

**Impacted modules (call chain)**

```
DocumentsTab.tsx (on mount)
  → GET /api/projects/:id/backlog-candidates/by-evidence?document_id=X
  → handlers/backlog_candidates.go (new handler ListByEvidence)
  → backlog_candidate_store.go (new method GetByEvidenceDocumentID / GetByEvidenceDriftSignalID)
  → backlog_candidates table (no schema change; queries evidence_detail JSONB)
  → chip + modal in DocumentsTab.tsx  ← NEW UI
DriftTab.tsx (same pattern with drift_signal_id)
```

**API contract impact**

New endpoint `GET /api/projects/:id/backlog-candidates/by-evidence`. Response:
```jsonc
{
  "data": [
    {
      "id": "uuid",
      "title": "Improve sync recovery UX",
      "status": "approved",
      "planning_run_id": "uuid",
      "requirement_id": "uuid",
      "requirement_title": "Sync recovery"
    }
  ],
  "error": null,
  "meta": { "page": 1, "per_page": 50, "total": 3 }
}
```

**DB / migration impact**

None. Queries existing `backlog_candidates.evidence_detail` JSONB. No new columns or indexes needed.

**State / navigation / UI impact**

`DocumentsTab.tsx` and `DriftTab.tsx`: additive lazy-loaded chip per row. New modal component (inline or extracted sibling). No changes to routing. The "View in workspace" link navigates to the Planning Workspace tab; the existing tab navigation in `ProjectDetail.tsx` handles this via tab index.

**Size estimate**: M. ~100 LOC Go (handler + store method, dual-driver JSON query), ~120 LOC TSX (chip + modal, two tabs), ~11 tests.

**Dependencies**: no blocking dependency. Can be implemented independently of 3-A slices.

---

### Slice 3-B-2: Task lineage full UI

**Scope**

`TasksTab.tsx` receives a new optional `lineageByTaskId` prop of type `Record<string, AppliedLineageEntry>`. `ProjectDetail.tsx` already calls `listProjectTaskLineage` for `AppliedLineage.tsx`; the same data is threaded through to `TasksTab` without a new API call (share the already-loaded result via the existing data-loading pattern in `ProjectDetail.tsx`).

Each task row that has a lineage entry of `lineage_kind == "applied_candidate"` renders a compact chip below the title:
```
Via planning: [requirement title] → [run status chip] → [candidate title]
```
Each segment is a clickable link. Clicking requirement or candidate deep-links to the Planning Workspace tab with the run pre-selected. Clicking the run status chip navigates the same way.

If `lineageByTaskId` prop is absent or the task has no lineage entry, the row renders exactly as today (no regression).

**Definition of Done — explicit test matrix**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-3B2-1 | `TasksTab` rendered with fixture containing one task + lineage entry | Lineage chip visible below task title |
| T-3B2-2 | Chip shows all three segments: requirement title, run status, candidate title | Text assertion |
| T-3B2-3 | Click on requirement title segment → `onSelectLineage(reqId)` called | Click handler assertion |
| T-3B2-4 | Click on candidate title segment → `onSelectLineage(reqId, runId, candidateId)` called | Click handler assertion |
| T-3B2-5 | Task with no lineage entry → no chip rendered | Render assertion |
| T-3B2-6 | `lineageByTaskId` prop absent → all rows render without chip | Render assertion (backwards compat) |
| T-3B2-7 | Integration: apply a candidate via `POST /api/backlog-candidates/:id/apply` → lineage appears in `GET /api/projects/:id/task-lineage` → chip visible in TasksTab | End-to-end integration assertion |

**Impacted modules (call chain)**

```
ProjectDetail.tsx (already loads task-lineage for AppliedLineage.tsx)
  → listProjectTaskLineage() — existing API call
  → passes lineageByTaskId to TasksTab.tsx  ← NEW PROP
TasksTab.tsx  ← NEW chip render branch
```

**API contract impact**

None. `GET /api/projects/:id/task-lineage` already exists and returns the full lineage with `requirement_id`, `planning_run_id`, `backlog_candidate_id`. No new endpoint.

**DB / migration impact**

None.

**State / navigation / UI impact**

`ProjectDetail.tsx`: minimal wiring to pass existing lineage data to `TasksTab`. `TasksTab.tsx`: new optional prop + chip render branch. No new hooks. No global state.

**Size estimate**: S. ~40 LOC TSX in `TasksTab.tsx`, ~20 LOC wiring in `ProjectDetail.tsx`, ~7 tests.

**Dependencies**: none. Can ship before or after 3-B-1.

---

### Slice 3-C-1: Dashboard pending decisions

**Scope**

1. New backend endpoint: `GET /api/projects/:id/pending-review-count`
   - Returns `{ "count": N }` where N is the number of `backlog_candidates` rows with `status = 'draft'` belonging to any `planning_run` with `status = 'completed'` scoped to this project.
   - Auth: authenticated user session (same as project GET).
   - No pagination (single-number aggregate).

2. `Dashboard.tsx` — on load, fire one additional fetch per project for pending-review count (or batch via a single `GET /api/dashboard/attention` endpoint if N-projects × 1 round-trip is unacceptably chatty — decide during implementation).

   Each project card gains a secondary line:
   ```
   3 candidates pending review
   ```
   when count > 0. When count is 0, the line is omitted.

3. New `AttentionSection` component below the project grid in `Dashboard.tsx`: a sorted list (most pending first) of projects with at least one pending candidate, each with a "Review now →" link to that project's Planning Workspace tab. Maximum 5 projects shown; "See all projects →" link if more.

4. `docs/api-surface.md` — document new endpoint.

**Definition of Done — explicit test matrix**

| ID | Scenario | Expected outcome |
|---|---|---|
| T-3C1-1 | Project has 3 draft candidates in completed runs → endpoint returns `{ count: 3 }` | Count correct |
| T-3C1-2 | Project has draft candidates in queued/running/failed runs (not completed) → not counted | Count excludes non-completed runs |
| T-3C1-3 | Project has no candidates at all → count is 0 | Returns `{ count: 0 }` |
| T-3C1-4 | Unauthenticated request | 401 |
| T-3C1-5 | Cross-project request (project not accessible to user) | 404 |
| T-3C1-6 | Dashboard smoke test: project card with count 3 → "3 candidates pending review" visible | Render assertion |
| T-3C1-7 | Dashboard smoke test: project card with count 0 → secondary line absent | Render assertion |
| T-3C1-8 | AttentionSection shows projects sorted by count descending | Render assertion with two fixture projects |
| T-3C1-9 | AttentionSection hidden when all projects have count 0 | Render assertion |

**Impacted modules (call chain)**

```
Dashboard.tsx (on load, per-project)
  → GET /api/projects/:id/pending-review-count  ← NEW
  → handlers/planning_runs.go (new handler PendingReviewCount)
  → backlog_candidate_store.go (new method CountDraftByProject)
  → backlog_candidates table (existing; no schema change)
  → Dashboard.tsx AttentionSection  ← NEW component
```

**API contract impact**

New endpoint `GET /api/projects/:id/pending-review-count`. Response:
```jsonc
{
  "data": { "count": 3 },
  "error": null,
  "meta": null
}
```

**DB / migration impact**

None. Simple aggregate query on existing `backlog_candidates` and `planning_runs` tables.

**State / navigation / UI impact**

`Dashboard.tsx`: additional async fetches per project on load. New `AttentionSection` component (extracted or inline). No new routing. "Review now" link navigates to `/projects/:id` with the Planning Workspace tab pre-selected (existing URL + tab-index convention from Phase 2 S5).

**Size estimate**: S. ~50 LOC Go (handler + store method), ~100 LOC TSX (card counts + AttentionSection), ~9 tests.

**Dependencies**: none. Can ship independently.

---

## 6. Implementation order

Within each group, items can be parallelized across agents.

```
Group 1 (independent, can start immediately)
  3-A-1  Probe persistence            (schema first → store → handler → frontend)
  3-A-2  Path B S5b                   (adapter → connector → heartbeat → frontend)

Group 2 (independent of group 1, can start immediately)
  3-B-1  Evidence cross-links         (backend endpoint → frontend DocumentsTab + DriftTab)
  3-B-2  Task lineage full UI         (frontend-only; wiring in ProjectDetail)
  3-C-1  Dashboard pending decisions  (backend endpoint → frontend Dashboard)
```

Ordering within each slice follows the rule: schema migrations first, API contracts second, core logic third, frontend integration fourth, tests alongside each step.

---

## 7. Risk assessment

| # | Risk | Likelihood | Impact | Mitigation | Owner |
|---|---|---|---|---|---|
| R1 | SQLite JSON path queries for `by-evidence` endpoint are incompatible with PostgreSQL syntax | Medium | Medium | Dual-driver implementation; use `json_each` for SQLite, `@>` operator for Postgres; existing migration runner and test suite covers both | implementer |
| R2 | Probe `RecordProbe` write fires after `writeSuccess`, so a write failure after 200 goes unnoticed | Low | Low | Log the error; probe recording is a best-effort annotation, not a data-integrity concern | implementer |
| R3 | N-projects × 1 pending-count round-trip chatty for large dashboards | Low | Low | Batch with a single `GET /api/dashboard/attention?project_ids=A,B,...` if needed; decide during 3-C-1 implementation | implementer |
| R4 | `TasksTab` receives `lineageByTaskId` prop but `ProjectDetail` does not thread it correctly | Low | Low | Smoke test T-3B2-6 (missing prop → no regression) catches this | implementer |
| R5 | Path B S5b adapter classifier matches too broadly in non-English locales | Low | Low | English-only classifier per design §6.4 caveat; `unknown` is the safe fallback; documented in adapter README | implementer |
| R6 | Health probe `--version` call blocked by rate-limiter or credential check on some CLI versions | Low | Medium | `unknown` fallback; `--cli-health-disabled` escape hatch; interval is 5 min so load is minimal | implementer |
| R7 | `AccountBindings.tsx` approaches 800 LOC with additional probe display | Low | Low | Extract `PersistentProbeStatus` as a sibling component if file grows past 750 LOC; design §7 UI constraint (extract above ~600 LOC) | implementer |
| R8 | `evidence_detail` JSONB structure varies across backlog candidates from different providers | Low | Medium | Validate `evidence_detail.documents[].document_id` presence before joining; treat missing field as empty array | implementer |

---

## 8. Open questions

1. **3-B-1 DB query strategy for `by-evidence`**: Is the expected number of backlog candidates per project small enough (< 200) that Go-layer scanning of `evidence_detail` JSON is acceptable, or should we write dual-dialect SQL (`@>` / `json_each`)? Recommend deciding at implementation time after checking typical candidate counts in local test data.

2. **3-C-1 Batching strategy**: Should pending-review counts be fetched per-project (N requests) or via a single aggregation endpoint? If the project list is typically < 20, N requests is fine. Document the threshold decision in DECISIONS.md when the slice lands.

3. **3-A-1 `updated_at` bump**: Should `RecordProbe` bump `account_bindings.updated_at`? Current design says yes (keeps `updated_at` as "last write"). If a future feature uses `updated_at` as a proxy for "binding edited by user," the probe bump could be noisy. Consider a separate `last_probe_at` timestamp column (already planned) as the authoritative probe signal, and omit the `updated_at` bump. Decide before landing.

4. **3-B-1 Cross-driver JSON path**: SQLite's `json_each` + WHERE filter returns correct results but is not indexed. For projects with > 500 candidates this could be slow. Add a note in the implementation PR to revisit with a GIN index or materialized column if benchmarks show > 50 ms query time.

---

## 9. Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| 3-A-1 — Probe persistence | not started | — | — |
| 3-A-2 — Path B S5b (adapter + health probe + UI) | not started | — | — |
| 3-B-1 — Evidence cross-links (Documents + Drift) | not started | — | — |
| 3-B-2 — Task lineage full UI | not started | — | — |
| 3-C-1 — Dashboard pending decisions | not started | — | — |

**Phase 3 complete** when all five slices are merged and all CI jobs pass.

---

Source: `[agent:feature-planner]`. References `docs/phase2-planning-workspace-design.md`, `docs/path-b-subscription-cli-bridge-design.md`, `docs/api-surface.md`, `docs/data-model.md`. Reads `backend/internal/handlers/remote_models.go`, `backend/internal/store/account_binding_store.go`, `frontend/src/pages/AccountBindings.tsx`, `frontend/src/pages/ProjectDetail/planning/`, `frontend/src/pages/Dashboard.tsx`, `adapters/backlog_adapter.py`, `backend/internal/connector/`. No code lands until owner approval of this document.
