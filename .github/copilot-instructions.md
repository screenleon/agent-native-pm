# Repository instructions for AI coding agents — Agent Native PM

- Read `AGENTS.md` first.
- **Agent-deference principle**: this project only adds rules the agent tool does not already provide natively. See `docs/operating-rules.md` → Agent-deference principle.
- **Trust level**: defaults to `semi-auto`. Read `prompt-budget.yml` for the current mode.
- **Profile-aware loading**: read `prompt-budget.yml` → `budget.profile` first. At `minimal`, use `docs/rules-quickstart.md` as your complete Layer 1.
- **Tool portability**: roles are conceptual ownership boundaries. In this repo they are implemented through `.claude/agents/`, source-of-truth docs, and these Copilot instructions rather than a `skills/*`-driven primary workflow.
- Follow `docs/operating-rules.md` for safety, scope, and validation rules.
- Follow layered configuration precedence: Project Context > Domain Rules > Global Rules.

## Role routing

- Use `feature-planner` for cross-module, ambiguous, contract, database, or security changes.
- Use `backend-architect` for Go backend contract work, schema changes, and migrations.
- Use `application-implementer` for React frontend work or general product implementation.
- Use `integration-engineer` to wire frontend to backend and close flow gaps.
- Use `documentation-architect` for architecture docs, ADRs, and documentation sync.
- Use `risk-reviewer` before finalizing behavior-changing or high-risk work.
- Use `critic` after a plan is produced, before user approval.

## Project-specific rules

- PostgreSQL is the active runtime database. Do not introduce SQLite-only assumptions unless explicitly working on historical migration cleanup.
- All API responses use envelope: `{ data, error, meta }`.
- Agent-generated content must include a source marker.
- Dashboard state must be computed from system data, not manual input.
- Consult `docs/mvp-scope.md` before adding features.

## Mandatory workflow

Before implementation:
1. Discover the codebase — read relevant files.
2. Classify task scale: Small, Medium, or Large.
3. Read `DECISIONS.md` and check for contradictions.
4. State assumptions, constraints, and proposed approach.

After any code change:
5. Run `make test` and `make lint`. Fix failures.
6. Escalate after 3 consecutive failures.
7. Record decisions in `DECISIONS.md` when applicable.
8. If scope expands: STOP and request approval (at `semi-auto`).
9. Produce a task completion summary.
