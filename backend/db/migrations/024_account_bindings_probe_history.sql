-- Migration 024: persist the last probe result on each account binding.
--
-- Three nullable columns added (all default to NULL so existing rows carry no
-- misleading data):
--
--   last_probe_at  -- wall-clock timestamp of the most recent probe attempt
--   last_probe_ok  -- TRUE = probe succeeded, FALSE = any failure
--   last_probe_ms  -- HTTP round-trip latency in milliseconds
--
-- No index added: these columns are read alongside the binding row and are
-- never used as a filter predicate.
--
-- schema_migrations prevents re-application, so do not write IF NOT EXISTS
-- on ALTER TABLE ADD COLUMN (modernc.org/sqlite refuses).

ALTER TABLE account_bindings ADD COLUMN last_probe_at  TIMESTAMPTZ;
ALTER TABLE account_bindings ADD COLUMN last_probe_ok  BOOLEAN;
ALTER TABLE account_bindings ADD COLUMN last_probe_ms  INTEGER;
