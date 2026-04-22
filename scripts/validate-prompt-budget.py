#!/usr/bin/env python3
"""
validate-prompt-budget.py

Validates prompt-budget.yml (and optionally prompt-budget.local.example.yml)
against an inline schema. Prints structured diagnostics and exits non-zero on
any error.

Usage:
    python3 scripts/validate-prompt-budget.py [--file path/to/prompt-budget.yml]
    python3 scripts/validate-prompt-budget.py --all   # validates main + local example

Requires only the Python standard library (no third-party packages needed).
"""

import sys
import re
import argparse
from pathlib import Path

# ---------------------------------------------------------------------------
# Minimal YAML scalar parser (no PyYAML dependency)
# ---------------------------------------------------------------------------

def parse_yaml_scalars(text: str) -> dict:
    """
    Extract top-level key: value pairs from a YAML file.
    Handles quoted and unquoted scalars.  Ignores comments, list items,
    and nested blocks — those are validated with targeted regex passes.
    Strips inline comments from unquoted values (e.g. "semi-auto  # comment").
    """
    result: dict = {}
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("#") or not stripped:
            continue
        m = re.match(r'^([A-Za-z_][A-Za-z0-9_]*):\s*(.*)', stripped)
        if m:
            key = m.group(1)
            val = m.group(2).strip()
            # Strip inline comment only when value is not quoted
            if not (val.startswith('"') or val.startswith("'")):
                val = re.sub(r'\s+#.*$', '', val)
            val = val.strip('"').strip("'")
            result[key] = val
    return result


def extract_nested_scalar(text: str, path: str) -> str | None:
    """
    Extract a scalar value from a dotted path like 'budget.profile'.
    Only handles one level of indentation nesting.
    """
    parts = path.split(".", 1)
    if len(parts) == 1:
        return parse_yaml_scalars(text).get(parts[0])
    parent_key, child_key = parts
    in_block = False
    for line in text.splitlines():
        stripped = line.strip()
        if re.match(rf'^{re.escape(parent_key)}:\s*$', stripped):
            in_block = True
            continue
        if in_block:
            if re.match(r'^[A-Za-z_]', line):
                break  # back to top-level — parent block ended
            m = re.match(rf'^\s+{re.escape(child_key)}:\s*(.*)', line)
            if m:
                return m.group(1).strip().strip('"').strip("'")
    return None


def extract_list_items(text: str, key: str) -> list[str]:
    """
    Extract items from a YAML list under a given key at any indentation level.
    Handles both inline [ ] and block - syntax.
    Tracks the key's own indentation depth and stops when a subsequent non-blank,
    non-comment line appears at the same or lesser indent (sibling or parent key).
    """
    items: list[str] = []
    # Inline: key: [a, b, c] (at any indent level)
    m = re.search(rf'^\s*{re.escape(key)}:\s*\[([^\]]*)\]', text, re.MULTILINE)
    if m:
        for item in m.group(1).split(","):
            v = item.strip().strip('"').strip("'")
            if v:
                items.append(v)
        return items
    # Block list — track indentation depth to stop at sibling keys
    in_block = False
    key_indent = 0
    for line in text.splitlines():
        stripped = line.strip()
        km = re.match(rf'^(\s*){re.escape(key)}:\s*$', line)
        if km:
            in_block = True
            key_indent = len(km.group(1))
            continue
        if in_block:
            if not stripped or stripped.startswith("#"):
                continue
            current_indent = len(line) - len(line.lstrip())
            if current_indent <= key_indent:
                break  # sibling or parent key — block is done
            if stripped.startswith("-"):
                v = stripped.lstrip("-").strip()
                # Strip inline comment only when value is not quoted
                if not (v.startswith('"') or v.startswith("'")):
                    v = re.sub(r'\s+#.*$', '', v)
                v = v.strip('"').strip("'")
                if v:
                    items.append(v)
    return items


# ---------------------------------------------------------------------------
# Validation rules
# ---------------------------------------------------------------------------

VALID_EXECUTION_MODES = {"supervised", "semi-auto", "autonomous"}
VALID_PROFILES = {"nano", "minimal", "standard", "full"}
VALID_TIERS = {"fast", "balanced", "deep"}
VALID_TIER_NAMES = {"default_tier", "escalation_tier"}

