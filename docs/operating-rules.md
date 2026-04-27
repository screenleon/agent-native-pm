# Operating Rules — Agent Native PM

This file is the source of truth for safety, scope control, validation, and project-specific constraints.

## Agent-deference principle

Prefer the agent's built-in behavior first. Only apply these rules when the agent tool does not already cover the capability.

This project adds:
- Project-specific decision log (`DECISIONS.md`) and contradiction checks
- Role routing for the Agent Native PM domain
- Documentation drift awareness
- Repository-specific conventions

## Trust level

Default: `semi-auto`. Override per session.

| Level | Description |
|-------|-------------|
| `supervised` | All checkpoints require human approval |
| `semi-auto` | Small/low-risk tasks run autonomously; checkpoints for Large/destructive work |
| `autonomous` | Proceeds without approval except destructive actions |

## Constitutional principles (non-bypassable)

1. Never expose credentials in any artifact
2. Never execute unvalidated input as code
3. Never modify production data without backup verification
4. Never disable authentication or authorization
5. Never suppress security test failures

Violation = hard stop regardless of execution mode.

## Safety rails

- Never perform destructive actions without explicit user approval (unless `dangerouslySkipAllCheckpoints: true`)
- Prefer minimum required permissions, scope, and file changes
- Treat branch protections and deployment safeguards as hard constraints

## Scope control

- Do not expand the task beyond the requested outcome without stating why
- If a task is ambiguous, reduce ambiguity through planning first. If two reasonable interpretations exist, present both and ask — do not silently pick one. State what would break if an assumption is wrong.
- Keep fixes local unless broader change is necessary for correctness
- Touch only what the task requires. Do not clean up pre-existing dead code, style issues, or unbroken logic unless cleanup is the explicit goal (→ GLOBAL-010)
- Apply Scrum-first order: backlog definition and prioritization must happen before implementation
- Do not treat post-implementation requirement backfill as an acceptable workflow

## Documentation maintenance

- Keep normative rule text in a canonical owner document instead of copy-expanding the same rule across many files.
- When a rule changes, sync only the surfaces that explicitly expose that rule, command, workflow term, or file path.
- `.claude/agents/` and `.github/copilot-instructions.md` are tool-specific implementations; they must stay aligned with `docs/operating-rules.md` and `docs/agent-playbook.md`.

## Always-dangerous operations (require approval)

- Deleting files or directories
- Dropping database tables or destructive migrations
- `git push --force`, `git reset --hard`, amending published commits
- Modifying CI/CD pipelines, deployment configs, or shared infrastructure
- Publishing packages, creating releases, or pushing to main/production

## Validation requirements

After every code change:

1. Run `make test` (or targeted test for Small tasks)
2. Run `make lint`
3. Fix failures before marking complete
4. Never skip or delete failing tests to make the suite pass
5. **Bug fixes**: write a failing reproduction test first, confirm it reproduces the issue, then fix and verify the test passes. Do not write the fix before the test exists (→ GLOBAL-011)

## Pre-PR verification (mandatory before `gh pr create`)

Implementation PRs (anything that changes code, not docs-only) must pass all three phases **in order**. Skipping a phase or opening a PR on a red pipeline is a workflow violation. Documentation-only and design-only PRs may skip Phase 1 and Phase 3 but still require Phase 2 governance lints.

### Phase 1 — Local verification pipeline

Run `make pre-pr` (or `bash scripts/pre-pr-check.sh`) before `gh pr create`. The script is the single authoritative list of checks; it must exit 0. Stages, in order:

1. `make lint-governance` — layered rule structure, docs consistency, prompt-budget schema
2. `make lint` — `go vet` and frontend `eslint`
3. `go build ./...` in `backend/`
4. `npx tsc --noEmit` in `frontend/`
5. `npm test -- --run` in `frontend/`
6. `bash scripts/test-with-sqlite.sh` — full Go suite against the local-mode driver
7. `bash scripts/test-with-postgres.sh` — full Go suite against PostgreSQL (skippable with `--skip-postgres` only when Docker is not installed locally; CI always runs it)
8. `npm run build` in `frontend/` — catches bundler-only regressions

