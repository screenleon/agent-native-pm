# Backlog-first plan — Project management core before connector automation

**Status**: approved for B1-B5 · 2026-05-01 · `[agent:feature-planner]`
**Gates**: Owner approved the `backlog_items` table, labels, default Backlog tab, generated-backlog-as-backlog behavior, and deferring connector auto-implementation until after B1-B5. Implementation still follows the normal pre-PR verification gate.
**Precondition**: Phase 6c is available on `main`. Connector auto-generation and auto-implementation remain deferred until the backlog management layer is usable on its own.
**Current PR scope**: B1 + B2 + the UX clarification slice in section 14. B3 planning-output materialization remains a follow-up; generated planning runs still use the legacy `backlog_candidates` path until B3 lands.

---

## 1. Problem statement

The current Planning Workspace can generate and review backlog candidates, but it does not yet feel like a project management system.

The main usability gap is not the connector. It is that backlog is still a by-product of a planning run:

- A user must navigate `requirement -> planning run -> candidate` before seeing work.
- Candidate review is optimized for inspecting one run, not for managing a project backlog over time.
- The only full candidate list API is run-scoped: `GET /api/planning-runs/:id/backlog-candidates`.
- Project-level counts in the Workspace can only see the currently loaded candidate set, not all open backlog.
- Connector/role-dispatch controls are already visible in the candidate detail, which makes the core backlog workflow feel heavier than necessary.

The product should first become a reliable local project management system for backlog capture, review, prioritization, and API-based synchronization. Connector auto-generation and implementation should build on that stable backlog contract later.

---

## 2. Product direction

Agent Native PM should treat backlog as the primary project management layer.

### Entity semantics

| Concept | Meaning |
|---|---|
| Requirement | A raw goal, feature idea, bug theme, or planning input. |
| Backlog item | A durable project-management work item that can be prioritized, edited, queried, and later committed to execution. |
| Backlog candidate | A legacy/generated suggestion artifact from a planning run. It may remain as an internal compatibility/evidence record, but user-facing generated backlog should materialize as backlog items directly. |
| Task | A committed execution item. It can be performed manually or dispatched to a connector in later phases. |

### Core flow

```text
manual input / API input / planning output
  -> backlog item
  -> review + priority + readiness
  -> committed task
  -> optional connector execution later
```

### UX principle

The default project surface should answer:

1. What should I work on?
2. What is blocked or needs review?
3. What changed recently?
4. What is ready to become a task?

It should not require the user to understand planning runs, adapters, execution modes, or connector dispatch before they can manage work.

---

## 3. Current state inventory

### Frontend

| File | Current state | Backlog-first implication |
|---|---|---|
| `frontend/src/pages/ProjectDetail/PlanningTab.tsx` | Workspace composes requirement queue, planning launcher, run list, candidate review, and applied lineage. | Keep it, but stop making it the only way to manage backlog. |
| `frontend/src/pages/ProjectDetail/planning/CandidateReviewPanel.tsx` | Dense one-run review surface with evidence, score, role dispatch, feedback, and apply controls. | Reuse detail pieces, but move project-level backlog scanning into a new surface. |
| `frontend/src/pages/ProjectDetail/planning/hooks/usePlanningWorkspaceData.ts` | Holds requirement/run/candidate state, role loading, and apply handlers. | Split project-level backlog fetching/mutation into a separate hook. |
| `frontend/src/pages/ProjectDetail/TasksTab.tsx` | Existing task execution board. | Keep as committed execution view, not as the draft backlog source of truth. |

### Backend/API

| Current endpoint | Limitation |
|---|---|
| `GET /api/planning-runs/:id/backlog-candidates` | Requires selecting one run before viewing candidates. |
| `PATCH /api/backlog-candidates/:id` | Edits generated candidate review fields only. |
| `POST /api/backlog-candidates/:id/apply` | Applies candidate directly to task, skipping a durable backlog item. |
| `GET /api/projects/:id/task-lineage` | Shows candidate-to-task traceability after apply, not pre-task backlog state. |
| `GET /api/projects/:id/backlog-candidates/by-evidence` | Evidence reverse lookup only; not a backlog management API. |

