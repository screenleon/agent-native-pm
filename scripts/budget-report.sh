#!/usr/bin/env bash
# scripts/budget-report.sh
#
# Estimates the token cost of each instruction layer so you can compare
# against the targets in prompt-budget.yml and catch budget drift before
# it silently inflates agent context.
#
# Estimation method: word count * 1.35 (approximates GPT/Claude tokenisation
# for mixed prose+code content; accurate within ±15% for English text).
# Override the multiplier with BUDGET_TOKEN_MULTIPLIER env var.
#
# Usage:
#   bash scripts/budget-report.sh
#   bash scripts/budget-report.sh --warn-only    # exit 0 even if over budget
#   bash scripts/budget-report.sh --json         # emit JSON instead of text
#
# Exit codes:
#   0 — all layers within target
#   1 — one or more layers exceed their target (unless --warn-only)
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTIPLIER="${BUDGET_TOKEN_MULTIPLIER:-1.35}"
WARN_ONLY=0
JSON_OUTPUT=0
EXIT_CODE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --warn-only) WARN_ONLY=1; shift ;;
    --json) JSON_OUTPUT=1; shift ;;
    *) echo "[budget-report] unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ── Read targets from prompt-budget.yml ──────────────────────────────────────

read_target() {
  local key="$1"
  local default="$2"
  grep -E "^\s+${key}:" "$ROOT_DIR/prompt-budget.yml" 2>/dev/null \
    | head -1 | awk '{print $2}' || echo "$default"
}

L1_TARGET="$(read_target layer1_target_tokens 4000)"
L2_MAX="$(read_target layer2_max_tokens 8000)"
L3_MAX="$(read_target layer3_max_tokens 3000)"

# ── Helper: estimate tokens from file list ────────────────────────────────────

estimate_tokens() {
  local total_words=0
  for f in "$@"; do
    if [[ -f "$f" ]]; then
      words="$(wc -w < "$f")"
      total_words=$(( total_words + words ))
    fi
  done
  # Use awk for float multiplication to avoid bc dependency
  awk -v w="$total_words" -v m="$MULTIPLIER" 'BEGIN { printf "%d", int(w * m + 0.5) }'
}

status_label() {
  local actual="$1"
  local target="$2"
  if [[ "$actual" -le "$target" ]]; then
    echo "OK"
  elif [[ "$actual" -le $(( target * 12 / 10 )) ]]; then
    echo "WARN (+$(( (actual - target) * 100 / target ))%)"
  else
    echo "OVER (+$(( (actual - target) * 100 / target ))%)"
  fi
}

# ── Layer 1: static governance instructions ───────────────────────────────────
# At standard/full: operating-rules.md + agent-playbook.md + AGENTS.md
# At minimal: rules-quickstart.md only
# At nano: rules-nano.md only

L1_FILES=(
  "$ROOT_DIR/docs/operating-rules.md"
  "$ROOT_DIR/docs/agent-playbook.md"
  "$ROOT_DIR/AGENTS.md"
)
L1_TOKENS="$(estimate_tokens "${L1_FILES[@]}")"
L1_QUICKSTART="$(estimate_tokens "$ROOT_DIR/docs/rules-quickstart.md")"
L1_NANO="$(estimate_tokens "$ROOT_DIR/docs/rules-nano.md")"
L1_STATUS="$(status_label "$L1_TOKENS" "$L1_TARGET")"
[[ "$L1_STATUS" == OVER* ]] && EXIT_CODE=1

# ── Layer 2: skills ───────────────────────────────────────────────────────────

L2_FILES=()
while IFS= read -r skill_dir; do
  skill_file="$skill_dir/SKILL.md"
  [[ -f "$skill_file" ]] && L2_FILES+=("$skill_file")
done < <(find "$ROOT_DIR/skills" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)

L2_TOKENS="$(estimate_tokens "${L2_FILES[@]}")"
L2_STATUS="$(status_label "$L2_TOKENS" "$L2_MAX")"
[[ "$L2_STATUS" == OVER* ]] && EXIT_CODE=1

# ── Layer 3: project state ────────────────────────────────────────────────────

