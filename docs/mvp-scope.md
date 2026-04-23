# Scope Pointer

> ⚠️ **The original Phase 1 MVP is shipped.** Phase 2 (Planning Workspace UX consolidation) and several quick-wins passes have also shipped on `main`. This document used to hold the Phase 1 in/out scope list; that list became misleading once implementation moved past it, so it has been removed.

## Where current scope lives

When an agent or human needs to evaluate **whether a feature belongs in the project right now**, read these files instead — in this order:

1. **`docs/product-blueprint.md`** — vision, target users, non-goals, phase roadmap. The non-goals list is the strongest signal that something is out of scope at any phase.
2. **`DECISIONS.md`** — active architectural and behavioural constraints. New features must not contradict an entry here. Use `make decisions-conflict-check TEXT="proposed decision..."` before planning a major change.
3. **`docs/api-surface.md`** — current REST contract. Confirms what is already implemented vs what would be a net-new endpoint.
4. **`docs/data-model.md`** — current data layer. Confirms which entities and tables already exist.
5. **`ARCHITECTURE.md`** — module responsibilities and the explicitly-deferred work list under "Near-Term Architectural Direction".

## Historical record

The original Phase 1 checklist and acceptance criteria are preserved in git history. Use `git log -- docs/mvp-scope.md` if you need to inspect the original Phase 1 baseline document.

The reasons each phase was framed (and the order Phase 1 → 4 was originally drawn) are still summarised in `docs/product-blueprint.md` → "Phase roadmap".

## Out-of-scope guidance shortcut

A change is **out of scope right now** if any of the following is true:

- It contradicts a non-goal in `docs/product-blueprint.md` (multi-tenancy, plugin marketplace, mobile-native apps, real-time collaborative editing, full Jira parity).
- It contradicts an active entry in `DECISIONS.md` without a supersession path.
- It would require a new top-level subsystem that does not appear in `ARCHITECTURE.md` and was not requested by the user as net-new scope.

When in doubt, surface the doubt explicitly to the user before implementing.