KNOWN_TRIGGER_IDS = {
    "root_cause_unknown",
    "repeated_failed_fix_loop",
    "security_sensitive",
    "public_contract_change",
    "high_blast_radius_change",
    "formatting_only",
    "docs_sync_only",
    "deterministic_transform",
    "narrow_known_fix",
    "simple_copy_edit",
}

KNOWN_ROLE_IDS = {
    "feature-planner",
    "backend-architect",
    "application-implementer",
    "ui-image-implementer",
    "integration-engineer",
    "documentation-architect",
    "risk-reviewer",
    "critic",
}

KNOWN_SKILL_IDS = {
    "demand-triage",
    "repo-exploration",
    "test-and-fix-loop",
    "error-recovery",
    "memory-and-state",
    "on-project-start",
    "self-reflection",
    "observability",
    "prompt-cache-optimization",
    "feature-planning",
    "backend-change-planning",
    "mcp-validation",
    "application-implementation",
    "design-to-code",
    "documentation-architecture",
    "skill-creator",
}


class Validator:
    def __init__(self, path: Path, is_override: bool = False):
        self.path = path
        self.is_override = is_override  # True for local/example override files
        self.errors: list[str] = []
        self.warnings: list[str] = []
        self.text = path.read_text(encoding="utf-8")

    def error(self, msg: str):
        self.errors.append(f"[error] {msg}")

    def warn(self, msg: str):
        self.warnings.append(f"[warn]  {msg}")

    def validate(self) -> bool:
        # Override/local files only carry partial config (e.g. model mappings).
        # Skip core fields that are intentionally absent in those files.
        if not self.is_override:
            self._check_execution_mode()
            self._check_budget_profile()
            self._check_autonomous_mode()
        self._check_model_routing()
        self._check_roles()
        self._check_skills()
        return len(self.errors) == 0

    # ── Core fields ────────────────────────────────────────────────────────

    def _check_execution_mode(self):
        val = parse_yaml_scalars(self.text).get("execution_mode", "")
        if not val:
            self.error("execution_mode is missing")
        elif val not in VALID_EXECUTION_MODES:
            self.error(
                f"execution_mode='{val}' is invalid. "
                f"Expected one of: {sorted(VALID_EXECUTION_MODES)}"
            )

    def _check_budget_profile(self):
        val = extract_nested_scalar(self.text, "budget.profile")
        if val is None:
            val = parse_yaml_scalars(self.text).get("profile", "")
        if not val:
            self.error("budget.profile is missing")
        elif val not in VALID_PROFILES:
            self.error(
                f"budget.profile='{val}' is invalid. "
                f"Expected one of: {sorted(VALID_PROFILES)}"
            )

    # ── model_routing ──────────────────────────────────────────────────────

    def _check_model_routing(self):
        if "model_routing:" not in self.text:
            return  # optional block
        enabled_val = self._nested_model_routing_scalar("enabled")
        if enabled_val not in (None, "true", "false"):
            self.error(f"model_routing.enabled must be true or false, got '{enabled_val}'")

        # Check tier names under tiers:
        tier_section = self._extract_block("tiers")
        if tier_section:
            declared_tiers = re.findall(r'^\s{4}(\w+):', tier_section, re.MULTILINE)
            for tier in declared_tiers:
                if tier not in VALID_TIERS:
                    self.error(
                        f"model_routing.tiers.{tier} is unknown. "
                        f"Expected: {sorted(VALID_TIERS)}"
                    )

        # Check policy scalars
        default_tier = self._nested_model_routing_policy_scalar("default_tier")
        if default_tier and default_tier not in VALID_TIERS:
            self.error(
                f"model_routing.policy.default_tier='{default_tier}' invalid. "
                f"Expected: {sorted(VALID_TIERS)}"
            )
        escalation_tier = self._nested_model_routing_policy_scalar("escalation_tier")
        if escalation_tier and escalation_tier not in VALID_TIERS:
            self.error(
                f"model_routing.policy.escalation_tier='{escalation_tier}' invalid. "
                f"Expected: {sorted(VALID_TIERS)}"
            )
        if default_tier and escalation_tier and default_tier == escalation_tier:
            self.warn(
                "model_routing.policy.default_tier and escalation_tier are the same — "
                "escalation will have no effect"
            )

        # Check max_attempts_at_current_tier
        max_attempts_raw = self._nested_model_routing_policy_scalar("max_attempts_at_current_tier")
        if max_attempts_raw:
            try:
                n = int(max_attempts_raw)
                if n < 1:
                    self.error("model_routing.policy.max_attempts_at_current_tier must be >= 1")
                elif n > 10:
                    self.warn(
                        f"model_routing.policy.max_attempts_at_current_tier={n} is unusually high"
                    )
            except ValueError:
                self.error(
                    f"model_routing.policy.max_attempts_at_current_tier='{max_attempts_raw}' "
                    "is not an integer"
                )

        # Check trigger IDs
        for list_key in ("direct_deep_triggers", "never_escalate_for"):
            items = self._extract_policy_list(list_key)
            for item in items:
                if item not in KNOWN_TRIGGER_IDS:
                    self.warn(
                        f"model_routing.policy.{list_key}: "
                        f"unknown trigger id '{item}' (not in canonical set — may be custom)"
                    )

        # Provider map: must not contain concrete model IDs in canonical tracked file
        if "provider_model_map:" in self.text and "prompt-budget.local" not in str(self.path):
            self.warn(
                "provider_model_map found in a tracked file. "
                "Concrete model IDs should be in prompt-budget.local.yml only."
            )

    def _nested_model_routing_scalar(self, key: str) -> str | None:
        in_mr = False
        for line in self.text.splitlines():
            stripped = line.strip()
            if re.match(r'^model_routing:\s*$', stripped):
                in_mr = True
                continue
            if in_mr:
                if re.match(r'^[A-Za-z_]', line):
                    break
                m = re.match(rf'^\s+{re.escape(key)}:\s*(.*)', line)
                if m:
                    return m.group(1).strip().strip('"').strip("'")
        return None

    def _nested_model_routing_policy_scalar(self, key: str) -> str | None:
        in_policy = False
        for line in self.text.splitlines():
            stripped = line.strip()
            if stripped == "policy:":
                in_policy = True
                continue
            if in_policy:
                if re.match(r'^\s{4}[A-Za-z_]', line) and not re.match(r'^\s{6}', line):
                    # back to same or higher indent level
                    if not re.match(r'^\s{4,}', line):
                        break
                m = re.match(rf'^\s+{re.escape(key)}:\s*(.*)', line)
                if m:
                    return m.group(1).strip().strip('"').strip("'")
        return None

    def _extract_block(self, key: str) -> str:
        """Extract the indented content block under a given key.

        Correctly handles keys at any indent level by tracking the key's own
        indent depth and stopping when a subsequent line is at the same or
        lesser depth (indicating a sibling or parent block).
        """
        lines = self.text.splitlines()
        result = []
        in_block = False
        block_indent: int = 0
        for line in lines:
            stripped = line.strip()
            m = re.match(rf'^(\s*){re.escape(key)}:\s*$', line)
            if m:
                in_block = True
                block_indent = len(m.group(1))
                continue
            if in_block:
                if not stripped:
                    result.append(line)
                    continue
                current_indent = len(line) - len(line.lstrip())
                if current_indent <= block_indent:
                    break  # back to same or higher level — block is done
                result.append(line)
        return "\n".join(result)

    def _extract_policy_list(self, key: str) -> list[str]:
        in_policy = False
        in_list = False
        items: list[str] = []
        for line in self.text.splitlines():
            stripped = line.strip()
            if stripped == "policy:":
                in_policy = True
                continue
            if in_policy:
                if stripped == f"{key}:":
                    in_list = True
                    continue
                if in_list:
                    if stripped.startswith("- "):
                        items.append(stripped[2:].strip().strip('"').strip("'"))
                    elif stripped and not stripped.startswith("#"):
                        break
        return items

    # ── roles / skills ──────────────────────────────────────────────────────

    def _check_roles(self):
        if "roles:" not in self.text:
            return
        enabled = extract_list_items(self.text, "enabled")
        disabled = extract_list_items(self.text, "disabled")
        for role in enabled:
            if role not in KNOWN_ROLE_IDS:
                self.warn(f"roles.enabled: unknown role '{role}' (not in canonical set)")
        overlap = set(enabled) & set(disabled)
        if overlap:
            self.error(f"role(s) appear in both enabled and disabled: {sorted(overlap)}")

    def _check_skills(self):
        if "skills:" not in self.text:
            return
        always = extract_list_items(self.text, "always_load")
        on_demand = extract_list_items(self.text, "on_demand")
        disabled = extract_list_items(self.text, "disabled")
        for skill in always + on_demand:
            if skill not in KNOWN_SKILL_IDS:
                self.warn(f"skills: unknown skill id '{skill}' (not in canonical set — may be custom)")
        overlap = set(always + on_demand) & set(disabled)
        if overlap:
            self.error(f"skill(s) in both active lists and disabled: {sorted(overlap)}")

    # ── autonomous_mode ────────────────────────────────────────────────────

    def _check_autonomous_mode(self):
        execution_mode = parse_yaml_scalars(self.text).get("execution_mode", "")
        if execution_mode != "autonomous":
            return
        # When autonomous, warn if destructive halts are disabled
        halt_val = self._nested_autonomous_scalar("halt_on_destructive_actions")
        if halt_val == "false":
            self.warn(
                "autonomous_mode.halt_on_destructive_actions=false — "
                "only safe in fully isolated/sandboxed environments"
            )
        stuck_val = self._nested_autonomous_scalar("halt_on_stuck_escalation")
        if stuck_val == "false":
            self.warn(
                "autonomous_mode.halt_on_stuck_escalation=false — "
                "risks infinite loops; ensure an external timeout/retry-cap exists"
            )

    def _nested_autonomous_scalar(self, key: str) -> str | None:
        in_block = False
        for line in self.text.splitlines():
            stripped = line.strip()
            if re.match(r'^autonomous_mode:\s*$', stripped):
                in_block = True
                continue
            if in_block:
                if re.match(r'^[A-Za-z_]', line):
                    break
                m = re.match(rf'^\s+{re.escape(key)}:\s*(.*)', line)
                if m:
                    return m.group(1).strip().strip('"').strip("'")
        return None

    # ── Report ──────────────────────────────────────────────────────────────

    def report(self):
        tag = f"[validate-prompt-budget] {self.path.name}"
        if not self.errors and not self.warnings:
            print(f"{tag} passed (no issues)")
            return
        for msg in self.warnings:
            print(f"{tag} {msg}")
        for msg in self.errors:
            print(f"{tag} {msg}")
        if self.errors:
            print(f"{tag} FAILED ({len(self.errors)} error(s))")
        else:
            print(f"{tag} passed with {len(self.warnings)} warning(s)")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Validate prompt-budget.yml schema")
    parser.add_argument(
        "--file", metavar="PATH",
        default="prompt-budget.yml",
        help="Path to the yaml file to validate (default: prompt-budget.yml)",
    )
    parser.add_argument(
        "--all", action="store_true",
        help="Validate prompt-budget.yml AND prompt-budget.local.example.yml if it exists",
    )
    args = parser.parse_args()

    root = Path(__file__).parent.parent

    files_to_validate: list[Path] = []
    if args.all:
        for name in ("prompt-budget.yml", "prompt-budget.local.example.yml"):
            candidate = root / name
            if candidate.exists():
                files_to_validate.append(candidate)
    else:
        target = Path(args.file)
        if not target.is_absolute():
            target = root / target
        files_to_validate.append(target)

    overall_ok = True
    for path in files_to_validate:
        if not path.exists():
            print(f"[validate-prompt-budget] ERROR: file not found: {path}")
            overall_ok = False
            continue
        # Files with 'local' in the name are partial overrides — skip core field checks.
        is_override = "local" in path.name
        v = Validator(path, is_override=is_override)
        ok = v.validate()
        v.report()
        if not ok:
            overall_ok = False

    sys.exit(0 if overall_ok else 1)


if __name__ == "__main__":
    main()
