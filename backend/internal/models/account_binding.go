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

// AccountBinding represents a per-user provider credential binding.
type AccountBinding struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	ProviderID       string    `json:"provider_id"`
	Label            string    `json:"label"`
	BaseURL          string    `json:"base_url"`
	ModelID          string    `json:"model_id"`
	ConfiguredModels []string  `json:"configured_models"`
	APIKeyConfigured bool      `json:"api_key_configured"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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
}

type UpdateAccountBindingRequest struct {
	Label            *string  `json:"label,omitempty"`
	BaseURL          *string  `json:"base_url,omitempty"`
	ModelID          *string  `json:"model_id,omitempty"`
	ConfiguredModels []string `json:"configured_models,omitempty"`
	APIKey           *string  `json:"api_key,omitempty"`
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
	IsActive         *bool    `json:"is_active,omitempty"`
}
