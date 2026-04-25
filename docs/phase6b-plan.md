# Phase 6b 計畫 — 關閉執行迴圈（Role Dispatch）

**Status**: draft · 2026-04-25 · `[agent:feature-planner]`
**前置條件**: Phase 6a（PR #23, #24）已合併到 `main`。

---

## 1. 問題陳述

Phase 1 ~ 6a 完成了「要件 → Planning Run → 候補清單 → 審查 → Apply → Task」的前半迴圈。

Apply 時若選 `execution_mode: "role_dispatch"`，Task 的 `source` 被打上 `"role_dispatch:backend-architect"` 標記，但沒有任何程式去「執行」它。

Phase 5 DECISIONS.md 的明確約束：
> "Phase 6 MUST introduce catalog enforcement for execution_role before shipping auto-dispatch."
> "Phase 6 blocker: the dispatcher that consumes role prompts MUST treat {{TASK_DESCRIPTION}} and {{PROJECT_CONTEXT}} as untrusted."

**結論**：Phase 6b 必要，是因為目前 `role_dispatch` task 是一個只有標籤、沒有執行的死路。不關閉這個迴圈，「AI agents 直接參與開發流程」的核心價值主張就沒有完成。

---

## 2. End State

完成後的使用者操作路徑：
1. 在候補清單審查頁面，將某個候補設定 `execution_role = "backend-architect"`
2. Apply 時選擇 `execution_mode = "role_dispatch"`
3. Task 建立，source = `"role_dispatch:backend-architect"`
4. Connector 在輪詢 claim-next-run（planning run）之外，**額外輪詢** `claim-next-task`
5. Connector 拿到 task，讀取 `source` 得知角色，用 `prompts.Render("roles/backend-architect", vars)` 組 prompt
6. Connector 呼叫 Claude CLI 執行 prompt
7. Claude 回傳結構化 JSON（`files`, `test_instructions`, `risks`, `followups`）
8. Connector 把結果 POST 回伺服器
9. 伺服器將 task `dispatch_status` 更新為 `completed`，並寫入 `execution_result` + `agent_runs` 記錄
10. Task 卡片顯示執行結果（可展開的 result panel）
11. 人類審核後手動將 task 推進到 `done`

---

## 3. Slice 計畫

### Slice B1：DB schema（0.5 天）

**Scope**：Migration 029，在 `tasks` 表新增兩個欄位。

```sql
-- 029_task_dispatch.sql
ALTER TABLE tasks ADD COLUMN dispatch_status TEXT NOT NULL DEFAULT 'none';
ALTER TABLE tasks ADD COLUMN execution_result JSONB;
CREATE INDEX idx_tasks_dispatch_status ON tasks(dispatch_status);
```

`dispatch_status` 狀態機：`none` → `queued`（apply 時若 role_dispatch）→ `running`（claim 時）→ `completed`/`failed`

**DoD**：Migration 通過 SQLite + PostgreSQL 測試，現有 task row 的 `dispatch_status = 'none'`。

---

### Slice B2：後端 Task Dispatch 端點（1.5 天）

**新增端點**：

```
POST /api/connector/claim-next-task
POST /api/connector/tasks/:id/execution-result
```

**claim-next-task Response**：
```jsonc
{
  "task": {
    "id": "uuid",
    "title": "...",
    "description": "...",
    "source": "role_dispatch:backend-architect",
    "dispatch_status": "running"
  },
  "requirement": { "id": "uuid", "title": "...", "summary": "..." },
  "project_context": "<rendered PROJECT_CONTEXT string>",
  "cli_binding": { ... }  // connector primary cli_config snapshot
}
```

**execution-result Request**：
```jsonc
{
  "success": true,
  "error_message": "",
  "error_kind": "session_expired",
  "result": { "files": [...], "test_instructions": "...", "risks": [...], "followups": [...] }
}
```

**DoD**（9 test cases）：

| T-6b-B2-1 | claim：queue 為空 | 回傳 `{ task: null }` |
| T-6b-B2-2 | claim：有 queued task，connector 是 project member | task claimed，dispatch_status=running |
| T-6b-B2-3 | claim：connector user 不是 project member | 回傳空 |
| T-6b-B2-4 | claim：execution_role 不在 catalog | 回傳空（skip） |
| T-6b-B2-5 | execution-result success | dispatch_status=completed，execution_result 儲存 |
| T-6b-B2-6 | execution-result failure | dispatch_status=failed |
| T-6b-B2-7 | execution-result unknown error_kind | normalized to "unknown" |
| T-6b-B2-8 | execution-result：task 不屬於 connector user | 404 |
| T-6b-B2-9 | execution-result：task dispatch_status != "running" | 400 |

