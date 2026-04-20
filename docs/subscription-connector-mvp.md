# Subscription Connector MVP

**Status**: Proposed  
**Date**: 2026-04-17  
**Owner**: [agent:documentation-architect]  
**Inputs**: [agent:feature-planner], [agent:risk-reviewer]  

---

## 1. Goal

本文件定義一條可落地的 subscription path MVP，讓單人、自架、無 API key 的使用者，可以在現有 planning workflow 中選擇「在本機執行」，由本機 connector 代跑 planning，再把結果回寫到既有的 planning domain。

MVP 成功標準：

1. 使用者可以在 Web 中配對自己的機器。
2. 使用者可以把 planning run 指派到本機 connector。
3. connector 可以用固定 contract 呼叫本機 adapter，並把 draft candidates 回寫給 server。
4. 現有 requirement → planning run → candidate review → apply 流程不需要重寫。

---

## 2. Non-Goals

以下內容不在 MVP：

1. 直接重用 GitHub Copilot / ChatGPT Web / VS Code subscription session。
2. 多使用者 connector pool。
3. 多 connector 排程與 capability-aware routing。
4. WebSocket push；MVP 用 polling。
5. 串流 logs、細粒度 timeline、背景事件匯流排。
6. 把 deterministic 或 server-callable OpenAI-compatible 模式移除。

---

## 3. Architecture Split

### Server

Server 保持 control plane：

- `requirements`
- `planning_runs`
- `backlog_candidates`
- audit / `agent_runs`
- connector pairing
- connector registry
- local run dispatch queue
- result validation and persistence

Server 不直接持有或重用 subscription token。

### Frontend

Frontend 提供：

- My Connector pairing flow
- connector status page
- execution source selection
- local-run status disclosure

Frontend 必須明確告知：subscription 不會被 server 直接使用。

### Local Connector

Connector 是新的 execution plane，跑在使用者本機，負責：

- connector 身分驗證
- heartbeat
- claim 下一個 local planning run
- 透過固定 `exec-json` adapter contract 呼叫本機命令
- 回傳 `draft candidates` 或標準化錯誤

Connector 可以連接任何本機可控的模型入口，但 MVP 不承諾任何特定 vendor subscription 一定可用。

---

## 4. Execution Model

新增 execution path 概念，但不重寫現有 planning status machine。

### Supported execution modes

1. `deterministic`
2. `server_provider`
3. `local_connector`

### Planning run lifecycle

既有 `status` 保持：

- `queued`
- `running`
- `completed`
- `failed`

新增 dispatch lifecycle：

- `not_required`
- `queued`
- `leased`
- `returned`
- `expired`

---

## 5. Data Model Changes

### New table: `local_connectors`

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | Connector ID |
| `user_id` | TEXT FK -> users.id | Owner |
| `label` | TEXT | User-facing device label |
| `platform` | TEXT | linux / macos / windows |
| `client_version` | TEXT | Connector version |
| `status` | TEXT | `pending`, `online`, `offline`, `revoked` |
| `capabilities` | JSONB | Adapter and runtime info |
| `token_hash` | TEXT | Hashed connector token |
| `last_seen_at` | TIMESTAMPTZ | Latest heartbeat |
| `last_error` | TEXT | Last connector-level error |
| `created_at` | TIMESTAMPTZ | Audit |
| `updated_at` | TIMESTAMPTZ | Audit |

### New table: `connector_pairing_sessions`

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | Pairing session ID |
| `user_id` | TEXT FK -> users.id | Owner |
| `pairing_code_hash` | TEXT | Never store plaintext code |
| `label` | TEXT | Optional target device label |
| `status` | TEXT | `pending`, `claimed`, `expired`, `cancelled` |
| `expires_at` | TIMESTAMPTZ | Short TTL |
| `connector_id` | TEXT FK -> local_connectors.id | Claimed connector |
| `created_at` | TIMESTAMPTZ | Audit |
| `updated_at` | TIMESTAMPTZ | Audit |

### Extend `planning_runs`

Add columns:

- `execution_mode`
- `dispatch_status`
- `connector_id` nullable
- `connector_label`
- `lease_expires_at` nullable
- `dispatch_error`

Rules:

1. `execution_mode=deterministic` or `server_provider` => `dispatch_status=not_required`
2. `execution_mode=local_connector` => run enters queued dispatch path before completion

---

## 6. API Surface Additions

### User-facing APIs

1. `POST /api/me/local-connectors/pairing-sessions`
   Create one short-lived pairing session and return pairing code.

2. `GET /api/me/local-connectors`
   Return current user's connector status summary.

3. `DELETE /api/me/local-connectors/:id`
   Revoke one connector.

### Connector-facing APIs

1. `POST /api/connector/pair`
   Claim pairing session and exchange pairing code for connector token.

2. `POST /api/connector/heartbeat`
   Refresh `last_seen_at`, update capabilities, surface latest connector error.

3. `POST /api/connector/claim-next-run`
   Lease one pending `local_connector` planning run.

4. `POST /api/connector/planning-runs/:id/result`
   Return success or failure for one leased run.

### Existing API changes

1. `POST /api/requirements/:id/planning-runs`
   Add `execution_mode` field.

