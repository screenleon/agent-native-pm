package models

import "time"

const (
	CredentialModeShared            = "shared"
	CredentialModePersonalPreferred = "personal_preferred"
	CredentialModePersonalRequired  = "personal_required"
)

var ValidCredentialModes = map[string]bool{
	CredentialModeShared:            true,
	CredentialModePersonalPreferred: true,
	CredentialModePersonalRequired:  true,
}

// CLI binding provider IDs (Path B Slice S1, design §5 D1).
//
// Adding a new `cli:*` value requires a paired DECISIONS.md entry per
// design §5 D1 / §9 R16.
const (
	AccountBindingProviderCLIClaude = "cli:claude"
	AccountBindingProviderCLICodex  = "cli:codex"
)

// AllowedAccountBindingProviderIDs is the server-side allowlist enforced by
// the Create/Update path. Anything outside this set is rejected with 400.
var AllowedAccountBindingProviderIDs = map[string]bool{
	PlanningProviderOpenAICompatible: true,
	AccountBindingProviderCLIClaude:  true,
	AccountBindingProviderCLICodex:   true,
}

// IsCLIAccountBindingProvider reports whether the given provider id is a
// CLI-based binding (i.e. dispatches to a local CLI subprocess instead of
// an HTTP API). Returns true for any `cli:*` value, even ones not yet in
// the allowlist — callers gating on LocalMode should use this rather than
// direct constant comparisons.
func IsCLIAccountBindingProvider(providerID string) bool {
	if len(providerID) < 4 {
		return false
	}
	return providerID[:4] == "cli:"
}

// Limits enforced on `configured_models` server-side (design §6.2 rule 4,
// §9 R5 envelope budget).
const (
	MaxAccountBindingConfiguredModels = 16
	MaxAccountBindingModelIDLength    = 64
)

// AccountBinding represents a per-user provider credential binding.
type AccountBinding struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	ProviderID       string     `json:"provider_id"`
	Label            string     `json:"label"`
	BaseURL          string     `json:"base_url"`
	ModelID          string     `json:"model_id"`
	ConfiguredModels []string   `json:"configured_models"`
	APIKeyConfigured bool       `json:"api_key_configured"`
	IsActive         bool       `json:"is_active"`
	CliCommand       string     `json:"cli_command"`
	IsPrimary        bool       `json:"is_primary"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	// Probe history (migration 024). All three are null until the first probe.
	LastProbeAt *time.Time `json:"last_probe_at"`
	LastProbeOk *bool      `json:"last_probe_ok"`
	LastProbeMs *int       `json:"last_probe_ms"`
}

// StoredAccountBinding includes the encrypted API key (never serialized to JSON).
type StoredAccountBinding struct {
	AccountBinding
	APIKeyCiphertext string `json:"-"`
}

type CreateAccountBindingRequest struct {
	ProviderID       string   `json:"provider_id"`
	Label            string   `json:"label"`
	BaseURL          string   `json:"base_url"`
	ModelID          string   `json:"model_id"`
	ConfiguredModels []string `json:"configured_models"`
	APIKey           *string  `json:"api_key,omitempty"`
	CliCommand       string   `json:"cli_command,omitempty"`
	IsPrimary        *bool    `json:"is_primary,omitempty"`
}

type UpdateAccountBindingRequest struct {
	Label            *string  `json:"label,omitempty"`
	BaseURL          *string  `json:"base_url,omitempty"`
	ModelID          *string  `json:"model_id,omitempty"`
	ConfiguredModels []string `json:"configured_models,omitempty"`
	APIKey           *string  `json:"api_key,omitempty"`
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
	IsActive         *bool    `json:"is_active,omitempty"`
	CliCommand       *string  `json:"cli_command,omitempty"`
	IsPrimary        *bool    `json:"is_primary,omitempty"`
}
