package models

import "time"

const PlanningSettingsSingletonID = "global"

type PlanningSettings struct {
	ProviderID       string     `json:"provider_id"`
	ModelID          string     `json:"model_id"`
	BaseURL          string     `json:"base_url"`
	ConfiguredModels []string   `json:"configured_models"`
	APIKeyConfigured bool       `json:"api_key_configured"`
	CredentialMode   string     `json:"credential_mode"`
	UpdatedBy        string     `json:"updated_by"`
	CreatedAt        *time.Time `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at"`
}

type StoredPlanningSettings struct {
	PlanningSettings
	APIKeyCiphertext string `json:"-"`
}

type UpdatePlanningSettingsRequest struct {
	ProviderID       string   `json:"provider_id"`
	ModelID          string   `json:"model_id"`
	BaseURL          string   `json:"base_url"`
	ConfiguredModels []string `json:"configured_models"`
	APIKey           *string  `json:"api_key,omitempty"`
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
	CredentialMode   *string  `json:"credential_mode,omitempty"`
}

type PlanningSettingsView struct {
	Settings           PlanningSettings `json:"settings"`
	SecretStorageReady bool             `json:"secret_storage_ready"`
}
