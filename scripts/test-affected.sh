#!/usr/bin/env bash
# Run Go tests only for packages affected by current git changes.
#
# Strategy:
#   1. git diff → changed .go files → their import paths
#   2. go list → find every module-local package whose Deps include a changed pkg
#   3. Run go test for that union
#   4. Fall back to full suite if ≥ 2/3 of all packages are in the affected set
#      (avoids the overhead of a long package list when almost everything changed)
#
# Used by: make test-affected, pre-pr-check.sh --fast
# Respects TEST_DATABASE_URL; defaults to sqlite for zero-infra local runs.
#
# Usage:
#   bash scripts/test-affected.sh            # default: compare against HEAD
#   bash scripts/test-affected.sh -v         # pass extra flags to go test

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR/backend"

export TEST_DATABASE_URL="${TEST_DATABASE_URL:-sqlite}"

# ── 1. Collect changed .go files ─────────────────────────────────────────────
# Include both staged and unstaged changes relative to HEAD.
changed=$(
  { git diff --name-only HEAD 2>/dev/null; git diff --name-only --cached 2>/dev/null; } \
  | grep '\.go$' | sort -u || true
)

if [ -z "$changed" ]; then
  echo "[test-affected] no .go files changed — nothing to test"
  exit 0
fi

module=$(go list -m)

# ── 2. Map changed files → module import paths ───────────────────────────────
# git paths are repo-relative; strip the leading "backend/" prefix when present.
changed_pkgs=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  f="${f#backend/}"         # strip "backend/" if path came from repo root
  [ -f "$f" ] || continue  # skip files deleted or outside backend/
  dir=$(dirname "$f")
  changed_pkgs="$changed_pkgs
$module/$dir"
done <<< "$changed"

changed_pkgs=$(printf '%s\n' $changed_pkgs | sort -u | grep -v '^$' || true)

if [ -z "$changed_pkgs" ]; then
  echo "[test-affected] changed .go files are not under backend/ — nothing to test"
  exit 0
fi

echo "[test-affected] directly changed packages:"
echo "$changed_pkgs" | sed 's/^/  /'

# ── 3. Reverse-dependency expansion ──────────────────────────────────────────
# For each package P in the module, check whether P's transitive Deps contains
# any changed package. If yes, P must be retested.
to_test="$changed_pkgs"

while IFS= read -r line; do
  pkg="${line%%:*}"
  deps=" ${line#*: } "        # pad with spaces for whole-word matching
  for cp in $changed_pkgs; do
    case "$deps" in
      *" $cp "*)
        to_test="$to_test
$pkg"
        ;;
    esac
  done
done < <(go list -f '{{.ImportPath}}: {{join .Deps " "}}' ./... 2>/dev/null)

to_test=$(printf '%s\n' $to_test | sort -u | grep -v '^$')

# ── 4. Fallback: run full suite when most packages are affected ───────────────
all_count=$(go list ./... 2>/dev/null | wc -l | tr -d '[:space:]')
affected_count=$(printf '%s\n' $to_test | wc -l | tr -d '[:space:]')
threshold=$(( all_count * 2 / 3 ))

if [ "$affected_count" -ge "$threshold" ] || [ "$all_count" -le 5 ]; then
  echo "[test-affected] $affected_count/$all_count packages affected — running full suite"
  go test ./... -count=1 "$@"
  exit $?
fi

echo "[test-affected] testing $affected_count/$all_count affected packages"

# ── 5. Build argument list and run ───────────────────────────────────────────
test_args=""
while IFS= read -r imp; do
  [ -z "$imp" ] && continue
  local_path="${imp#"$module/"}"
  test_args="$test_args ./$local_path"
done <<< "$to_test"

# shellcheck disable=SC2086
go test $test_args -count=1 "$@"
