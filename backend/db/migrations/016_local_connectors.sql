-- Migration 016: Local connector pairing and registry

CREATE TABLE IF NOT EXISTS local_connectors (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label          TEXT NOT NULL DEFAULT '',
    platform       TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    capabilities   JSONB NOT NULL DEFAULT '{}'::jsonb,
    token_hash     TEXT NOT NULL UNIQUE,
    last_seen_at   TIMESTAMPTZ,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_local_connectors_user_id ON local_connectors(user_id);
CREATE INDEX IF NOT EXISTS idx_local_connectors_user_status ON local_connectors(user_id, status);

CREATE TABLE IF NOT EXISTS connector_pairing_sessions (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pairing_code_hash TEXT NOT NULL UNIQUE,
    label             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',
    expires_at        TIMESTAMPTZ NOT NULL,
    connector_id      TEXT REFERENCES local_connectors(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_connector_pairing_sessions_user_id ON connector_pairing_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_connector_pairing_sessions_status ON connector_pairing_sessions(status, expires_at);

COMMENT ON TABLE local_connectors IS 'User-owned local execution connectors paired from personal machines';
COMMENT ON TABLE connector_pairing_sessions IS 'Short-lived pairing sessions used to claim one local connector token';