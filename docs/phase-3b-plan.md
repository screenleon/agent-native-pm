# Phase 3B 計畫 — Planning 品質改進

**Status**: draft v1.0 · 2026-04-27 · `[agent:feature-planner]`
**前置條件**: Phase 6c（全部 5 PR）已合併；Phase 6d（role_dispatch_auto）dogfood 資料已累積足夠 router 信心樣本（目標：≥20 筆 suggest-role 操作）。
**後置影響**: 完成後為 Phase 3A（Connector 可行性 spike）的規劃品質工作提供完整的 context-pack v2 基礎；並為 Phase 6d auto-dispatch 提供更可信的 confidence 訊號。

---

## 1. 問題陳述

### 1.1 現有的三個品質缺口

**Gap 1：Context pack 過於淺薄**

`wire.PlanningContextV1`（見 `docs/context-strategy.md §7.1`）缺少以下欄位：

| 欄位 | 現狀 | 影響 |
|------|------|------|
| `pack_id` | 缺失，以 `planning_run_id` 代替 | 無法跨 run 追蹤同一 context pack 的結果 |
| `role` | 缺失（隱式 planner） | LLM 無法知道自己要扮演哪個角色 |
| `intent_mode` | 缺失 | 永遠是隱式的 implement 模式 |
| `task_scale` | 缺失（heuristic 在 adapter 端） | 無法在 context 層統一傳遞給不同 adapter |
| `source_of_truth` | 缺失 | LLM 沒有指向 canonical doc 的指針 |
| `approved_scope` | 缺失 | LLM 不知道允許觸碰哪些模組/檔案範圍 |

後果：LLM 生成 candidate 時缺乏 grounding，容易偏離需求、建議不相干的任務、或誤解 intent。

**Gap 2：Evidence 對使用者不可見**

Candidate 生成後，使用者只看到 title + description，無法知道：
- 哪些 open tasks 被納入 context？
- 哪些 drift signals 被考慮？
- 哪些 recent documents 有貢獻？
- 是哪個 requirement 的哪個 planning run 的第幾號 candidate？

後果：使用者無法評估 candidate 品質，也無法告訴系統「這個 candidate 是垃圾因為 context 根本錯了」。

**Gap 3：沒有品質回饋迴路**

目前 accept/reject candidate 只改狀態（`approved`/`rejected`），沒有：
- 接受/拒絕的理由分類
- 品質評分
- 系統性的回饋積累

後果：router 無法從使用者行為學習；planning quality 永遠停在初始水準，無法隨 dogfood 改進。

### 1.2 為什麼要在 Phase 6d 之後做

Phase 6d 的 `role_dispatch_auto` 要求 router confidence 可信，而 router confidence 的精準度直接依賴更完整的 context pack（Gap 1）。若 Phase 3B 先做好 context-pack v2，Phase 6d 的 auto-dispatch 準確率會有結構性提升。

---

## 2. End State

完成所有 PR 後可驗證的行為：

### 2.1 Context Pack v2（PR-1）

1. `PlanningContextV2` 結構體新增：`PackID`（UUID）、`Role`、`IntentMode`、`TaskScale`（枚舉 small/medium/large）、`SourceOfTruth`（canonical doc 清單）
2. `task_scale` heuristic 從 adapter 端提升到 planning 組裝層，統一推算後寫入 wire
3. 每個 planning run 的 context pack 以 `pack_id` 做唯一標識，存入 `planning_runs.context_pack_id`
4. 所有 adapter（Go built-in + future connector）都讀 v2 欄位；v1 欄位保持向後相容（過渡期 adapter 可忽略新欄位）

### 2.2 Evidence Panel（PR-2）

5. Candidate 卡片加入 "Evidence" 展開抽屜（預設折疊）
6. 抽屜顯示：
   - 納入 context 的 open tasks（title + status）
   - 納入 context 的 drift signals（affected file + age）
   - 納入 context 的 recent documents（doc name + staleness）
   - `dropped_counts`（哪些來源因 byte cap 被截斷）
7. Evidence 資料從 `planning_runs.context_pack_id` JOIN `planning_context_snapshots` 取得（需新增 snapshot 儲存，見 PR-1 backend）
8. Candidate review panel（`CandidateReviewPanel`）與 planning workspace sidebar 均支援 evidence drawer

### 2.3 品質回饋收集（PR-3）

9. Candidate `approved`/`rejected` 操作增加可選的 `feedback_kind` 欄位：
   - 接受時：`good_fit` / `modified` / `fallback`
   - 拒絕時：`wrong_scope` / `too_broad` / `duplicate` / `low_quality` / `other`
