# Layered Configuration Guide

Use this guide when defining or reviewing repository constraints across Global, Domain, and Project layers.

## Why this exists

A simple precedence statement is necessary but not sufficient. Teams also need clear placement criteria, deterministic conflict resolution, and maintenance hygiene to keep rules understandable over time.

## Layer responsibilities

| Layer | Owns | Should avoid |
|---|---|---|
| Global (`rules/global/`) | universal guardrails, baseline quality and safety | stack-specific implementation details |
| Domain (`rules/domain/`) | reusable constraints for a technical domain | repo-local deployment or legacy exceptions |
| Project (`project/project-manifest.md`) | repo-specific boundaries, operational constraints, exception handling | restating generic global/domain rules |

## Rule placement decision table

| Question | If yes | If no |
|---|---|---|
| Should this rule apply across nearly all repos? | Global | next question |
| Is this tied to one technical domain and reusable across repos in that domain? | Domain | next question |
| Is this specific to this repo/team/runtime/deployment context? | Project | Domain (fallback) |

## Resolution model

Use this sequence for every conflicting topic:

1. Project overrides Domain and Global.
2. Domain overrides Global.
3. Same-layer conflicts: choose narrower scope first.
4. If still tied: choose latest dated rule.
5. Document tie-break decisions in `DECISIONS.md`.

### Example

Global rule says "use REST for all APIs." Domain rule says "use gRPC for internal service communication." Project manifest says "legacy payment service uses SOAP; do not migrate."

Resolution: the payment service uses SOAP (project override); internal services use gRPC (domain override); all other APIs default to REST (global baseline).

## Governance checks

Run this checklist when updating layered rules:

1. No duplicate full-text rules across layers.
2. Every exception in Project points to the overridden base rule.
3. Superseded rules are explicitly marked as superseded.
4. Tool-specific instructions remain aligned with source-of-truth docs.
5. At least one concrete example exists for each active Domain profile.

## Anti-patterns

- Global layer contains framework-version constraints.
- Project manifest copy-pastes the entire global baseline.
- Domain layer mixes backend and frontend concerns into one broad file.
- Rule conflicts resolved implicitly without a recorded tie-break.

## Suggested maintenance cadence

- Per task: update layered files when constraints change.
- Weekly or every 10 tasks: run a quick conflict and duplication pass.
- Quarterly: prune obsolete domain profiles and superseded project exceptions.

## Stability dimension

Scope layers (Global / Domain / Project) and stability levels (`core` / `behavior` / `experimental`) are orthogonal. A rule in any scope layer can have any stability level.

### Scope × Stability matrix

| | `core` | `behavior` | `experimental` |
|---|---|---|---|
| **Global** | Safety rails, security baseline | Communication norms, output format | New prompt patterns |
| **Domain** | API compatibility contracts | Coding style, framework idioms | New tool integrations |
| **Project** | Auth model, data classification | Build commands, test conventions | Prototype workflows |

### Governance by stability

- `core` rules should change rarely. Propose changes through `risk-reviewer` and record in `DECISIONS.md`.
- `behavior` rules may change per sprint/cycle. Validate through the test-and-fix loop.
- `experimental` rules may change freely. Track in `DECISIONS.md` for auditability but do not require approval gates.

See `docs/operating-rules.md` → Rule stability classification for the full change protocol.

## Configuration file layering [OPTIONAL]

In addition to rule layering (Global / Domain / Project), the operational configuration file `prompt-budget.yml` supports an optional override chain for per-developer or per-environment customization.

### Override precedence

| Priority | Source | Location | Tracked in git? |
|----------|--------|----------|-----------------|
| 1 (highest) | Environment hint | `AGENT_BUDGET_PROFILE` env var | No |
| 2 | Local override | `prompt-budget.local.yml` (same directory) | No (add to `.gitignore`) |
| 3 (lowest) | Repository default | `prompt-budget.yml` | Yes |

### How agents apply overrides

1. Read `prompt-budget.yml` as the base configuration.
2. If `prompt-budget.local.yml` exists in the same directory, merge its keys on top (local wins on conflict).
3. If `AGENT_BUDGET_PROFILE` is set in the environment, use its value to override `budget.profile` (e.g., `AGENT_BUDGET_PROFILE=minimal`).
4. Proceed with the merged configuration.

This allows individual developers to set personal budget profiles or disable skills locally without modifying the shared repository config.

### Sensitive value masking

When agents read or log configuration values, apply these masking rules:

- Any YAML key containing `key`, `secret`, `token`, `password`, or `credential` (case-insensitive) must be masked in all output: display as `***MASKED***` instead of the actual value.
- This applies to: task summaries, trace files, handoff artifacts, decision log entries, and any agent-generated output.
- The masking rule is informational — agents enforce it by convention, not by code.

If your project stores sensitive configuration outside `prompt-budget.yml` (e.g., in environment variables or secret managers), document the key names in `project/project-manifest.md` → Security and compliance boundaries so agents know which values to never output.
