package store

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/secrets"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

const testAppSettingsMasterKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestPlanningSettingsStoreUpsertEncryptsAPIKey(t *testing.T) {
	db := testutil.OpenTestDB(t)
	box, err := secrets.NewBox(testAppSettingsMasterKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	store := NewPlanningSettingsStore(db, box)
	apiKey := "lan-secret-token"

	settings, err := store.Upsert(models.UpdatePlanningSettingsRequest{
		ProviderID:       models.PlanningProviderOpenAICompatible,
		ModelID:          "gpt-5-mini",
		BaseURL:          "https://example.com/v1",
		ConfiguredModels: []string{"gpt-5-mini", "gpt-4.1-mini"},
		APIKey:           &apiKey,
	}, "admin")
	if err != nil {
		t.Fatalf("upsert settings: %v", err)
	}
	if !settings.APIKeyConfigured {
		t.Fatal("expected api_key_configured to be true")
	}
	if settings.APIKeyCiphertext == "" {
		t.Fatal("expected encrypted api key to be stored")
	}
	if settings.APIKeyCiphertext == apiKey {
		t.Fatal("expected encrypted api key to differ from plaintext")
	}
	decrypted, err := store.DecryptAPIKey(settings.APIKeyCiphertext)
	if err != nil {
		t.Fatalf("decrypt api key: %v", err)
	}
	if decrypted != apiKey {
		t.Fatalf("expected decrypted key %q, got %q", apiKey, decrypted)
	}

	settings2, err := store.Upsert(models.UpdatePlanningSettingsRequest{
		ProviderID:       models.PlanningProviderOpenAICompatible,
		ModelID:          "gpt-5-mini",
		BaseURL:          "https://example.com/v1",
		ConfiguredModels: []string{"gpt-5-mini"},
		APIKey:           &apiKey,
	}, "admin")
	if err != nil {
		t.Fatalf("second upsert settings: %v", err)
	}
	if settings2.APIKeyCiphertext == settings.APIKeyCiphertext {
		t.Fatal("expected ciphertext to rotate between writes")
	}
}

func TestPlanningSettingsStoreUpsertDefaultsModelToFirstConfiguredModel(t *testing.T) {
	db := testutil.OpenTestDB(t)
	box, err := secrets.NewBox(testAppSettingsMasterKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	store := NewPlanningSettingsStore(db, box)

	settings, err := store.Upsert(models.UpdatePlanningSettingsRequest{
		ProviderID:       models.PlanningProviderOpenAICompatible,
		BaseURL:          "https://example.com/v1",
		ConfiguredModels: []string{"kimi-k2", "qwen3-coder"},
	}, "admin")
	if err != nil {
		t.Fatalf("upsert settings: %v", err)
	}
	if settings.ModelID != "kimi-k2" {
		t.Fatalf("expected first configured model to become default, got %q", settings.ModelID)
	}
	if len(settings.ConfiguredModels) != 2 {
		t.Fatalf("expected configured models to be preserved, got %v", settings.ConfiguredModels)
	}
}

func TestPlanningSettingsStoreGetNormalizesMissingDefaultModel(t *testing.T) {
	db := testutil.OpenTestDB(t)
	box, err := secrets.NewBox(testAppSettingsMasterKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	store := NewPlanningSettingsStore(db, box)

	_, err = db.Exec(`
		UPDATE planning_settings
		SET provider_id = $2,
			model_id = $3,
			base_url = $4,
			configured_models = $5,
			api_key_ciphertext = $6,
			api_key_configured = $7,
			updated_by = $8,
			updated_at = NOW()
		WHERE id = $1
	`, models.PlanningSettingsSingletonID, models.PlanningProviderOpenAICompatible, "", "https://example.com/v1", `["kimi-k2","qwen3-coder"]`, "", false, "admin")
	if err != nil {
		t.Fatalf("update legacy settings: %v", err)
	}

	settings, err := store.Get()
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.ModelID != "kimi-k2" {
		t.Fatalf("expected normalized default model, got %q", settings.ModelID)
	}
	if len(settings.ConfiguredModels) != 2 {
		t.Fatalf("expected configured models to remain available, got %v", settings.ConfiguredModels)
	}
}

func TestPlanningSettingsStoreSwitchToDeterministicClearsRemoteSecret(t *testing.T) {
	db := testutil.OpenTestDB(t)
	box, err := secrets.NewBox(testAppSettingsMasterKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	store := NewPlanningSettingsStore(db, box)
	apiKey := "lan-secret-token"

	_, err = store.Upsert(models.UpdatePlanningSettingsRequest{
		ProviderID:       models.PlanningProviderOpenAICompatible,
		ModelID:          "gpt-5-mini",
		BaseURL:          "https://example.com/v1",
		ConfiguredModels: []string{"gpt-5-mini"},
		APIKey:           &apiKey,
	}, "admin")
	if err != nil {
		t.Fatalf("seed remote settings: %v", err)
	}

	settings, err := store.Upsert(models.UpdatePlanningSettingsRequest{
		ProviderID: models.PlanningProviderDeterministic,
	}, "admin")
	if err != nil {
		t.Fatalf("switch to deterministic: %v", err)
	}
	if settings.ProviderID != models.PlanningProviderDeterministic {
		t.Fatalf("expected deterministic provider, got %s", settings.ProviderID)
	}
	if settings.APIKeyConfigured {
		t.Fatal("expected deterministic settings to clear stored api key")
	}
	if settings.APIKeyCiphertext != "" {
		t.Fatal("expected deterministic settings to clear ciphertext")
	}
}
