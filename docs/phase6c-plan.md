# Phase 6c 計畫 — Catalog SoT + Authoring 完整化 + LLM Router + Activity Visibility

**Status**: draft v5.1 (post-critic-round-3, B2 + C1 拍板) · 2026-04-25 · `[agent:feature-planner]`
**前置條件**: Phase 6b（PR #25）已合併到 `main`；PR-1（catalog skeleton + L0 safety boundary）已實作完成、待開 PR。
**來源**: 由 dogfood Phase 6b 的 What's Next 規劃流程產出（candidates `bad629dc` + `fb040ce6`），加上後續設計 review 衍生的 authoring catch-22 修正、LLM router 智能層、connector activity visibility 三項。

**設計原則（per user feedback `feedback_no_simple_approach`）**: 不取簡單路徑、不為 single-operator dogfood scope 妥協 — 設計目標是「未來會用到的東西現在就做對」。

**演進歷史**:
- v1：catalog SoT + L0 safety boundary（單一 PR）
- v2：critic findings 整合（拆 3 PR）
- v3：per-role timeout（Role.DefaultTimeoutSec）+ critic round 2 findings
- v4：authoring catch-22 修正 + LLM router 設計（4 PR）
- v5：connector activity tracking + SSE visibility（5 PR）
- **v5.1（當前）**：critic round 3 拍板 — actor_audit 為 SoT（drop 重複欄位）；PR-3 縮 scope 為 suggest-only，role_dispatch_auto 延 6d；PR 間 hard deps 解耦；router adversarial corpus 強化；activity 不寫 audit；dispatcher 移到 `prompts/meta/`

---

## 1. 問題陳述

Phase 6b 完成了 role-dispatch 的 backend + 部分 UI，但**整條路徑無法從 UI 走通**，加上一些 Phase 5 / 6b 合計留下的設計缺口：

### 1.1 三個獨立但互鎖的 gap

**Gap 1：Catalog 與安全邊界（PR-1 已修）**
- Role catalog 三處不同步、無 enforcement
- Subprocess 執行無 wall-clock timeout / output cap / schema validation
→ PR-1 已實作完成（catalog skeleton + L0 safety boundary）

**Gap 2：Authoring catch-22 與不完整生命週期**
- `execution_role` 是 candidate 上的 nullable 欄位但**沒任何 UI 可以設**
- `role_dispatch` radio 用 `execution_role` 是否存在 enable，但既然沒人能設 → 永遠 disabled
- Phase 5 §(d) 標 "Phase 6 必做 catalog enforcement" 但只在 connector 端做了（既有 `prompts.Exists`）；server-side、frontend、apply API 都沒做
- `execution_role` 沒有 audit trail（誰在何時設、是 operator 還是 router 設）
→ PR-2 解這個

**Gap 3：智能層完全空白**
- 沒有 model-based 的 task → role 路由建議
- operator 每次手動評估 6 個 role 哪個適合
- Phase 5 prompts 只有 6 個 role，沒有 meta-agent 層
→ PR-3 解這個（**6c 只做 suggest，不做 auto-apply**）

> **Critic round 3 約束**：v5 原本把 `mode=role_dispatch_auto` 也納入 PR-3。Critic 指出 router 品質尚未經 dogfood 驗證、直接做 auto-apply 是 premature optimization；user 拍板 **B2** = 6c 只做 suggest（advisory），auto-apply 模式延到 PR-6 / Phase 6d，等 PR-5 dogfood 累積 router 信心數據後再決定。

**Gap 4：執行黑盒子**
- Connector 跑長任務（backend-architect 90 min）時 frontend 完全看不到「正在做什麼」
- Task `dispatch_status` 只有 queued/running/completed/failed，沒有「正在 routing」「正在跑 CLI」「正在解析」這種 phase 訊號
- Dogfood 時無法區分 task 卡住的原因（網路慢？CLI 凍住？server 沒收到？）
→ PR-4 解這個

### 1.2 為什麼這些必須一起在 6c 完成

各別都可以「之後再做」，但合在一起看才是完整的 dogfood-ready story：
- 沒 PR-2 → 仍卡 catch-22，UI 上無法用
- 沒 PR-3 → operator 每次手動選 role，違背「agent 自主執行」核心價值
- 沒 PR-4 → dogfood 是黑盒、無法 debug、無法給 PR-5 dogfood 提供觀察依據
- 沒 PR-1（已修）→ 即使前面都做，安全保證不足

**結論**：6c 的 4 個 PR 是**一個完整能力**的不同切面，分批 ship 但不可省略任何一片。

---

## 2. End State

完成全部 5 PR 後可驗證行為：

### 2.1 Authoring（PR-2 完整）

1. Operator 可在 candidate 卡片**直接編輯** execution_role（`<select>` 從 catalog 拉，inline edit popover）
2. Operator 可在 apply panel **at apply time** 設 / 改 execution_role（pre-fill 自 candidate latest audit row）
3. Apply payload 帶 `execution_role`；server 在 4 個進入點做 catalog enforcement（PATCH / suggest / apply / claim-next-task）
4. Stale role（candidate 既有 role 但已不在 catalog）顯示 inline warning + 預設清空 dropdown
5. 所有 execution_role 變更走 `actor_audit` table，actor_kind ∈ {user, router, system}，含 rationale + timestamp。**Audit 是唯一的 set_by/at/confidence SoT**（critic #1 — 不在 candidate 上重複欄位；frontend 顯示時走 audit JOIN）
6. `MarkTaskRoleNotFound` 在 claim-next-task 時把 stale-role task `queued → failed` 原子轉移

### 2.2 LLM Router — Suggest-only（PR-3）

7. 新 prompt `prompts/meta/dispatcher.md`（category=meta），輸出 `{role_id, confidence, reasoning, alternatives[]}`（critic #10 — 放 `meta/` 子目錄，不和 `roles/` 並列也不和 backlog/whatsnext 並列）
8. `POST /api/backlog-candidates/:id/suggest-role` endpoint：呼叫 router、回傳結果**不持久化**
9. Apply panel + Candidate card 都加 "💡 Suggest" 按鈕：呼叫 router → 預填 dropdown + tooltip 顯示 reasoning + alternatives
10. Router 呼叫重用 PR-1 的 invokeBuiltinCLI（含 timeout / output cap / signal escalation）；server 端在 process 內呼叫（單機假設，文件化）
11. Router timeout 來自 catalog（dispatcher role default 60s）
12. 1 個新 error_kind：`router_no_match`（router 自己判斷沒匹配）+ 既有 PR-1 kinds 涵蓋其他失敗（output_too_large / dispatch_timeout / invalid_result_schema）
13. **Auto-apply mode（`mode=role_dispatch_auto`）延到 Phase 6d**（critic #2 / user 拍板 B2）— 待 PR-5 dogfood 收集 router 品質訊號後再決定

### 2.3 Activity Visibility（PR-4 完整）

14. Connector 在每個 phase 邊界（idle / claiming_run / planning / claiming_task / dispatching / submitting）呼叫 `ActivityReporter.Report`。**Phase 變化用 enqueue 不是 overwrite**（critic #5）— 確保連續 phase 切換 `claiming_task → dispatching → submitting` 都會被推送，即使在 coalesce 視窗內。`routing` phase **延到 Phase 6d**（auto-apply 上線後才需要）。
15. `POST /api/connector/activity` lightweight endpoint 接收上報；server-side activity hub 維護 in-memory state + DB snapshot 欄位（不寫 actor_audit — critic #8，避免 write storm）
16. `GET /api/connectors/:id/activity-stream` SSE 推送即時 activity 變化；polling fallback `GET /api/connectors/:id/activity`（C1 拍板：保留 SSE）
17. Frontend `useConnectorActivity` hook：SSE 為主、polling 為輔、reconnect 邏輯、stale 偵測
18. `ConnectorActivityBadge` 3 種 density（compact / standard / full）；整合進 PlanningTab、TasksTab、CandidateReviewPanel apply 後 watch
19. `GET /api/projects/:id/active-connectors` project-level aggregate

