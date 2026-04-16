# Agent Native PM — Makefile

.PHONY: all build test test-local lint dev clean docker-build docker-up docker-down

# Default
all: build

# ── Backend ──────────────────────────────────────────────────────────────────

build-backend:
	cd backend && go build -o ../bin/server ./cmd/server

test:
	./scripts/test-with-postgres.sh

test-local:
	cd backend && go test ./... -v -count=1

test-integration:
	cd backend && go test ./... -v -tags=integration -count=1

lint:
	cd backend && go vet ./...

# ── Frontend ─────────────────────────────────────────────────────────────────

install-frontend:
	cd frontend && npm install

build-frontend: install-frontend
	cd frontend && npm run build

lint-frontend:
	cd frontend && npm run lint

# ── Combined ─────────────────────────────────────────────────────────────────

build: build-backend build-frontend

dev:
	@echo "Starting backend and frontend in development mode..."
	@echo "Backend: http://localhost:18765"
	@echo "Frontend: http://localhost:3000"
	@cd backend && PORT=18765 DATABASE_PATH=./data/agent-native-pm.db FRONTEND_DIR="" go run ./cmd/server &
	@cd frontend && npm run dev

clean:
	rm -rf bin/ backend/data/ frontend/dist/ frontend/node_modules/

# ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	docker compose build

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
