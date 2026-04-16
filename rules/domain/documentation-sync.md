# Domain Rules: Documentation Sync — Agent Native PM

This is a custom domain rule set specific to Agent Native PM. It enforces the project's core value proposition: keeping documentation aligned with code.

## Rule entries

### Rule: DOC-001
- Owner layer: Domain
- Domain: documentation-sync
- Stability: core
- Status: active
- Scope: API changes
- Statement: Code changes that add, modify, or remove API endpoints must update `docs/api-surface.md` in the same commit, or create a drift signal explaining what changed and what doc needs updating.
- Rationale: The API surface doc is the contract reference for frontend and agent consumers. Stale API docs cause integration errors.
- Verification: Post-commit check: compare handler registrations against `docs/api-surface.md` entries.
- Supersedes: N/A
- Superseded by: N/A

### Rule: DOC-002
- Owner layer: Domain
- Domain: documentation-sync
- Stability: core
- Status: active
- Scope: schema changes
- Statement: Code changes that add, modify, or remove database tables or columns must update `docs/data-model.md` in the same commit, or create a drift signal.
- Rationale: The data model doc is the canonical schema reference. Stale schema docs cause migration confusion.
- Verification: Post-commit check: compare migration files against `docs/data-model.md` tables.
- Supersedes: N/A
- Superseded by: N/A

### Rule: DOC-003
- Owner layer: Domain
- Domain: documentation-sync
- Stability: core
- Status: active
- Scope: agent-generated content
- Statement: All content created or modified by an agent must include a source marker identifying the agent role (e.g., `[agent:documentation-architect]`, `source: "agent:backend-architect"`).
- Rationale: Enables auditing which content was human-authored vs. agent-generated. Supports drift detection and trust decisions.
- Verification: Grep for source markers in agent-produced commits.
- Supersedes: N/A
- Superseded by: N/A

### Rule: DOC-004
- Owner layer: Domain
- Domain: documentation-sync
- Stability: core
- Status: active
- Scope: dashboard and summary computation
- Statement: Dashboard state and project health metrics must be computed from system data (task counts, document timestamps, sync results), not from free-form human-entered status text.
- Rationale: The project's core value is deriving status from reality, not from what someone remembered to type. Free-form status undermines the automated tracking promise.
- Verification: Review summary computation logic; ensure no direct user-text-to-dashboard path.
- Supersedes: N/A
- Superseded by: N/A

### Rule: DOC-005
- Owner layer: Domain
- Domain: documentation-sync
- Stability: behavior
- Status: active
- Scope: architecture changes
- Statement: Changes that modify module boundaries, add new modules, or change inter-module dependencies must update `ARCHITECTURE.md`.
- Rationale: ARCHITECTURE.md is the first doc agents read to understand system structure. Stale architecture docs cause misdirected changes.
- Verification: Post-commit check: compare Go package structure against ARCHITECTURE.md module list.
- Supersedes: N/A
- Superseded by: N/A

### Rule: DOC-006
- Owner layer: Domain
- Domain: documentation-sync
- Stability: behavior
- Status: active
- Scope: decision tracking
- Statement: Behavioral or architectural decisions made during implementation must be recorded in `DECISIONS.md` before the task is marked complete.
- Rationale: Decisions not recorded are decisions lost. Future agents and humans need the context.
- Verification: Post-task checklist item; risk-reviewer checks decision log compliance.
- Supersedes: N/A
- Superseded by: N/A
