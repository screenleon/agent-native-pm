# Phase 5 plan — Prompt externalization + execution role library

**Status**: approved · 2026-04-24 · `[agent:feature-planner]`
**Gates**: owner approval satisfied. This document drives the implementation in PR that follows and records what landed vs deferred.
**Precondition**: Phase 4 (PR #21) is merged on `main`. The three-phase pre-PR gate codified in `docs/operating-rules.md` § "Pre-PR verification" applies to this PR.

---

## 1. Problem statement

Phase 4 ended with `gated-autonomy` positioning: human picks which requirement → agent executes. The next step toward "agent writes backend / DB / API / UI" (the user-stated north star) hits two concrete obstacles today:

**Gap A — Prompts are hardcoded strings in two languages.**
`adapters/backlog_adapter.py` `_build_prompt` (≈55 lines) and `adapters/whatsnext_adapter.py` `_build_prompt` (≈60 lines) are Python f-strings. `backend/internal/connector/builtin_adapter.go` `buildBuiltinPrompt` is a separate Go implementation of the same prompt text. Every prompt tweak requires editing two files in two languages; S5b already caused one drift incident between them. There is no version marker, no metadata, no way to A/B a prompt, and no way to add a new prompt without writing Go code.

**Gap B — There is no "role library".**
To move toward execution (where a task is dispatched to a specialized agent), we need a set of role prompts — one per execution specialty (backend, UI, DB schema, API contract, test, code review). None exist today. Backlog candidates also have no `execution_role` field to bind them to a specialist; the only type signal is the free-form `suggestion_type` string which is unused downstream.

**Operator-facing consequence today:**
- "How do I change the backlog prompt?" → edit two files, re-test, re-build the connector binary.
- "Which execution specialist handles this candidate?" → undefined; current UI has no concept of role.
- "Can I reuse a prompt from GPT-Prompt-Hub?" → no, prompts are baked into code.

---

## 2. Current state inventory

### Prompt surfaces

| File | LOC | Role |
|---|---|---|
| `adapters/backlog_adapter.py` `_build_prompt` | 55 | Planner — decompose requirement into N candidates |
| `adapters/whatsnext_adapter.py` `_build_prompt` | 60 | Strategic advisor — identify highest-leverage directions |
| `backend/internal/connector/builtin_adapter.go` `buildBuiltinPrompt` | 115 | Go reimplementation of both above for zero-flag connector |

### Backlog candidate schema

| Column | Type | Current |
|---|---|---|
| `suggestion_type` | TEXT | Free-form, set by planner, not enforced by any catalog |
| (no `execution_role` column) | — | — |

### Migration state

Latest applied: `025_local_connectors_metadata.sql`. Next available: `026`.

### Frontend

`CandidateReviewPanel.tsx` renders candidate cards with title / description / rationale / evidence / apply button. No role indicator today. Apply payload only takes `{ planning_run_id, candidate_id }`; there is no `execution_mode` concept.

---

## 3. End state

### Tier A — Prompt externalization (mandatory foundation)

**A1: Canonical prompt package at `backend/internal/prompts/`**

New Go package. Holds the `.md` files AND the tiny renderer. Rationale for the path (not repo-root `prompts/`): Go's `//go:embed` cannot reach outside the containing package's directory, and we need the connector binary to ship prompts embedded at build time. Python adapters read the same files at runtime via a known relative path.

Structure:
```
backend/internal/prompts/
├── README.md              # describes the schema + variable contract
├── embed.go               # //go:embed *.md and roles/*.md
├── render.go              # tiny regex-based template engine
├── render_test.go
├── backlog.md             # planner prompt (was _build_prompt)
├── whatsnext.md           # strategic advisor prompt
└── roles/
    ├── README.md
    ├── backend-architect.md
    ├── ui-scaffolder.md
    ├── db-schema-designer.md
    ├── api-contract-writer.md
    ├── test-writer.md
    └── code-reviewer.md
```

**Template syntax: `{{VAR_NAME}}`** — double brace, uppercase + underscores. Single-pass regex substitution (`\{\{(\w+)\}\}`). Unknown variables are left as-is so partial prompts surface the missing variable rather than silently erasing it. JSON schema examples in the prompt body use single braces (`{`, `}`) — unambiguous against `{{VAR}}`.

**Frontmatter** matches GPT-Prompt-Hub so we can cross-pollinate later:
```yaml
---
title: "Backlog Planner"
category: planning
tags: [backlog, decomposition]
model: any
use_case: "..."
version: 1
---
```

**A2: Migrate backlog + whatsnext prompts verbatim**

The first-pass migration must produce **byte-identical rendered output** versus the current hardcoded Python string for the same inputs. A new Go unit test pins the rendered-output hash for both prompts at a known fixture; a Python test does the same. Any future intentional change to prompt wording must update the hash test.

**A3: Both adapters read from the canonical location**

- `builtin_adapter.go` `buildBuiltinPrompt(adapterType, input)` → calls `prompts.Render("backlog", vars)` or `prompts.Render("whatsnext", vars)`.
- `adapters/backlog_adapter.py` and `whatsnext_adapter.py` load `backend/internal/prompts/<name>.md` (path resolved relative to the Python file or via an env override for CI), strip frontmatter, and apply the same regex substitution.
- The Python adapter gets a small shared helper `_prompt_loader.py` so both backlog and whatsnext share loader logic.

After A3 the Go and Python adapters render from the **same source file**. Future prompt changes edit one markdown file.

### Tier B — Execution role library + schema breadcrumb (no execution yet)

**B1: Six execution role prompts as a library**

Written as markdown following the Prompt-Hub schema. Each role prompt declares:
- Role name + 1-sentence objective
- Inputs (what the caller provides)
- Output format (structured: JSON or diff or file list, per role)
- Constraints (guardrails specific to that role)

**These are not invoked by the planner in this PR.** They are static library content for Tier C to consume later. The value today is: the role definitions exist, they are version-controlled, and the planner's Tier C extension can grep them.

Scope choice: we ship 6 roles as the minimum viable library covering the user's stated north star (backend / UI / DB / API / test / review). Further roles (mobile-scaffolder, data-pipeline-engineer, ML-model-builder, …) are explicitly out of scope.

**B2: Backlog candidate gains `execution_role` (nullable)**

Migration `026_backlog_candidate_execution_role.sql`:
```sql
ALTER TABLE backlog_candidates ADD COLUMN execution_role TEXT;
```
Sibling `026_*.down.sql` drops the column.

- `models.BacklogCandidate` gains `ExecutionRole *string`.
- `models.BacklogCandidateDraft` gains optional `ExecutionRole string`.
- `ConnectorBacklogCandidateDraft` gains optional `execution_role` (wire).
- `backlog_candidate_store.go` `ListByPlanningRun` / `GetByID` / `CreateDraftsForPlanningRun` include the new column.
- `UpdateBacklogCandidate` handler accepts `execution_role` patch.
- Python + Go adapters are extended to **optionally emit** `execution_role` in their candidate JSON — BUT the two planner prompts in this PR do NOT ask the model to fill it. That is deliberate: Tier C is where the planner prompt learns the role catalog. This PR only adds the column + API field so Tier C has a home to write into.

**Planner prompt stays unchanged on this front.** Only the wire/schema surface changes.

**B3: `execution_mode` on apply (breadcrumb UI only)**

`POST /api/backlog-candidates/:id/apply` gains an optional request body field:
```jsonc
{ "execution_mode": "manual" | "role_dispatch" }
```
Default is `"manual"` — matches current behaviour exactly (create a task row, done).

`"role_dispatch"` is accepted but **currently behaves identically to `"manual"`**. It marks the task's `source` field with `"role_dispatch:<execution_role>"` so audit logs can tell apart tasks that were explicitly earmarked for future auto-execution. The actual auto-dispatch is Tier C.

Frontend:
- `CandidateReviewPanel` renders a small chip `Role: backend-architect` when `execution_role` is set.
- Apply dialog gains a radio `Execution: [Manual] [Auto-dispatch (future)]`. The second option is disabled with a `(coming in Phase 6)` hint — the radio exists today so the UI affordance is ready the day Tier C ships.

### Tier C — Out of scope (roadmap only)

Explicitly NOT in this PR:
- Planner prompt learning the role catalog and emitting `execution_role`.
- Execution adapter that takes a task + role prompt and runs the agent.
- PR / commit emission, rollback, test harness.
- Role-dispatch actually dispatching.

---

## 4. Non-goals

- **No prompt template engine beyond regex substitution.** No conditionals, loops, includes. If a prompt needs branching logic, the caller pre-computes the branch and substitutes a single variable.
- **No YAML frontmatter parser in Python.** We only need `title`, `description`, and the body. Strip `---` … `---` at the top; use the body. Go does the same.
- **No database migration for role definitions.** Roles are files on disk, not DB rows. They are versioned with the code.
- **No changes to existing planner behaviour.** Candidates produced today will continue to have `execution_role = NULL`; the column is future-facing.
- **No dispatch of role-earmarked tasks.** `role_dispatch` is a marker, not a trigger.
- **No new migration for `suggestion_type`.** That column stays as-is; we are NOT replacing it with `execution_role`. Both coexist: `suggestion_type` is the planner's semantic tag; `execution_role` is the execution-time specialist.

---

## 5. Slice plan

### Slice P5-A1: `backend/internal/prompts/` package skeleton

**Scope**
1. New package `backend/internal/prompts/`.
2. `render.go` exports `Render(name string, vars map[string]string) (string, error)`. Implementation: load `<name>.md` from embed.FS, strip `---\n…\n---\n` frontmatter, run `regexp.MustCompile(`\{\{(\w+)\}\}`).ReplaceAllStringFunc` with the vars map, return body.
3. `embed.go` — `//go:embed *.md roles/*.md` → `embed.FS`.
4. `render_test.go` — unit tests for: unknown-var-preserved, multi-pass-safety, frontmatter-stripped, missing-file errors.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-P5-A1-1 | `Render("nonexistent", …)` | returns error |
| T-P5-A1-2 | `Render("backlog", {"PROJECT_NAME": "X"})` | returns body with `{{PROJECT_NAME}}` replaced by `"X"` |
| T-P5-A1-3 | Unknown variable `{{FOO}}` left as-is | preserves marker |
| T-P5-A1-4 | Value containing `{{OTHER}}` not re-substituted | single-pass safety |
| T-P5-A1-5 | Frontmatter `---\ntitle: ...\n---\n` stripped | body only |
| T-P5-A1-6 | JSON schema `{ "a": 1 }` in body preserved | single-brace not treated as template |

**Size**: S. ~80 LOC Go + 60 LOC test.

### Slice P5-A2: Migrate backlog + whatsnext markdowns

**Scope**
1. `backend/internal/prompts/backlog.md` — verbatim migration of Python `_build_prompt` for backlog, with `{{PROJECT_NAME}}`, `{{PROJECT_DESCRIPTION_LINE}}`, `{{REQUIREMENT}}`, `{{MAX_CANDIDATES}}`, `{{CONTEXT}}`, `{{SCHEMA_VERSION}}` placeholders.
2. `backend/internal/prompts/whatsnext.md` — same for whatsnext.
3. `backend/internal/prompts/README.md` — documents the variable contract per prompt.
4. Fixture test: given fixed inputs, `Render` produces the byte-identical rendered output that today's Python `_build_prompt` produces. Hash-pinned in the test.

**Size**: S. ~150 lines markdown + ~30 LOC fixture test.

### Slice P5-A3: Adapter refactor

**Scope**
1. `builtin_adapter.go` `buildBuiltinPrompt` → calls `prompts.Render("backlog", …)` / `prompts.Render("whatsnext", …)`. Delete the Go implementation of the prompt text.
2. `adapters/_prompt_loader.py` — new shared helper. Loads a prompt by name, strips frontmatter, runs the same regex substitution.
3. `adapters/backlog_adapter.py` `_build_prompt` → `prompt_loader.load_and_render("backlog", vars)`.
4. `adapters/whatsnext_adapter.py` similar.
5. Existing adapter tests continue to pass byte-identical.
6. A new cross-language test (Python side) confirms the Python-rendered prompt matches the Go-rendered prompt for the same fixture inputs.

**Size**: M. ~80 LOC Go diff + ~60 LOC Python + ~30 LOC test.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-P5-A3-1 | Existing `builtin_adapter_test.go` `TestBuildBuiltinPrompt_*` still pass | Refactor is behaviour-preserving |
| T-P5-A3-2 | Python `test_backlog_adapter.py` still pass | Same |
| T-P5-A3-3 | Py-rendered == Go-rendered for a fixed requirement fixture | Cross-lang parity |
| T-P5-A3-4 | Editing `backlog.md` changes both Python and Go rendered output | Single source of truth confirmed |

### Slice P5-B1: Six role prompts

**Scope**
1. `backend/internal/prompts/roles/backend-architect.md`
2. `backend/internal/prompts/roles/ui-scaffolder.md`
3. `backend/internal/prompts/roles/db-schema-designer.md`
4. `backend/internal/prompts/roles/api-contract-writer.md`
5. `backend/internal/prompts/roles/test-writer.md`
6. `backend/internal/prompts/roles/code-reviewer.md`
7. `backend/internal/prompts/roles/README.md` — explains role id = filename stem; declares the shared output contract every role must respect (JSON-fenced block with stable schema); lists which roles are in the library today.

Each role's prompt is written to be executable by `claude` / `codex` / a hosted chat API. Input variables use `{{TASK_TITLE}}`, `{{TASK_DESCRIPTION}}`, `{{PROJECT_CONTEXT}}`, `{{REQUIREMENT}}` as a consistent contract (not all roles use all variables).

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-P5-B1-1 | `Render("roles/backend-architect", …)` returns body | Loading works for nested path |
| T-P5-B1-2 | Each role file has frontmatter with `title`, `role_id`, `version` | Schema present |
| T-P5-B1-3 | `roles/README.md` lists all 6 roles that exist on disk | Kept in sync |

**Size**: M. ~200 lines markdown × 6 = ~1200 lines markdown + 60 LOC test.

### Slice P5-B2: `execution_role` column

**Scope**
1. Migration `026_backlog_candidate_execution_role.sql` + `.down.sql`.
2. `models.BacklogCandidate.ExecutionRole *string`, JSON `"execution_role"` with `omitempty`.
3. `models.BacklogCandidateDraft.ExecutionRole string` (plain, empty means unset).
4. `ConnectorBacklogCandidateDraft.ExecutionRole string` + wire tests.
5. `backlog_candidate_store.go` — extend SELECT in `ListByPlanningRun`, `GetByID`, `ListByRequirement`, INSERT in `CreateDraftsForPlanningRun`, and UPDATE in `UpdateStatus`-family methods.
6. `planning_runs.go` handler `UpdateBacklogCandidate` accepts `execution_role` patch.
7. Frontend types + API client + type echoes.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-P5-B2-1 | Migration 026 applies clean under SQLite and Postgres | Dual-driver |
| T-P5-B2-2 | `GET /api/planning-runs/:id/backlog-candidates` returns `execution_role` (nullable) | API contract |
| T-P5-B2-3 | `PATCH /api/backlog-candidates/:id { "execution_role": "ui-scaffolder" }` | Patch works |
| T-P5-B2-4 | Patching with an unknown role string | Accepted (no catalog enforcement yet); document as Tier C hardening |
| T-P5-B2-5 | Creating a candidate without `execution_role` | Column is NULL, API returns `null` |

**Size**: M. ~30 LOC migration + ~80 LOC Go (model+store+handler) + ~30 LOC frontend types.

### Slice P5-B3: Apply dialog + `execution_mode`

**Scope**
1. `POST /api/backlog-candidates/:id/apply` body optionally takes `execution_mode: "manual" | "role_dispatch"`. Unknown values → 400.
2. When `execution_mode = "role_dispatch"` AND the candidate has `execution_role` set, the created task's `source` field becomes `"role_dispatch:<execution_role>"` (up to 80 chars) so the audit trail reflects the marker. Otherwise behaviour unchanged.
3. Frontend `CandidateReviewPanel` renders a chip `Role: backend-architect` when `candidate.execution_role` is set.
4. Apply dialog: new radio group `Execution: ● Manual  ○ Auto-dispatch (coming in Phase 6)`. The second option is disabled. Wire the selected value through to the apply request.

**DoD**

| ID | Scenario | Expected |
|---|---|---|
| T-P5-B3-1 | Apply with no body (back-compat) | Same as before; task source unchanged |
| T-P5-B3-2 | Apply with `execution_mode=manual` | Explicit form of back-compat |
| T-P5-B3-3 | Apply with `execution_mode=role_dispatch` + role set | Task `source` = `"role_dispatch:<role>"` |
| T-P5-B3-4 | Apply with `execution_mode=role_dispatch` + no role on candidate | Accepted, source = `"role_dispatch"` (no role suffix) |
| T-P5-B3-5 | Apply with `execution_mode=invalid` | 400 |
| T-P5-B3-6 | Frontend chip renders when `execution_role` present | Smoke test |
| T-P5-B3-7 | Frontend radio `Auto-dispatch` is disabled | Smoke test |

**Size**: S. ~40 LOC Go (handler + tests) + ~80 LOC TSX + ~30 LOC test.

---

## 6. Implementation order

```
Day 1:
  P5-A1  prompts package + render engine + unit tests
  P5-A2  migrate backlog.md + whatsnext.md verbatim + fixture tests

Day 2:
  P5-A3  refactor Go + Python adapters; cross-lang parity test
  P5-B1  6 role markdowns + roles/README.md

Day 3:
  P5-B2  migration + model + store + handler + frontend types
  P5-B3  apply endpoint + UI chip + radio group

Day 4:
  Docs sync (api-surface + data-model + DECISIONS + ARCHITECTURE)
  Pre-PR pipeline (make pre-pr)
  Critic subagent pass
  /security-review + risk-reviewer pass
  Fix findings
  Open PR
```

---

## 7. Risk assessment

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | Go + Python rendered output drifts despite shared source | Low | High | Cross-lang fixture test (T-P5-A3-3) pins both outputs against the same hash |
| R2 | `//go:embed` path restriction forces awkward layout | Low | Low | Prompts live inside `backend/internal/prompts/`; documented prominently in the package README |
| R3 | Unknown-role strings in `execution_role` (typo, deleted role) cause silent downstream confusion | Medium | Low | Document as known limitation in this PR; Tier C will introduce catalog enforcement |
| R4 | Existing prompt tests assume exact string match against the hardcoded Go/Python body | Medium | Medium | Byte-identical migration target + hash-pinned fixture test catches any drift |
| R5 | Frontmatter stripping misses edge cases (no trailing newline, nested `---`) | Low | Low | Specific regex: match only `^---\n.*?\n---\n` non-greedy at start-of-file |
| R6 | `execution_mode=role_dispatch` accepted today but dispatch never fires → operators think the feature is broken | Medium | Medium | UI explicitly labels the option "(coming in Phase 6)"; radio is disabled; server marks task source for audit only |
| R7 | Role markdown files grow large and bloat the connector binary | Low | Low | Current 6 roles ≈ 30KB total after embed; connector binary ≈ 15MB; impact negligible |

---

## 8. Open questions

1. **Python path resolution for `backend/internal/prompts/*.md`**: should the Python adapter resolve the path relative to its `__file__` (works in the checkout) or accept an env override `ANPM_PROMPTS_DIR`? Current plan: both — relative default, env override for CI and unusual deployments.
2. **Role catalog enforcement**: should the server's `PATCH /api/backlog-candidates/:id { "execution_role": ... }` validate the role id against the on-disk role files? Current plan: NO in this PR (Tier C owns that). Document as a Tier C must-do.
3. **Prompt versioning**: frontmatter has a `version: 1` field. Should the server record which version was used for a given planning run? Current plan: NO in this PR; the prompt files are version-controlled in git, and `planning_runs.created_at` + git blame answers the question. Revisit if A/B testing is introduced.

---

## 9. Status tracking

| Slice | Status | PR |
|---|---|---|
| P5-A1 — prompts package + render | implemented | bundled |
| P5-A2 — backlog.md + whatsnext.md migration | implemented | bundled |
| P5-A3 — adapter refactor | implemented | bundled |
| P5-B1 — 6 role prompts | implemented | bundled |
| P5-B2 — `execution_role` column | implemented | bundled |
| P5-B3 — apply dialog + UI hint | implemented | bundled |
| Docs sync | implemented | bundled |

**Phase 5 complete** when all 6 slices + docs sync are merged AND `make pre-pr` is green AND `critic` + `/security-review` + `risk-reviewer` have all run with must-fix findings resolved.

---

Source: `[agent:feature-planner]`. References `docs/phase4-plan.md`, `docs/operating-rules.md` § "Pre-PR verification", the GPT-Prompt-Hub repo (https://github.com/LichAmnesia/GPT-Prompt-Hub) as a prompt-schema reference. No code lands until the Phase 5 PR passes the three-phase gate.
