# Credential Binding Architecture — Design Document

> Status: Draft
> Author: [agent:feature-planner]
> Date: 2026-04-17

## Problem

The current `planning_settings` singleton stores one global API key for one shared provider. This has three limitations:

1. **No personal accounts**: Users cannot bind their own credentials; only an admin configures one shared key.
2. **No subscription-friendly path**: Users with GitHub Copilot / ChatGPT subscriptions (not API keys) have no way to route planning through their subscription access.
3. **Testing gap**: Without an API key, there is no way to test the LLM planning flow end-to-end.

## Constraints

- User has **no API keys** for testing. Only subscription accounts (GitHub Copilot, ChatGPT).
- Must remain testable with **Ollama** (free, local, OpenAI-compatible, no API key).
- Must not break the existing singleton settings or deterministic fallback.
- PostgreSQL is the runtime database.
- Secrets encrypted at rest with `APP_SETTINGS_MASTER_KEY` via `secrets.Box` (AES-GCM).

## Architecture Overview

### Provider Modes (3 tiers)

```
┌─────────────────────────────────────────────────────┐
│  Tier 1: Built-in Fallback (deterministic)          │
│  ─ No credentials needed                            │
│  ─ Heuristic planning engine                        │
│  ─ Always available                                 │
├─────────────────────────────────────────────────────┤
│  Tier 2: OpenAI-Compatible (shared or personal)     │
│  ─ Shared: admin configures base_url + api_key      │
│  ─ Personal: user binds own credentials             │
│  ─ Ollama/LM Studio: no api_key, local base_url    │
│  ─ OpenRouter/OpenAI/etc: api_key required          │
├─────────────────────────────────────────────────────┤
│  Tier 3: CLI Bridge (personal only, future)         │
│  ─ Subscription-only (Copilot, ChatGPT desktop)     │
│  ─ Runs on user's local machine, not server         │
│  ─ Requires client-side agent/extension              │
│  ─ Out of scope for v1                              │
└─────────────────────────────────────────────────────┘
```

### Credential Resolution Order

When a planning run is triggered:

1. **User has personal binding for the active provider?** → Use personal credentials.
2. **Shared/workspace binding exists?** → Use shared credentials (current singleton behavior).
3. **Provider allows no-credential access (Ollama)?** → Proceed without credentials.
4. **Nothing available** → Fall back to deterministic provider.

### v1 Scope (This Implementation)

- Extend `planning_settings` to support **workspace-level** (shared) settings (current behavior, preserved).
- Add `account_bindings` table for **per-user** credential binding.
- Support `openai-compatible` provider in both shared and personal modes.
- No API key required for local providers (Ollama, LM Studio, etc.).
- Encrypted storage for personal API keys (same `secrets.Box`).
- CLI bridge (Tier 3) deferred — requires client-side extension architecture.

## Data Model Changes

### New Table: `account_bindings`

```sql
CREATE TABLE IF NOT EXISTS account_bindings (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id   TEXT NOT NULL,                    -- 'openai-compatible'
    label         TEXT NOT NULL DEFAULT '',          -- user-facing display name
    base_url      TEXT NOT NULL DEFAULT '',          -- e.g. http://localhost:11434/v1
    model_id      TEXT NOT NULL DEFAULT '',          -- preferred default model
    configured_models JSONB NOT NULL DEFAULT '[]',  -- available models
    api_key_ciphertext TEXT NOT NULL DEFAULT '',     -- encrypted; empty = no key needed
    api_key_configured BOOLEAN NOT NULL DEFAULT FALSE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, provider_id, label)
);
```

### Modified Planning Resolution

`planning_settings` (singleton) remains the **workspace default**. The new `account_bindings` table holds **per-user overrides**.

Planning run resolution becomes:

```
request → check user's active account_binding
        → if found, use personal binding credentials
        → else, fall back to planning_settings (workspace default)
        → else, use deterministic fallback
```

### Column: `planning_settings.credential_mode`

Add to existing singleton:

```sql
ALTER TABLE planning_settings
  ADD COLUMN credential_mode TEXT NOT NULL DEFAULT 'shared';
  -- 'shared' = use workspace api_key (current behavior)
  -- 'personal_preferred' = prefer user binding, fall back to shared
  -- 'personal_required' = must have personal binding, no shared fallback
```