### Data model

`requirements`, `planning_runs`, `backlog_candidates`, and `task_lineage` are already in place. The missing layer is a first-class project backlog table that can represent human-created, API-imported, and planning-derived work before task commitment.

Owner clarification on 2026-05-01: manual backlog items do not need a requirement. Requirements are for situations where the user has a need but wants the system to decompose it. If the user clearly knows the work item, it can be created directly as backlog.

---

## 4. Proposed data model

Add a new `backlog_items` table.

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PRIMARY KEY | UUID v4 |
| `project_id` | TEXT NOT NULL | FK -> `projects.id` |
| `requirement_id` | TEXT NULL | Optional origin requirement |
| `planning_run_id` | TEXT NULL | Optional origin planning run |
| `backlog_candidate_id` | TEXT NULL | Optional generated candidate origin |
| `task_id` | TEXT NULL | Set when committed to a task |
| `title` | TEXT NOT NULL | User-editable |
| `description` | TEXT NOT NULL DEFAULT '' | User-editable |
| `status` | TEXT NOT NULL DEFAULT 'triage' | `triage`, `ready`, `committed`, `blocked`, `archived` |
| `priority` | TEXT NOT NULL DEFAULT 'medium' | `low`, `medium`, `high`, `urgent`; `urgent` means interrupt/current-plan attention, while `high` means next-priority planned work |
| `source` | TEXT NOT NULL DEFAULT 'human' | `human`, `api:<name>`, `candidate:<id>`, `agent:<name>`, `analysis` |
| `rank` | INTEGER NOT NULL DEFAULT 0 | Manual ordering inside a project |
| `labels` | JSON/TEXT NOT NULL DEFAULT '[]' | Keep SQLite/Postgres parity |
| `acceptance_criteria` | TEXT NOT NULL DEFAULT '' | Optional DoD/validation |
| `blocked_reason` | TEXT NOT NULL DEFAULT '' | Visible only when blocked |
| `created_at` | TIMESTAMPTZ | Existing dialect pattern |
| `updated_at` | TIMESTAMPTZ | Existing dialect pattern |

Indexes:

- `(project_id, status, rank, updated_at DESC)`
- `(project_id, priority, updated_at DESC)`
- `(project_id, backlog_candidate_id)` unique where `backlog_candidate_id IS NOT NULL`
- `(project_id, task_id)` where `task_id IS NOT NULL`

### Status lifecycle

```text
triage -> ready -> committed
triage -> blocked -> ready
triage/ready/blocked -> archived
```

Rules:

- `committed` means a task exists and `task_id` is set.
- Archived backlog items stay queryable by API but hidden by default in UI.
- Manual backlog item creation does not create or require a requirement.
- Planning runs that generate backlog should create backlog items directly.
- A legacy candidate can create at most one backlog item when using compatibility flows.
- A backlog item can create at most one task.
- When an `urgent` backlog item is committed to a task, the task priority remains `urgent`.

---

## 5. Proposed API surface

### Project backlog list

`GET /api/projects/:id/backlog-items`

Query params:

- `status`
- `priority`
- `source`
- `q`
- `include_archived`
- `page`
- `per_page`
- `sort=rank|priority|updated_at|created_at`
- `order=asc|desc`

Response uses the standard envelope.

### Create backlog item

`POST /api/projects/:id/backlog-items`

Request:

```json
{
  "title": "Add project-level backlog API",
  "description": "Expose backlog items independent of planning runs.",
  "priority": "high",
  "labels": ["api", "planning"],
  "acceptance_criteria": "API supports list, create, update, and commit-to-task."
}
```

Manual create defaults:

- `requirement_id` is omitted unless the user explicitly starts from a requirement.
- `source` defaults to `human`.
- `labels` are supported in B1 because external API sync is expected to need structured grouping.

### Update backlog item

`PATCH /api/backlog-items/:id`

Mutable fields:

- `title`
- `description`
- `status`
- `priority`
- `rank`
- `labels`
- `acceptance_criteria`
- `blocked_reason`

### Planning run output to backlog

