# `backend/internal/prompts/`

Canonical home for agent prompts. Both the Go connector (via `//go:embed`) and the Python reference adapters (via relative path at `adapters/*.py`) render from the `.md` files here.

## Why this location?

Go's `//go:embed` cannot reach files outside the containing package directory. The connector ships as a single binary; its prompts must be baked in at build time. So the markdown has to live next to a Go source file that embeds it.

Repo-root `prompts/` would be friendlier visually but would require either a build-time copy script or a separate Go module for embed — more moving parts than it's worth. Phase 5 accepted this tradeoff; see `docs/phase5-plan.md` §3 A1.

## Layout

```
backend/internal/prompts/
├── README.md                     # this file
├── embed.go                      # (reserved — currently the //go:embed
│                                 #   directive lives in render.go)
├── render.go                     # Render, Load, Exists, ListRoles
├── render_test.go
├── backlog.md                    # planner: decompose requirement → candidates
├── whatsnext.md                  # strategic advisor: identify directions
└── roles/
    ├── README.md                 # role catalog
    ├── backend-architect.md      # execution specialist — backend scaffolding
    ├── ui-scaffolder.md          # execution specialist — UI scaffolding
    ├── db-schema-designer.md     # execution specialist — DB schema
    ├── api-contract-writer.md    # execution specialist — API/OpenAPI design
    ├── test-writer.md            # execution specialist — test generation
    └── code-reviewer.md          # execution specialist — pre-merge review
```

## Template syntax

One marker only: **`{{VAR_NAME}}`** (double brace, uppercase + digits + underscore). Single-pass regex substitution. Unknown variables are left as-is so a mismatch between the prompt and its caller is visible rather than silent.

JSON schema examples inside the prompt body use **single braces** (`{`, `}`). That is unambiguous against `{{VAR}}` so the examples render verbatim.

### Variable contract

| Prompt | Variables it consumes |
|---|---|
| `backlog` | `{{PROJECT_NAME}}`, `{{PROJECT_DESCRIPTION_LINE}}`, `{{REQUIREMENT}}`, `{{MAX_CANDIDATES}}`, `{{CONTEXT}}`, `{{SCHEMA_VERSION}}` |
| `whatsnext` | `{{PROJECT_NAME}}`, `{{PROJECT_DESCRIPTION_LINE}}`, `{{SCOPE_SECTION}}`, `{{MAX_CANDIDATES}}`, `{{CONTEXT}}`, `{{SCHEMA_VERSION}}` (plus `{{REQUIREMENT}}`, currently required by the loader contract but not substituted by the template body — pass an empty string to future-proof the adapter) |
| `roles/*` | `{{TASK_TITLE}}`, `{{TASK_DESCRIPTION}}`, `{{PROJECT_CONTEXT}}`, `{{REQUIREMENT}}` (not every role uses every variable) |

Callers pre-compute conditional content into a single variable. For example, `PROJECT_DESCRIPTION_LINE` is either `"Description: <text>"` or the empty string — the prompt body stays branch-free.

## Frontmatter

YAML-ish frontmatter at the top of each file (`---` delimited). The renderer strips it before substitution; only the body is sent to the model. Frontmatter carries metadata for humans and future tooling (A/B tests, role catalog introspection) and is intentionally compatible with the [GPT-Prompt-Hub](https://github.com/LichAmnesia/GPT-Prompt-Hub) format so we can cross-pollinate prompts later.

Required keys:
- `title` — short human-readable name
- `category` — one of `planning`, `advisory`, `role`
- `version` — integer, bumped on any prompt wording change

Optional keys:
- `tags` — array for search/filter
- `role_id` — only for files under `roles/`; equals the filename stem
- `model` — model hint (`any`, `claude`, `codex`, `gpt-4`, …) — advisory only
- `use_case` — one-sentence description

## Editing a prompt

1. Edit the `.md` file.
2. Bump the `version` in the frontmatter.
3. Update the byte-identical fixture test if the rendered output changes intentionally.
4. Run `make pre-pr` — the prompt-render + cross-language parity tests catch drift between the Go and Python sides.

## Python side

`adapters/_prompt_loader.py` is the shared loader. It resolves the canonical path as `<repo-root>/backend/internal/prompts/<name>.md` (relative to the adapter's `__file__`), with an `ANPM_PROMPTS_DIR` env override for CI.

Both languages implement the same frontmatter-strip + `{{VAR}}` substitution. A cross-language parity test pins Go-rendered == Python-rendered for a fixed fixture.
