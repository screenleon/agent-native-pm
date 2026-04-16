# Agent Playbook — Agent Native PM

## Three-layer architecture

All agent work follows three layers:

1. **Rules** (`docs/operating-rules.md`) — hard constraints: safety, scope, agent-deference, trust level, codebase discovery, validation loop, error recovery, project-specific constraints, decision log.
2. **Skills** — this project does not use a separate `skills/` directory. Skill behaviors (triage, exploration, test-fix loop, error recovery, memory management) are executed natively by the agent using tool capabilities.
3. **Loop** — every implementation follows: Discover → **Triage** → Plan → **Critique** → **Approve** → Implement → Test → Fix → Repeat → Record → **Summarize**. Steps in **bold** are trust-level-gated; see `docs/operating-rules.md` → Trust level.

> **Note:** This project adapts the template's 16-step workflow into a streamlined 9-step workflow appropriate for a Phase 1 greenfield project. As the codebase matures, steps can be expanded.

## Layered configuration model

1. **Global Rules** — `rules/global/core.md`
2. **Domain Rules** — `rules/domain/backend-api.md`, `rules/domain/frontend-components.md`, `rules/domain/documentation-sync.md`
3. **Project Context** — `project/project-manifest.md`

Precedence: Project Context > Domain Rules > Global Rules.

## Budget profiles

This project uses a simplified budget model. Set `budget.profile` in `prompt-budget.yml`:

| Profile | Roles available | Recommended when |
|---------|-----------------|------------------|
| **minimal** | application-implementer, critic | Single-file Small tasks, tight token budgets |
| **standard** (default) | feature-planner, backend-architect, application-implementer, integration-engineer, documentation-architect, risk-reviewer, critic | Typical development work |
| **full** | All roles, full validation | Large or high-risk changes |

## Repository asset map

- Global entrypoint: `AGENTS.md`
- Project subagents: `.claude/agents/*.md`
- Reusable templates: `docs/agent-templates.md`
- Repo-wide Copilot instructions: `.github/copilot-instructions.md`
- Decision log: `DECISIONS.md`
- Architecture overview: `ARCHITECTURE.md`
- Product blueprint: `docs/product-blueprint.md`
- Data model: `docs/data-model.md`
- API surface: `docs/api-surface.md`
- MVP scope: `docs/mvp-scope.md`

## Source of truth and precedence

Use this precedence order when documents overlap:

1. `docs/operating-rules.md` for safety, scope control, validation, and destructive-action rules
2. `docs/agent-playbook.md` (this file) for routing, role definitions, and workflow ownership
3. `AGENTS.md` as the short root entrypoint into those two files
4. `docs/agent-templates.md` as reusable prompt scaffolds
5. `.claude/agents/`, `.github/copilot-instructions.md` as tool-specific implementations

If a tool-specific file drifts from the source-of-truth docs, update the tool-specific file to match.

## Default routing

### Use the planning agent first when

- a request impacts more than one module (e.g., changes to both `tasks` and `drift`)
- a request changes API contracts, database schema, or migrations
- a request touches authentication, API keys, or agent permission logic
- a request is ambiguous and needs scope clarification
- a request affects the drift detection or sync pipelines

### Use specialist agents directly when

- backend contract work is isolated to a single module
- general application or frontend work is bounded and low ambiguity
- integration work is mostly wiring existing pieces together
- documentation is the primary deliverable

## Role definitions

### `feature-planner`

- Defines scope, non-goals, impacted modules, dependencies, order, and validation
- Owns ambiguity reduction before implementation starts
- For Agent Native PM: plans cross-module features (e.g., drift detection pipeline, agent run logging)

### `backend-architect`

- Owns contract-first backend design, schema changes, and high-risk backend behavior
- For Agent Native PM: designs Go module APIs, SQLite schema, migration strategy

### `application-implementer`

- Owns general product implementation — frontend components, service layer, app behavior
- For Agent Native PM: implements React dashboard, task board, document list, CRUD pages

### `integration-engineer`

- Owns wiring across API, state, navigation, side effects, and complete user journeys
- For Agent Native PM: connects React frontend to Go API, wires sync triggers, drift signal display

### `documentation-architect`

- Owns repository instructions, architecture docs, ADRs, and process documentation
- Responsible for maintaining `DECISIONS.md`, `ARCHITECTURE.md`, and project constraints
- For Agent Native PM: keeps data-model.md and api-surface.md aligned with code

