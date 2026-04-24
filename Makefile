# Agent Native PM — Makefile

.PHONY: all build build-backend build-anpm build-connector test test-local lint dev serve clean release docker-build docker-up docker-down lint-governance lint-rules lint-docs budget-report validate-prompt-budget decisions-conflict-check test-frontend pre-pr pre-pr-fast

# Default
all: build

# ── Backend ──────────────────────────────────────────────────────────────────

build-backend:
	cd backend && go build -o ../bin/server ./cmd/server
	cd backend && go build -o ../bin/anpm-connector ./cmd/connector

build-anpm:
	cd backend && go build -o ../bin/anpm ./cmd/anpm

build-connector:
	cd backend && go build -o ../bin/anpm-connector ./cmd/connector

test:
	./scripts/test-with-postgres.sh

test-local:
	cd backend && go test ./... -v -count=1

test-integration:
	cd backend && go test ./... -v -tags=integration -count=1

lint:
	cd backend && go vet ./...
	$(MAKE) lint-frontend

# Mandatory pre-PR verification. `make pre-pr` runs the full pipeline that
# CI runs. `make pre-pr-fast` skips the PostgreSQL suite and the npm build
# for quicker iteration — NOT a substitute before opening a PR.
pre-pr:
	bash scripts/pre-pr-check.sh

pre-pr-fast:
	bash scripts/pre-pr-check.sh --fast

lint-backend:
	cd backend && go vet ./...

# ── Frontend ─────────────────────────────────────────────────────────────────

install-frontend:
	cd frontend && npm install

build-frontend: install-frontend
	cd frontend && npm run build

lint-frontend:
	cd frontend && npm run lint

test-frontend:
	cd frontend && npm test

# ── Combined ─────────────────────────────────────────────────────────────────

build: build-backend build-anpm build-frontend

release:
	goreleaser release --clean

# ── Local mode (no Docker / Postgres needed) ─────────────────────────────────

# Run from inside any git repo:
#   make -C /path/to/agent-native-pm serve
# The script auto-builds backend + frontend if stale, then starts the server.
serve:
	@./scripts/serve.sh

dev:
	@echo "Starting backend and frontend in development mode..."
	@echo "Backend:  http://localhost:18765"
	@echo "Frontend: http://localhost:5173"
	@cd backend && PORT=18765 DATABASE_URL=postgres://anpm:anpm@localhost:5432/anpm?sslmode=disable FRONTEND_DIR="" go run ./cmd/server &
	@cd frontend && npm run dev

clean:
	rm -rf bin/ backend/data/ frontend/dist/ frontend/node_modules/
	rm -rf backend/bin/ backend/anpm backend/server

# ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	docker compose build

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

# ── Governance (rules, docs, prompt budget) ──────────────────────────────────

lint-rules:
	@bash scripts/lint-layered-rules.sh

lint-docs:
	@bash scripts/lint-doc-consistency.sh

validate-prompt-budget:
	@python3 scripts/validate-prompt-budget.py

budget-report:
	@bash scripts/budget-report.sh --warn-only

# Usage: make decisions-conflict-check TEXT="switch auth to cookies"
decisions-conflict-check:
	@python3 scripts/decisions-conflict-check.py --text "$(TEXT)"

lint-governance: lint-rules lint-docs validate-prompt-budget
