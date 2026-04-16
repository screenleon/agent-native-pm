# Global Rules — Agent Native PM

These rules apply universally across all modules and domains.

## Communication norms

### Rule: GLOBAL-001
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all agent output
- Statement: Agents must state assumptions, constraints, and proposed approach before writing code.
- Rationale: Prevents misaligned implementation and wasted effort.
- Verification: First output of any task includes a structured preamble.

### Rule: GLOBAL-002
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all agent output
- Statement: Agent-generated content must include a source marker (e.g., `[agent:documentation-architect]`).
- Rationale: Enables traceability for content created by agents vs. humans.
- Verification: Grep for source markers in agent-produced files.

### Rule: GLOBAL-009
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all implementation tasks
- Statement: Follow Scrum-first execution order: define the work, record backlog items, prioritize them, then implement in priority order. Do not implement first and backfill requirements later.
- Rationale: Front-loading planning reduces rework, missed requirements, and priority inversion.
- Verification: Task output includes a visible pre-implementation backlog and priority order.

## Code quality baseline

### Rule: GLOBAL-003
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all code changes
- Statement: No code change is complete until `make test` and `make lint` pass.
- Rationale: Prevents broken builds and style drift.
- Verification: CI or manual validation after every change.

### Rule: GLOBAL-004
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all code changes
- Statement: Follow existing repository patterns unless the user explicitly requests a refactor.
- Rationale: Consistency reduces cognitive load and merge conflicts.
- Verification: Review diffs for pattern divergence.

## Security baseline

### Rule: GLOBAL-005
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all artifacts
- Statement: Never commit credentials, API keys, or secrets to the repository.
- Rationale: Credential exposure is a critical security risk.
- Verification: `.gitignore` covers sensitive files; pre-commit hook (optional).

### Rule: GLOBAL-006
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all code paths
- Statement: Never execute unvalidated user input as code or SQL.
- Rationale: Prevents injection attacks.
- Verification: Code review and static analysis for parameterized queries.

## Safety constraints

### Rule: GLOBAL-007
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all destructive operations
- Statement: Destructive operations (file deletion, table drops, force push) require explicit user approval.
- Rationale: Irreversible actions must have a human checkpoint.
- Verification: Checkpoint gate in the workflow.

### Rule: GLOBAL-008
- Owner layer: Global
- Stability: core
- Status: active
- Scope: decision tracking
- Statement: All architectural decisions must be recorded in `DECISIONS.md`.
- Rationale: Prevents decision amnesia across sessions and agents.
- Verification: Post-task check for new entries.
