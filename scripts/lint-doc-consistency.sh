#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXIT_CODE=0

SEARCH_TOOL=""
if command -v rg >/dev/null 2>&1; then
  SEARCH_TOOL="rg"
elif command -v grep >/dev/null 2>&1; then
  SEARCH_TOOL="grep"
else
  echo "[doc-lint][error] neither ripgrep (rg) nor grep is installed"
  exit 1
fi

echo "[doc-lint] checking prompt-budget and docs consistency..."

run_search() {
  local mode="$1"
  local pattern="$2"
  shift 2

  if [[ "$SEARCH_TOOL" == "rg" ]]; then
    if [[ "$mode" == "files" ]]; then
      rg -l "$pattern" "$@"
    else
      rg -n "$pattern" "$@"
    fi
  else
    if [[ "$mode" == "files" ]]; then
      grep -R -l -E "$pattern" "$@"
    else
      grep -R -n -E "$pattern" "$@"
    fi
  fi
}

check_grep() {
  local description="$1"
  local pattern="$2"
  shift 2
  local output
  local status=0

  set +e
  output=$(run_search matches "$pattern" "$@" 2>&1)
  status=$?
  set -e

  if [[ $status -eq 0 ]]; then
    echo "[doc-lint][error] ${description}"
    echo "$output"
    EXIT_CODE=1
  elif [[ $status -eq 1 ]]; then
    :
  else
    echo "[doc-lint][error] ripgrep failed while checking: ${description}"
    echo "$output"
    EXIT_CODE=1
  fi
}

