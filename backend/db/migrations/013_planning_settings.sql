CREATE TABLE IF NOT EXISTS planning_settings (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    configured_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    api_key_ciphertext TEXT NOT NULL DEFAULT '',
    api_key_configured BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO planning_settings (id, provider_id, model_id, base_url, configured_models, api_key_ciphertext, api_key_configured, updated_by)
VALUES ('global', 'deterministic', 'deterministic-v1', '', '["deterministic-v1"]'::jsonb, '', FALSE, 'system')
ON CONFLICT (id) DO NOTHING;