-- Phase 2 補強: Drift signal 結構化 trigger_meta、severity 與 sync_run 關聯

ALTER TABLE drift_signals
    ADD COLUMN IF NOT EXISTS trigger_meta  JSONB    NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS severity      SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS sync_run_id   TEXT     REFERENCES sync_runs(id) ON DELETE SET NULL;

-- 便於依緊急度排序
CREATE INDEX IF NOT EXISTS idx_drift_signals_severity   ON drift_signals(severity DESC);
-- 便於依 sync run 分群查詢
CREATE INDEX IF NOT EXISTS idx_drift_signals_sync_run   ON drift_signals(sync_run_id);