L3_FILES=(
  "$ROOT_DIR/DECISIONS.md"
  "$ROOT_DIR/ARCHITECTURE.md"
  "$ROOT_DIR/project/project-manifest.md"
)
L3_TOKENS="$(estimate_tokens "${L3_FILES[@]}")"
L3_STATUS="$(status_label "$L3_TOKENS" "$L3_MAX")"
[[ "$L3_STATUS" == OVER* ]] && EXIT_CODE=1

# ── Layer 4: volatile context (informational only) ────────────────────────────
# Trace files, runtime notes — no hard target, just informational

L4_FILES=()
shopt -s nullglob
l4_candidates=("$ROOT_DIR"/.agent-trace/*.trace.yaml)
L4_FILES+=("${l4_candidates[@]}")
L4_TOKENS="$(estimate_tokens "${L4_FILES[@]}")"

# ── Individual skill breakdown ────────────────────────────────────────────────

skill_breakdown=""
for skill_file in "${L2_FILES[@]}"; do
  skill_name="$(basename "$(dirname "$skill_file")")"
  tokens="$(estimate_tokens "$skill_file")"
  skill_breakdown+="    ${skill_name}: ~${tokens} tokens"$'\n'
done

# ── Output ────────────────────────────────────────────────────────────────────

if [[ "$JSON_OUTPUT" -eq 1 ]]; then
  cat <<EOF
{
  "layers": {
    "L1_standard": { "tokens": $L1_TOKENS, "target": $L1_TARGET, "status": "$L1_STATUS" },
    "L1_minimal": { "tokens": $L1_QUICKSTART, "target": $L1_TARGET, "status": "$(status_label "$L1_QUICKSTART" "$L1_TARGET")" },
    "L1_nano": { "tokens": $L1_NANO, "target": $L1_TARGET, "status": "$(status_label "$L1_NANO" "$L1_TARGET")" },
    "L2_all_skills": { "tokens": $L2_TOKENS, "target": $L2_MAX, "status": "$L2_STATUS" },
    "L3_project_state": { "tokens": $L3_TOKENS, "target": $L3_MAX, "status": "$L3_STATUS" },
    "L4_volatile": { "tokens": $L4_TOKENS, "target": null, "status": "info" }
  },
  "multiplier": $MULTIPLIER,
  "exit_code": $EXIT_CODE
}
EOF
else
  echo "╔══════════════════════════════════════════════════════════════╗"
  echo "║  budget-report.sh  (multiplier=${MULTIPLIER})                "
  echo "╠══════════════════════════════════════════════════════════════╣"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 1 (standard profile)" "~${L1_TOKENS}" "${L1_TARGET}" "$L1_STATUS"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 1 (minimal profile)"  "~${L1_QUICKSTART}" "${L1_TARGET}" "$(status_label "$L1_QUICKSTART" "$L1_TARGET")"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 1 (nano profile)"     "~${L1_NANO}" "${L1_TARGET}" "$(status_label "$L1_NANO" "$L1_TARGET")"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 2 (all skills)"       "~${L2_TOKENS}" "${L2_MAX}" "$L2_STATUS"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 3 (project state)"    "~${L3_TOKENS}" "${L3_MAX}" "$L3_STATUS"
  printf "║  %-30s  %7s / %-7s  %s\n" "Layer 4 (volatile/traces)"  "~${L4_TOKENS}" "n/a" "info"
  echo "╠══════════════════════════════════════════════════════════════╣"
  echo "║  Skill breakdown (Layer 2):"
  echo "$skill_breakdown" | while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    printf "║  %s\n" "$line"
  done
  echo "╚══════════════════════════════════════════════════════════════╝"
  echo ""
  if [[ "$EXIT_CODE" -ne 0 ]]; then
    echo "[budget-report] WARNING: one or more layers exceed their target token budget."
    echo "  Consider trimming skill content or archiving old DECISIONS.md entries."
  else
    echo "[budget-report] All layers within target budget."
  fi
fi

[[ "$WARN_ONLY" -eq 1 ]] && exit 0 || exit "$EXIT_CODE"