# Helper: collect only existing paths from a list.
existing_paths() {
  local paths=()
  for p in "$@"; do
    [[ -e "$p" ]] && paths+=("$p")
  done
  if [[ ${#paths[@]} -eq 0 ]]; then
    return 1
  fi
  printf '%s\n' "${paths[@]}"
}

# Disallow stale profile wording.
mapfile -t stale_profile_paths < <(existing_paths "$ROOT_DIR/CHANGELOG.md" "$ROOT_DIR/README.md" "$ROOT_DIR/docs" "$ROOT_DIR/examples" "$ROOT_DIR/skills")
if [[ ${#stale_profile_paths[@]} -gt 0 ]]; then
  check_grep \
    "stale three-profile wording found" \
    "three named budget profiles|three profile example blocks" \
    "${stale_profile_paths[@]}"
fi

# Disallow stale top-level trust_level config examples on config-like surfaces.
mapfile -t trust_level_paths < <(existing_paths "$ROOT_DIR/docs/adoption-guide.md" "$ROOT_DIR/docs/prompt-budget-examples.md" "$ROOT_DIR/examples" "$ROOT_DIR/skills/prompt-cache-optimization/SKILL.md")
if [[ ${#trust_level_paths[@]} -gt 0 ]]; then
  check_grep \
    "stale top-level trust_level config found" \
    "^[[:space:]]*trust_level:" \
    "${trust_level_paths[@]}"
fi

# Non-autonomous config examples should not define autonomous_mode.
mapfile -t non_auto_search_paths < <(existing_paths "$ROOT_DIR/examples" "$ROOT_DIR/skills/prompt-cache-optimization/SKILL.md")
non_autonomous_output=""
non_autonomous_status=0
if [[ ${#non_auto_search_paths[@]} -gt 0 ]]; then
  set +e
  non_autonomous_output=$(run_search files "^[[:space:]]*execution_mode:[[:space:]]*(supervised|semi-auto)" \
    "${non_auto_search_paths[@]}" 2>&1)
  non_autonomous_status=$?
  set -e
fi

if [[ $non_autonomous_status -ge 2 ]]; then
  echo "[doc-lint][error] ripgrep failed while discovering non-autonomous config examples"
  echo "$non_autonomous_output"
  exit 1
fi

mapfile -t non_autonomous_files <<< "$non_autonomous_output"
for file in "${non_autonomous_files[@]}"; do
  [[ -z "$file" ]] && continue

  file_output=""
  file_status=0
  set +e
  file_output=$(run_search matches "^[[:space:]]*autonomous_mode:" "$file" 2>&1)
  file_status=$?
  set -e

  if [[ $file_status -eq 0 ]]; then
    echo "[doc-lint][error] non-autonomous example defines autonomous_mode: ${file#$ROOT_DIR/}"
    set +e
    run_search matches "^[[:space:]]*(execution_mode:|autonomous_mode:)" "$file"
    file_status=$?
    set -e
    if [[ $file_status -ge 2 ]]; then
      echo "[doc-lint][error] ripgrep failed while printing context for ${file#$ROOT_DIR/}"
      EXIT_CODE=1
    fi
    EXIT_CODE=1
  elif [[ $file_status -eq 1 ]]; then
    :
  else
    echo "[doc-lint][error] ripgrep failed while checking autonomous_mode in ${file#$ROOT_DIR/}"
    echo "$file_output"
    EXIT_CODE=1
  fi
done

# Canonical prompt-budget keys should be used in examples/docs.
mapfile -t canonical_key_paths < <(existing_paths "$ROOT_DIR/README.md" "$ROOT_DIR/docs" "$ROOT_DIR/examples" "$ROOT_DIR/skills")
if [[ ${#canonical_key_paths[@]} -gt 0 ]]; then
  check_grep \
    "stale prompt-budget key found" \
    "require_risk_reviewer_for_all_changes|require_adr_for_architecture_change|block_destructive_actions_without_manual_approval|small_tasks_skip_compliance_block|targeted_tests_for_small_tasks|critic_required_only_for_large_changes|prefer_existing_code_practice|require_explicit_approval_for_pattern_replacement|legacy_modules_require_archive_decision_search" \
    "${canonical_key_paths[@]}"
fi

# `prompt-budget.yml` is not the example gallery file anymore.
mapfile -t gallery_paths < <(existing_paths "$ROOT_DIR/README.md" "$ROOT_DIR/docs" "$ROOT_DIR/skills")
if [[ ${#gallery_paths[@]} -gt 0 ]]; then
  check_grep \
    "stale prompt-budget example reference found" \
    'prompt-budget\.yml` for example configurations per profile' \
    "${gallery_paths[@]}"
fi

# Validate hardcoded asset counts in README.md match actual filesystem.
if [[ -f "$ROOT_DIR/README.md" ]]; then
  if [[ -d "$ROOT_DIR/skills" ]]; then
    actual_skills=$(find "$ROOT_DIR/skills" -mindepth 2 -name 'SKILL.md' | wc -l)
  else
    actual_skills=0
  fi

  if [[ -d "$ROOT_DIR/.claude/agents" ]]; then
    actual_agents=$(find "$ROOT_DIR/.claude/agents" -name '*.md' | wc -l)
  else
    actual_agents=0
  fi

  readme_skills=$(awk -F'Reusable skills: ' '/Reusable skills: [0-9]+/ { split($2, parts, /[^0-9]/); print parts[1]; exit }' "$ROOT_DIR/README.md")
  readme_agents=$(awk -F'Claude subagents: ' '/Claude subagents: [0-9]+/ { split($2, parts, /[^0-9]/); print parts[1]; exit }' "$ROOT_DIR/README.md")

  if [[ -n "$readme_skills" && "$readme_skills" -ne "$actual_skills" ]]; then
    echo "[doc-lint][error] README.md says $readme_skills skills but found $actual_skills in skills/*/SKILL.md"
    EXIT_CODE=1
  fi

  if [[ -n "$readme_agents" && "$readme_agents" -ne "$actual_agents" ]]; then
    echo "[doc-lint][error] README.md says $readme_agents agents but found $actual_agents in .claude/agents/*.md"
    EXIT_CODE=1
  fi
fi

if [[ $EXIT_CODE -ne 0 ]]; then
  echo "[doc-lint] failed"
else
  echo "[doc-lint] passed"
fi

exit $EXIT_CODE
