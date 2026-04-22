#!/usr/bin/env bash
# anpm serve — build (if needed) and start the server in local mode.
#
# Run from inside any git repository:
#   /path/to/agent-native-pm/scripts/serve.sh
#
# The server will create .anpm/data.db in the git root, derive a
# stable port from the repo path, and open on that port.

set -euo pipefail

ANPM_HOME="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ANPM_HOME/bin/server"
FRONTEND_DIST="$ANPM_HOME/frontend/dist"

# ── helpers ───────────────────────────────────────────────────────────────────

log()  { printf '\033[0;34m[anpm]\033[0m %s\n' "$*"; }
ok()   { printf '\033[0;32m[anpm]\033[0m %s\n' "$*"; }
warn() { printf '\033[0;33m[anpm]\033[0m %s\n' "$*" >&2; }
err()  { printf '\033[0;31m[anpm error]\033[0m %s\n' "$*" >&2; }

EMBED_DST="$ANPM_HOME/backend/internal/frontend/dist"

# backend_is_stale returns true (0) if $BIN is missing OR any input that gets
# baked into it is newer than $BIN. Inputs are Go sources and the embedded
# dist directory (backend/internal/frontend/dist/**, declared via //go:embed
# in backend/internal/frontend/frontend.go). When either changes, go build
# must re-run so the new bundle is compiled in.
backend_is_stale() {
  [ ! -f "$BIN" ] && return 0
  find "$ANPM_HOME/backend" \
    \( -name '*.go' -o -path "$EMBED_DST/*" \) \
    -newer "$BIN" -print -quit 2>/dev/null | grep -q .
}

newer_frontend_file() {
  [ ! -f "$FRONTEND_DIST/index.html" ] && return 0
  find "$ANPM_HOME/frontend/src" \( -name '*.tsx' -o -name '*.ts' -o -name '*.css' \) \
    -newer "$FRONTEND_DIST/index.html" -print -quit 2>/dev/null | grep -q .
}

# ── pre-flight checks ─────────────────────────────────────────────────────────

if ! command -v go >/dev/null 2>&1; then
  err "go is not installed — https://go.dev/dl/"
  exit 1
fi

# Warn if DATABASE_URL is set; it will override local mode.
if [ -n "${DATABASE_URL:-}" ]; then
  warn "DATABASE_URL is set in environment — local mode will NOT activate."
  warn "Unset it to use local SQLite mode: unset DATABASE_URL"
fi

# Check we're inside a git repo.
if ! git -C "$(pwd)" rev-parse --git-dir >/dev/null 2>&1; then
  err "Not inside a git repository. cd into your project first."
  exit 1
fi

# ── build frontend ────────────────────────────────────────────────────────────
# Frontend must run BEFORE the backend build because its output is embedded
# into the Go binary via //go:embed. Staging updates the embed dir's mtime,
# and backend_is_stale below will pick that up.

if newer_frontend_file; then
  if ! command -v node >/dev/null 2>&1; then
    err "node is not installed — needed to build the frontend: https://nodejs.org"
    exit 1
  fi
  log "building frontend..."
  (cd "$ANPM_HOME/frontend" && npm install --silent && npm run build --silent)
  ok "frontend ready"
fi

# Copy dist into the Go embed directory so it gets compiled into the binary.
# The copy is skipped when the embed dir is already up-to-date (embed
# index.html is not older than the source index.html).
if [ -f "$FRONTEND_DIST/index.html" ]; then
  if [ ! -f "$EMBED_DST/index.html" ] || [ "$FRONTEND_DIST/index.html" -nt "$EMBED_DST/index.html" ]; then
    log "staging frontend for embedding..."
    rm -rf "$EMBED_DST"
    cp -r "$FRONTEND_DIST" "$EMBED_DST"
  fi
fi

# ── build backend ─────────────────────────────────────────────────────────────
# Runs AFTER frontend staging so any new dist content is baked into $BIN.

if backend_is_stale; then
  log "building backend..."
  mkdir -p "$ANPM_HOME/bin"
  (cd "$ANPM_HOME/backend" && go build -o "$BIN" ./cmd/server)
  ok "backend ready"
fi

# ── start ─────────────────────────────────────────────────────────────────────

# FRONTEND_DIR is kept as a fallback for dev builds that skipped embedding.
export FRONTEND_DIR="$FRONTEND_DIST"

ok "starting server from: $(pwd)"
log "the server will print its port below — open http://localhost:<port> in your browser"
echo ""

exec "$BIN"