### 2.4 Dogfood + Docs（PR-5 完整）

20. `docs/phase6c-dogfood-notes.md`：7 個 dogfood 步驟（5 個原 v3 觸發新 error_kind + 2 個 v5.1 觀察 router suggest 與 activity badge 切換；auto-apply / PhaseRouting 預覽留 6d）
21. `docs/operating-rules.md` 新「Role-dispatch safety + visibility model」一節，含 L0 / L1 / L2 觸發條件 + activity model 約束
22. DECISIONS.md 補完 Phase 6c 條目（涵蓋 v5 全部設計）

---

## 3. Slice 計畫（5 PR）

### 3.1 PR-1：Catalog skeleton + L0 safety boundary（**已實作完成**）

詳見 v3 plan 內容（保留）：
- `backend/internal/roles/catalog.go`（Role struct + 6 entries + DefaultTimeoutSec + IsKnown / ByID / TimeoutFor / All）
- `backend/internal/roles/catalog_test.go`（drift detector + 9 tests）
- `backend/internal/connector/dispatch_safety.go`（boundedWriter + signal escalation + validateExecutionResult）
- `backend/internal/connector/dispatch_safety_test.go`（11 dispatch + 4 unit + 1 timeout-truncation precedence test）
- `invokeBuiltinCLI` 簽名擴 `(string, bool, string)`（+ truncated）
- `RunOnceTask` 用 `roles.TimeoutFor` + truncation/runErr precedence + schema validation + classifyDispatchRunError
- 4 個新 error_kind（dispatch_timeout / output_too_large / invalid_result_schema / role_not_found）

**Critic round 2 修正**：
- TestMain 雙 sentinel guard（避免 user shell env 誤觸）
- ExecuteBuiltin truncation 補 ErrorKindOutputTooLarge
- 移除 redundant resolveAgentFromBinary
- runErr-over-truncated precedence + 對應 test
- boundedWriter 改 atomic.Int64（H2 防禦）
- Codex PTY io.Copy goroutine + ptmx.Close 序列（H1 修正）
- SIGTERM-ignore test slack 5s（M1 防 CI flake）
- TimeoutFor whitespace env test（L8）

**Status**: 待開 PR；plan v5 確認後一起開 PR-1。

---

### 3.2 PR-2：Authoring 完整化 + audit log + multi-point catalog enforcement（4.6 天）

#### 3.2.1 Migration 030

```sql
-- 030_authoring_audit.sql

-- 通用 actor_audit 表 — 是 execution_role 的 single source of truth
-- (critic #1：不在 candidate 上加重複欄位)
CREATE TABLE actor_audit (
    id TEXT PRIMARY KEY,
    subject_kind TEXT NOT NULL,   -- 'backlog_candidate' | 'task' | 'planning_run' | 'connector'
    subject_id TEXT NOT NULL,
    field TEXT NOT NULL,           -- 'execution_role' | 'status' | 'po_decision' | ...
    old_value TEXT,
    new_value TEXT,
    actor_kind TEXT NOT NULL,      -- 'user' | 'router' | 'system' | 'connector'
                                   -- 'router' is reserved for Phase 6d auto-apply;
                                   -- NO writer in 6c (PR-3 suggest writes 'user' after operator confirms)
    actor_id TEXT,                 -- user_id | router prompt version | system component name
    rationale TEXT,                -- router confidence + reasoning，or system reason
    confidence REAL,               -- 0.0-1.0；only set when actor_kind='router'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_actor_audit_subject ON actor_audit(subject_kind, subject_id, created_at DESC);
CREATE INDEX idx_actor_audit_subject_field ON actor_audit(subject_kind, subject_id, field, created_at DESC);
```

`backlog_candidates.execution_role` 既有欄位**保留**（v3 Phase 5 已加，是當前 task source 的 input）。但「誰設的、何時設、信心多少」一律從 `actor_audit` 查 — 不在 candidate row 上重複欄位。

**Helper 函式**：`backend/internal/audit/audit.go` 提供 `LatestAuthoring(subjectKind, subjectID, field)` 回傳最新一筆 audit row（with actor_kind/at/confidence/rationale）。Frontend `GET /api/backlog-candidates/:id` response 加 `execution_role_authoring` 欄位（透過此 helper 回填，非 column）。

#### 3.2.2 Backend changes

**Store 層**：
```go
// backlog_candidate_store.go
func (s *Store) UpdateExecutionRole(
    ctx context.Context, id, role string, actor ActorInfo,
) error
// 單一 transaction：
//   1. 驗 role 在 catalog（roles.IsKnown）— role="" 視為 clear，不需 catalog 檢查
//   2. SELECT old_value（for audit）
//   3. UPDATE candidate.execution_role
//   4. INSERT actor_audit row（含 actor_kind/actor_id/rationale/confidence）
//   5. COMMIT

// 既有 ApplyToTaskWithMode 簽名擴：
func (s *Store) ApplyToTaskWithMode(
    id, executionMode, executionRole string, actor ActorInfo,
) (*ApplyResult, error)
// 內部：
//   - mode=role_dispatch && role 空 → ErrApplyMissingRole
//   - mode=role_dispatch && !roles.IsKnown(role) → ErrApplyUnknownRole
//   - mode=manual → ignore role
//   - 同 transaction 寫 candidate.execution_role (若有變) + audit + create task
```

**Handler 層**：
```go
// PATCH /api/backlog-candidates/:id 擴：accept execution_role 欄位
type UpdateBacklogCandidateRequest struct {
    POdecision      *string `json:"po_decision,omitempty"`
    ExecutionRole   *string `json:"execution_role,omitempty"`  // pointer = explicitly set/clear vs not-mentioned
}

// POST /api/backlog-candidates/:id/apply 擴：
type ApplyBacklogCandidateRequest struct {
    ExecutionMode string `json:"execution_mode"`
    ExecutionRole string `json:"execution_role,omitempty"`
}
```

Validation:
- mode=`role_dispatch` + role empty → 400 `"execution_role required when execution_mode=role_dispatch"`
- mode=`role_dispatch` + role 不在 catalog → 400 with current catalog list
- mode=`manual` → ignore role
- mode=`role_dispatch_auto` → PR-3 處理

**`MarkTaskRoleNotFound` + claim-next-task enforcement**（從 v3 帶過來）：
```go
func (s *TaskStore) MarkTaskRoleNotFound(
    ctx, taskID, roleID string,
) error
// 條件 update：dispatch_status='queued' → 'failed'
// 同 transaction 寫 execution_result {success:false, error_kind:'role_not_found'}
// + actor_audit row（actor_kind='system'）
// 若 task 已被 lease（status=running）→ 0 rows → ErrTaskNotInQueuedState
```

```go
// connector_dispatch.go ClaimNextTask 加：
roleID := parseRoleIDFromSource(task.Source)
if !roles.IsKnown(roleID) {
    if err := store.MarkTaskRoleNotFound(ctx, task.ID, roleID); err != nil {
        log.Printf("mark role_not_found failed: %v", err)
    }
    continue  // 看下一個 task
}
```

