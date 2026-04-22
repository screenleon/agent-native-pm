# Agent Native PM

A lightweight, agent-aware project tracking system built to keep documentation in sync with code and give you real-time visibility into project state.

## What this is

This is **not** a general-purpose Jira/Plane replacement. It is a focused tool that:

1. Tracks project status from repository state, not manual card-filling
2. Detects when documentation drifts from code changes
3. Lets agents update descriptions, summaries, and task status via API
4. Runs on minimal resources (~200-500MB RAM)

## Tech stack

| Component | Technology |
|-----------|-----------|
| Backend API | Go |
| Frontend | React + Vite (static build, embedded into the Go binary) |
| Runtime DB | SQLite (local mode) · PostgreSQL (server mode) |
| Distribution | Single `anpm` / `server` binary via goreleaser · Docker Compose for server mode |
| Documentation | Markdown in repo |

## Install & run

The project ships two runtime modes that share one schema and one codebase:

- **Local mode** — `anpm serve` inside any git repo. SQLite at `$REPO_ROOT/.anpm/data.db`, no login, port derived from the repo path. Best for single-operator / developer-laptop use.
- **Server mode** — `docker compose up` (or the standalone `server` binary with `DATABASE_URL` set). PostgreSQL, full auth, multi-user, multi-project.

### Local mode (quick start)

From inside any git repo:

```bash
# Option A: run straight from this source tree
cd agent-native-pm
make serve             # auto-builds backend + frontend if stale, then starts

# Option B: use the installed CLI (after goreleaser install or local build)
make build-anpm        # produces ./bin/anpm
cd /path/to/your/project   # any git repo
/path/to/agent-native-pm/bin/anpm serve
```

`anpm serve` auto-detects the git root, creates `.anpm/data.db` there, picks a stable port in `[3100, 3999]` based on the repo path, and opens the SPA at `http://localhost:<port>`. Run `anpm status` from the same repo to check whether a server is already listening, or `anpm version` for the build tag.

Setting `DATABASE_URL` in the environment skips local mode and falls through to server mode.

### Server mode (Docker Compose)

```bash
git clone <repo-url>
cd agent-native-pm
docker compose up --build
# http://localhost:18765
```

Server mode runs PostgreSQL alongside the Go binary and enables the full session-auth / multi-user / multi-project surface. Use this when multiple operators share one deployment or when you need full-text search over Postgres `tsvector`.

### Real-time updates

Notifications and planning-run state changes stream over Server-Sent Events at `GET /api/notifications/stream` (`text/event-stream`, session token via `?token=` since `EventSource` cannot set custom headers). The frontend falls back to 20 s polling + the `anpm:refresh-notifications` window event when `EventSource` is unavailable or the connection drops.

## Central model settings

Planning decomposition can stay fully local with the built-in deterministic provider, or it can call a real external model through one OpenAI-compatible chat completions endpoint. Provider configuration now lives inside the app under the admin-only `Model Settings` page instead of being hard-coded in deployment env vars.

Runtime notes:

- The only env you need for secret storage is `APP_SETTINGS_MASTER_KEY`, a base64-encoded AES key used to encrypt stored provider API keys at rest.
- The remote model only generates candidate content fields such as title, description, and rationale.
- Priority score, confidence, ranking, duplicate checks, and typed evidence detail remain server-owned.
- Starting a planning run no longer accepts a user-selected provider/model override. New runs always use the centrally saved server configuration.
- Selecting the remote provider sends compact planning context metadata to the configured upstream endpoint.
- For private deployments, keep the app behind LAN-only access or a protected reverse proxy so the admin settings surface is not public.

Source: `[agent:documentation-architect]`

## Local connector runtime

The server-side local connector control plane is now backed by a local Go CLI named `anpm-connector`. This is the path to use when you want planning runs to execute on your own machine instead of asking the server to call a remote provider directly.

Build the connector binary:

```bash
make build-connector
```

Current commands:

```bash
./bin/anpm-connector pair --server http://localhost:18765 --code <pairing-code>
./bin/anpm-connector doctor
./bin/anpm-connector serve
```

Adapter flags can be supplied during `pair`, `doctor`, or `serve` and are persisted back into the local connector state file when they change:

```bash
./bin/anpm-connector pair \
	--server http://localhost:18765 \
	--code <pairing-code> \
	--adapter-command /absolute/path/to/adapter \
	--adapter-working-dir /absolute/path/to/worktree
```

The connector persists its local token and adapter configuration to:

- `$ANPM_CONNECTOR_STATE_PATH`, when set
- otherwise `~/.config/agent-native-pm/connector.json`

The state file is written with `0600` permissions.

`exec-json` adapter contract:

- stdin JSON includes `run`, `requirement`, and `requested_max_candidates`
- stdout JSON must return `{"candidates":[...]}` or `{"error_message":"..."}`
- adapter execution is bounded by a timeout and stdout/stderr size limit

Source: `[agent:application-implementer]`

## Repository setup for sync

The sync engine now supports three source models:

1. Mirror mapping mode via project repo mappings under `/mirrors/*`.
2. Managed clone mode via `repo_url`.
3. Direct manual path mode via `repo_path`.

### Recommended for Docker Compose: mirror mappings under `/mirrors`

Mirror mappings are the preferred local workflow because they let sync see unpushed working-tree changes while still keeping the container mount narrow.

The bundled compose file already mounts this workspace as read-only:

```yaml
services:
	app:
		volumes:
			- .:/mirrors/agent-native-pm:ro
```

After the app starts, open the project detail page and add a repo mapping such as:

```text
alias = app
repo_path = /mirrors/agent-native-pm
default_branch = main
is_primary = true
```

For additional repositories, mount each one under `/mirrors/...` and register another mapping. Secondary repositories use alias-prefixed document and link paths.

Example:

```text
alias = docs-repo
repo_path = /mirrors/agent-native-pm-docs
document file_path = docs-repo/docs/guide.md
document link code_path = docs-repo/src/service.ts
```

Repo mappings are validated server-side:

- `repo_path` must be an absolute path under the configured mirror root (default `/mirrors`)
- the target path must be a readable git repository
- aliases must use lowercase letters, numbers, dots, underscores, or hyphens

### Managed clone mode with `repo_url`

Provide a remote repository URL and branch in the project settings.

Example:

```text
repo_url = https://github.com/example/my-service.git
default_branch = main
```

On first sync, the backend will clone the repository into the managed repo cache inside the container. On later sync runs, it will fetch and refresh that local checkout automatically.

Use this when the authoritative source is a remote repository and local working-tree visibility is not required.

### Direct manual path mode with `repo_path`

`repo_path` remains available as a fallback for host-run or custom deployments.

If you run the backend outside Docker, use the host absolute path directly.

Example:

```text
/home/screenleon/github/my-service
```

If you run with Docker Compose, `repo_path` must still be a path that exists inside the `app` container.

### Common failure cases

- `project has no repo_path or repo_url configured`
	- No primary repo mapping, `repo_path`, or `repo_url` is configured.
- `repo_path must stay under the configured mirror root`
	- A repo mapping points outside `/mirrors`.
- `repo_path is not a git repository`
	- The path exists but does not contain a readable git repository inside the runtime environment.
- `git rev-list count failed` or `git log failed`
	- Usually the branch is wrong, or the mounted repo is not accessible from the container.

If you change Docker mounts, rebuild and restart:

```bash
docker compose up --build -d
```

Source: `[agent:documentation-architect]`

## Project structure

```text
agent-native-pm/
├── backend/                    # Go API server
│   ├── cmd/
│   │   ├── server/             # HTTP server entry point
│   │   ├── anpm/               # CLI: serve / status / version
│   │   └── connector/          # Local connector daemon
│   ├── db/                     # SQL migrations (both SQLite + PostgreSQL)
│   └── internal/
│       ├── config/             # Config + workspace auto-detection for local mode
│       ├── database/           # Driver dispatch + migrations
│       ├── events/             # In-process SSE broker
│       ├── frontend/           # //go:embed target for SPA (populated at build)
│       ├── handlers/           # HTTP handlers (projects, tasks, drift, planning, etc.)
│       ├── middleware/         # Session / API-key / InjectLocalAdmin
│       ├── planning/           # Planning orchestrator + wire DTO (context.v1)
│       ├── store/              # Data access (dialect-aware)
│       └── router/             # Chi router wiring
├── frontend/                   # React + Vite SPA
│   ├── src/
│   │   ├── pages/              # Top-level routes (ProjectDetail, ProjectList, Dashboard, …)
│   │   ├── components/         # Tab/panel components extracted from ProjectDetail
│   │   ├── utils/              # formatters, syncGuidance, …
│   │   ├── api/                # API client
│   │   └── test/               # vitest setup
│   └── dist/                   # Built static assets (copied into backend/internal/frontend/dist before go build)
├── adapters/                   # Reference exec-json adapters (backlog_adapter.py)
├── docs/                       # Product / agent / data-model documentation
├── project/                    # Project-local agent manifest
├── rules/                      # Layered agent rules (global, domain)
├── scripts/                    # serve.sh, governance lints, test-with-postgres
├── .claude/agents/             # Claude subagent definitions
├── .goreleaser.yml             # Multi-OS / arch release config
└── Makefile                    # Build / test / lint / governance entry points
```

## Agent workflow

Start with `AGENTS.md`. It routes you to the correct docs and rules.

For implementation tasks, follow:
1. Discover → 2. Triage → 3. Plan (if Medium/Large) → 4. Implement → 5. Validate → 6. Record decisions

## Governance checks

| Command | What it does |
|---|---|
| `make lint-rules` | Rule-ID uniqueness, stability field, supersession chain integrity |
| `make lint-docs` | Prompt-budget wording / doc consistency |
| `make validate-prompt-budget` | Schema check of `prompt-budget.yml` |
| `make budget-report` | Per-layer token estimate vs the targets in `prompt-budget.yml` |
| `make decisions-conflict-check TEXT="..."` | Overlap check against `DECISIONS.md` before a new plan |
| `make lint-governance` | Runs rule-lint + doc-lint + budget validator together |
| `make test-frontend` | vitest suite (unit + component smoke) |
| `make test` | Full Go test suite against PostgreSQL (starts a temp container) |

## Phase roadmap

| Phase | Focus | Key deliverables |
|-------|-------|-----------------|
| 1 | Core CRUD + Dashboard | Projects, tasks, documents, summaries, SQLite, basic dashboard |
| 2 | Repo sync + Drift detection | Git scan, drift signals, agent activity log |
| 3 | Agent integration | Agent update API, doc refresh, rule-based doc sync |
| 4 | Collaboration features | Auth, search, notifications, PostgreSQL migration |

See `docs/product-blueprint.md` for the full roadmap.

## License

TBD