Planning runs whose requested output is backlog should persist generated work as `backlog_items` directly.

Behavior:

- The user-facing result of "generate backlog" is backlog items.
- `planning_run_id` and `requirement_id` are stored on each generated backlog item when available.
- `backlog_candidate_id` is optional and only used if the implementation keeps candidate rows as a compatibility/evidence artifact during migration.
- The system must not require an extra "accept to backlog" click after the user asked to generate backlog.
- If the user explicitly asks to generate tasks instead, the run may create committed tasks through the task flow rather than backlog items.

### Commit backlog item to task

`POST /api/backlog-items/:id/commit-to-task`

Behavior:

- Requires backlog item status `ready` or `triage`.
- Creates a task with copied title/description/priority.
- Sets backlog item `status='committed'` and `task_id`.
- Writes lineage with `lineage_kind='backlog_item'` or extends lineage model to carry `backlog_item_id`.
- Idempotent if already committed.

### External API use

Human session and API-key callers should use the same endpoints. Project-scoped API keys may create/update backlog only inside their allowed project.

Example external sync:

```text
GET /api/projects/:id/backlog-items?status=triage,ready
POST /api/projects/:id/backlog-items
PATCH /api/backlog-items/:id
POST /api/backlog-items/:id/commit-to-task
```

---

## 6. Frontend information architecture

### Project primary rail

Recommended primary tabs:

1. **Backlog** — new default working surface
2. **Workspace** — planning/generation/review
3. **Tasks** — committed execution
4. **Documents**
5. **Overview**

Owner decision: `Backlog` should become the default ProjectDetail tab in the backlog-first implementation.

### Backlog page layout

Use a dense operational layout, not a marketing/card-heavy surface.

```text
Backlog
Filters: status | priority | source | search | archived

Triage      Ready        Blocked       Committed
item        item         item          item -> task
item        item
```

Modes:

- **List mode**: best for scanning many items.
- **Board mode**: best for status movement.
- **Detail drawer**: edit title, description, labels, criteria, origin links, and commit action.

### Candidate review changes

Candidate review becomes an input pipeline:

- Generated backlog runs should land in the Backlog tab directly.
- Keep legacy candidate review for compatibility, evidence inspection, and older planning runs.
- Keep `Apply/Commit to Task` available only as a secondary shortcut inside backlog detail or explicit "generate task" flow.
- Keep role dispatch and suggest-role controls hidden under `Advanced execution`.

### Empty state

The first screen should offer three clear entries:

1. Add backlog item manually.
2. Generate backlog from a requirement.
3. Run What's Next analysis.

The user should not need to pick execution mode on the first screen.

---

## 7. Connector and auto-implementation boundary

Connector auto-generation and auto-implementation are explicitly deferred until backlog is stable.

Allowed in this phase:

- Keep existing connector planning runs working.
- Preserve role authoring fields on candidates.
- Keep current role-dispatch APIs from regressing.
- Show connector status only where it explains an active run.

Not allowed in this phase:

- New `role_dispatch_auto` behavior.
- Automatic task claiming from backlog.
- Connector-generated code execution from a backlog item.
- New sandboxing or adapter framework work, unless needed to prevent a regression.

Future connector path:

```text
backlog_item ready
  -> choose execution role
  -> dispatch task / implementation plan
  -> connector claims
  -> structured result
  -> task status + artifact links update
```

---

## 8. Slice plan

### Slice B1 — Backlog model + API

Scope:

1. Add migration for `backlog_items`.
2. Add Go model, store, handler, router entries.
3. Implement list/create/update/commit-to-task.
4. Add project-scoped authorization checks.
5. Update `docs/data-model.md` and `docs/api-surface.md`.
6. Support labels in the B1 contract.
7. Support `urgent` as a real backlog and task priority.

Definition of Done:

| ID | Scenario | Expected |
|---|---|---|
| B1-1 | Create backlog item with valid title | 201 and item is returned |
| B1-2 | Create with blank title | 400 |
| B1-3 | List by project | Only project-visible items returned |
| B1-4 | API key scoped to another project | 404 to avoid leaking project existence |
| B1-5 | Patch status/priority/rank | Updated item returned |
| B1-6 | Manual backlog item create without requirement | Item is created with `requirement_id=null` |
| B1-7 | Commit urgent item to task | Task priority is `urgent`; backlog item still reads `urgent` |
| B1-8 | Commit to task | Task created, item marked committed, idempotent replay returns same task |
| B1-9 | SQLite and Postgres suites | Both pass |

### Slice B2 — Backlog tab frontend

Scope:

1. Add `BacklogTab` under `frontend/src/pages/ProjectDetail/`.
2. Add `useProjectBacklogData` hook.
3. Add API client methods and TypeScript types.
4. Add list view with filters and quick edit.
5. Add detail drawer for full editing and commit-to-task.

Definition of Done:

| ID | Scenario | Expected |
|---|---|---|
| B2-1 | Open project detail | Backlog tab is the default selected tab |
| B2-2 | Empty project | Shows manual add, generate, and What's Next entries |
| B2-3 | Project with items | List is scannable without selecting a requirement/run |
| B2-4 | Filter by status/priority/source/label | Results update without layout jump |
| B2-5 | Edit item title/priority/status/labels | PATCH persists and UI updates |
| B2-6 | Commit item | Task appears and backlog item becomes committed |
| B2-7 | Mobile width | Text and actions do not overlap |

### Slice B3 — Planning output writes backlog

Scope:

1. Update planning completion so backlog-generation runs create `backlog_items` directly.
2. Add planning-origin fields to backlog item response.
3. Keep candidate rows only as compatibility/evidence artifacts if needed by existing code.
4. Update Planning Workspace to route completed backlog generation to the Backlog tab.
5. Keep direct task generation as an explicit separate mode, not the default backlog-generation path.
6. Add lineage/deep-link from backlog item back to requirement/run and candidate when present.

Definition of Done:

| ID | Scenario | Expected |
|---|---|---|
| B3-1 | Generate backlog from requirement | Backlog items are created without an extra accept step |
| B3-2 | Generate backlog twice for same run | No duplicate backlog items for the same generated artifact |
| B3-3 | Open generated backlog item | Detail drawer shows origin requirement/run links |
| B3-4 | Explicit generate-task flow | Creates tasks only when user selected task output |
| B3-5 | Existing candidate/apply flow | Still works for older workflows |

### Slice B4 — Backlog as API integration point

Scope:

1. Document API-key usage for external backlog sync.
2. Add request examples for create/update/list/commit.
3. Add source conventions: `api:<tool>`, `agent:<name>`, `candidate:<id>`.
4. Add server validation for source prefix length and allowed characters.
5. Add a minimal `scripts/backlog-api-smoke.sh` optional local smoke script.

Definition of Done:

| ID | Scenario | Expected |
|---|---|---|
| B4-1 | API key creates item | Source is stored and response is scoped |
| B4-2 | API key lists project backlog | Only allowed project returned |
| B4-3 | Invalid source prefix | 400 |
| B4-4 | Docs show full curl flow | User can reproduce manually |

### Slice B5 — Project-level backlog summary

Scope:

1. Add backlog counts to project dashboard summary.
2. Show counts in ProjectList and ProjectOverviewTab.
3. Replace currently-selected-run candidate counts where project-level counts are more useful.

Definition of Done:

| ID | Scenario | Expected |
|---|---|---|
| B5-1 | Project has triage/ready/blocked items | Summary returns correct counts |
| B5-2 | Project list | Shows backlog attention without opening project |
| B5-3 | Workspace sidebar | Counts reflect project backlog, not only selected run |

### Slice B6 — Connector execution planning follow-up

Scope:

Planning only. No execution implementation in this slice.

1. Define how `backlog_items` become connector-dispatchable tasks.
2. Decide whether dispatch attaches to `backlog_items`, `tasks`, or both.
3. Define execution result storage and artifact links.
4. Run risk/security review before implementation because this touches subprocess execution.

Exit criteria:

- Backlog APIs are stable.
- Backlog UI is usable for manual/API project management.
- Users can dogfood backlog for at least one project without connector auto-implementation.