### `risk-reviewer`

- Owns bug finding, regression detection, security review, and testing gaps
- Provides early risk assessment during planning for high-risk work
- For Agent Native PM: reviews schema migrations, API key auth, drift detection correctness

### `critic`

- Adversarial design reviewer invoked after a proposal and before user approval
- Challenges over-engineering, hidden coupling, missing edge cases, scope creep
- Does not rewrite proposals — states what is wrong

## Mandatory workflow

### Streamlined 10-step workflow

1. **Discover** — read relevant files and understand existing patterns before coding
2. **Triage** — classify task scale (Small / Medium / Large) based on file count, module count, and risk
3. **Check decisions** — read `DECISIONS.md` and verify no contradiction
4. **Plan backlog** — capture concrete backlog items and acceptance criteria before writing code
5. **Prioritize** — order backlog by user value, risk, and dependencies
6. **Implement** — write code with minimal scope; follow existing patterns and priority order
7. **Validate** — run `make test` and `make lint`; fix failures before marking complete
8. **Recover** — if validation fails, read the error, identify root cause, fix, re-validate; escalate after 3 failed attempts
9. **Record** — log decisions in `DECISIONS.md`; update `ARCHITECTURE.md` if structure changed
10. **Summarize** — produce a task completion summary (see `docs/agent-templates.md`)

### Step phase classification

| Phase | Steps | Behavior |
|-------|-------|----------|
| **PRE** (context) | 1-Discover, 2-Triage, 3-Check decisions, 4-Plan backlog, 5-Prioritize | Run before producing any deliverable |
| **CORE** (work) | 6-Implement, 7-Validate, 8-Recover | Run during the main work loop |
| **POST** (finalize) | 9-Record, 10-Summarize | Run after work is complete; do not skip |

### Checkpoint gates

Checkpoint activation depends on trust level (see `docs/operating-rules.md`):

- Destructive actions → always STOP
- Scope expansion → STOP at `supervised` and `semi-auto`
- Plan approval → STOP at `supervised`; STOP for Large at `semi-auto`

### Workflow variants

#### New feature (Medium/Large)

`feature-planner` → `critic` → **user decision** → `backend-architect` and/or `application-implementer` → `integration-engineer` → `documentation-architect` as needed → `risk-reviewer`

#### High-risk backend change

`feature-planner` → `critic` → `risk-reviewer` (plan assessment) → **user decision** → `backend-architect` → `risk-reviewer` (final review)

#### Small change

`application-implementer` (with inline 1-2 sentence plan) → targeted validation only

No planning agent, critic, or risk-reviewer required.

#### Documentation-heavy change

`documentation-architect` → `risk-reviewer` when technical correctness matters

## Context isolation

Each agent role should run in its own context. Do not chain roles in a single long conversation.

- Each step in a workflow is a **separate agent invocation**.
- Agents communicate through **handoff artifacts**, not shared conversation history.
- If the tool does not support separate sessions, summarize output into a handoff artifact and restart.

```text
[Context 1] feature-planner → produces plan artifact
[Context 2] critic → receives plan artifact → produces critique artifact
[User]      reviews plan + critique → decides
[Context 3] backend-architect → receives approved plan → produces implementation
[Context 4] risk-reviewer → receives implementation summary → produces review
```

## Documentation sync awareness

This project has a custom domain rule for documentation sync (`rules/domain/documentation-sync.md`). Key rules:

- **DOC-001**: Code changes that affect API endpoints must update `docs/api-surface.md` or create a drift signal
- **DOC-002**: Schema changes must update `docs/data-model.md` or create a drift signal
- **DOC-003**: Agent-generated content must include a source marker
- **DOC-004**: Dashboard state must be derived from system data, not free-form input

## Ownership principles

- Planning agents define scope, order, dependencies, and validation.
- Implementation agents stay inside their domain and avoid unnecessary expansion.
- Integration agents close loops across state, navigation, side effects, and data flow.
- Documentation agents keep instructions and architecture docs aligned with the actual codebase.
- Review agents lead with findings, not summaries.

## Maintenance principles

- Keep root guidance (`AGENTS.md`) short and stable.
- Put details in focused docs.
- Promote repeated prompts into `docs/agent-templates.md`.
- Prefer one conceptual role model with tool-specific implementations.
