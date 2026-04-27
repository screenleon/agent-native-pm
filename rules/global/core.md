# Global Rules — Agent Native PM

These rules apply universally across all modules and domains.

## Communication norms

### Rule: GLOBAL-001
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all agent output
- Statement: Agents must state assumptions, constraints, and proposed approach before writing code. If a requirement has two reasonable interpretations, present both and ask which to implement — do not silently pick one. For each non-trivial assumption, state what would break or change if the assumption turns out to be wrong.
- Rationale: Prevents misaligned implementation and wasted effort. Silent assumption-picking is the primary cause of expensive rewrites in multi-agent workflows.
- Verification: First output of any task includes a structured preamble listing assumptions and their failure impact. Ambiguous requirements trigger a clarification question before any code is written.

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

### Rule: GLOBAL-010
- Owner layer: Global
- Stability: core
- Status: active
- Scope: all code changes
- Statement: Touch only what the task requires. Do not clean up pre-existing dead code, fix pre-existing style issues, or refactor logic that is not broken, unless cleanup or refactoring is the explicit goal of the task. Clean up only the mess you introduced.
- Rationale: Unrelated changes inflate diff noise, raise review burden, and risk introducing regressions — especially in multi-agent workflows where multiple roles edit the same codebase concurrently.
- Verification: Every changed line in the diff must trace back to the task's stated objective. Lines changed for reasons unrelated to the task are a violation.

### Rule: GLOBAL-011
- Owner layer: Global
- Stability: core
- Status: active
- Scope: bug fix tasks
- Statement: For bug fixes, write a failing reproduction test first, confirm it reproduces the problem, then implement the fix and verify the test passes. Do not write the fix before the test exists.
- Rationale: A test-first approach proves the bug is real, prevents false fixes, and guards against regression. Fixing without a reproduction test leaves the fix unverifiable.
- Verification: Commit history or diff shows a failing test added before the fix. CI must be green after the fix.

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
