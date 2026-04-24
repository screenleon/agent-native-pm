"""Shared prompt loader for the reference Python adapters.

The canonical prompt source lives at
`backend/internal/prompts/<name>.md` (see docs/phase5-plan.md §3 A1 for
why the path is inside the Go package rather than repo-root).

This loader keeps the Python side rendering from the same source file as
the Go built-in adapter. A prompt change lands in one file; both adapters
pick it up.

Template syntax: `{{VAR_NAME}}`, single-pass regex substitution. Matches
the Go implementation in `backend/internal/prompts/render.go`. Unknown
variables are deliberately preserved in the output so a mismatch surfaces
during review instead of silently producing an empty line.
"""
from __future__ import annotations

import os
import re
from pathlib import Path


_TEMPLATE_VAR = re.compile(r"\{\{([A-Z][A-Z0-9_]*)\}\}")
_FRONTMATTER_DELIM = re.compile(
    r"\A---\s*\n.*?\n---\s*\n",
    re.DOTALL,
)


def _prompts_dir() -> Path:
    """Resolve the canonical prompts directory.

    Honours `ANPM_PROMPTS_DIR` for CI and unusual deployments; otherwise
    walks from this file's location up to the repo root.
    """
    override = os.environ.get("ANPM_PROMPTS_DIR")
    if override:
        return Path(override)
    # adapters/_prompt_loader.py → repo-root/backend/internal/prompts/
    return (
        Path(__file__).resolve().parent.parent
        / "backend"
        / "internal"
        / "prompts"
    )


def load(name: str) -> str:
    """Return the raw markdown source INCLUDING frontmatter."""
    path = _prompts_dir() / f"{name}.md"
    return path.read_text(encoding="utf-8")


def render(name: str, variables: dict[str, str]) -> str:
    """Load prompt `name`, strip frontmatter, and apply {{VAR}} substitution.

    Single-pass: a value that itself contains `{{OTHER}}` is NOT
    re-expanded. Unknown variables are left as-is.
    """
    raw = load(name)
    body = _strip_frontmatter(raw)
    return _substitute(body, variables)


def _strip_frontmatter(body: str) -> str:
    match = _FRONTMATTER_DELIM.match(body)
    if not match:
        return body
    return body[match.end():]


def _substitute(body: str, variables: dict[str, str]) -> str:
    def repl(match: re.Match[str]) -> str:
        key = match.group(1)
        if key in variables:
            return variables[key]
        return match.group(0)

    return _TEMPLATE_VAR.sub(repl, body)