## API Changes

### Personal Account Binding

| Method | Path | Description |
|--------|------|-------------|
| GET    | `/api/me/account-bindings` | List current user's bindings |
| POST   | `/api/me/account-bindings` | Create a personal binding |
| PATCH  | `/api/me/account-bindings/:id` | Update a personal binding |
| DELETE | `/api/me/account-bindings/:id` | Delete a personal binding |

#### Create binding request

```json
{
  "provider_id": "openai-compatible",
  "label": "My Ollama",
  "base_url": "http://localhost:11434/v1",
  "model_id": "llama3.2",
  "configured_models": ["llama3.2", "codellama"],
  "api_key": ""
}
```

#### Binding response

```json
{
  "data": {
    "id": "uuid",
    "provider_id": "openai-compatible",
    "label": "My Ollama",
    "base_url": "http://localhost:11434/v1",
    "model_id": "llama3.2",
    "configured_models": ["llama3.2", "codellama"],
    "api_key_configured": false,
    "is_active": true,
    "created_at": "2026-04-17T12:00:00Z",
    "updated_at": "2026-04-17T12:00:00Z"
  },
  "error": null,
  "meta": null
}
```

### Planning Settings Extension

`PATCH /api/settings/planning` gains a new optional field:

```json
{
  "credential_mode": "personal_preferred"
}
```

### Planning Provider Options Extension

`GET /api/projects/:id/planning-provider-options` response gains:

```json
{
  "data": {
    "default_selection": { ... },
    "providers": [ ... ],
    "credential_mode": "personal_preferred",
    "user_has_binding": true
  }
}
```

## Security

- Personal API keys encrypted at rest with same `secrets.Box` (AES-GCM).
- Personal bindings scoped by `user_id` — a user can only CRUD their own bindings.
- Admins cannot read other users' plaintext keys (no admin override for personal secrets).
- `api_key_ciphertext` never returned in API responses.
- `ON DELETE CASCADE` ensures binding cleanup when user is deleted.
- Credential resolution logs which binding was used (personal vs shared) in `planning_runs.selection_source`.

## Testing Strategy

### Without API Keys (Ollama)

```bash
# Install and run Ollama locally
curl -fsSL https://ollama.com/install.sh | sh
ollama pull llama3.2

# Ollama serves OpenAI-compatible API at localhost:11434
# No API key needed
```

Configure in the app:
- Provider: `openai-compatible`
- Base URL: `http://host.docker.internal:11434/v1` (from Docker) or `http://localhost:11434/v1` (native)
- Model: `llama3.2`
- API Key: (leave empty)

This works for both shared settings and personal bindings.

### Unit Tests

- Account binding CRUD store tests (same pattern as existing store tests)
- Credential resolver tests: personal > shared > fallback
- Encryption/decryption round-trip for personal keys

### Integration Tests

- Create personal binding → trigger planning run → verify personal credentials used
- Delete personal binding → verify fallback to shared
- `credential_mode=personal_required` with no binding → verify graceful error

## Implementation Order

1. Migration `014_account_bindings.sql`
2. Model: `account_binding.go`
3. Store: `account_binding_store.go` + tests
4. Credential resolver: extend `SettingsBackedPlanner` to check personal bindings
5. Handlers: `account_bindings.go` (CRUD under `/api/me/`)
6. Router: register new routes
7. Frontend: personal binding management UI
8. Frontend: update Model Settings to show credential mode
9. Docs: update `api-surface.md`, `data-model.md`

## Open Questions

1. Should personal bindings support multiple providers per user (e.g., one Ollama + one OpenAI)?
   - **Proposed**: Yes, via `UNIQUE(user_id, provider_id, label)`. Multiple bindings, one active per provider.
2. Should `credential_mode` be per-project or global?
   - **Proposed**: Global for v1 (on `planning_settings`). Per-project override is a future extension.
3. CLI bridge (Tier 3) timeline?
   - **Proposed**: Deferred. Requires client-side architecture (VS Code extension or local agent). Design separately.

Source: [agent:feature-planner]