**`GET /api/roles`**（公開）：
```go
type RoleResponse struct {
    ID                string `json:"id"`
    Title             string `json:"title"`
    Version           int    `json:"version"`
    UseCase           string `json:"use_case"`
    DefaultTimeoutSec int    `json:"default_timeout_sec"`
    Category          string `json:"category"`  // "role" | "meta"
}

func (h *Handler) ListRoles(w, r) {
    roles := roles.All()
    // filter category="role" — meta-roles (dispatcher) 不暴露給 apply panel
    out := []RoleResponse{}
    for _, r := range roles {
        if r.Category == "role" {
            out = append(out, toResponse(r))
        }
    }
    writeJSON(w, 200, out)
}
```

⚠️ 需要在 `roles/catalog.go` 加 `Role.Category` 欄位（之前 v3 沒有）— PR-2 順便加。dispatcher role（PR-3 加）會用 `Category: "meta"`。

#### 3.2.3 Frontend changes

**新檔 `frontend/src/types/roles.ts`**:
```typescript
export const KNOWN_ROLE_IDS = [
  'backend-architect',
  'ui-scaffolder',
  'db-schema-designer',
  'api-contract-writer',
  'test-writer',
  'code-reviewer',
] as const;
export type KnownRoleId = typeof KNOWN_ROLE_IDS[number];

export interface RoleInfo {
  id: KnownRoleId;
  title: string;
  version: number;
  use_case: string;
  default_timeout_sec: number;
  category: 'role' | 'meta';
}
```

**新檔 `frontend/src/api/roles.ts`**:
```typescript
export async function listRoles(): Promise<RoleInfo[]>
export async function suggestRoleForCandidate(candidateID: string): Promise<RouterResult>  // PR-3
```

**Drift test** `roles.test.ts`：fetch `/api/roles` → assert id 集合 = `KNOWN_ROLE_IDS`。

**CandidateReviewPanel 重寫 execution-mode UI**：
```tsx
// Radio 永遠 enabled（不看 candidate.execution_role）
const [chosenRole, setChosenRole] = useState(candidateInitialRole)
const candidateRole = selectedCandidate?.execution_role
const roleStaleWarning = candidateRole && !KNOWN_ROLE_IDS.includes(candidateRole)

<input type="radio" name="execution-mode"
  checked={selectedExecutionMode === 'role_dispatch'}
  onChange={() => onSelectedExecutionModeChange('role_dispatch')} />

{selectedExecutionMode === 'role_dispatch' && (
  <>
    <select value={chosenRole} onChange={e => setChosenRole(e.target.value)}>
      <option value="">— 選擇角色 —</option>
      {roles.map(r => (
        <option key={r.id} value={r.id} title={r.use_case}>
          {r.title} (v{r.version}) — 預估 {Math.round(r.default_timeout_sec/60)} 分鐘
        </option>
      ))}
    </select>
    {roleStaleWarning && (
      <div className="warning-inline">
        ⚠ Previously suggested role <code>{candidateRole}</code> is no longer in the catalog.
      </div>
    )}
    {/* Suggest 按鈕 在 PR-3 加 */}
  </>
)}

// Apply button disabled 條件加：
// (selectedExecutionMode === 'role_dispatch' && !chosenRole)
```

**新 component `CandidateRoleEditor.tsx`**（在 candidate card 上）：
```tsx
<div className="candidate-role-editor">
  {candidate.execution_role ? (
    <span className="role-chip" title={`Set by ${candidate.execution_role_set_by} at ${candidate.execution_role_set_at}`}>
      [{role.title}]
    </span>
  ) : (
    <span className="role-empty">— no role set —</span>
  )}
  <button onClick={openEditor}>edit</button>
  {/* popover with role <select> */}
</div>
```

#### 3.2.4 Test 矩陣（28 tests）

| ID | Layer | 案例 | 期望 |
|---|---|---|---|
| **Backend store / handler** |
| T-6c-C1-A1 | apply API | mode=role_dispatch + role 空 | 400 |
| T-6c-C1-A2 | apply API | mode=role_dispatch + role 不在 catalog | 400 |
| T-6c-C1-A3 | apply API | mode=role_dispatch + 合法 role | 201, source=`role_dispatch:X`, candidate 寫回, audit 有 row |
| T-6c-C1-A4 | apply API | mode=manual + role 任意 | 201, ignore role, no audit row for role |
| T-6c-C1-A5 | apply API | mode=role_dispatch（舊 client 不帶 role） | 400 |
| T-6c-C1-A6 | apply API | apply 兩次 idempotent | 既有 behavior 不變 |
| T-6c-C1-P1 | PATCH API | UpdateExecutionRole 設合法 role | 200, set_by='operator', set_at, audit row |
| T-6c-C1-P2 | PATCH API | UpdateExecutionRole 不合法 role | 400, no DB change |
| T-6c-C1-P3 | PATCH API | UpdateExecutionRole 清空 (`""`) | 200, set_by='', set_at NULL, audit row 記錄 clear |
| T-6c-C1-P4 | PATCH API | concurrent PATCH × 2 | 第二個 update 看到第一個的 commit；audit 有兩 rows |
| T-6c-C1-S1 | source parsing | `parseRoleIDFromSource("role_dispatch:")` | `""` |
| T-6c-C1-S2 | source parsing | `parseRoleIDFromSource("role_dispatch:backend-architect")` | `"backend-architect"` |
| T-6c-C1-S3 | claim API | source=`role_dispatch:nonexistent` | task → failed, error_kind=role_not_found, claim 回 null |
| T-6c-C1-S4 | claim API | source=`role_dispatch:` | 同 S3 |
| T-6c-C1-S5 | store | `MarkTaskRoleNotFound` 對 status=running | 0 rows, ErrTaskNotInQueuedState |
| T-6c-C1-S6 | store | `MarkTaskRoleNotFound` 對 status=queued | queued → failed, audit row, execution_result 寫入 |
| T-6c-C1-E1 | API | `GET /api/roles` | 200, 6 roles, category='role' only, 含 default_timeout_sec |
| T-6c-C1-E2 | API | `GET /api/roles` 不回 dispatcher (category='meta') | dispatcher not in response |
| T-6c-C1-AU1 | audit | actor_audit 寫入後查詢 by subject_id | rows in correct order |
| T-6c-C1-AU2 | audit | actor_audit cascade delete with candidate | rows gone |
| **Frontend** |
| T-6c-C1-F1 | UI | role_dispatch radio 永遠 enabled | pass |
| T-6c-C1-F2 | UI | 選 role_dispatch → select 出現 | pass |
| T-6c-C1-F3 | UI | select 含 6 個 role + 預估時間 + use_case tooltip | pass |
| T-6c-C1-F4 | UI | role_dispatch + 未選 role → Apply disabled | pass |
| T-6c-C1-F5 | UI | Apply payload 含 execution_role | pass |
| T-6c-C1-F6 | UI | candidate.execution_role 在 catalog → select 預選 | pass |
| T-6c-C1-F7 | UI | candidate.execution_role 不在 catalog → 顯示 warning + select 預設空 | pass |
| T-6c-C1-F8 | UI | CandidateRoleEditor PATCH success | role chip 更新 |
| T-6c-C1-X1 | drift | `roles.test.ts` /api/roles vs KNOWN_ROLE_IDS diff | pass |

**DoD**：28 個 test 全綠；race detector 綠；`make pre-pr` 綠；critic + security + risk review 全過。

---

### 3.3 PR-3：LLM Router — Suggest only（2.0 天，B2 後縮減）

