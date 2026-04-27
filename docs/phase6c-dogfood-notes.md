# Phase 6c Dogfood Notes

**Status**: template (to be filled during dogfood run)
**Date**: 2026-04-27
**Phase**: 6c (PR-1 through PR-4 merged)

These notes record the 7 dogfood steps designed to validate the Phase 6c feature set end-to-end. Each step has a setup, expected outcome, and space for observations.

---

## Step 1 — Role-dispatch happy path

**Goal**: Confirm role_dispatch execution works end-to-end from UI through connector.

**Setup**:
1. Start `anpm-server` and `anpm-connector serve` on a paired device.
2. Create a requirement, run planning, and approve a backlog candidate.
3. In CandidateReviewPanel, select execution_mode=role_dispatch and pick `backend-architect`.
4. Click Apply.

**Expected outcome**:
- Task created with `source = "role_dispatch:backend-architect"` and `dispatch_status = "queued"`.
- Connector claims the task (`dispatch_status → running`), invokes the CLI with the backend-architect prompt.
- Task completes with `dispatch_status = "completed"` and `execution_result.success = true`.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 2 — role_not_found error induction

**Goal**: Trigger `role_not_found` by applying with a role that no longer exists.

**Setup**:
1. Apply a candidate with `execution_role = "nonexistent-role"` via the PATCH API directly, bypassing UI enforcement.
   ```
   curl -X PATCH .../api/backlog-candidates/{id} -d '{"execution_role": "nonexistent-role"}'
   ```
2. Then apply that candidate with execution_mode=role_dispatch.

**Expected outcome**:
- Server returns 400 with "role not in catalog" message.
- If a task was somehow queued, connector marks it `dispatch_status = "failed"` with `error_kind = "role_not_found"`.
- Remediation hint points to the catalog.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 3 — dispatch_timeout induction

**Goal**: Trigger `dispatch_timeout` by running a long-running CLI with a short timeout.

**Setup**:
1. Set environment variable `ANPM_DISPATCH_TIMEOUT=10s` on the connector host.
2. Use a role that invokes a slow CLI (e.g., add `sleep 30` to the adapter command).
3. Apply a candidate with that role and wait for the task to be claimed.

**Expected outcome**:
- Task fails with `error_kind = "dispatch_timeout"` after 10s.
- `execution_result.error_kind = "dispatch_timeout"`.
- Remediation hint suggests checking `ANPM_DISPATCH_TIMEOUT`.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 4 — output_too_large induction

**Goal**: Trigger `output_too_large` by having the CLI print excessive output.

**Setup**:
1. Replace the adapter command with a script that prints ~6 MB to stdout:
   ```bash
   #!/bin/bash
   python3 -c "print('x' * 6000000)"
   ```
2. Apply a candidate with this role.

**Expected outcome**:
- Task fails with `error_kind = "output_too_large"`.
- Output is truncated; the raw cap value appears in the remediation hint.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 5 — invalid_result_schema induction

**Goal**: Trigger `invalid_result_schema` by having the CLI print malformed JSON.

**Setup**:
1. Replace the adapter command with a script that prints invalid JSON:
   ```bash
   #!/bin/bash
   echo "this is not json"
   ```
2. Apply a candidate with this role.

**Expected outcome**:
- Task fails with `error_kind = "invalid_result_schema"`.
- Remediation hint explains the expected JSON structure.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 6 — Router suggest button

**Goal**: Validate that the "💡 Suggest role" button works and the advisory UX is correct.

**Setup**:
1. Create a requirement with a clear technical description (e.g., "Implement the authentication middleware using JWT tokens with refresh logic").
2. Run planning to produce a backlog candidate.
3. In CandidateReviewPanel, select execution_mode=role_dispatch.
4. Click "💡 Suggest role" (without having selected a role first).

**Expected outcome**:
- The button shows a loading state during the LLM call.
- The dropdown pre-fills with the suggested role (e.g., `backend-architect`).
- A tooltip or inline section shows confidence (e.g., 85%), reasoning, and 1–2 alternatives.
- The save button remains required; the suggestion is not auto-applied.
- Clicking an alternative from the suggestion section sets the dropdown to that role.

**Variations to try**:
- Task with ambiguous description → expect low confidence or `no_match`.
- Task clearly matching `ui-scaffolder` vs `backend-architect`.

**Observations**:
<!-- Fill during dogfood -->

---

## Step 7 — ConnectorActivityBadge phase transitions

**Goal**: Observe the activity badge update in real time as the connector progresses through phases.

**Setup**:
1. Apply a candidate with execution_mode=role_dispatch.
2. Open the Planning tab and watch the active planning run card.
3. Observe the `ConnectorActivityBadge` as the connector transitions:
   - `idle` (before claim)
   - `claiming_run` (connector polling)
   - `planning` (if connector runs a planning phase)
   - `claiming_task` (claiming the dispatched task)
   - `dispatching` (CLI executing)
   - `submitting` (writing result back)
   - `idle` (after completion)

**Expected outcome**:
- Badge phase label updates within ~1s of each transition.
- Badge dims to "stale" if 90s pass without an update.
- Phase sequence matches expected flow (no phases skipped or repeated unexpectedly).
- After connector returns to idle, badge shows `● idle`.

**What to record**:
- Phase transition latency (from connector phase change to badge update).
- Any transitions that appear to be missing or duplicated.
- Whether SSE connection drops and polling fallback kicks in.

**Observations**:
<!-- Fill during dogfood -->

---

## Summary

| Step | Feature | Status | Notes |
|------|---------|--------|-------|
| 1 | Role-dispatch happy path | pending | |
| 2 | role_not_found induction | pending | |
| 3 | dispatch_timeout induction | pending | |
| 4 | output_too_large induction | pending | |
| 5 | invalid_result_schema induction | pending | |
| 6 | Router suggest button UX | pending | |
| 7 | Activity badge phase transitions | pending | |

## Phase 6d trigger signals to watch

While running these steps, note:
- Any case where the router suggests a role with high confidence but the suggestion is clearly wrong (→ Phase 6d adversarial corpus)
- Any activity phase transition that takes >2s to appear (→ Phase 6d coalesce tuning)
- Any case where the operator wishes the suggestion was auto-applied (→ Phase 6d auto-apply signal)
