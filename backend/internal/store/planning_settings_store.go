package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/secrets"
)

type PlanningSettingsStore struct {
	db  *sql.DB
	box *secrets.Box
}

func NewPlanningSettingsStore(db *sql.DB, box *secrets.Box) *PlanningSettingsStore {
	return &PlanningSettingsStore{db: db, box: box}
}

func (s *PlanningSettingsStore) SecretStorageReady() bool {
	return s != nil && s.box != nil && s.box.Ready()
}

func (s *PlanningSettingsStore) Get() (*models.StoredPlanningSettings, error) {
	if s == nil {
		return nil, nil
	}
	var settings models.StoredPlanningSettings
	var configuredModelsRaw []byte
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	err := s.db.QueryRow(`
		SELECT provider_id, model_id, base_url, configured_models, api_key_ciphertext, api_key_configured, credential_mode, updated_by, created_at, updated_at
		FROM planning_settings
		WHERE id = $1
	`, models.PlanningSettingsSingletonID).Scan(
		&settings.ProviderID,
		&settings.ModelID,
		&settings.BaseURL,
		&configuredModelsRaw,
		&settings.APIKeyCiphertext,
		&settings.APIKeyConfigured,
		&settings.CredentialMode,
		&settings.UpdatedBy,
		&createdAt,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(configuredModelsRaw) > 0 {
		if err := json.Unmarshal(configuredModelsRaw, &settings.ConfiguredModels); err != nil {
			return nil, fmt.Errorf("decode planning configured models: %w", err)
		}
	}
	if createdAt.Valid {
		settings.CreatedAt = &createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = &updatedAt.Time
	}
	normalizeStoredPlanningSettings(&settings)
	return &settings, nil
}

func (s *PlanningSettingsStore) Upsert(req models.UpdatePlanningSettingsRequest, updatedBy string) (*models.StoredPlanningSettings, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("provider_id is required")
	}
	updatedBy = strings.TrimSpace(updatedBy)
	if updatedBy == "" {
		updatedBy = "unknown"
	}

	settings, err := s.Get()
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = &models.StoredPlanningSettings{PlanningSettings: defaultPlanningSettings()}
	}

	switch providerID {
	case models.PlanningProviderDeterministic:
		settings.ProviderID = models.PlanningProviderDeterministic
		settings.ModelID = models.PlanningProviderModelDeterministic
		settings.BaseURL = ""
		settings.ConfiguredModels = []string{models.PlanningProviderModelDeterministic}
		settings.APIKeyCiphertext = ""
		settings.APIKeyConfigured = false
	case models.PlanningProviderOpenAICompatible:
		baseURL, err := normalizePlanningBaseURL(req.BaseURL)
		if err != nil {
			return nil, err
		}
		modelsList := normalizeConfiguredModels(req.ConfiguredModels)
		modelID := strings.TrimSpace(req.ModelID)
		if modelID == "" && len(modelsList) > 0 {
			modelID = modelsList[0]
		}
		if modelID == "" {
			return nil, fmt.Errorf("model_id is required")
		}
		if len(modelsList) == 0 {
			modelsList = []string{modelID}
		}
		if !stringSliceContains(modelsList, modelID) {
			modelsList = append([]string{modelID}, modelsList...)
			modelsList = normalizeConfiguredModels(modelsList)
		}
		settings.ProviderID = models.PlanningProviderOpenAICompatible
		settings.ModelID = modelID
		settings.BaseURL = baseURL
		settings.ConfiguredModels = modelsList
		if req.ClearAPIKey {
			settings.APIKeyCiphertext = ""
			settings.APIKeyConfigured = false
		} else if req.APIKey != nil {
			plaintext := strings.TrimSpace(*req.APIKey)
			if plaintext == "" {
				settings.APIKeyCiphertext = ""
				settings.APIKeyConfigured = false
			} else {
				ciphertext, err := s.encryptAPIKey(plaintext)
				if err != nil {
					return nil, err
				}
				settings.APIKeyCiphertext = ciphertext
				settings.APIKeyConfigured = true
			}
		}
	default:
		return nil, fmt.Errorf("unsupported provider_id: %s", providerID)
	}

	if req.CredentialMode != nil {
		mode := strings.TrimSpace(*req.CredentialMode)
		if mode != "" {
			if !models.ValidCredentialModes[mode] {
				return nil, fmt.Errorf("unsupported credential_mode: %s", mode)
			}
			settings.CredentialMode = mode
		}
	}
	if settings.CredentialMode == "" {
		settings.CredentialMode = models.CredentialModeShared
	}

	configuredModelsJSON, err := json.Marshal(settings.ConfiguredModels)
	if err != nil {
		return nil, fmt.Errorf("encode configured models: %w", err)
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`
		INSERT INTO planning_settings (id, provider_id, model_id, base_url, configured_models, api_key_ciphertext, api_key_configured, credential_mode, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		ON CONFLICT (id) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			model_id = EXCLUDED.model_id,
			base_url = EXCLUDED.base_url,
			configured_models = EXCLUDED.configured_models,
			api_key_ciphertext = EXCLUDED.api_key_ciphertext,
			api_key_configured = EXCLUDED.api_key_configured,
			credential_mode = EXCLUDED.credential_mode,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at
	`, models.PlanningSettingsSingletonID, settings.ProviderID, settings.ModelID, settings.BaseURL, configuredModelsJSON, settings.APIKeyCiphertext, settings.APIKeyConfigured, settings.CredentialMode, updatedBy, now)
	if err != nil {
		return nil, err
	}
	return s.Get()
}

func (s *PlanningSettingsStore) DecryptAPIKey(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Decrypt(ciphertext)
}

func (s *PlanningSettingsStore) encryptAPIKey(plaintext string) (string, error) {
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Encrypt(plaintext)
}

func defaultPlanningSettings() models.PlanningSettings {
	return models.PlanningSettings{
		ProviderID:       models.PlanningProviderDeterministic,
		ModelID:          models.PlanningProviderModelDeterministic,
		BaseURL:          "",
		ConfiguredModels: []string{models.PlanningProviderModelDeterministic},
		APIKeyConfigured: false,
		CredentialMode:   models.CredentialModeShared,
		UpdatedBy:        "",
	}
}

func normalizeStoredPlanningSettings(settings *models.StoredPlanningSettings) {
	if settings == nil {
		return
	}
	switch strings.TrimSpace(settings.ProviderID) {
	case "", models.PlanningProviderDeterministic:
		settings.ProviderID = models.PlanningProviderDeterministic
		settings.ModelID = models.PlanningProviderModelDeterministic
		settings.BaseURL = ""
		settings.ConfiguredModels = []string{models.PlanningProviderModelDeterministic}
		settings.APIKeyConfigured = false
	case models.PlanningProviderOpenAICompatible:
		settings.ConfiguredModels = normalizeConfiguredModels(settings.ConfiguredModels)
		settings.ModelID = strings.TrimSpace(settings.ModelID)
		if settings.ModelID == "" && len(settings.ConfiguredModels) > 0 {
			settings.ModelID = settings.ConfiguredModels[0]
		}
		if len(settings.ConfiguredModels) == 0 && settings.ModelID != "" {
			settings.ConfiguredModels = []string{settings.ModelID}
		}
	}
	if !models.ValidCredentialModes[settings.CredentialMode] {
		settings.CredentialMode = models.CredentialModeShared
	}
}

func normalizeConfiguredModels(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func normalizePlanningBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("base_url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid base_url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("base_url must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("base_url host is required")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("base_url must not include embedded credentials")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