> **B2 拍板後縮減**：v5 原本含 `mode=role_dispatch_auto` + 422 modal + min_confidence + auto-apply 路徑。Critic round 3 + user 拍板 B2 後，這些都延到 PR-6 / Phase 6d。PR-3 只做 advisory suggest — operator 看完仍要手動 confirm。

#### 3.3.1 Catalog 加 dispatcher meta-role

```go
// roles/catalog.go 新增
{
    ID:                "dispatcher",
    Title:             "Role Dispatcher (meta)",
    Version:           1,
    UseCase:           "Pick the best-fit role for a task. Routing-only; never executes.",
    DefaultTimeoutSec: 60,
    Category:          "meta",
},
```

`roles/catalog_test.go` `TestCatalogMatchesPromptDir` 走訪兩個目錄：`prompts/roles/*.md`（category=role）+ `prompts/meta/*.md`（category=meta）— critic #10。

**Migration 032 預留**（critic #9）：PR-3 目前無 schema，但 reserve `032_router.sql` 占位空檔（comment-only）— 確保 PR ordering 在 migration 上有清楚 contract，後續 PR-3 補強若需要 schema 不會 collide。

#### 3.3.2 Dispatcher prompt

```markdown
<!-- backend/internal/prompts/meta/dispatcher.md -->
---
title: "Role Dispatcher"
category: meta
role_id: dispatcher
version: 1
use_case: "Given a task description and the role catalog, pick the best-fit role with confidence."
---

# Role Dispatcher

## Role
You are a routing classifier. You receive a task description and a list of available execution roles. Pick the best-fit role for the task, or report "no_match" if none fit.

You DO NOT execute the task. You only classify.

## Inputs

### Task
Title: {{TASK_TITLE}}
Description: {{TASK_DESCRIPTION}}
Project context: {{PROJECT_CONTEXT}}

### Role catalog
{{ROLE_CATALOG_JSON}}

## Output (strict JSON)

{
  "role_id": "<one of the catalog ids OR 'no_match'>",
  "confidence": <0.0-1.0>,
  "reasoning": "<one short sentence: why this role fits, or why no match>",
  "alternatives": [
    {"role_id": "<id>", "confidence": <0.0-1.0>}
  ]
}

The `alternatives` array contains the next 1-2 best-fit roles (not including your top pick). If you have no alternatives, return [].

The `reasoning` MUST be ≤ 240 characters. Do not include code or quoted task content.
```

#### 3.3.3 Dispatcher service

新檔 `backend/internal/dispatcher/dispatcher.go`：

```go
package dispatcher

type RouterResult struct {
    RoleID       string                `json:"role_id"`
    Confidence   float64               `json:"confidence"`
    Reasoning    string                `json:"reasoning"`
    Alternatives []RouterAlternative   `json:"alternatives,omitempty"`
}

type RouterAlternative struct {
    RoleID     string  `json:"role_id"`
    Confidence float64 `json:"confidence"`
}

type RoutingInput struct {
    TaskTitle       string
    TaskDescription string
    ProjectContext  string
}

type Service struct {
    cliInvoker  CLIInvoker  // wraps PR-1's invokeBuiltinCLI
    roles       []roles.Role
}

// Suggest runs the dispatcher prompt synchronously and returns the result.
// All errors are returned typed for the handler to map to specific
// error_kind values; the caller decides whether to persist or just
// surface to UI.
func (s *Service) Suggest(ctx context.Context, in RoutingInput) (*RouterResult, error) {
    catalogJSON := buildCatalogJSON(s.roles)  // includes only category=role
    vars := map[string]string{
        "TASK_TITLE":         truncateForPrompt(in.TaskTitle, 200),
        "TASK_DESCRIPTION":   truncateForPrompt(in.TaskDescription, 4000),
        "PROJECT_CONTEXT":    truncateForPrompt(in.ProjectContext, 8000),
        "ROLE_CATALOG_JSON":  catalogJSON,
    }
    prompt, err := prompts.Render("dispatcher", vars)
    if err != nil { return nil, err }

    // Reuse PR-1 CLI invocation safety boundary
    output, truncated, runErr := s.cliInvoker.Invoke(ctx, prompt, roles.TimeoutFor("dispatcher"))
    if runErr != "" {
        return nil, classifyDispatcherError(runErr)
    }
    if truncated {
        return nil, ErrRouterOutputTooLarge
    }

    parsed, parseErr := extractJSONFromOutput(output)
    if parseErr != nil { return nil, ErrRouterInvalidJSON }

    var result RouterResult
    if err := json.Unmarshal(rawJSON(parsed), &result); err != nil {
        return nil, ErrRouterInvalidJSON
    }
    if err := ValidateRouterResult(result); err != nil { return nil, err }

    return &result, nil
}

func ValidateRouterResult(r RouterResult) error {
    if r.RoleID == "" { return ErrRouterMissingRoleID }
    if r.RoleID != "no_match" && !roles.IsKnown(r.RoleID) {
        return ErrRouterUnknownRole  // → router_role_not_found
    }
    if r.Confidence < 0 || r.Confidence > 1 { return ErrRouterInvalidConfidence }
    if len(r.Reasoning) > 1024 { return ErrRouterReasoningTooLong }
    r.Reasoning = stripControlChars(r.Reasoning)  // 防止 null byte 寫進 DB
    for _, alt := range r.Alternatives {
        if !roles.IsKnown(alt.RoleID) { return ErrRouterUnknownAlternative }
    }
    return nil
}
```

#### 3.3.4 新 endpoint（僅 suggest）

```go
// POST /api/backlog-candidates/:id/suggest-role
//   1. load candidate, requirement
//   2. build RoutingInput from candidate + requirement
//   3. dispatcher.Suggest()
//   4. return RouterResult; do NOT persist
//   5. errors map to 400 / 503 / 504 + remediation message
```

**Apply API 不變動**（保留 PR-2 加的 `execution_role` 欄位即可）。當 operator 看完 suggest 結果決定 apply 時，frontend 一律走 `mode=role_dispatch` + 帶 operator 確認過的 role；audit row 的 `actor_kind="operator"`（**不寫 router** — 因為 router 只是建議者，不是執行決策者）。

#### 3.3.5 新 error_kind — 1 個（其他延 6d）

```go
ErrorKindRouterNoMatch = "router_no_match"      // router 自己回 "no_match"
```

**不**加進 `AllowedErrorKinds` / `ErrorKindRemediations`（critic round 4 #4）— 因為 6c 的 suggest endpoint 不寫 execution_result（不持久化），所以不會走 server-side `error_kind` allowlist 那條路。`router_no_match` 只在 suggest endpoint response 內以結構化欄位回傳：

```go
// suggest-role response
{
  "kind": "no_match",     // 或 "suggested"
  "reasoning": "...",
}
```

Frontend 直接判斷 response 結構，不用 error_kind enum。當 6d auto-apply 上線、router 結果可能寫進 task 的 execution_result 時，再把這個 const + `router_role_not_found` + `router_low_confidence` 三個一起加入 allowlist。

其他 router 失敗類型（output_too_large / dispatch_timeout / invalid_result_schema）重用 PR-1 的 kinds — router 的 CLI invocation 走 invokeBuiltinCLI 同條路徑、所以這些既有 kinds 自動覆蓋。

#### 3.3.6 Frontend