2. `GET /api/projects/:id/planning-provider-options`
   Add:
   - `available_execution_modes`
   - `paired_connector_available`
   - `active_connector_label`

---

## 7. Security And Control Rules

1. Pairing code must be single-use and short-lived.
2. Pairing session must be bound to the logged-in user.
3. Pairing completion should immediately invalidate the pairing code.
4. Connector token and pairing code are different credentials.
5. Store token hashes only.
6. `claim-next-run` and result callback must use idempotency and lease fencing.
7. Reject stale callbacks after lease expiry.
8. Connector token may only call connector endpoints.

### `exec-json` adapter guardrails

1. No shell string concatenation.
2. Fixed executable path + validated args schema.
3. Input over stdin JSON only.
4. Output over stdout JSON only.
5. Timeout, output size limit, and working-directory restriction required.

---

## 8. Frontend Additions

### My Connector page

Add a dedicated page adjacent to My Bindings:

- status: `unpaired`, `pairing`, `online`, `offline`, `revoked`
- pairing code generation
- last seen
- active connector label
- last error
- revoke / replace action

### Model Settings

Add `Execution Source` as the primary concept:

1. `Run on server`
2. `Run on this machine`
3. keep advanced provider settings below

### Project Detail / Planning tab

Add:

- execution mode selection
- connector availability badge
- local dispatch status badge
- clearer limitation message when no connector exists

### Wording priorities

1. Use “在這台機器執行” before talking about provider details.
2. Explicitly say “伺服器不會直接使用你的訂閱登入”.
3. When local path fails, show connector offline / lease expired / adapter error before generic planning failure.

---

## 9. Connector Runtime Responsibilities

### Minimum commands

1. `anpm-connector pair --server <url> --code <pairing-code>`
2. `anpm-connector serve`
3. `anpm-connector doctor`
4. `anpm-connector disconnect`

### Current implementation status

Implemented now:

1. `anpm-connector pair`
2. `anpm-connector doctor`
3. `anpm-connector serve`
4. local state persistence with a `0600` config file
5. one `exec-json` adapter runner with timeout and output limits

Still deferred:

1. `disconnect`
2. lease renewal during long-running execution
3. vendor-specific subscription adapters
4. multi-connector routing or richer reconnect orchestration

### Minimum responsibilities

1. Save connector token locally.
2. Heartbeat on interval.
3. Poll for one pending run.
4. Invoke local adapter with requirement + planning context JSON.
5. Return normalized candidates or normalized error.

### Adapter contract

MVP supports one adapter type: `exec-json`.

Input JSON:

- requirement
- run
- requested max candidates

Output JSON:

- candidates[]
- optional error

Current concrete shape:

```json
{
   "run": {
      "id": "planning-run-id",
      "project_id": "project-id",
      "requirement_id": "requirement-id"
   },
   "requirement": {
      "id": "requirement-id",
      "project_id": "project-id",
      "title": "Improve sync recovery UX",
      "summary": "...",
      "description": "..."
   },
   "requested_max_candidates": 3
}
```

Adapter stdout must be valid JSON:

```json
{
   "candidates": [
      {
         "title": "Candidate title",
         "description": "Candidate description",
         "rationale": "Why this is recommended",
         "suggestion_type": "implementation"
      }
   ]
}
```

Or:

```json
{
   "error_message": "local planner failed to produce usable output"
}
```

This keeps the connector plane implementable before any vendor-specific subscription adapter exists.

---

## 10. State Machine Notes

### Pairing

`pending -> claimed -> active`  
`pending -> expired`  
`active -> revoked`

### Run dispatch

`queued(local_connector)` -> `leased` -> `returned` -> `completed`  
`queued(local_connector)` -> `leased` -> `failed`  
`leased` -> `expired` -> `queued(local_connector)`

---

## 11. Implementation Order

### Batch 1 — Pairing And Registry

1. migrations for `local_connectors` and `connector_pairing_sessions`
2. models + stores
3. pairing session API
4. connector token auth middleware
5. My Connector status page

### Batch 2 — Dispatch And Connector Runtime

1. `execution_mode=local_connector`
2. planning run queue / lease / result callback
3. `anpm-connector` binary skeleton
4. `exec-json` adapter contract
5. Planning tab local-run UX

### Batch 3 — Hardening

1. lease expiry and stale callback rejection
2. offline / reconnect UX
3. audit trail enrichment
4. docs updates
5. focused integration tests

---

## 12. Scope Cuts To Keep MVP Realistic

1. Only support one active connector per user.
2. Do not build connector list management first; support replace / revoke only.
3. Do not add WebSocket or streaming logs.
4. Do not promise vendor-specific subscription adapters in MVP.
5. Do not add per-run timeline UI; badge + last error is enough.

---

## 13. Open Risks

1. Some subscription clients may not expose stable local automation surfaces.
2. Vendor ToS may restrict automated usage even if a local session exists.
3. `exec-json` adapters can become a local RCE surface without strict validation.
4. Lease expiry and retry semantics must be precise or runs will get stuck.

Until those risks are resolved, this design should be treated as an implementation-ready MVP proposal, not a guaranteed vendor bridge.

Source: [agent:documentation-architect]