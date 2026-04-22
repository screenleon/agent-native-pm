# Phase 2 design — Planning Workspace UX consolidation

**Status**: draft · 2026-04-22 · `[agent:feature-planner]`
**Gates this phase**: no Phase 2 coding lands until this doc is approved.

---

## 1. Problem statement

`ARCHITECTURE.md` → Near-Term Direction names Planning Workspace as the next
architectural focus, but does not define what the workspace IS. Today the
planning-domain surface is scattered across seven places:

- `PlanningTab.tsx` — 1318 lines. Requirement intake form, requirement
  queue, planning-run dispatch, candidate review, apply-to-task.
- `TasksTab.tsx` — applied tasks live here but with no lineage back to the
  candidate / requirement / run that produced them.
- `DocumentsTab.tsx` + `DriftTab.tsx` — the evidence sources that planning
  ought to cite, but the citation only runs in one direction (planning run
  cites doc; doc page doesn't show "this doc contributed to candidate X").
- `ProjectOverviewTab.tsx` — summary numbers are card-level counts; there
  is no "what needs my attention in the planning loop right now" surface.
- `AgentsTab.tsx` — shows planning runs as agent activity but treats them
  as opaque audit entries.
- `SyncStatusPanel.tsx` — orthogonal to planning, but feeds drift signals
  that drift-aware planning runs consume.
- Dashboard page (`Dashboard.tsx`) — cross-project aggregation, doesn't
  drill into per-project planning flow.

The operator's real question on landing in a project is:
> "What's blocked on my review right now, and what are the options for each
> decision I owe?"

No single surface answers that today. The Planning tab is closest but is
organised around *system entities* (requirements → runs → candidates) rather
than around *pending human decisions*.

## 2. Current state inventory

### Frontend

| Surface | File | Lines | Responsibility |
|---|---|---|---|
| Requirement intake | `PlanningTab.tsx` | ~200 | Create / list requirements |
| Planning run dispatch | `PlanningTab.tsx` | ~300 | Trigger run, pick provider/adapter, handle local-connector dispatch |
| Candidate review | `PlanningTab.tsx` | ~500 | Display candidates, edit fields, approve/reject, apply |
| Stepper | `PlanningStepper.tsx` | — | Visual progress indicator |
| Smoke test | `PlanningTab.test.tsx` | 79 | 3 render assertions |

### Backend (already implemented, per ARCHITECTURE)

- `requirements` · `planning_runs` · `backlog_candidates` · `task_lineage`
- `POST /api/connector/claim-next-run` → `SubmitPlanningRunResult` with SSE notification
- `/api/notifications/stream` for real-time updates
- Context.v1 sanitized payload feeding adapters

### API surface (stable, no backend change required for Phase 2)

- `listRequirements` / `getRequirement` / `createRequirement`
- `listPlanningRuns` / `getPlanningRun` / `createPlanningRun` / `cancelPlanningRun`
- `listPlanningRunBacklogCandidates` / `updateBacklogCandidate` / `applyBacklogCandidate`
- `getPlanningProviderOptions`, `getPlanningSettings`, `updatePlanningSettings`

## 3. End state

A single **Planning Workspace** replaces the Planning tab as the
per-project default landing surface. It has four lanes, top-to-bottom:

```
┌──────────────────────────────────────────────────────────────────┐
│ Attention Row                                                    │
│   N requirements awaiting planning · M candidates awaiting      │
│   review · K applied tasks open · drift signals (with quick jump) │
├──────────────────────────────────────────────────────────────────┤
│ Backlog intake                                                   │
│   [+ New requirement] form (collapsed when requirements exist)  │
│   Requirement queue with status chips + "Run planning" button    │
├──────────────────────────────────────────────────────────────────┤
│ Active planning runs                                             │
│   Each run: status, dispatch (server / local connector), elapsed │
│   time, evidence summary, "Review candidates →" link             │
├──────────────────────────────────────────────────────────────────┤
│ Candidate review (expanded when a run is selected)               │
│   One card per candidate: title, score, rationale, evidence      │
│   (linked to source doc), inline approve / reject / edit / apply │
├──────────────────────────────────────────────────────────────────┤
│ Applied task lineage                                             │
│   Tasks that came from this planning loop, each with a back-link │
│   to the candidate → run → requirement chain (traceability)     │
└──────────────────────────────────────────────────────────────────┘
```

Tasks / Documents / Drift / Agents / Settings tabs remain for deep dives.
The workspace surfaces the aggregate; the tabs surface the depth.

### What changes vs today

| Concern | Today | End state |
|---|---|---|
| Primary surface | "Overview" tab (summary counts) | "Workspace" tab (decisions + actions) |
| Planning flow | One 1318-line component | 5-6 focused components under `pages/ProjectDetail/planning/` |
| Candidate evidence | Text-only reference | Clickable link to doc / drift signal |
| Task lineage | Not surfaced | First-class lane with requirement → candidate → task trail |
| Pending decisions | User has to scan multiple tabs | Attention row consolidates them |

## 4. Non-goals

- **No backend schema change.** Lineage already exists in `task_lineage`;
  the new UI just surfaces it.
- **No subscription CLI bridge.** Remains blocked on client-side
  architecture (2026-04-17 decision).
- **No SSE horizontal-scaling work.** Per-process broker is sufficient.
- **No rewrite of Tasks / Documents / Drift tabs.** They stay; we only add
  cross-links *into* them from the workspace.
- **No new provider / model plumbing.** The 2026-04-22 central settings
  model stays as-is.
- **No AI-driven auto-approve.** Every candidate still requires explicit
  human approval (2026-04-17 decision).

## 5. Information architecture

### Route choice

**Decision: replace the "Planning" tab with a "Workspace" tab at the same
tab index**, rather than introduce a new route. Rationale:

- Users already know the Planning tab. Renaming keeps muscle memory.
- `ProjectDetail` is already the cross-cutting surface; adding another top
  level route would fragment.
- Tab index stability matters for deep links — keep the URL contract the
  same even as the content widens.

Rejected: `/projects/:id/workspace` new route (fragmentation); making
Overview the workspace (conflates summary with action surface).

### State ownership

- **Data layer**: React hooks co-located with each lane (e.g.
  `usePlanningRuns(projectId)`, `useBacklogCandidates(runId)`). No global
  store. State in `ProjectDetail.tsx` remains the projectId source of truth.
- **SSE subscription**: centralised in `ProjectDetail.tsx` (already there);
  workspace subscribes via the existing `anpm:refresh-notifications` event
  plus React Query–style refetch where useful.

## 6. UI constraints (2026-04-22 Tier-3)

From the 2026-04-22 DECISIONS entry:

> New product features added to this page MUST be added as siblings
> (extracted components or hooks under `frontend/src/pages/ProjectDetail/`)
> rather than appended to the existing function.

For Phase 2 this means:

- All new components land under `frontend/src/pages/ProjectDetail/planning/`.
- Existing tab components in `frontend/src/components/*Tab.tsx` are
  **grandfathered** — do not relocate them in Phase 2. They can migrate in
  a later cleanup pass.
- Any new hook lands under `frontend/src/pages/ProjectDetail/hooks/`.
- Every new component lands with a smoke test in the same PR (2026-04-22
  ProjectDetail-split-shipped decision).

## 7. Incremental slice plan

The work is split into 5 slices, each independently mergeable, each with
its own PR. No slice depends on an unmerged slice.

### S1 — Structural split of PlanningTab (no behaviour change)

**Scope**: Move the current 1318-line `PlanningTab.tsx` into focused
sub-components under `frontend/src/pages/ProjectDetail/planning/`:

- `RequirementIntake.tsx` (~200 lines)
- `RequirementQueue.tsx` (~150 lines)
- `PlanningRunList.tsx` (~300 lines)
- `CandidateReviewPanel.tsx` (~500 lines)
- `hooks/usePlanningWorkspaceData.ts` — aggregates the existing props into
  one shape so the top-level `PlanningTab.tsx` shrinks to a shell.

**Non-behaviour-changing**: all current flows work identically. Zero user-
visible change except component tree.

**Tests**: one smoke test per new component (render + one assertion on
empty + populated state). Mirror the pattern from PR #2.

**Size**: M. Touches 1 file to delete content, 5 new files plus 5 tests.

### S2 — Attention Row

**Scope**: Add a new `AttentionRow.tsx` component at the top of the
workspace surfacing four counts with click-through to the relevant lane:

- Requirements awaiting planning
- Candidates awaiting review (per-run and total)
- Applied tasks still open
- Open drift signals (links to Drift tab)

Derived entirely from already-loaded project state; no new API calls.

**Tests**: counts + click-through.

**Size**: S. One new component + test + wiring into `PlanningTab.tsx`.

### S3 — Evidence-linked candidate review

**Scope**: In `CandidateReviewPanel`, render evidence entries as clickable
links when they reference a registered document (open document preview
modal inline) or a drift signal (deep-link to Drift tab with the signal
preselected). Add a small "Evidence" section per candidate card with
lineage chips.

**Backend**: no change. Uses existing `evidence_detail.documents[].document_id`.

**Tests**: render with evidence → verify clickable links; click → verify
handler fires with correct id.

**Size**: M. One component change + new evidence chip component + tests.

### S4 — Applied-task lineage lane

**Scope**: New `AppliedLineage.tsx` at the bottom of the workspace. Lists
tasks created via candidate-apply in this project, each with a visible
chain: `requirement.title → run status → candidate title → task title`.
Click any segment to jump to that entity.

**Backend**: uses existing `task_lineage` + the existing listRequirements /
listPlanningRuns / listPlanningRunBacklogCandidates APIs. If cross-cutting
query is expensive, add a single new helper endpoint — decide during
implementation.

**Tests**: render with a fixture that includes lineage; verify three-link
chain appears and each click fires the expected handler.

**Size**: M. One component + test. Possibly one backend endpoint.

### S5 — Rename "Planning" tab to "Workspace" + polish

**Scope**: Rename the tab label, update `ARCHITECTURE.md` to describe the
final layout, add screenshots to this design doc (as a post-merge note).

**Tests**: unchanged test targets still pass; workspace smoke test asserts
the new label.

**Size**: S.

### Dependency graph

```
S1 (structural split) ──┬── S2 (attention row)
                        ├── S3 (evidence links)
                        └── S4 (lineage lane) ──── S5 (rename + polish)
```

S1 must merge first. S2 / S3 / S4 are independent of each other. S5
comes last.

## 8. Acceptance criteria (per slice)

Every slice PR MUST:

1. Pass all four CI jobs (governance / frontend / backend-sqlite / backend-postgres).
2. Not grow `PlanningTab.tsx` beyond 200 lines after the shell refactor
   (enforced by reviewer on S1 and subsequent PRs).
3. Include at least one smoke test per new component.
4. Update `docs/phase2-planning-workspace-design.md` → "Status" table with
   slice completion.
5. Not touch backend schema unless explicitly called out (S4 may add one
   endpoint; the others must not).

## 9. Open questions / decisions needed before S1

- **Q1**: Should the `Workspace` tab become the default tab index on
  `ProjectDetail`, replacing "Overview"? Current default is Overview.
  **Proposal**: yes — the Workspace is where action happens; Overview is
  read-only. Decide before S5.
- **Q2**: Do we add an ADR now ("Planning Workspace is the per-project
  primary surface") or wait until S5?
  **Proposal**: add it at the end of S5 when the shape is concrete.
- **Q3**: Evidence links in S3 — do we open documents in a modal (existing
  preview pattern) or deep-link to the Documents tab with the doc
  preselected?
  **Proposal**: modal first (less state plumbing); promote to deep-link if
  operators ask for it.

## 10. Out of scope references

Items explicitly not part of Phase 2 (tracked elsewhere):

- Phase 3: Subscription CLI bridge, SSE horizontal scaling, deeper UI
  behavioural tests (on-demand), DECISIONS second archival pass.
- Dialect parity: full-text search SQLite fallback, recency stats gating.

## Status tracking

| Slice | Status | PR | Merged |
|---|---|---|---|
| S1 — structural split | not started | — | — |
| S2 — attention row | not started | — | — |
| S3 — evidence links | not started | — | — |
| S4 — applied lineage | not started | — | — |
| S5 — rename + polish | not started | — | — |
