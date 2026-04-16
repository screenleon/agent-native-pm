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
| Frontend | React + Vite (static build) |
| Database | SQLite (Phase 1), PostgreSQL (Phase 4) |
| Deployment | Docker Compose |
| Documentation | Markdown in repo |

## Quick start

```bash
# Clone
git clone <repo-url>
cd agent-native-pm

# Build and run
docker compose up --build

# Open
# http://localhost:18765
```

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
├── backend/          # Go API server (planned — Phase 1 implementation)
│   ├── cmd/          # Entry points
│   ├── internal/     # Core modules
│   │   ├── projects/
│   │   ├── tasks/
│   │   ├── documents/
│   │   ├── sync/
│   │   ├── agent_runs/
│   │   ├── summaries/
│   │   └── drift/
│   └── db/           # SQLite migrations
├── frontend/         # React + Vite (planned — Phase 1 implementation)
│   ├── src/
│   │   ├── pages/
│   │   ├── components/
│   │   └── api/
│   └── dist/         # Built static assets
├── docs/             # Product and agent documentation
├── project/          # Project-local agent constraints
├── rules/            # Layered agent rules
├── .claude/agents/   # Claude subagent definitions
└── .github/          # Copilot instructions
```

## Agent workflow

Start with `AGENTS.md`. It routes you to the correct docs and rules.

For implementation tasks, follow:
1. Discover → 2. Triage → 3. Plan (if Medium/Large) → 4. Implement → 5. Validate → 6. Record decisions

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