---

## 9. Backend implementation notes

### Store boundaries

Add a dedicated `BacklogItemStore`.

Do not overload `BacklogCandidateStore`; candidates are generated suggestions, while backlog items are user-managed project state.

### Lineage

Preferred option:

- Add `backlog_item_id` to `task_lineage`.
- Keep existing candidate fields.
- For task created from backlog item accepted from candidate, lineage can carry all four IDs:
  `requirement_id`, `planning_run_id`, `backlog_candidate_id`, `backlog_item_id`.

Fallback option:

- Keep `task_lineage` unchanged in B1.
- Store `task_id` on `backlog_items`.
- Add lineage expansion in B3/B5.

Recommendation: use the preferred option if B1 already touches migrations. The traceability model is central to the product.

### Candidate status compatibility

Do not add a new candidate status unless necessary. Generated backlog should be represented by `backlog_items`; candidate rows, if retained, are compatibility/evidence artifacts. This avoids widening every existing candidate status switch.

---

## 10. Frontend implementation notes

### File placement

New ProjectDetail siblings:

```text
frontend/src/pages/ProjectDetail/BacklogTab.tsx
frontend/src/pages/ProjectDetail/BacklogTab.test.tsx
frontend/src/pages/ProjectDetail/backlog/
  BacklogList.tsx
  BacklogBoard.tsx
  BacklogDetailDrawer.tsx
  BacklogFilters.tsx
  hooks/useProjectBacklogData.ts
```

This respects the existing ProjectDetail structural rule: new product additions live under `frontend/src/pages/ProjectDetail/`.

### Interaction design

Keep the first implementation utilitarian:

- Dense rows.
- Inline status/priority controls.
- Detail drawer for longer fields.
- Stable dimensions for badges, controls, and row actions.
- No nested cards.
- No decorative hero sections.

### Candidate review simplification

Candidate detail should show:

1. Title and summary.
2. Why suggested.
3. Origin/evidence links.
4. Link to generated backlog item when one exists.
5. Advanced execution collapsed.

Move long score breakdowns behind a disclosure.

---

## 11. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---:|---:|---|
| Entity overlap confuses users: candidate vs backlog item vs task | High | High | Use one default flow: generated backlog -> backlog item -> task. Keep candidate language out of the primary UI. |
| API grows without stable workflow | Medium | High | Ship B1 + B2 together before connector work. |
| Direct candidate apply and backlog commit diverge | Medium | Medium | Keep direct apply compatibility but make backlog commit the primary path. |
| New table duplicates task features | Medium | Medium | Keep backlog items pre-commit only; tasks remain execution state. |
| Connector assumptions leak into backlog model | Medium | High | No connector-specific fields on backlog item in B1 except optional future role hint if already needed. |
| SQLite/Postgres migration parity | Medium | High | Use dialect-compatible schema and run both test suites. |

---

## 12. Open questions

1. Should API-created backlog items be allowed to commit directly to tasks, or require human session confirmation first?

Recommended defaults:

1. Allow API-key commit only for project-scoped keys; audit source clearly.

---

## 13. Approval checkpoint

Owner decisions recorded 2026-05-01:

1. Approved the new `backlog_items` table.
2. Approved `Backlog` as the default ProjectDetail tab.
3. Approved labels in B1.
4. Approved `urgent` as a first-class backlog and task priority, distinct from `high`.
5. Approved manual backlog creation without a requirement.
6. Clarified that generated backlog is backlog; it should not require a separate accept step unless the user explicitly requested candidate review.
7. Approved deferring connector auto-implementation until after B1-B5.

Implementation should start with Slice B1 and B2 as one coherent capability. Connector automation should not resume until the backlog layer has passed local dogfood.

---

## 14. Project detail UX clarification plan

**Status**: proposed · 2026-05-01 · `[agent:feature-planner]`

This section records the first dogfood UX issue after adding the backlog-first project surface: the current project detail page exposes too many first-level concepts at once (`Workspace`, `Backlog`, `Overview`, `Tasks`, `Documents`, and `More`), while the dark theme makes muted text and secondary badges hard to read. For a new user, the page does not clearly explain what each area is for or what action should happen next.

