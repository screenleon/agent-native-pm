# Project Manifest — Agent Native PM

## Project identity

- Name: Agent Native PM
- Repository type: Application (backend + frontend monorepo)
- Primary language(s): Go (backend), TypeScript (frontend)
- Runtime framework(s): Go stdlib + Chi, React 18, Vite

## Non-negotiable constraints

- PostgreSQL is the active runtime database — do not introduce SQLite-only assumptions
- Single Go binary serves API and static frontend — no separate Node.js runtime
- All API responses use JSON envelope: `{ data, error, meta }`
- Dashboard state must be computed from system data, not manual input
- RAM target: 200-500 MB for the full running application
- Agent-generated content must include a source marker

## Build and validation commands

- Build: `make build`
- Unit tests: `make test`
- Integration tests: `make test-integration`
- Lint/static analysis: `make lint`
- Development server: `make dev`

## Deployment and operations boundaries

- Environments: local Docker Compose, production Docker
- Release process: build Docker image, tag, push
- Incident/rollback rule: redeploy previous image and restore PostgreSQL data from the latest verified backup when needed

## Security and compliance boundaries

- Secret handling: environment variables only — never commit secrets
- Auth/permission model: sessions for human users, API keys for automation routes, project-scoped access controls where applicable
- Data classification: project metadata — no PII in Phase 1-3

## Architecture context

- System style: modular monolith
- Critical integration dependencies: Git CLI, PostgreSQL
- Known technical debt: none (greenfield)

## Override notes

- No ui-image-implementer role — this project does not use screenshot-driven UI workflow
- The template's `skills/` directory is not used — skill behaviors are executed natively

## Override registry

| Base Rule ID | Project Rule ID | Reason | Status |
|---|---|---|---|
| N/A | N/A | Greenfield project — no overrides yet | active |

## Workspace boundaries

| Path glob | Active domain rules | Masked domain rules |
|---|---|---|
| `backend/**` | backend-api, documentation-sync | frontend-components |
| `frontend/**` | frontend-components, documentation-sync | backend-api |
| `docs/**` | documentation-sync | backend-api, frontend-components |

## MCP tool declarations

Not used in this project.
