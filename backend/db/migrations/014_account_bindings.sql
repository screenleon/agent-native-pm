-- Migration 014: Account bindings for personal credential management
-- Adds per-user account binding table and credential_mode to planning_settings.

-- Per-user account bindings
CREATE TABLE IF NOT EXISTS account_bindings (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id        TEXT NOT NULL,
    label              TEXT NOT NULL DEFAULT '',
    base_url           TEXT NOT NULL DEFAULT '',
    model_id           TEXT NOT NULL DEFAULT '',
    configured_models  JSONB NOT NULL DEFAULT '[]',
    api_key_ciphertext TEXT NOT NULL DEFAULT '',
    api_key_configured BOOLEAN NOT NULL DEFAULT FALSE,
    is_active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, provider_id, label)
);

CREATE INDEX IF NOT EXISTS idx_account_bindings_user_id ON account_bindings(user_id);
CREATE INDEX IF NOT EXISTS idx_account_bindings_user_provider ON account_bindings(user_id, provider_id) WHERE is_active = TRUE;

-- Credential mode on planning_settings singleton
ALTER TABLE planning_settings
    ADD COLUMN IF NOT EXISTS credential_mode TEXT NOT NULL DEFAULT 'shared';

COMMENT ON COLUMN planning_settings.credential_mode IS 'shared | personal_preferred | personal_required';
COMMENT ON TABLE account_bindings IS 'Per-user provider credential bindings for planning and agent operations';
