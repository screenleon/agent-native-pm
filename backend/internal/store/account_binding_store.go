package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/secrets"
)

type AccountBindingStore struct {
	db  *sql.DB
	box *secrets.Box
}

type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func NewAccountBindingStore(db *sql.DB, box *secrets.Box) *AccountBindingStore {
	return &AccountBindingStore{db: db, box: box}
}

func deactivateOtherActiveBindings(exec sqlExecutor, userID, providerID, keepID string) error {
	query := `
		UPDATE account_bindings
		SET is_active = FALSE,
		    updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND provider_id = $2 AND is_active = TRUE
	`
	args := []interface{}{userID, providerID}
	if strings.TrimSpace(keepID) != "" {
		query += ` AND id <> $3`
		args = append(args, keepID)
	}
	_, err := exec.Exec(query, args...)
	return err
}

func (s *AccountBindingStore) ListByUser(userID string) ([]models.AccountBinding, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_configured, is_active, created_at, updated_at
		FROM account_bindings
		WHERE user_id = $1
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bindings []models.AccountBinding
	for rows.Next() {
		var b models.AccountBinding
		var configuredModelsRaw []byte
		if err := rows.Scan(
			&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
			&configuredModelsRaw, &b.APIKeyConfigured, &b.IsActive, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(configuredModelsRaw) > 0 {
			if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
				return nil, fmt.Errorf("decode account binding configured models: %w", err)
			}
		}
		if b.ConfiguredModels == nil {
			b.ConfiguredModels = []string{}
		}
		bindings = append(bindings, b)
	}
	if bindings == nil {
		bindings = []models.AccountBinding{}
	}
	return bindings, rows.Err()
}

func (s *AccountBindingStore) GetByID(id, userID string) (*models.StoredAccountBinding, error) {
	var b models.StoredAccountBinding
	var configuredModelsRaw []byte
	err := s.db.QueryRow(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_ciphertext, api_key_configured, is_active, created_at, updated_at
		FROM account_bindings
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
		&configuredModelsRaw, &b.APIKeyCiphertext, &b.APIKeyConfigured, &b.IsActive,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(configuredModelsRaw) > 0 {
		if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
			return nil, fmt.Errorf("decode account binding configured models: %w", err)
		}
	}
	if b.ConfiguredModels == nil {
		b.ConfiguredModels = []string{}
	}
	return &b, nil
}