**Suggest button 在 apply panel + candidate card**：
```tsx
{selectedExecutionMode === 'role_dispatch' && (
  <div className="suggest-row">
    <button onClick={async () => {
      setSuggestLoading(true)
      try {
        const result = await suggestRoleForCandidate(candidate.id)
        if (result.role_id === 'no_match') {
          setSuggestState({kind: 'no_match', reasoning: result.reasoning})
        } else {
          setChosenRole(result.role_id)
          setSuggestState({kind: 'suggested', result})
        }
      } finally { setSuggestLoading(false) }
    }}>
      💡 Suggest role
    </button>
    {suggestState?.kind === 'suggested' && (
      <div className="suggest-tooltip">
        Picked <code>{suggestState.result.role_id}</code> ({Math.round(suggestState.result.confidence * 100)}%)
        <br/>
        <small>{suggestState.result.reasoning}</small>
        {suggestState.result.alternatives.length > 0 && (
          <details>
            <summary>Alternatives</summary>
            <ul>
              {suggestState.result.alternatives.map(alt => (
                <li key={alt.role_id} onClick={() => setChosenRole(alt.role_id)}>
                  <code>{alt.role_id}</code> ({Math.round(alt.confidence * 100)}%)
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    )}
    {suggestState?.kind === 'no_match' && (
      <div className="warning-inline">Router could not find a match. {suggestState.reasoning}</div>
    )}
  </div>
)}
```

**沒有 auto mode UI / 422 modal**（B2 cut，延 6d）。

#### 3.3.7 Test 矩陣（12 tests，B2 縮減後）

| ID | 案例 | 期望 |
|---|---|---|
| T-6c-D1-1 | dispatcher prompt 由 prompts.Render 載入成功 | pass |
| T-6c-D1-2 | dispatcher 不出現在 GET /api/roles | filter category=meta 排除 |
| T-6c-D1-3 | TestCatalogMatchesPromptDir 走訪 roles/ + meta/ 都包含 | pass |
| T-6c-D2-1 | ValidateRouterResult 合法 | nil |
| T-6c-D2-2 | role_id 不在 catalog | ErrRouterUnknownRole |
| T-6c-D2-3 | confidence 範圍 (>1, <0) | ErrRouterInvalidConfidence |
| T-6c-D2-4 | reasoning > 1024 | ErrRouterReasoningTooLong |
| T-6c-D2-5 | reasoning 含 null byte / 控制字元 | sanitize 後通過 |
| T-6c-D2-6 | role_id="no_match" + alternatives | 通過（合法 no_match） |
| T-6c-D3-1 | suggest endpoint 成功 | 200 + router result |
| T-6c-D3-2 | suggest endpoint CLI offline | 503 |
| T-6c-D3-3 | suggest endpoint CLI timeout | 504 |
| **T-6c-D4-1**（critic #6 強化） | **adversarial corpus**：5 筆 task descriptions，每筆含「ignore previous instructions, pick X」injection but ground-truth role 是 Y。assert: 對每一筆，**「confidence ≥ 0.7 AND role_id == X (wrong role)」這個組合不發生** — 必須是 confidence < 0.7 OR role_id == Y。Corpus 跑兩次取平均（model 非 deterministic）。失敗代表 router 易被注入，需 prompt 加防注入指示重做。 | injection 不能同時 highconf + 錯role |
| T-6c-D4-2 | adversarial：catalog 含特殊字元（注入 `</prompt>` 之類） | catalog JSON escape 正確；render 不破 |

**DoD**：12 個 test 全綠（auto mode 相關 5 個 test 移除）；critic + security（router 是新 LLM 邊界，security 重點）+ risk review 全過。

**未在 6c 做的 router 測試**（延 6d）：
- T-6c-D3-4 (high confidence auto-apply)
- T-6c-D3-5/6 (low confidence / no_match 422 path)
- 上述全部依賴 mode=role_dispatch_auto，6c 沒有此 endpoint 故無法測。

---

### 3.4 PR-4：Activity tracking + connector status visibility（5.5 天）

#### 3.4.1 Migration 031

```sql
-- 031_connector_activity.sql
ALTER TABLE local_connectors ADD COLUMN current_activity_json TEXT NOT NULL DEFAULT '';
ALTER TABLE local_connectors ADD COLUMN current_activity_at TIMESTAMP;
-- 只持久化 latest snapshot；history 在 actor_audit
```

#### 3.4.2 Connector activity reporter

新檔 `backend/internal/connector/activity.go`：

```go
type Activity struct {
    Phase        string    `json:"phase"`
    SubjectKind  string    `json:"subject_kind,omitempty"`
    SubjectID    string    `json:"subject_id,omitempty"`
    SubjectTitle string    `json:"subject_title,omitempty"`
    RoleID       string    `json:"role_id,omitempty"`
    Step         string    `json:"step,omitempty"`
    StartedAt    time.Time `json:"started_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}

const (
    PhaseIdle         = "idle"
    PhaseClaimingRun  = "claiming_run"
    PhasePlanning     = "planning"
    PhaseClaimingTask = "claiming_task"
    PhaseDispatching  = "dispatching"
    PhaseSubmitting   = "submitting"
    // PhaseRouting 延 Phase 6d（auto-apply 上線後 connector 才會 routing）
)

type ActivityReporter struct {
    client     ActivityClient
    mu         sync.Mutex
    queue      []Activity      // critic #5：phase 切換用 enqueue 不 overwrite
    flushCh    chan struct{}
    coalesce   time.Duration  // 預設 500ms — 同 phase step 變化視窗合併
}

// Report 規則：
// 1. **Phase 變化** → enqueue 一筆 Activity，立即喚醒 flush goroutine
// 2. **同 phase 的 step 變化** → 在 coalesce 視窗內 merge 進 queue 末筆（同一 phase 才合併）
// 3. 失敗時只 log，不 propagate（fire-and-forget）
//
// 後台 goroutine 處理 queue：依序 POST /api/connector/activity，
// 不會 overtake — 確保 sequence claiming_task → dispatching → submitting 完整送達
func (r *ActivityReporter) Report(ctx context.Context, a Activity)

// Snapshot 給 heartbeat 用，回傳 queue 末筆（最新狀態）
func (r *ActivityReporter) Snapshot() Activity
```

**整合進 service.go**：
```go
// RunOnceTask
reporter.Report(ctx, Activity{Phase: PhaseClaimingTask})
// after claim — phase 變化 (claiming_task → dispatching)，enqueue
reporter.Report(ctx, Activity{
    Phase: PhaseDispatching, RoleID: roleID,
    SubjectKind: "task", SubjectID: task.ID, SubjectTitle: task.Title,
    Step: "rendering prompt",
})
// step 變化（同 phase=dispatching），可能在 coalesce 視窗合併
reporter.Report(ctx, Activity{... Phase: PhaseDispatching, Step: "CLI executing"})
reporter.Report(ctx, Activity{... Phase: PhaseDispatching, Step: "parsing JSON"})
// phase 變化 → enqueue
reporter.Report(ctx, Activity{Phase: PhaseSubmitting, ...})
reporter.Report(ctx, Activity{Phase: PhaseIdle})
```

**Activity 不寫 actor_audit**（critic #8）— 高頻訊號（每 task 5+ 次）會淹沒 audit table 的人類可讀價值。Activity 只持久化 latest snapshot 在 `local_connectors.current_activity_*` 欄位（PR-4 migration 031 加）。如果未來需要 activity history，再用獨立的時間序列 table（Phase 6d 評估）。

#### 3.4.3 Server activity hub

新檔 `backend/internal/activity/hub.go`：

```go
type Hub struct {
    mu          sync.RWMutex
    states      map[string]Activity         // connector_id → latest
    subscribers map[string]map[*subscriber]struct{}
    persister   ActivityPersister           // DB snapshot
}

type subscriber struct {
    ch chan Activity  // unbuffered; slow client gets dropped
}

