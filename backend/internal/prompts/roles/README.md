---
title: "Execution Role Catalog"
category: role
version: 1
use_case: "Index for the execution role library used by Tier C auto-dispatch."
---

# Execution role catalog

Each markdown file in this directory is a **role prompt** — a specialised agent persona for one class of execution work. The role id equals the filename stem (`backend-architect.md` → `backend-architect`).

**Status today**: these prompts are the library. They are not invoked by any planner or dispatcher in this PR. Phase 6 (Tier C) will introduce the execution pipeline that consumes them.

## Roles shipped in Phase 5

| Role id | What it does | Typical input | Typical output |
|---|---|---|---|
| `backend-architect` | Scaffolds new backend services (HTTP API, business logic, persistence layer) | Task description + project stack | Fenced code block(s) with full source files |
| `ui-scaffolder` | Scaffolds frontend components or whole pages | Task description + design constraints | Fenced code block with React/Vue/Svelte source |
| `db-schema-designer` | Proposes DB schema — tables, columns, indexes, migrations | Requirement + existing schema | SQL migration + ERD description |
| `api-contract-writer` | Writes OpenAPI / JSON API contracts before implementation | Feature description + auth model | OpenAPI YAML + example requests |
| `test-writer` | Writes unit/integration tests for a given code surface | Source file + test framework | Test file source |
| `code-reviewer` | Pre-merge review against a checklist (correctness, security, style) | Diff + context | Structured review comments |

## Shared contract

Every role prompt:
1. Opens with a crisp `## Role` identity (who the agent is).
2. Declares `## Inputs needed` so the caller knows what to prepare.
3. Specifies `## Output format` unambiguously — the dispatcher parses this, so drift here breaks Tier C.
4. Enumerates `## Constraints` (guardrails specific to the role).

Template variables available in all roles:

- `{{TASK_TITLE}}` — the concrete unit of work being handed off
- `{{TASK_DESCRIPTION}}` — longer form of the task
- `{{PROJECT_CONTEXT}}` — repo topology, existing conventions, stack hints
- `{{REQUIREMENT}}` — the owning requirement's summary, for upstream traceability

Not every role uses every variable. The renderer leaves un-supplied variables as literal `{{VAR}}` in the output so a mismatch is visible at review time rather than silent.

## Adding a role

1. New file under this directory. Filename stem = the role id.
2. Follow the shared contract above.
3. Frontmatter must include `title`, `category: role`, `role_id: <id>`, `version: 1`.
4. Update the table in this README.
5. If the role is referenced from a new `execution_role` value, note that in `DECISIONS.md` (Tier C will also add catalog enforcement).
