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
- If a task is ambiguous, reduce ambiguity through planning first
- Keep fixes local unless broader change is necessary for correctness
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