// GetActiveByUserAndProvider returns the first active binding for a user+provider pair.
func (s *AccountBindingStore) GetActiveByUserAndProvider(userID, providerID string) (*models.StoredAccountBinding, error) {
	var b models.StoredAccountBinding
	var configuredModelsRaw []byte
	err := s.db.QueryRow(`
		SELECT id, user_id, provider_id, label, base_url, model_id, configured_models,
		       api_key_ciphertext, api_key_configured, is_active, created_at, updated_at
		FROM account_bindings
		WHERE user_id = $1 AND provider_id = $2 AND is_active = TRUE
		ORDER BY created_at ASC
		LIMIT 1
	`, userID, providerID).Scan(
		&b.ID, &b.UserID, &b.ProviderID, &b.Label, &b.BaseURL, &b.ModelID,
		&configuredModelsRaw, &b.APIKeyCiphertext, &b.APIKeyConfigured, &b.IsActive,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(configuredModelsRaw) > 0 {
		if err := json.Unmarshal(configuredModelsRaw, &b.ConfiguredModels); err != nil {
			return nil, fmt.Errorf("decode account binding configured models: %w", err)
		}
	}
	if b.ConfiguredModels == nil {
		b.ConfiguredModels = []string{}
	}
	return &b, nil
}

func (s *AccountBindingStore) Create(userID string, req models.CreateAccountBindingRequest) (*models.AccountBinding, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("provider_id is required")
	}
	if providerID != models.PlanningProviderOpenAICompatible {
		return nil, fmt.Errorf("unsupported provider_id for personal binding: %s", providerID)
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "default"
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	modelID := strings.TrimSpace(req.ModelID)
	configuredModels := normalizeConfiguredModels(req.ConfiguredModels)
	if modelID == "" && len(configuredModels) > 0 {
		modelID = configuredModels[0]
	}
	if len(configuredModels) == 0 && modelID != "" {
		configuredModels = []string{modelID}
	}

	var apiKeyCiphertext string
	apiKeyConfigured := false
	if req.APIKey != nil {
		plaintext := strings.TrimSpace(*req.APIKey)
		if plaintext != "" {
			ciphertext, err := s.encryptAPIKey(plaintext)
			if err != nil {
				return nil, err
			}
			apiKeyCiphertext = ciphertext
			apiKeyConfigured = true
		}
	}

	configuredModelsJSON, err := json.Marshal(configuredModels)
	if err != nil {
		return nil, fmt.Errorf("encode configured models: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := deactivateOtherActiveBindings(tx, userID, providerID, ""); err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		INSERT INTO account_bindings (id, user_id, provider_id, label, base_url, model_id, configured_models,
		                              api_key_ciphertext, api_key_configured, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, $10, $10)
	`, id, userID, providerID, label, baseURL, modelID, configuredModelsJSON,
		apiKeyCiphertext, apiKeyConfigured, now)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return nil, fmt.Errorf("a binding with this provider and label already exists")
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.AccountBinding{
		ID:               id,
		UserID:           userID,
		ProviderID:       providerID,
		Label:            label,
		BaseURL:          baseURL,
		ModelID:          modelID,
		ConfiguredModels: configuredModels,
		APIKeyConfigured: apiKeyConfigured,
		IsActive:         true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (s *AccountBindingStore) Update(id, userID string, req models.UpdateAccountBindingRequest) (*models.AccountBinding, error) {
	existing, err := s.GetByID(id, userID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("account binding not found")
	}

	if req.Label != nil {
		existing.Label = strings.TrimSpace(*req.Label)
	}
	if req.BaseURL != nil {
		existing.BaseURL = strings.TrimSpace(*req.BaseURL)
	}
	if req.ModelID != nil {
		existing.ModelID = strings.TrimSpace(*req.ModelID)
	}
	if req.ConfiguredModels != nil {
		existing.ConfiguredModels = normalizeConfiguredModels(req.ConfiguredModels)
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if req.ClearAPIKey {
		existing.APIKeyCiphertext = ""
		existing.APIKeyConfigured = false
	} else if req.APIKey != nil {
		plaintext := strings.TrimSpace(*req.APIKey)
		if plaintext == "" {
			existing.APIKeyCiphertext = ""
			existing.APIKeyConfigured = false
		} else {
			ciphertext, err := s.encryptAPIKey(plaintext)
			if err != nil {
				return nil, err
			}
			existing.APIKeyCiphertext = ciphertext
			existing.APIKeyConfigured = true
		}
	}

	configuredModelsJSON, err := json.Marshal(existing.ConfiguredModels)
	if err != nil {
		return nil, fmt.Errorf("encode configured models: %w", err)
	}

	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if existing.IsActive {
		if err := deactivateOtherActiveBindings(tx, userID, existing.ProviderID, existing.ID); err != nil {
			return nil, err
		}
	}

	_, err = tx.Exec(`
		UPDATE account_bindings
		SET label = $1, base_url = $2, model_id = $3, configured_models = $4,
		    api_key_ciphertext = $5, api_key_configured = $6, is_active = $7, updated_at = $8
		WHERE id = $9 AND user_id = $10
	`, existing.Label, existing.BaseURL, existing.ModelID, configuredModelsJSON,
		existing.APIKeyCiphertext, existing.APIKeyConfigured, existing.IsActive, now, id, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.AccountBinding{
		ID:               existing.ID,
		UserID:           existing.UserID,
		ProviderID:       existing.ProviderID,
		Label:            existing.Label,
		BaseURL:          existing.BaseURL,
		ModelID:          existing.ModelID,
		ConfiguredModels: existing.ConfiguredModels,
		APIKeyConfigured: existing.APIKeyConfigured,
		IsActive:         existing.IsActive,
		CreatedAt:        existing.CreatedAt,
		UpdatedAt:        now,
	}, nil
}

func (s *AccountBindingStore) Delete(id, userID string) error {
	result, err := s.db.Exec(`DELETE FROM account_bindings WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("account binding not found")
	}
	return nil
}

func (s *AccountBindingStore) DecryptAPIKey(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Decrypt(ciphertext)
}

func (s *AccountBindingStore) encryptAPIKey(plaintext string) (string, error) {
	if s == nil || s.box == nil {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	return s.box.Encrypt(plaintext)
}