`make pre-pr-fast` skips stages 7 and 8 for quicker iteration during implementation. It is NOT a substitute for `make pre-pr` before opening a PR.

Adding, removing, or reordering a stage above without updating `scripts/pre-pr-check.sh` (or vice-versa) is a drift signal — both surfaces must move together.

### Phase 2 — Pre-PR critic review (subagent)

After Phase 1 is green, spawn a `critic` subagent against the branch diff. The critic must specifically check:

1. **Incomplete call site coverage** — grep for every usage of any changed pattern, field, or method. All call sites must be updated, not just the files explicitly listed in the implementation brief.
2. **Pattern consistency** — any new method or helper must fully replicate the established pattern in the codebase (e.g., two-pass envelope decode, error handling style, nil guard order). "Similar to X" in a brief is not sufficient; the critic must verify the implementation matches X line-for-line on the critical invariants.
3. **Missing edge cases** — scenarios the brief did not cover but the production code path must handle.

Fix all critic findings before moving to Phase 3. If a finding must be deferred (e.g., scope creep), document it explicitly in the PR description as a known follow-up.

### Phase 3 — Risk + security coverage

Depending on the touched surface, also run:

- `/security-review` skill — mandatory when the diff touches authentication, authorization, subprocess execution, connector protocol, user-uploaded content, or any new HTTP endpoint.
- `risk-reviewer` subagent — mandatory when the diff introduces new state machines, cross-tenant boundaries, schema changes, or declares DoD test matrices the critic did not verify.

Fix must-fix findings before `gh pr create`. Should-fix and nits may ship in the same PR or be explicitly deferred in the PR description with a follow-up reference.

### Exemptions

- Docs-only changes to `docs/`, `DECISIONS.md`, `ARCHITECTURE.md`, `README*.md`, or `*.md` under `rules/` skip Phase 1 stages 2-8 and Phase 3. They still must pass Phase 1 stage 1 (`make lint-governance`).
- Design-only changes (new plan documents, ADR drafts) also skip Phase 2 critic review.
- Emergency hotfixes may waive Phase 3 with explicit owner approval noted in the PR description.

## Error recovery

1. Read the full error message
2. Identify root cause (specific file and line)
3. Fix only what is needed
4. Re-run validation
5. Escalate after 3 failed attempts

## Layered configuration

1. **Global Rules** (`rules/global/`) — universal guardrails
2. **Domain Rules** (`rules/domain/`) — backend-api, frontend-components, documentation-sync
3. **Project Context** (`project/project-manifest.md`) — repo-local boundaries

Precedence: Project Context > Domain Rules > Global Rules.

## Project-specific constraints

### Language and framework

- Backend: Go (latest stable)
- Frontend: React 18+ with TypeScript, built with Vite
- Database: PostgreSQL is the active runtime database
- No server-side rendering

### Code conventions

- Go: follow `gofmt` and `golangci-lint` defaults
- TypeScript: follow ESLint + Prettier defaults
- SQL: use migrations (numbered, forward-only in Phase 1)
- API: RESTful JSON with consistent envelope (`{ data, error, meta }`)

### Documentation sync rule

Every behavior change that affects:
- API endpoints or request/response shapes
- Data model (new/modified tables or columns)
- Dashboard state computation
- Agent interaction patterns

...must either:
1. Update the relevant doc (`docs/api-surface.md`, `docs/data-model.md`, `ARCHITECTURE.md`), OR
2. Create a drift signal entry explaining what changed and what doc needs updating

### Testing expectations

- Unit tests for all business logic in Go
- API integration tests for endpoint contracts
- Frontend: component tests for critical UI paths
- No e2e browser tests required in Phase 1

### Build and validation commands

```bash
make build         # Build Go binary + frontend assets
make test          # Run all Go unit tests
make test-integration  # Run API integration tests
make lint          # Run backend go vet and frontend lint
cd frontend && npm run build  # Validate frontend production build
make dev           # Start development server with hot reload
```

