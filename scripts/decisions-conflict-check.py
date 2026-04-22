#!/usr/bin/env python3
"""
decisions-conflict-check.py

Adapter-neutral pre-plan contradiction check against DECISIONS.md.

Accepts a proposed decision description (via stdin, --text, or --file) and
surfaces the historical DECISIONS.md entries most likely to conflict with it.
Uses a keyword-overlap heuristic with negation/antonym boosting — zero third-
party dependencies, fast, deterministic, and explainable.

Intent:
    - Help any agent (not just claude-code) perform a real contradiction
      check instead of glancing at the file.
    - Run *before* planning starts, so surprises surface early.
    - Stay zero-dep: no embeddings, no network, no tokenizer models.

Usage:
    echo "switch auth to session cookies" | python3 scripts/decisions-conflict-check.py
    python3 scripts/decisions-conflict-check.py --text "switch auth to cookies"
    python3 scripts/decisions-conflict-check.py --file proposal.md
    python3 scripts/decisions-conflict-check.py --text "..." --top 5 --format json

Exit codes:
    0  No likely conflicts (top overlap score below threshold).
    1  Likely conflict found (score >= --warn-threshold).
    2  Error (DECISIONS.md missing, no input, etc.).
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Iterable

DEFAULT_DECISIONS = "DECISIONS.md"
DEFAULT_TOP = 3
DEFAULT_WARN_THRESHOLD = 0.20

# Words that rarely carry decision semantics; dropping them reduces noise.
STOPWORDS = set("""
a an the of to in on for with by from as at is are was were be been being
this that these those it its do does did done this our your their my i we
you they he she them us will would should could can may might not no yes
if but or and so then than into onto over under between within without
about across around per via
also only just still even more most less least few many some any all
new add adds added adding remove removes removed removing change changes
changed changing update updates updated updating use uses used using
like such e.g. ie eg
""".split())

# Cheap negation / antonym surface forms. When a candidate contains one and
# the proposal contains the other (or vice versa), we boost the score.
NEGATION_PAIRS = [
    ("enable", "disable"),
    ("allow", "block"),
    ("allow", "forbid"),
    ("allow", "deny"),
    ("use", "stop using"),
    ("add", "remove"),
    ("add", "delete"),
    ("create", "drop"),
    ("introduce", "retire"),
    ("adopt", "deprecate"),
    ("require", "optional"),
    ("mandatory", "optional"),
    ("keep", "replace"),
    ("prefer", "avoid"),
    ("only", "any"),
    ("centralize", "distribute"),
    ("sync", "async"),
    ("synchronous", "asynchronous"),
    ("on", "off"),
    ("always", "never"),
]

# Lightweight stemmer: strip common English suffixes. Good enough for overlap.
SUFFIX_RE = re.compile(r"(ing|ed|es|s|ly|tion|sion|ment|ness)$")


def tokenize(text: str) -> list[str]:
    # Lowercase + alnum words of length >= 3. Strip stopwords. Stem.
    words = re.findall(r"[A-Za-z][A-Za-z0-9_-]*", text.lower())
    out: list[str] = []
    for w in words:
        if w in STOPWORDS or len(w) < 3:
            continue
        stem = SUFFIX_RE.sub("", w)
        if len(stem) < 3:
            stem = w
        out.append(stem)
    return out


def token_set(text: str) -> set[str]:
    return set(tokenize(text))


def parse_decisions(md: str) -> list[dict]:
    """
    Split DECISIONS.md into entries. An entry starts with `## ` at line start.
    Returns list of dicts: {heading, body}.
    """
    entries: list[dict] = []
    current_heading: str | None = None
    current_body: list[str] = []
    for line in md.splitlines():
        if line.startswith("## "):
            if current_heading is not None:
                entries.append({
                    "heading": current_heading,
                    "body": "\n".join(current_body).strip(),
                })
            current_heading = line[3:].strip()
            current_body = []
        elif current_heading is not None:
            current_body.append(line)
    if current_heading is not None:
        entries.append({
            "heading": current_heading,
            "body": "\n".join(current_body).strip(),
        })
    return entries


def jaccard(a: set[str], b: set[str]) -> float:
    if not a or not b:
        return 0.0
    inter = len(a & b)
    union = len(a | b)
    return inter / union if union else 0.0


def _phrase_regex(phrase: str) -> re.Pattern[str]:
    # Word-boundary match. Handles single words and multi-word phrases;
    # allows any whitespace between words (so "stop   using" still matches).
    parts = [re.escape(p) for p in phrase.strip().split()]
    pattern = r"\b" + r"\s+".join(parts) + r"\b"
    return re.compile(pattern, re.IGNORECASE)


_NEGATION_PAIR_PATTERNS = [
    (a, b, _phrase_regex(a), _phrase_regex(b)) for a, b in NEGATION_PAIRS
]


def negation_boost(proposal: str, entry_text: str) -> tuple[float, list[str]]:
    """
    Returns (boost, reasons). If the proposal says "X" and the entry says
    the opposite (or vice versa), add 0.15 per matched pair (cap at 0.30).
    Uses word-boundary matching so "add" does not match "address" and
    "on" does not match "only".
    """
    boost = 0.0
    reasons: list[str] = []
    for a, b, ra, rb in _NEGATION_PAIR_PATTERNS:
        p_has_a = bool(ra.search(proposal))
        p_has_b = bool(rb.search(proposal))
        e_has_a = bool(ra.search(entry_text))
        e_has_b = bool(rb.search(entry_text))
        if p_has_a and e_has_b and not (p_has_b and e_has_a):
            boost += 0.15
            reasons.append(f'"{a}" (proposal) ↔ "{b}" (decision)')
        elif p_has_b and e_has_a and not (p_has_a and e_has_b):
            boost += 0.15
            reasons.append(f'"{b}" (proposal) ↔ "{a}" (decision)')
    return (min(boost, 0.30), reasons)


def score_entry(proposal: str, entry: dict) -> dict:
    entry_text = f"{entry['heading']}\n{entry['body']}"
    ts_p = token_set(proposal)
    ts_e_full = token_set(entry_text)
    ts_e_head = token_set(entry["heading"])

    base = jaccard(ts_p, ts_e_full)
    # Heading-level overlap is a stronger signal than body overlap because the
    # heading typically names the subject of the decision.
    head_overlap = jaccard(ts_p, ts_e_head) if ts_e_head else 0.0
    head_boost = 0.4 * head_overlap

    neg_boost, neg_reasons = negation_boost(proposal, entry_text)
    overlap_tokens = sorted(ts_p & ts_e_full)
    score = min(base + head_boost + neg_boost, 1.0)
    return {
        "heading": entry["heading"],
        "score": round(score, 3),
        "base_overlap": round(base, 3),
        "heading_overlap": round(head_overlap, 3),
        "negation_boost": round(neg_boost, 3),
        "shared_keywords": overlap_tokens[:10],
        "negation_pairs": neg_reasons,
    }


def read_proposal(args: argparse.Namespace) -> str:
    if args.text is not None:
        return args.text
    if args.file is not None:
        try:
            return Path(args.file).read_text(encoding="utf-8")
        except FileNotFoundError:
            print(f"ERROR: input file not found: {args.file}", file=sys.stderr)
            raise SystemExit(2)
        except OSError as exc:
            print(f"ERROR: could not read input file {args.file}: {exc}",
                  file=sys.stderr)
            raise SystemExit(2)
    if sys.stdin.isatty():
        return ""
    return sys.stdin.read()


def format_table(results: list[dict], top: int, threshold: float) -> str:
    lines: list[str] = []
    near = threshold - 0.10
    any_warn = any(r["score"] >= threshold for r in results[:top])
    any_near = any(threshold > r["score"] >= near for r in results[:top])
    if any_warn:
        verdict = "LIKELY CONFLICT"
    elif any_near:
        verdict = "POSSIBLE CONFLICT — review top candidate manually"
    else:
        verdict = "no likely conflicts"
    lines.append(f"Verdict: {verdict} (threshold {threshold})")
    lines.append("")
    for i, r in enumerate(results[:top], start=1):
        flag = "⚠" if r["score"] >= threshold else " "
        lines.append(f"{flag} #{i}  score={r['score']:.3f}  {r['heading']}")
        lines.append(
            f"     base={r['base_overlap']:.3f}  heading={r['heading_overlap']:.3f}  "
            f"negation_boost={r['negation_boost']:.3f}"
        )
        if r["shared_keywords"]:
            lines.append(f"     shared: {', '.join(r['shared_keywords'])}")
        if r["negation_pairs"]:
            for p in r["negation_pairs"]:
                lines.append(f"     negation: {p}")
        lines.append("")
    return "\n".join(lines).rstrip()


def main() -> int:
    ap = argparse.ArgumentParser(description="Pre-plan contradiction check against DECISIONS.md")
    ap.add_argument("--file", help="Read proposal from a file")
    ap.add_argument("--text", help="Proposal text on the command line")
    ap.add_argument("--decisions", default=DEFAULT_DECISIONS, help="Path to DECISIONS.md")
    ap.add_argument("--top", type=int, default=DEFAULT_TOP, help="How many candidates to show")
    ap.add_argument("--warn-threshold", type=float, default=DEFAULT_WARN_THRESHOLD,
                    help="Score at or above this is flagged as likely conflict")
    ap.add_argument("--format", choices=["table", "json"], default="table")
    args = ap.parse_args()

    dec_path = Path(args.decisions)
    if not dec_path.exists():
        print(f"ERROR: {dec_path} not found", file=sys.stderr)
        return 2

    proposal = read_proposal(args)
    if not proposal.strip():
        print("ERROR: no proposal text provided (use --text, --file, or pipe to stdin)", file=sys.stderr)
        return 2

    entries = parse_decisions(dec_path.read_text(encoding="utf-8"))
    if not entries:
        # No historical decisions is not an error — it just means no conflict.
        output = {"verdict": "no_history", "candidates": []}
        if args.format == "json":
            print(json.dumps(output, indent=2))
        else:
            print("No historical decisions to check against.")
        return 0

    scored = [score_entry(proposal, e) for e in entries]
    scored.sort(key=lambda r: -r["score"])

    top_results = scored[: args.top]
    conflict = any(r["score"] >= args.warn_threshold for r in top_results)

    near = args.warn_threshold - 0.10
    possible = any(args.warn_threshold > r["score"] >= near for r in top_results)
    if conflict:
        verdict_str = "likely_conflict"
    elif possible:
        verdict_str = "possible_conflict"
    else:
        verdict_str = "no_likely_conflict"

    if args.format == "json":
        print(json.dumps({
            "verdict": verdict_str,
            "threshold": args.warn_threshold,
            "candidates": top_results,
        }, indent=2))
    else:
        print(format_table(scored, args.top, args.warn_threshold))

    return 1 if conflict else 0


if __name__ == "__main__":
    raise SystemExit(main())