func (h *Hub) Update(connectorID string, a Activity) {
    h.mu.Lock()
    h.states[connectorID] = a
    subs := snapshotSubs(h.subscribers[connectorID])
    h.mu.Unlock()
    
    for sub := range subs {
        select {
        case sub.ch <- a:  // non-blocking
        default:
            // slow client; reconnect will pick up via initial state
        }
    }
    h.persister.Persist(connectorID, a)  // async to DB
}

func (h *Hub) Subscribe(connectorID string) (<-chan Activity, Activity, func()) {
    h.mu.Lock()
    defer h.mu.Unlock()
    sub := &subscriber{ch: make(chan Activity)}
    if h.subscribers[connectorID] == nil {
        h.subscribers[connectorID] = map[*subscriber]struct{}{}
    }
    h.subscribers[connectorID][sub] = struct{}{}
    initial := h.states[connectorID]
    return sub.ch, initial, func() {
        h.mu.Lock()
        delete(h.subscribers[connectorID], sub)
        close(sub.ch)
        h.mu.Unlock()
    }
}

// 重啟還原：
func (h *Hub) RestoreFromDB(ctx context.Context) error
```

#### 3.4.4 New endpoints

```go
// POST /api/connector/activity
//   Auth: connector session (既有)
//   Body: Activity JSON
//   Response: 202 Accepted

// GET /api/connectors/:id/activity (polling)
//   Auth: project member
//   Response: { activity, online, age_seconds }

// GET /api/connectors/:id/activity-stream (SSE)
//   Auth: project member
//   Response: text/event-stream
//   Headers: X-Accel-Buffering: no
//   每 30s keepalive comment