### Problem diagnosis

| Area | Current issue | Product impact |
|---|---|---|
| Primary rail | Six first-level choices compete for attention. `Workspace` and `Overview` are especially ambiguous beside `Backlog`. | New users must understand the product taxonomy before they can manage work. |
| Default mental model | Backlog, task, workspace, and overview appear as peers. | The backlog-first workflow is less obvious than the underlying implementation structure. |
| Dark theme contrast | Essential metadata often uses muted text on a dark background. | Labels, row metadata, hints, and secondary actions are readable only when the user already knows where to look. |
| Backlog presentation | Items are visible, but the page does not yet clearly teach priority, status, labels, and next action as one work-management unit. | The backlog behaves like a data list instead of a project command surface. |

### Information architecture direction

Reduce first-level navigation to workflow surfaces, not implementation entities.

Recommended primary rail:

1. **Backlog** — default project command surface: triage, ready work, blocked work, urgent items, and commit-to-task.
2. **Planning** — renamed from `Workspace`; requirement capture and generated backlog planning live here.
3. **Tasks** — committed execution work only.
4. **Docs** — project documents and knowledge base.
5. **More** — settings, connectors, advanced activity, and historical/debug views.

Recommended demotions:

- `Overview` should become a summary band at the top of `Backlog`, not a primary tab. It should answer "what needs attention now" with counts and recent change signals.
- `Workspace` should be renamed to `Planning` or `Generate` because "workspace" is too broad and does not explain the job-to-be-done.
- Connector controls should stay out of first-level navigation until connector auto-implementation resumes after B1-B5.

### Backlog page clarity direction

The first screen inside a project should show a scannable operational layout:

```text
Project name / repo signal
Current focus: urgent, blocked, ready, recently committed

Backlog filters: status | priority | label | search | archived

Backlog list:
  priority | status | title | labels | source | next action

Selected item detail:
  description | acceptance criteria | blocked reason | origin | commit-to-task
```

Rules:

- Every backlog row must show `priority`, `status`, `labels`, and one obvious next action.
- `urgent` must be visually distinct from `high`; urgent means "interrupt/current-plan attention", while high means "next planned priority".
- Empty state should offer exactly three entries: add backlog item, generate backlog from a requirement, and run What's Next analysis.
- Advanced planning-run, candidate, role-dispatch, and connector language should stay behind details or advanced controls.

### Visual contrast direction

Treat text roles as usability contracts:

- Primary text: item titles, section labels, selected tab labels.
- Secondary text: useful metadata such as source, timestamps, counts, and labels.
- Tertiary text: long explanatory hints and disabled information only.

Implementation target:

- Raise secondary text contrast on dark backgrounds; do not use muted gray for required metadata.
- Increase badge foreground contrast and use clearer borders for priority/status labels.
- Keep dark UI if desired, but use fewer near-black layers and stronger separation between the page background, panels, rows, and active controls.
- Validate mobile and desktop layouts so labels and row actions do not overlap or disappear.

### Proposed follow-up slices

| Slice | Scope | Definition of Done |
|---|---|---|
| UX-1 | Rename and simplify project rail. | First-level nav has no more than five entries, with Backlog default and Overview demoted into a summary band. |
| UX-2 | Improve dark-theme contrast tokens and backlog badge colors. | Secondary metadata and priority/status labels are readable without relying on white-only text. |
| UX-3 | Rework BacklogTab into list plus selected detail. | New users can identify item priority, status, labels, and next action in one scan. |
| UX-4 | Add dogfood sample backlog data path. | Local data includes at least one urgent UX backlog item for checking row presentation. |

### Dogfood sample created

A local backlog item was created in `.anpm/data.db` on 2026-05-01 for visual verification:

- Title: `Dogfood: verify urgent backlog row clarity`
- Priority: `urgent`
- Status: `triage`
- Labels: `ux`, `contrast`, `dogfood`
- Purpose: verify that urgent priority, labels, status, long description, and acceptance criteria are readable in the current Backlog tab.