10. Candidate 卡片 approve/reject 按鈕加 optional feedback popover（可略過，不強制）
11. Feedback 存入 `backlog_candidates.feedback_kind TEXT` + `backlog_candidates.feedback_note TEXT`（migration）
12. `GET /api/planning-runs/:id` response 帶出 `quality_summary`：`{total, approved, rejected, acceptance_rate, feedback_distribution}`

### 2.4 Planning Run 品質視圖（PR-4）

13. Planning workspace 在所有 candidate 已 review 後顯示 run-level 品質摘要：
    - 接受率 / 拒絕率
    - 各 feedback_kind 分佈
    - 哪些來源 context 被截斷（dropped_counts > 0 的警示）
14. Dashboard planning 區塊加入 `avg_acceptance_rate`（過去 7 天 planning runs 均值）

---

## 3. PR 拆分與依賴

```
PR-1: Context Pack v2（backend）
  → 新增 PlanningContextV2 結構體
  → 提升 task_scale heuristic 到 planning 層
  → 新增 planning_context_snapshots 表（migration）
  → pack_id 寫入 planning_runs

PR-2: Evidence Panel（frontend + thin backend）
  ← 依賴 PR-1（需要 pack_id + snapshot 資料）
  → GET /api/planning-runs/:id 帶 evidence 欄位
  → CandidateReviewPanel evidence drawer

PR-3: 品質回饋（backend + frontend）
  ← 可與 PR-2 並行（無 hard dep）
  → migration: backlog_candidates 加 feedback_kind / feedback_note
  → PATCH /api/backlog-candidates/:id 接受 feedback 欄位
  → Candidate 卡片 feedback popover

PR-4: Planning Run 品質視圖（frontend + thin backend）
  ← 依賴 PR-3（需要 feedback_distribution）
  → quality_summary 計算 endpoint
  → Dashboard avg_acceptance_rate
  → Run-level summary panel
```

---

## 4. 實作細節

### 4.1 Context Pack v2 Schema（Go）

```go
// PlanningContextV2 — Context pack v2 contract
// Backward-compatible: adapters that read V1 fields continue to work.
type PlanningContextV2 struct {
    SchemaVersion string            `json:"schema_version"` // "context.v2"
    PackID        string            `json:"pack_id"`        // UUID, stable per planning run
    Role          string            `json:"role"`           // e.g. "backend-architect"
    IntentMode    IntentMode        `json:"intent_mode"`    // analyze | implement | review | document
    TaskScale     TaskScale         `json:"task_scale"`     // small | medium | large
    Limits        ContextLimits     `json:"limits"`
    SourceOfTruth []SourceRef       `json:"source_of_truth"` // canonical doc pointers
    Sources       ContextSources    `json:"sources"`
    Meta          ContextMeta       `json:"meta"`
}

type SourceRef struct {
    Name string `json:"name"` // e.g. "docs/operating-rules.md"
    Path string `json:"path"`
    Role string `json:"role"` // e.g. "safety-rules"
}
```

### 4.2 Planning Context Snapshot 儲存

新增 `planning_context_snapshots` 表：

```sql
CREATE TABLE planning_context_snapshots (
    id           TEXT PRIMARY KEY,
    pack_id      TEXT NOT NULL,
    planning_run_id TEXT NOT NULL REFERENCES planning_runs(id),
    schema_version TEXT NOT NULL DEFAULT 'context.v2',
    snapshot     JSONB NOT NULL,
    sources_bytes INTEGER NOT NULL DEFAULT 0,
    dropped_counts JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_context_snapshots_run ON planning_context_snapshots(planning_run_id);
```

- Snapshot 只在 run 建立時寫一次，不可修改
- `GET /api/planning-runs/:id` 增加 `context_snapshot` 欄位（opt-in，加 `?include=context` query param）
- Evidence Panel 消費 `context_snapshot.sources` + `context_snapshot.dropped_counts`

### 4.3 Candidate Feedback Migration

```sql
ALTER TABLE backlog_candidates
    ADD COLUMN feedback_kind TEXT,
    ADD COLUMN feedback_note TEXT;

-- feedback_kind 允許值（server-side validation）：
-- 接受: good_fit | modified | fallback
-- 拒絕: wrong_scope | too_broad | duplicate | low_quality | other
```

### 4.4 TaskScale Heuristic 提升

現有 adapter 的 task scale 推算邏輯（以 description 字數 + 關鍵字 heuristic）提升到
`backend/internal/planning/scale/scale.go`，成為 planning 組裝層的公用函式：

```go
func EstimateTaskScale(title, description string, fileCount int) TaskScale
```

Adapter 端刪除重複邏輯，改呼叫 wire 中的 `TaskScale` 欄位。

---

## 5. 驗收標準

