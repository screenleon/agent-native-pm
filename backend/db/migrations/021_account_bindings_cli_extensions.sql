-- Migration 021: Account bindings CLI extensions (Path B Slice S1).
--
-- Extends `account_bindings` so the same row can describe a CLI binding
-- (`provider_id LIKE 'cli:%'`) used by the local connector to dispatch
-- planning runs to a Claude Code or Codex CLI on the operator's host. See
-- design doc §5 (D1, D2, D8) and §6.1.
--
-- Runner contract: the schema_migrations table prevents this from running
-- twice, so `IF NOT EXISTS` is intentionally NOT used on the ALTER TABLE
-- statements (modernc.org/sqlite does not support that clause on ADD COLUMN).
-- See backend/internal/database/migrations.go.

ALTER TABLE account_bindings ADD COLUMN cli_command TEXT NOT NULL DEFAULT '';
ALTER TABLE account_bindings ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE;

-- One primary binding per (user_id, provider_id_namespace) where the
-- namespace is `cli` for cli:* providers and `api` for everything else.
-- The CASE expression keeps API-key bindings and CLI bindings in separate
-- primary slots so a user can have one primary API-key binding AND one
-- primary CLI binding. NOTE: the CLI namespace is shared across all cli:*
-- providers, so the user has only ONE primary CLI binding total — switching
-- the launcher's default from cli:claude to cli:codex requires flipping
-- is_primary on the desired row, which auto-demotes the previous primary
-- within the same TX (see store.demoteOtherPrimaryBindings).
CREATE UNIQUE INDEX idx_account_bindings_primary_unique
    ON account_bindings(user_id,
                        (CASE WHEN provider_id LIKE 'cli:%' THEN 'cli' ELSE 'api' END))
    WHERE is_primary = TRUE;
