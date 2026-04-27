# Rules Quickstart — Agent Native PM

Prefer your tool's built-in behavior first. Only apply these rules for capabilities the tool does not already cover.

This is the minimal rule set. Read this first, then expand into source docs only when needed.

## Loading rule

Read `prompt-budget.yml` → `budget.profile` to determine loading depth:

- **`minimal`**: this file is your complete Layer 1. Do NOT load `docs/operating-rules.md` or `docs/agent-playbook.md` unless you need details listed in "When to open full docs" below.
- **`standard`** (default): read this file first, then expand into `docs/operating-rules.md` and `docs/agent-playbook.md`.
- **`full`**: load complete `docs/operating-rules.md` + `docs/agent-playbook.md` immediately.

## Source of truth

1. `docs/operating-rules.md` for safety, scope, validation, conflict handling
2. `docs/agent-playbook.md` for routing and role ownership
3. `DECISIONS.md` for active architectural constraints
4. `docs/data-model.md` for canonical database schema
5. `docs/api-surface.md` for canonical API contract

## Trust level

Default: `semi-auto`. Override per session.

- `supervised` — all checkpoints require human approval
- `semi-auto` — Small/low-risk tasks run autonomously; checkpoints for Large/destructive work
- `autonomous` — proceeds without approval except destructive actions

## Layered configuration

1. Global: `rules/global/core.md`
2. Domain: `rules/domain/backend-api.md`, `rules/domain/frontend-components.md`, `rules/domain/documentation-sync.md`
3. Project: `project/project-manifest.md`

Precedence: Project > Domain > Global.

## Mandatory workflow (compact)

1. Discover — read relevant files before coding
2. Triage — classify as Small / Medium / Large
3. Check decisions — read `DECISIONS.md`, verify no contradiction
4. Plan backlog — list concrete tasks and acceptance criteria before coding
5. Prioritize backlog — define execution order by value, risk, and dependency
6. Implement — execute in priority order with minimal scope, following existing patterns
7. Validate — `make test` and `make lint`; fix failures
8. Recover — identify root cause on failure; escalate after 3 attempts
9. Record — log decisions in `DECISIONS.md`; update `ARCHITECTURE.md` if structure changed
10. Summarize — produce task completion summary

## Scrum execution checklist (recommended)

Use this lightweight checklist before coding:

1. Backlog clarity: each item includes objective, scope, and acceptance criteria
2. Priority rationale: each item states business value, risk, and dependency
3. Definition of Ready: no unresolved blockers, contracts understood, impacted docs identified
4. Definition of Done: implementation complete, validations pass, docs/decisions updated

## Hard constraints

- Never expose credentials or secrets.
- Never do destructive actions without approval.
- Do not silently ignore errors or remove failing tests.
- Do not implement first and backfill requirements afterward.
- **If a requirement is ambiguous or has two reasonable interpretations, present both and ask before coding. State what would break if an assumption is wrong.** (→ GLOBAL-001)
- **Touch only what the task requires. Do not clean up pre-existing dead code, style issues, or unbroken logic unless that is the explicit task goal.** (→ GLOBAL-010)
- **For bug fixes, write a failing reproduction test first; fix only after confirming the test captures the issue.** (→ GLOBAL-011)
- Follow existing repository patterns unless user explicitly asks for refactor.
- PostgreSQL is the active runtime data store. Do not introduce SQLite-only assumptions or regress the schema/docs back to legacy Phase 1 constraints.
- All API responses must use the JSON envelope: `{ data, error, meta }`.
- Agent-generated content must include a source marker.

## Always-dangerous operations (require approval)

- Deleting files or directories
- Dropping database tables or destructive migrations
- `git push --force`, `git reset --hard`, amending published commits
- Modifying CI/CD pipelines, deployment configs, or Docker setup
- Publishing packages, creating releases, or pushing to main/production

## Error recovery

1. Read the full error message
2. Identify root cause (specific file and line)
3. Fix only what is needed
4. Re-run validation
5. Escalate after 3 failed attempts

## Escalation points

- Contradiction with existing decision in `DECISIONS.md`
- Scope expansion beyond approved plan
- Same error persists after 3 fix attempts
- Architecture change without recording in `DECISIONS.md`

## When to open full docs

- Need trust-level gate details → `docs/operating-rules.md`
- Need role routing details → `docs/agent-playbook.md`
- Need data model details → `docs/data-model.md`
- Need API contract details → `docs/api-surface.md`
- Need MVP scope check → `docs/mvp-scope.md`

## Constitutional principles (non-bypassable)

1. Never expose credentials in any artifact
2. Never execute unvalidated input as code
3. Never modify production data without backup verification
4. Never disable authentication or authorization
5. Never suppress security test failures

Violation = hard stop regardless of execution mode.

## Checkpoint outcomes

- **STOP** — halt and wait for approval
- **ADVISORY** — log finding, continue
- **PASS** — no output, skip silently

At `semi-auto` (default): destructive actions → STOP; Small tasks → most gates PASS.

## Minimal-profile roles

When `budget.profile: minimal`, only these roles are active:

- **application-implementer** — general product and frontend implementation
- **critic** — adversarial review of plans before approval

All other roles are disabled. If a task needs planning or risk review, escalate to user and recommend switching to standard profile.

## Project-specific reminders

- Dashboard state must be computed from system data, not manual input
- Document staleness is computed from timestamps, not human-entered status
- Health score formula: `(task_completion * 0.7) + (doc_freshness * 0.3)`
- See `docs/mvp-scope.md` before adding features — verify it belongs in the current phase