### PR-1
- [ ] `PlanningContextV2` 結構體建立，schema_version = `context.v2`
- [ ] `pack_id` UUID 在 planning run 建立時生成，寫入 `planning_runs`
- [ ] `planning_context_snapshots` 表建立（migration），snapshot 在 run 建立時持久化
- [ ] `task_scale` 統一從 planning 層推算，adapter 端不再重複
- [ ] Go built-in adapter 讀取 v2 欄位（v1 欄位仍保持向後相容）
- [ ] `make pre-pr` 綠燈

### PR-2
- [ ] `GET /api/planning-runs/:id?include=context` 回傳 `context_snapshot`
- [ ] Candidate card 顯示 "Evidence" 展開抽屜
- [ ] 抽屜正確顯示：open tasks / drift signals / documents / dropped_counts 警示
- [ ] 無 context snapshot 的舊 candidates 優雅降級（evidence 抽屜顯示 "context data unavailable"）
- [ ] `make pre-pr` 綠燈

### PR-3
- [ ] `PATCH /api/backlog-candidates/:id` 接受 `feedback_kind` + `feedback_note`
- [ ] Server 拒絕不在允許清單的 `feedback_kind` 值（400 + error message）
- [ ] Candidate 卡片 approve/reject 操作後顯示 optional feedback popover（可跳過）
- [ ] `feedback_kind` 和 `feedback_note` 正確持久化
- [ ] `make pre-pr` 綠燈

### PR-4
- [ ] `GET /api/planning-runs/:id` 帶出 `quality_summary.{total, approved, rejected, acceptance_rate, feedback_distribution}`
- [ ] 所有 candidate 已 review 後，Planning workspace 顯示 run-level 品質摘要
- [ ] Dashboard planning 區塊顯示 `avg_acceptance_rate`（過去 7 天）
- [ ] dropped_counts > 0 時 planning run 顯示 context truncation 警示
- [ ] `make pre-pr` 綠燈

---

## 6. 後續路徑（Phase 3B 完成後）

```
Phase 3B 完成
    ↓
Phase 3A: Connector 可行性 spike
  - 驗證 trust boundary、pairing protocol、vendor compatibility（Copilot/ChatGPT）
  - 使用 context-pack v2 作為 connector dispatch payload
  - 目標：決定「connector 路線是否成立」，影響後續所有 connector 相關投資

    ↓（如果 3A 可行）
Phase 4（新）: Connector MVP 完整化
  - Pairing session 完整流程
  - Context pack v2 作為 connector dispatch 標準格式
  - Result callback + execution result 可見性
  - Task retry / cancel / regenerate 基礎 UX

    ↓
Phase 5（新）: Execution Mode UX Clarity
  - 重新設計 execution mode picker（deterministic / server-provider / connector 三路徑）
  - Connector developer onboarding 文件
  - Task 執行結果 detail view

    ↓（Phase 6d auto-dispatch 有足夠 feedback 資料後）
Phase 6: Planning 自動化
  - role_dispatch_auto mode（靠 Phase 3B feedback 資料驗證 router 信心）
  - 多 connector 自動路由
  - 非同步 job queuing（長任務）
```

> **注意**：Phase 3A spike 是「要不要繼續投資 connector 路線」的決策點。若 3A 結果是「不可行」，Phase 4 整個跳過，轉向加深 server-provider 路線。Phase 3B 的品質改進無論如何都有價值，與 connector 路線無耦合。

---

## 7. 範圍外（明確不做）

- Context pack 的 `approved_scope` 欄位：需要 project-level approval surface，屬於 Phase 4+ 的功能，本 phase 不做
- LLM-based context 品質評分（自動化評估 context pack 好不好）：Phase 6+ 的 ML 功能
- Planning run 歷史對比視圖（比較兩次 run 的 candidate 品質）：nice to have，不是核心
- Planning feedback 的 ML 訓練管道：資料積累階段先做，訓練屬於未來 SaaS 功能

---

## 8. 設計原則（per operating-rules.md）

- **假設顯式化**（GLOBAL-001）：context-pack v2 的欄位設計假設所有 adapter 都遵從 v2 schema；若有 adapter 在過渡期仍讀 v1 欄位，必須在 PR 說明中列出相容性假設及失敗影響。
- **Surgical changes**（GLOBAL-010）：PR-1 只動 planning context 相關程式碼，不順手清理其他模組的 dead code。
- **Test-first for bugs**（GLOBAL-011）：若 context-pack 組裝邏輯發現 regression，先寫 failing test 再修。
- **Documentation sync**（operating-rules.md）：`docs/context-strategy.md §7.1` 中的狀態表需在 PR-1 合併後同步更新，標記原本 `Planned` 的欄位為 `Live`。