---

### Slice B3：Connector Task 執行 loop（2 天）

在 `connector/service.go` 的主 loop 中，新增 `RunOnceTask(ctx)` 方法：

1. 呼叫 `client.ClaimNextTask()`
2. 解析 `task.source` 得到 `role_id`（`"role_dispatch:backend-architect"` → `"backend-architect"`）
3. Catalog enforcement：`prompts.Exists("roles/" + roleID)`，若不存在則送失敗結果
4. 呼叫 `resolveBuiltinCLI()`（複用現有函式）
5. 建構 vars：`TASK_TITLE`, `TASK_DESCRIPTION`, `REQUIREMENT`, `PROJECT_CONTEXT`
6. 呼叫 `prompts.Render("roles/"+roleID, vars)`
7. 呼叫 `invokeBuiltinCLI()`（複用現有函式）
8. 呼叫 `extractJSONFromOutput()` 解析輸出
9. POST 回 `tasks/:id/execution-result`

**DoD**（6 test cases）：RunOnceTask 的各種 CLI 結果、catalog missing、JSON parse 失敗等路徑。

---

### Slice B4：前端 Task card 執行結果 panel（1 天）

- `GET /api/projects/:id/tasks` 回傳新欄位（`dispatch_status`, `execution_result`）
- TasksTab：
  - `dispatch_status = "running"` → "In progress" badge
  - `dispatch_status = "completed"` → result panel（files 清單、test_instructions、risks）
  - `dispatch_status = "failed"` → error message

---

### Slice B5：Apply UI 啟用 role_dispatch（0.5 天）

- `CandidateReviewPanel` 移除 role_dispatch radio 的 `disabled` 狀態
- 只有當 `execution_role` 有值且存在於 role catalog 時才可選
- Apply 後 task `dispatch_status = "queued"`

---

## 4. 實作順序

```
B1 → B2（handler + store）→ B5（apply 端）→ B3（connector）→ B4（前端 result panel）
```

---

## 5. 非目標（Non-Goals）

- Docker / sandbox（deferred）
- API key mode（只支援 local_connector）
- Per-task connector 選擇
- Real-time push（SSE/WebSocket）
- Role prompt 輸出的 server-side schema 驗證
- Task result 的 git 自動寫入
- Retry logic
- 新增 role prompt

---

## 6. 風險

| 風險 | 可能性 | 影響 | 緩解 |
|---|---|---|---|
| R1：PROJECT_CONTEXT 資訊密度不夠 | 中 | 中 | 重用 PlanningContextV1；Phase 6c 細化 |
| R2：claim-next-task ownership check 邏輯複雜 | 中 | 高 | 明確單元測試；參考 planning run ownership check |
| R3：role prompt 輸出各角色格式不一致 | 低 | 低 | server 儲存原始 JSONB，前端顯示通用欄位 |
| R4：connector 雙重 poll 負載 | 低 | 低 | RunOnceTask 只在 planning run idle 時才呼叫 |
| R5：TASK_DESCRIPTION prompt injection（Phase 5 D7 約束） | 低 | 高 | 明確記錄限制；不做 sandbox（本 phase） |

---

## 7. Open Questions

- Q1（阻擋 B3）：`PROJECT_CONTEXT` 變數的組裝方式 — 重用 `PlanningContextV1` 還是輕量 summary？建議：重用 PlanningContextV1。
- Q2：`dispatch_status = "queued"` 卡住時不做 expiry（Phase 6b 不做，人類手動 cancel）。
- Q3：cli_binding 使用 connector primary cli_config（不支援 per-task 選擇）。
- Q4：catalog enforcement 兩側都做（server skip + connector Exists 檢查）。

---

## 8. 狀態追蹤

| Slice | 狀態 | PR |
|---|---|---|
| B1 — DB migration 029 | pending | — |
| B2 — API endpoints | pending | — |
| B3 — Connector loop | pending | — |
| B4 — 前端 result panel | pending | — |
| B5 — Apply UI 啟用 | pending | — |
