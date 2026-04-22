#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXIT_CODE=0

echo "[rule-lint] checking layered rule structure..."

# Required files
required=(
  "$ROOT_DIR/rules/global/README.md"
  "$ROOT_DIR/rules/domain/README.md"
  "$ROOT_DIR/project/project-manifest.md"
)

for f in "${required[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "[rule-lint][error] missing required file: ${f#$ROOT_DIR/}"
    EXIT_CODE=1
  fi
done

# Check override annotation format in project manifest
manifest="$ROOT_DIR/project/project-manifest.md"
if [[ -f "$manifest" ]]; then
  invalid_override_lines=$(grep -n "Overrides:" "$manifest" | grep -v "<" | grep -vE "Overrides: [A-Za-z0-9_-]+ -> [A-Za-z0-9_-]+" || true)
  if [[ -n "$invalid_override_lines" ]]; then
    echo "[rule-lint][error] invalid override format lines in project manifest:"
    echo "$invalid_override_lines"
    EXIT_CODE=1
  fi
fi

# Lightweight duplicate detection across layered files.
# This intentionally checks bullet/statement-like lines only.
mapfile -t layered_files < <(find "$ROOT_DIR/rules" "$ROOT_DIR/project" -type f -name "*.md" | sort)

# Rule ID uniqueness and relationship integrity checks.
declare -A RULE_FILE
declare -A RULE_LINE
declare -A RULE_STATUS
declare -A RULE_STABILITY
declare -A RULE_SUPERSEDES
declare -A RULE_SUPERSEDED_BY

for f in "${layered_files[@]}"; do
  while IFS=$'\t' read -r line_no rule_id status stability supersedes superseded_by; do
    [[ -z "$rule_id" ]] && continue
    [[ "$rule_id" == *"<"* ]] && continue

    if [[ -n "${RULE_FILE[$rule_id]:-}" ]]; then
      echo "[rule-lint][error] duplicate rule id '$rule_id' in ${f#$ROOT_DIR/}:$line_no (already defined in ${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
      EXIT_CODE=1
      continue
    fi

    RULE_FILE[$rule_id]="${f#$ROOT_DIR/}"
    RULE_LINE[$rule_id]="$line_no"
    RULE_STATUS[$rule_id]="$status"
    RULE_STABILITY[$rule_id]="$stability"
    RULE_SUPERSEDES[$rule_id]="$supersedes"
    RULE_SUPERSEDED_BY[$rule_id]="$superseded_by"
  done < <(
    awk '
      function trim(s) { gsub(/^[[:space:]]+|[[:space:]]+$/, "", s); return s }
      function emit() {
        if (id != "") {
          st = (stability == "") ? "__MISSING__" : trim(stability)
          print line_no "\t" trim(id) "\t" trim(status) "\t" st "\t" trim(supersedes) "\t" trim(superseded_by)
        }
      }
      /^### Rule:[[:space:]]*/ {
        emit()
        id = $0
        sub(/^### Rule:[[:space:]]*/, "", id)
        line_no = NR
        status = ""
        stability = ""
        supersedes = ""
        superseded_by = ""
        next
      }
      /^-[[:space:]]*Status:[[:space:]]*/ {
        status = $0
        sub(/^-+[[:space:]]*Status:[[:space:]]*/, "", status)
      }
      /^-[[:space:]]*Stability:[[:space:]]*/ {
        stability = $0
        sub(/^-+[[:space:]]*Stability:[[:space:]]*/, "", stability)
      }
      /^-[[:space:]]*Supersedes:[[:space:]]*/ {
        supersedes = $0
        sub(/^-+[[:space:]]*Supersedes:[[:space:]]*/, "", supersedes)
      }
      /^-[[:space:]]*Superseded by:[[:space:]]*/ {
        superseded_by = $0
        sub(/^-+[[:space:]]*Superseded by:[[:space:]]*/, "", superseded_by)
      }
      END { emit() }
    ' "$f"
  )
done

for rule_id in "${!RULE_FILE[@]}"; do
  status="${RULE_STATUS[$rule_id],,}"
  supersedes="${RULE_SUPERSEDES[$rule_id]}"
  superseded_by="${RULE_SUPERSEDED_BY[$rule_id]}"

  if [[ -n "$supersedes" && "$supersedes" != "N/A" && "$supersedes" != *"<"* ]]; then
    if [[ -z "${RULE_FILE[$supersedes]:-}" ]]; then
      echo "[rule-lint][error] rule '$rule_id' supersedes unknown rule '$supersedes' (${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
      EXIT_CODE=1
    fi
  fi

  if [[ -n "$superseded_by" && "$superseded_by" != "N/A" && "$superseded_by" != *"<"* ]]; then
    if [[ -z "${RULE_FILE[$superseded_by]:-}" ]]; then
      echo "[rule-lint][error] rule '$rule_id' references unknown replacement '$superseded_by' (${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
      EXIT_CODE=1
    fi
  fi

  if [[ "$status" == "superseded" ]]; then
    if [[ -z "$superseded_by" || "$superseded_by" == "N/A" || "$superseded_by" == *"<"* ]]; then
      echo "[rule-lint][error] superseded rule '$rule_id' must define '- Superseded by:' with a real rule ID (placeholders such as '<RULE_ID>' are not allowed) (${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
      EXIT_CODE=1
    fi
  fi

  # Validate Stability field
  stability="${RULE_STABILITY[$rule_id],,}"
  if [[ -z "$stability" || "$stability" == "__missing__" || "$stability" == *"<"* ]]; then
    echo "[rule-lint][error] rule '$rule_id' is missing '- Stability:' field (${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
    EXIT_CODE=1
  elif [[ "$stability" != "core" && "$stability" != "behavior" && "$stability" != "experimental" ]]; then
    echo "[rule-lint][error] rule '$rule_id' has invalid stability '$stability' — must be core, behavior, or experimental (${RULE_FILE[$rule_id]}:${RULE_LINE[$rule_id]})"
    EXIT_CODE=1
  fi
done

tmpfile="$(mktemp)"
for f in "${layered_files[@]}"; do
  awk -v file="${f#$ROOT_DIR/}" '
    /^[[:space:]]*[-*][[:space:]]+/ {
      line=$0
      gsub(/^[[:space:]]*[-*][[:space:]]+/, "", line)
      gsub(/[[:space:]]+/, " ", line)
      if (length(line) >= 24 && line !~ /<[^>]+>/) {
        print line "\t" file
      }
    }
  ' "$f" >> "$tmpfile"
done

if [[ -s "$tmpfile" ]]; then
  duplicates=$(awk -F '\t' '
    {
      key=$1
      if (index("," files[key] ",", "," $2 ",") == 0) {
        files[key]=files[key] "," $2
      }
      count[key]++
    }
    END {
      for (k in count) {
        split(files[k], arr, ",")
        distinct=0
        for (i in arr) {
          if (arr[i] != "") distinct++
        }
        if (distinct > 1) {
          print count[k] "\t" k "\t" files[k]
        }
      }
    }
  ' "$tmpfile" | sort -rn)

  if [[ -n "$duplicates" ]]; then
    echo "[rule-lint][warn] potential duplicate rule text across layered files:"
    echo "$duplicates" | head -n 20 | while IFS=$'\t' read -r c text files; do
      echo "  - count=$c text=\"$text\" files=${files#,}"
    done
    echo "[rule-lint][warn] review and deduplicate where needed"
  fi
fi

rm -f "$tmpfile"

if [[ $EXIT_CODE -ne 0 ]]; then
  echo "[rule-lint] failed"
else
  echo "[rule-lint] passed"
fi

exit $EXIT_CODE
