#!/usr/bin/env bash
# Mandatory pre-PR verification pipeline.
#
# Run this before `gh pr create` (or `git push` on a branch intended for a
# PR). It reproduces locally what CI runs so a green pipeline here strongly
# predicts a green CI check. The ordering is cheapest-first so the common
# "lint catches it" case fails fast.
#
# Usage:
#   bash scripts/pre-pr-check.sh
#   bash scripts/pre-pr-check.sh --skip-postgres   # skip the slow PG path
#   bash scripts/pre-pr-check.sh --fast            # skip PG + skip npm build
#
# Exit status:
#   0  every stage passed — safe to open a PR
#   >0 first stage to fail (stderr carries the raw tool output)
#
# This script is the REFEREED surface: every stage it runs is also enforced
# by docs/operating-rules.md § "Pre-PR verification". Adding a new stage
# here without updating the rules doc (and vice-versa) is a drift signal.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SKIP_POSTGRES="false"
FAST="false"
for arg in "$@"; do
  case "$arg" in
    --skip-postgres) SKIP_POSTGRES="true" ;;
    --fast)          SKIP_POSTGRES="true"; FAST="true" ;;
    -h|--help)
      sed -n '1,20p' "$0" >&2
      exit 0
      ;;
    *)
      echo "[pre-pr] unknown flag: $arg" >&2
      exit 2
      ;;
  esac
done

step() {
  printf '\n\033[1;36m[pre-pr] %s\033[0m\n' "$1"
}

# 1. Governance lints (rule layering, docs consistency, prompt budget).
step "governance lints"
make lint-governance

# 2. Go vet + frontend eslint.
step "make lint (go vet + eslint)"
make lint

# 3. Go build — every target must compile.
step "go build ./..."
( cd backend && go build ./... )

# 4. Frontend typecheck — tsc --noEmit catches type regressions not visible to eslint.
step "frontend typecheck (tsc --noEmit)"
( cd frontend && npx tsc --noEmit )

# 5. Frontend unit tests — vitest.
step "frontend tests (npm test -- --run)"
( cd frontend && npm test -- --run )

# 6. Backend tests against SQLite (the local-mode driver).
step "backend tests (SQLite driver)"
bash scripts/test-with-sqlite.sh

# 7. Backend tests against PostgreSQL (the server-mode driver). Skipped
#    when --skip-postgres / --fast is passed or when DOCKER is unavailable.
if [ "$SKIP_POSTGRES" = "true" ]; then
  step "backend tests (PostgreSQL driver) — SKIPPED via flag"
else
  if ! command -v docker >/dev/null 2>&1; then
    step "backend tests (PostgreSQL driver) — SKIPPED (docker not on PATH; pass --skip-postgres to silence this)"
  else
    step "backend tests (PostgreSQL driver)"
    bash scripts/test-with-postgres.sh
  fi
fi

# 8. Frontend production build — catches things only visible in the bundled output.
if [ "$FAST" = "true" ]; then
  step "frontend production build — SKIPPED via --fast"
else
  step "frontend production build"
  ( cd frontend && npm run build )
fi

printf '\n\033[1;32m[pre-pr] all stages passed\033[0m\n'
printf '[pre-pr] safe to open a PR. Reminder: spawn the `critic` subagent and /security-review skill too (docs/operating-rules.md § Pre-PR verification).\n'