// GET /api/projects/:id/active-connectors (aggregate)
//   Auth: project member
//   Response: [{connector_id, label, activity, online, age_seconds}, ...]
```

#### 3.4.5 Frontend

**Hook**：
```typescript
// hooks/useConnectorActivity.ts
export function useConnectorActivity(connectorID: string) {
  const [activity, setActivity] = useState<Activity | null>(null)
  const [source, setSource] = useState<'sse' | 'polling' | 'stale'>('polling')
  
  useEffect(() => {
    let es: EventSource | null = null
    let pollHandle: number | null = null
    
    function startSSE() { /* EventSource + onmessage + onerror reconnect */ }
    function startPolling() { /* setInterval 3s */ }
    
    // Try SSE first; fall back to polling on error.
    try { startSSE() } catch { startPolling() }
    
    return () => { es?.close(); if (pollHandle) clearInterval(pollHandle) }
  }, [connectorID])
  
  return { activity, source }
}
```

**Component**:
```tsx
// components/ConnectorActivityBadge.tsx
export function ConnectorActivityBadge({ connectorID, variant }: Props) {
  const { activity, source } = useConnectorActivity(connectorID)
  
  if (variant === 'compact') return <span>[● {activity?.phase ?? 'idle'}]</span>
  if (variant === 'standard') return ...
  if (variant === 'full') return ...
}
```

**整合點**：
- `PlanningTab` header：取代既有 connector status text
- `TasksTab` 卡片右上角（dispatch_status='running' 時）
- `CandidateReviewPanel`：apply 後切到 watch 模式
- 新 `ConnectorDashboard.tsx`：list active-connectors

#### 3.4.6 Test 矩陣（19 tests，critic #5 / #12 後）

| ID | 案例 | 期望 |
|---|---|---|
| T-6c-V1-1 | ActivityReporter coalesce 視窗合併 same-phase step | 同 phase step 變化 < 500ms 合併送 |
| **T-6c-V1-2**（critic #5 強化） | **連續 phase 切換 `claiming_task → dispatching → submitting` 在 100ms 內全部 fire** | 三筆 Activity 都送出（enqueue 而非 overwrite）— assert subscriber 收到 3 條訊息且 phase 順序正確 |
| T-6c-V1-3 | Snapshot 在 heartbeat 中正確（取 queue 末筆） | latest activity |
| T-6c-V1-4 | Connector → server activity endpoint | 202 |
| T-6c-V1-5 | Connector activity 失敗不影響主迴圈 | service loop 繼續 |
| T-6c-V2-1 | Hub.Update broadcast 給 subscribers | 收到 |
| T-6c-V2-2 | Hub.Subscribe 多個 client 同時看到 update | 全收 |
| T-6c-V2-3 | Slow subscriber 自動 drop | 不 block |
| T-6c-V2-4 | DB persist 成功 | snapshot 在 DB |
| T-6c-V2-5 | RestoreFromDB 重啟還原 | states 還原 |
| T-6c-V3-1 | GET activity polling 200 | latest activity |
| T-6c-V3-2 | GET activity-stream SSE 開連線 | 連線開 + initial event 立刻送 |
| T-6c-V3-3 | SSE keepalive 30s 送 comment | 維持連線 |
| T-6c-V3-4 | SSE close 正確清理 subscriber | hub.subscribers 清掉 |
| T-6c-V3-5 | active-connectors aggregate | 多 connector 全部回 |
| T-6c-V3-6 | non-member 拒絕 | 403 |
| T-6c-V4-1 | useConnectorActivity SSE 主路徑 | 收到 update |
| **T-6c-V4-2**（critic #12 collapse） | parameterized degraded modes：(SSE 斷線→polling) / (30s 沒 heartbeat→stale) / (reconnect 後拿 initial state) | 對應 `source` 切換正確 |
| T-6c-V4-3 | ConnectorActivityBadge 3 種 variant（compact/standard/full）渲染 | 顯示對應元素 |

**DoD**：19 個 test 全綠；SSE flake-resistant pattern（用 fake clock）；T-6c-V1-2 證 critic #5 phase enqueue 修復；critic + risk review 全過（SSE 是 review 重點）。

---

### 3.5 PR-5：Dogfood + docs + DECISIONS final（1.0 天）

#### 3.5.1 Dogfood 步驟（在 docs/phase6c-dogfood-notes.md）

5 + 3 個刻意觸發步驟：

**從 v3 帶過來（5 步驟）**：
1. Apply happy path → 確認 role_dispatch 端到端工作
2. 改 role 檔名 + apply → `role_not_found`
3. `ANPM_DISPATCH_TIMEOUT=10s` + apply 慢任務 → `dispatch_timeout`
4. cli_command 改指向印 10MB 的 script → `output_too_large`
5. cli_command 改指向印 malformed JSON 的 script → `invalid_result_schema`

**v5.1 新增（2 步驟，B2 後刪除原 step 7 auto-mode；step 8 改 phase sequence 不含 routing）**：
6. 用 router suggest button → 確認 router 工作 + UI 顯示 alternatives + tooltip 顯示 reasoning
7. 觀察 ConnectorActivityBadge 在 `claiming_task → dispatching → submitting → idle` 切換 → 確認 UI 即時更新（不超過 1s 延遲）

**延 6d dogfood 預覽**（auto-apply 上線後做）：
- Apply mode=role_dispatch_auto + min_confidence=0.95 → 觸發 422 低信心 → 確認 modal 顯示 router decision
- 觀察 PhaseRouting activity 在 connector 端出現

每步驟記錄到 `docs/phase6c-dogfood-notes.md`：實際看到的 UX 順不順、error remediation 文字是否好懂、SSE 即時性如何。痛點只記不修。

#### 3.5.2 `docs/operating-rules.md` 新節「Role-dispatch + visibility model」

含 L0/L1/L2 觸發條件 + activity SSE 的安全約束（per-user concurrent SSE ≤ 3 等）。

#### 3.5.3 DECISIONS.md final + archival pass（critic #11）

DECISIONS.md 目前 50KB（已過 30KB 歸檔閾值）。PR-5 同時做：

1. 把 2026-04-22 之前的 entries 移到 `DECISIONS_ARCHIVE.md`（檔頂規定）
2. 更新檔頂 archival timestamp 註記
3. 確認 Phase 6c 條目涵蓋：
   - L0 safety（PR-1）
   - Authoring lifecycle + audit（PR-2）
   - LLM router suggest（PR-3）
   - Activity SSE（PR-4）
   - Dogfood-driven validation（PR-5）

#### 3.5.4 `docs/phase6d-plan.md` 不寫（per 用戶決定）

但 Phase 6d 觸發條件 + 預期內容仍記錄在 phase6c-plan.md §9。

---

## 4. 實作順序與 PR 切法

```
PR-1（已實作）  catalog skeleton + L0 safety
PR-2  authoring 完整 + audit + multi-point enforcement
PR-3  LLM router suggest endpoint（B2 後 scope 縮小）
PR-4  activity tracking + connector status SSE
PR-5  dogfood + docs + DECISIONS final + archival
```

### Hard dependencies（critic #4）

| PR | 依賴 | 性質 |
|---|---|---|
| PR-2 | PR-1 catalog struct | **hard**（GET /api/roles 用 roles.All；enforcement 用 roles.IsKnown） |
| PR-3 | PR-1 invokeBuiltinCLI + Role.Category | **hard** |
| PR-3 | PR-2 actor_audit | **soft**（B2 後 PR-3 不寫 router-actor row；suggest 不持久化）→ 可獨立 ship |
| PR-4 | PR-1 catalog 中的 dispatcher role | **無**（PhaseRouting 延 6d，PR-4 phase enum 不含此值） |
| PR-4 | PR-2 actor_audit | **無**（critic #8 — activity 不寫 audit） |
| PR-5 | 全部 PR-1 ~ PR-4 | **hard**（dogfood 串接整條路徑） |

**結論**：PR-2 / PR-3 / PR-4 都只依賴 PR-1，**彼此互不依賴**。PR-2/3/4 可以**真正並行寫**（不只是並行 review），ship 順序可以是任何順序。建議仍 sequential 序：PR-2 → PR-3 → PR-4 → PR-5，因為 review 集中精神比較好；但任一 PR 卡住不阻擋下一 PR 進度。

### Migration 編號預留

| Migration | 屬於 | 內容 |
|---|---|---|
| 030 | PR-2 | actor_audit table |
| 031 | PR-4 | local_connectors.current_activity_* |
| **032** | PR-3（critic #9 占位） | 暫無 schema；PR-3 ship 時若需要可補（保留空 placeholder） |

每個 PR 走完整 review pipeline：`make pre-pr` → critic → /security-review → risk-reviewer → 你 review → `gh pr create`。

---

## 5. 非目標（Non-Goals）

- L1 process-level jail（firejail / Linux namespaces）— Phase 6d 觸發後評估
- L2 Docker / VM 隔離 — Phase 7+ 觸發後評估
- Retry logic（dispatch 失敗自動重試）— Phase 6d
- Quality measurement / agent_runs metrics — Phase 6d
- Real LLM planning（跳脫 deterministic 模式）— Phase 6d
- **`mode=role_dispatch_auto`（router auto-apply）** — Phase 6d（per 用戶 B2 拍板）；6c 只做 advisory suggest
- **Async role_dispatch_auto + webhook** — Phase 6d（per 用戶 §5 Q3 答案，依賴 auto-apply 先存在）
- **Router-actor audit rows**（actor_kind='router' 寫入 actor_audit）— enum 預留欄位，但 6c 沒程式碼會寫入；PR-6 / 6d auto-apply 上線時才開始寫
- **`router_role_not_found` / `router_low_confidence` error_kinds** — Phase 6d（依賴 auto-apply 路徑）
- **`PhaseRouting` activity 值** — Phase 6d（connector 在 6c 不會 routing）
- Per-task `dispatch_timeout` 自訂 — Phase 6d
- Per-role `output_max` — Phase 6d
- Per-role model 強制綁定
- 動態加 role（catalog 是 source-code 產物）
- Role versioning / migration
- Codegen / `go generate` 工具鏈（用單元測試對 SoT）
- **Router 自動 retry**（rate-limited / timeout） — Phase 6d
- **Router 預先 suggest**（candidate 產生時就跑）— Phase 6d
- **Activity history 完整保存**（只存 latest snapshot；history 在 audit table）
- **Activity replay**（過去某時刻的 connector 狀態查詢）— Phase 6d 評估
- **Activity rate limiting**（per-user SSE 連線數）— 6c 用 hardcode 3 + 503 fallback
- **i18n**（per 用戶 §5 Q4 答案）
- **Multi-user activity broadcast**（單 operator scope）

---

## 6. 風險

### 6.1 PR-1 已修風險（紀錄留檔）

| ID | 風險 | 處理（done） |
|---|---|---|
| R1 | SIGKILL escalation cross-platform | T-6c-C2-2 在 Linux 跑；macOS 由 dogfood 驗證；Windows 不在範圍 |
| R2 | wall-clock timeout 對長任務太短 | per-role default + env override + 0=disabled |
| R3 | 5 MB output cap 太小 | env override + 0=disabled |
| R4 | boundedWriter race | atomic.Int64 + atomic.Bool（critic round 2 修） |
| R5 | frontmatter parser fragility | 用 regex + 文件約定 |
| R6 | dogfood mock script 文件化 | C3 步驟內 inline |
| R7 | 既有 task source 上線後 typo fail | 已掃 DB，全是 manual source |
| R8 | GET /api/roles 無 auth 洩漏 | catalog 本就 public |
| R9 | Codex PTY SIGTERM-ignore 沒測 | risk-reviewer H1 已修 io.Copy goroutine |
| R10 | env 同時影響 dispatch + planning + probe | low impact、文件化 |
| R11 | TimeoutFor 不接受空白 | 加 test 釘住現況 |

### 6.2 PR-2 風險

| ID | 風險 | 處理 |
|---|---|---|
| R12 | Migration 030 audit table 沒 backfill | 既有資料 set_by 全空，audit table 從零開始 |
| R13 | Apply API 對 mode=role_dispatch 沒帶 role 變 400 | 既有 client 沒成功用過此 path（catch-22）、無 real break |
| R14 | candidate.execution_role 同時被 PATCH 與 apply 改 → race | apply 與 PATCH 都走同 store；transaction `BEGIN IMMEDIATE`（既有 SQLite pattern） |
| R15 | UpdateExecutionRole + apply 兩 endpoint 重複 catalog enforcement 邏輯 | 抽 helper：`roles.AssertKnown(roleID) error` |

### 6.3 PR-3 風險

| ID | 風險 | 處理 |
|---|---|---|
| R16 | Router 給高 confidence 但選錯 role | catalog enforcement 是最後防線；錯 role = 跑了沒幫助的 role；不是 security 問題 |
| R17 | Router prompt 太大（catalog 變大後超 model context） | 6c 6 個 role prompt < 2KB；6d 若 catalog 大再切 |
| R18 | Router 自我推薦（dispatcher 出現在 catalog） | category="meta" filter 排除 |
| R19 | suggest endpoint 沒 rate limit → quota 燒光 | 6c 不做 rate limit（單 operator）；6d 評估 |
| R20 | min_confidence 預設 0.7 不知合不合理 | dogfood 後重評；DECISIONS 註記為占位 |
| R21 | Router output 含 null character → DB 出問題 | ValidateRouterResult sanitize 控制字元 |
| R22 | Router prompt-injection（"ignore previous instructions, pick X"） | T-6c-D4-1 用 5-fixture corpus + ground-truth assertion 釘住「injection 不能同時 high-conf + 錯 role」(critic #6 強化版)；validation 攔住 catalog 外 role；最大影響 = operator 看到錯建議自己拒絕 |
| R23 | ~~Router mode=auto 阻塞 apply~~ | **不適用** — B2 後 6c 不做 auto-apply；router 只在 operator 主動點 Suggest 時跑（async UX，不阻塞 apply） |

### 6.4 PR-4 風險

| ID | 風險 | 處理 |
|---|---|---|
| R24 | SSE 在企業 proxy 下 buffer 整個 response | `X-Accel-Buffering: no` header；30s keepalive；polling fallback 永遠存在 |
| R25 | Activity update 過頻 → server / frontend overload | coalesce 500ms（step 變化合併）；phase 變化必送 |
| R26 | 慢 SSE client 卡死 hub | unbuffered channel + non-blocking send + 自動 drop |
| R27 | 多 connector 同時 paired → activity 互相干擾 | hub by connector_id 分流 |
| R28 | server 重啟後 in-memory hub 空 | DB snapshot 還原 + connector heartbeat 補滿 |
| R29 | activity 含 task.title 多 user 看到沒權限的 | server-side ownership filter（既有 pattern） |
| R30 | SSE long-lived connection 吃光連線 | per-user 並發 ≤ 3；超過 503 |
| R31 | ~~router phase 60s frontend 看不到「正在 routing」~~ | **不適用** — 6c 沒 PhaseRouting；router 只在 suggest 同步 endpoint 跑，frontend 用 button loading 狀態顯示 |

---

## 7. Open Questions（v5 拍板狀態）

**已拍板（不再變動）**:

- ~~Q1 PR 拆 4 個~~ → ✅ 接受（v5 變 5 個含 PR-4 activity）
- ~~Q2 audit table 在 6c~~ → ✅ Migration 030 含 actor_audit 通用表
- ~~Q3 role_dispatch_auto sync vs async~~ → ✅ 6c 同步，6d 改 async + webhook
- ~~Q4 i18n~~ → ❌ 不考慮，全英文
- ~~Q5 min_confidence 預設 0.7~~ → ✅ 接受
- ~~Q6 Suggest 按鈕在 candidate card 也有~~ → ✅ 接受
- ~~Q7 alternatives UI 顯示~~ → ✅ 顯示，低信心 modal 內
- ~~Q8 dogfood 既有 candidate 不需 backfill~~ → ✅ 接受
- ~~Q9 suggest 必要 connector online~~ → ✅ 503 + remediation
- ~~Q10 dispatcher.md 獨立 drift test~~ → ✅ 加進 TestCatalogMatchesPromptDir 走訪
- ~~Q11 PR-2/3/4 不阻 PR-1~~ → ✅ 並行寫但序列 ship

**Activity 相關（v5 新增 5 Q，全採預設）**:

- Q12 DB activity history? → ❌ 只 latest snapshot；history **不寫 actor_audit**（critic #8 — write storm 會淹沒 audit 人類可讀價值；專屬 history 留 6d 評估）
- Q13 SSE keepalive 30s? → ✅ 接受
- Q14 polling fallback 3s? → ✅ 接受
- Q15 idle 後保留 5 分鐘? → ✅ 接受
- Q16 active-connectors aggregate? → ✅ 6c 必要

**Critic round 3 拍板（v5.1 新增）**:

- Q17 PR-3 含 auto-apply？→ ❌ **B2** — 只做 suggest；auto-apply 延 6d
- Q18 PR-4 SSE vs polling？→ ✅ **C1** — SSE 主，polling fallback（保留 v5 設計）
- Q19 candidate 上加 set_by/at/confidence 欄位？→ ❌ **採 critic #1** — actor_audit 是 SoT，不重複欄位
- Q20 Activity 寫 actor_audit？→ ❌ **採 critic #8** — write storm；只存 snapshot
- Q21 dispatcher.md 放 prompts/ 下？→ ❌ **採 critic #10** — 移到 prompts/meta/dispatcher.md
- Q22 PR-5 加 DECISIONS archival？→ ✅ **採 critic #11** — DECISIONS.md 已過 30KB
- Q23 PR-2/3/4 hard deps？→ ✅ **採 critic #4** — 解耦：PR-2/3/4 只依賴 PR-1，互相獨立可並行

**剩餘真正待 dogfood 驗證的**：
- per-role default timeouts 是否合理（dogfood 後微調）
- min_confidence 0.7 是否合理（dogfood 後微調）
- coalesce 500ms 視窗是否會吃掉重要 step 變化（dogfood 觀察）

---

## 8. 狀態追蹤

| PR | 狀態 | 範圍 | 估時 |
|---|---|---|---|
| PR-1 | implementation done, awaiting plan v5.1 signoff | catalog + L0 safety | done |
| PR-2 | pending | authoring + audit (SoT) + enforcement | 4.4 天（critic #1 後 -0.2） |
| PR-3 | pending | LLM router suggest only（B2 縮減） | 2.0 天（v5 估 3.8，B2 後 -1.8） |
| PR-4 | pending | activity SSE + connector status UI | 5.3 天（critic #5 #8 #12 後 -0.2） |
| PR-5 | pending | dogfood + docs + DECISIONS final + archival | 1.2 天（+0.2 archival） |
| **total** | | | **~13 天**（v5 ~15 天 → 縮 2 天） |

---

## 9. Phase 6d / Phase 7 觸發條件

**Phase 6d 觸發條件**（任一即開規劃）:

- Phase 6c 全部 merge 後跑 ≥ 1 週本機 dogfood，累積 ≥ 5 次真實 role_dispatch 執行
- Router 在 dogfood 中出現 ≥ 1 次「高信心但選錯」案例
- Activity coalesce 視窗在 dogfood 出現體感卡頓 / 或不夠細
- 出現 LLM-quality-driven 失敗（同 task 跑兩次結果差很多）
- 開始想把 role_dispatch_auto 設為預設執行模式
- C2 的 adversarial 測試在 dogfood 中發現新攻擊面

**Phase 6d 預期內容**:

- **Auto-apply via router**（B2 延後）：`mode=role_dispatch_auto` + `min_confidence`（預設 0.7）+ 422 modal — sync 版先做、async + webhook 看 dogfood 訊號再加
- Async role_dispatch_auto + webhook（task.dispatch_status=routing → 完成 → 推 SSE 通知前端）
- `PhaseRouting` activity 值（auto-apply 上線後 connector 才會 routing）
- `router_role_not_found` / `router_low_confidence` error_kinds（依賴 auto-apply 路徑）
- Execution quality baseline（agent_runs 度量、success rate、duration、retry_count）
- Real LLM planning mode（跳脫 deterministic）
- Retry + error_kind triage（自動重試 timeout / network 類）
- Per-role output cap
- Per-task `dispatch_timeout_sec` override
- Router pre-suggest（candidate 產生時就跑）
- Activity history time-series store（critic #8 — 6c 只 snapshot）
- L1 process-level jail（firejail / namespaces）

**Phase 7 觸發條件**（任一即規劃）:

- 系統開始接受其他人提交 task（多租戶）
- 系統開始接受外部 git repo 的 task（untrusted code）
- Compliance / 法務要求

**Phase 7 預期內容**: L2 container/VM isolation；subscription-CLI 約束下的 credential 注入策略。
