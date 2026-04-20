package planning

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type fakePlanningSettingsSource struct {
	settings   *models.StoredPlanningSettings
	decryptErr error
	apiKey     string
}

func (f *fakePlanningSettingsSource) Get() (*models.StoredPlanningSettings, error) {
	return f.settings, nil
}

func (f *fakePlanningSettingsSource) DecryptAPIKey(ciphertext string) (string, error) {
	if f.decryptErr != nil {
		return "", f.decryptErr
	}
	if ciphertext == "" {
		return "", nil
	}
	return f.apiKey, nil
}

func TestSettingsBackedPlannerResolveSelectionUsesSavedSettings(t *testing.T) {
	planner := &SettingsBackedPlanner{
		settings: &fakePlanningSettingsSource{settings: &models.StoredPlanningSettings{
			PlanningSettings: models.PlanningSettings{
				ProviderID:       models.PlanningProviderOpenAICompatible,
				ModelID:          "gpt-5-mini",
				BaseURL:          "https://example.com/v1",
				ConfiguredModels: []string{"gpt-5-mini", "gpt-4.1-mini"},
			},
		}},
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
		maxCandidates:    3,
	}

	selection, err := planner.ResolveSelection(models.CreatePlanningRunRequest{
	})
	if err != nil {
		t.Fatalf("resolve selection: %v", err)
	}
	if selection.ProviderID != models.PlanningProviderOpenAICompatible || selection.ModelID != "gpt-5-mini" {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if selection.SelectionSource != models.PlanningSelectionSourceServerDefault {
		t.Fatalf("expected server default selection source, got %s", selection.SelectionSource)
	}
	if selection.BindingSource != models.PlanningBindingSourceShared {
		t.Fatalf("expected shared binding source, got %s", selection.BindingSource)
	}
}

func TestSettingsBackedPlannerResolveSelectionAllowsModelOverride(t *testing.T) {
	planner := &SettingsBackedPlanner{
		settings: &fakePlanningSettingsSource{settings: &models.StoredPlanningSettings{
			PlanningSettings: models.PlanningSettings{
				ProviderID:       models.PlanningProviderOpenAICompatible,
				ModelID:          "gpt-5-mini",
				BaseURL:          "https://example.com/v1",
				ConfiguredModels: []string{"gpt-5-mini", "gpt-4.1-mini"},
			},
		}},
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
		maxCandidates:    3,
	}

	selection, err := planner.ResolveSelection(models.CreatePlanningRunRequest{ModelID: "gpt-4.1-mini"})
	if err != nil {
		t.Fatalf("resolve selection with model override: %v", err)
	}
	if selection.ProviderID != models.PlanningProviderOpenAICompatible || selection.ModelID != "gpt-4.1-mini" {
		t.Fatalf("unexpected override selection: %+v", selection)
	}
	if selection.SelectionSource != models.PlanningSelectionSourceRequestOverride {
		t.Fatalf("expected request override selection source, got %s", selection.SelectionSource)
	}
}

func TestSettingsBackedPlannerGenerateFailsWhenStoredSecretCannotBeDecrypted(t *testing.T) {
	planner := &SettingsBackedPlanner{
		settings: &fakePlanningSettingsSource{
			settings: &models.StoredPlanningSettings{
				PlanningSettings: models.PlanningSettings{
					ProviderID:       models.PlanningProviderOpenAICompatible,
					ModelID:          "gpt-5-mini",
					BaseURL:          "https://example.com/v1",
					ConfiguredModels: []string{"gpt-5-mini"},
				},
				APIKeyCiphertext: "encrypted",
			},
			decryptErr: fmt.Errorf("decrypt failed"),
		},
		maxResponseBytes: defaultOpenAICompatibleMaxBytes,
		maxCandidates:    3,
	}

	_, err := planner.Generate(context.Background(), &models.Requirement{ProjectID: "project-1", Title: "Use model config"}, models.PlanningProviderSelection{})
	if err == nil || !strings.Contains(err.Error(), "decrypt failed") {
		t.Fatalf("expected decrypt failure, got %v", err)
	}
}
