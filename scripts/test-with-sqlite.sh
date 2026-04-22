#!/usr/bin/env bash
# Run the Go test suite against SQLite (no Docker, no PostgreSQL required).
#
# This mirrors `scripts/test-with-postgres.sh` but for the local-mode driver.
# The 2026-04-22 Dual-runtime-mode decision requires the suite to pass under
# both drivers. This script is the zero-infrastructure path — CI and dev
# machines can run it without pulling a container image.
#
# Usage:
#   bash scripts/test-with-sqlite.sh [go test args...]
#
# The TEST_DATABASE_URL is set to a sentinel value that testutil.OpenTestDB
# recognises as the SQLite dispatch path; each test file receives its own
# disposable SQLite DB under t.TempDir().

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[test-sqlite] running go test with SQLite driver"
cd "$ROOT_DIR/backend"

# testutil.OpenTestDB checks database.IsSQLiteDSN; "sqlite" is also accepted
# as a shorthand sentinel so callers do not need to invent a path.
export TEST_DATABASE_URL="sqlite"

# Unset DATABASE_URL so no leaked connection string routes tests at Postgres.
unset DATABASE_URL || true

go test "${@:-./...}" -count=1
