# Domain Rules

Domain-specific constraints used to adapt cross-cutting guardrails to a technical area within this project.

Current domains:

- `backend-api.md` — Go HTTP handler and data-access rules
- `frontend-components.md` — React + Vite component conventions
- `documentation-sync.md` — rules for keeping docs aligned with code

When Domain Rules conflict with Project Context (`project/project-manifest.md`), Project Context wins.

## Recommended rule schema

Each rule entry should include:

- Rule ID
- Owner layer (`Domain`)
- Domain (e.g. `backend-api`)
- Stability (`core`, `behavior`, or `experimental`)
- Status (`active` or `superseded`)
- Scope
- Statement
- Rationale
- Verification
- Supersedes / Superseded by

See `scripts/lint-layered-rules.sh` for the automated check that enforces rule ID uniqueness, stability presence, and supersession chain integrity.
