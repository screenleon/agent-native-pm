# Agent Playbook — Agent Native PM

Read these files before starting work:

0. Read `prompt-budget.yml` → `budget.profile` to determine loading depth:
   - `minimal`: load `docs/rules-quickstart.md` as the complete Layer 1. Skip to step 3.
   - `standard` / `full`: continue to step 1.
1. `docs/rules-quickstart.md` — minimal rules for first-pass loading
2. `docs/operating-rules.md` — safety, scope, validation, project-specific constraints
   `docs/agent-playbook.md` — routing rules and role definitions
3. `docs/agent-templates.md` — reusable task and prompt templates

Read `docs/product-blueprint.md` to understand the product vision and MVP scope.
Read `docs/data-model.md` when working on backend data layer changes.
Read `docs/api-surface.md` when working on API endpoints.
Read `docs/mvp-scope.md` when evaluating whether a feature belongs in the current phase.

## Three-layer architecture

1. **Rules** — `docs/operating-rules.md` (safety, scope, validation, constraints)
2. **Skills** — executed natively by agent tool capabilities (this project does not use a separate `skills/` directory)
3. **Loop** — `Discover → Triage → Plan → Critique → Approve → Implement → Test → Fix → Repeat → Record → Summarize`

Roles in this repository are conceptual ownership boundaries. They are implemented through `.claude/agents/*.md`, repo docs, and tool-specific instruction surfaces such as `.github/copilot-instructions.md`.

## Configuration layering

Keep constraints layered: `rules/global/` → `rules/domain/` → `project/project-manifest.md`.

Precedence: Project Context > Domain Rules > Global Rules.

## Core rules

- Use a planning agent first for cross-module, ambiguous, high-risk, API, DB, or security work.
- Use an application implementer for general product or frontend work.
- Use a documentation-focused agent when the main output is docs, ADRs, or architecture notes.
- Keep reusable instructions in version-controlled files, not only in chat history.
- Prefer specialized agents with clear ownership over one general-purpose agent.
- Never treat code as complete until the validation loop passes.
- Each role runs in its own context. Pass structured handoff artifacts between roles.

## Project-specific rules

- All behavior changes that affect user-facing API, data model, or status computation must either update linked docs or emit a drift signal.
- Agent-generated content must include a source marker (e.g., `[agent:documentation-architect]`).
- Dashboard state must be computed from system data, not from free-form human input.
- PostgreSQL is the active runtime data store. Treat older SQLite references as historical unless a task explicitly targets legacy assumptions.

## Source of truth

- `docs/operating-rules.md` — safety, scope, validation, review rules
- `docs/agent-playbook.md` — role routing and role ownership
- `docs/product-blueprint.md` — product vision and phase roadmap
- `docs/data-model.md` — canonical data model
- `docs/api-surface.md` — canonical API contract
- `DECISIONS.md` — active architectural constraints
- `prompt-budget.yml` — execution mode, enabled roles, token budget
- `.claude/agents/` and `.github/copilot-instructions.md` — tool-specific implementations that must stay aligned with the source-of-truth docs above