---

## Role-dispatch safety + visibility model

Added in Phase 6c. Governs how roles are assigned, validated, and monitored during task dispatch.

### L0 — subprocess safety boundary (Phase 6c PR-1)

Applied inside `invokeBuiltinCLI` for every CLI invocation:

| Constraint | Default | Override env var |
|---|---|---|
| Wall-clock timeout | per-role `DefaultTimeoutSec` | `ANPM_DISPATCH_TIMEOUT` (0 = disabled) |
| Output cap | 5 MB | `ANPM_DISPATCH_MAX_OUTPUT_BYTES` (0 = disabled) |
| Result schema validation | strict | not configurable |
| SIGTERM → SIGKILL escalation | 5s | not configurable |

**L0 is unconditional** — it applies to all CLI invocations regardless of role or connector. Operators can increase limits via env vars but cannot disable them in production without modifying source.

**L1 (process-level jail)** and **L2 (container/VM isolation)** are deferred to Phase 6d / Phase 7.

### L1 trigger conditions (Phase 6d)

L1 activates when any of the following occur:
- A connector executes external code from an untrusted source (non-catalog adapter).
- Multi-tenancy is introduced (multiple users sharing a server).
- A Phase 6d dogfood run shows L0 is being consistently bypassed.

### L2 trigger conditions (Phase 7)

L2 activates when:
- The system accepts tasks from untrusted external repositories.
- Compliance or legal requirements mandate isolation.

### Catalog enforcement points

`roles.IsKnown(roleID)` is checked at four points:

1. **PATCH /api/backlog-candidates/:id** — when `execution_role` is set via the editor.
2. **POST /api/backlog-candidates/:id/apply** — when mode=role_dispatch; missing role → 400.
3. **POST /api/connector/claim-next-task** — when the task source includes a role suffix; stale role → `MarkTaskRoleNotFound` → `dispatch_status=failed`.
4. **`invokeBuiltinCLI`** — checks `prompts.Exists(roleID)` before spawning subprocess.

Empty role suffix (`role_dispatch:` without a role id) is treated as `error_kind=role_dispatch_malformed` at points 3 and 4.

### Actor audit (Phase 6c PR-2)

Every `execution_role` change is recorded in `actor_audit` with:
- `actor_kind`: `user` (session), `api_key` (automation), `system` (claim-time enforcement), `connector` (connector-reported).
- `actor_kind=router` is reserved for Phase 6d auto-apply; no code writes it in Phase 6c.
- Audit rows are append-only; no cascade-delete with the subject row.

### Activity SSE constraints (Phase 6c PR-4)

| Constraint | Value | Notes |
|---|---|---|
| Heartbeat interval | 30s | SSE keepalive comment |
| Stale threshold | 90s | 3× heartbeat; frontend dims badge |
| Polling fallback interval | 15s | kicks in when SSE fails |
| Coalesce window | 500ms | same-phase step changes merged by connector |
| Phase changes | always enqueued | phase changes never coalesced |
| Concurrent SSE per user | ≤ 3 (planned) | deferred to Phase 6d rate limiting |
| Activity history | latest snapshot only | full history deferred to Phase 6d |

**Activity is operational telemetry**, not an authoring lifecycle event. It is never written to `actor_audit`. The server persists only the latest snapshot per connector to `local_connectors.current_activity_json`.

### Advisory router constraints (Phase 6c PR-3)

- `POST /api/backlog-candidates/:id/suggest-role` is advisory-only; it never persists a result.
- The operator must explicitly confirm the suggestion before it is saved (UI-008).
- Router errors (`dispatch_timeout`, `output_too_large`, `invalid_result_schema`) surface in the suggest response body, not as 4xx/5xx HTTP errors (API-008).
- `router_no_match` is a valid outcome when the dispatcher prompt returns `role_id = "no_match"`.
- Auto-apply mode (`role_dispatch_auto`) is deferred to Phase 6d.
