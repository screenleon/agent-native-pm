package planning

import (
	"context"
	"fmt"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type planningSettingsSource interface {
	Get() (*models.StoredPlanningSettings, error)
	DecryptAPIKey(ciphertext string) (string, error)
}

type accountBindingSource interface {
	GetActiveByUserAndProvider(userID, providerID string) (*models.StoredAccountBinding, error)
	DecryptAPIKey(ciphertext string) (string, error)
}

type SettingsBackedPlanner struct {
	builder          ContextBuilder
	settings         planningSettingsSource
	bindings         accountBindingSource
	callerUserID     string
	requestTimeout   int64
	maxResponseBytes int64
	maxCandidates    int
}

type planningResolution struct {
	registry          *ProviderRegistry
	credentialMode    string
	bindingSource     string
	bindingLabel      string
	canRun            bool
	unavailableReason string
}

func NewSettingsBackedPlanner(tasks taskContextSource, documents documentContextSource, driftSignals driftContextSource, syncRuns syncContextSource, agentRuns agentRunContextSource, settings planningSettingsSource, maxResponseBytes int64) DraftPlanner {
	return &SettingsBackedPlanner{
		builder:          NewProjectContextBuilder(tasks, documents, driftSignals, syncRuns, agentRuns),
		settings:         settings,
		maxResponseBytes: maxResponseBytes,
		maxCandidates:    3,
	}
}

// NewSettingsBackedPlannerWithBindings creates a planner that checks personal bindings before shared settings.
func NewSettingsBackedPlannerWithBindings(tasks taskContextSource, documents documentContextSource, driftSignals driftContextSource, syncRuns syncContextSource, agentRuns agentRunContextSource, settings planningSettingsSource, bindings accountBindingSource, callerUserID string, maxResponseBytes int64) DraftPlanner {
	return &SettingsBackedPlanner{
		builder:          NewProjectContextBuilder(tasks, documents, driftSignals, syncRuns, agentRuns),
		settings:         settings,
		bindings:         bindings,
		callerUserID:     callerUserID,
		maxResponseBytes: maxResponseBytes,
		maxCandidates:    3,
	}
}

func (p *SettingsBackedPlanner) ResolveSelection(request models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error) {
	resolved, err := p.resolve(false, false)
	if err != nil {
		return models.PlanningProviderSelection{}, err
	}
	_, selection, err := resolved.registry.Resolve(request)
	if err != nil {
		return models.PlanningProviderSelection{}, err
	}
	selection.BindingSource = resolved.bindingSource
	selection.BindingLabel = resolved.bindingLabel
	return selection, nil
}

func (p *SettingsBackedPlanner) Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	resolved, err := p.resolve(true, false)
	if err != nil {
		return nil, err
	}
	_, resolvedSelection, err := resolved.registry.Resolve(models.CreatePlanningRunRequest{
		ProviderID: selection.ProviderID,
		ModelID:    selection.ModelID,
	})
	if err != nil {
		return nil, err
	}
	resolvedSelection.BindingSource = resolved.bindingSource
	resolvedSelection.BindingLabel = resolved.bindingLabel

	planningContext := PlanningContext{
		OpenTasks:        []models.Task{},
		RecentDocuments:  []models.Document{},
		OpenDriftSignals: []models.DriftSignal{},
		RecentAgentRuns:  []models.AgentRun{},
	}
	if p.builder != nil {
		builtContext, err := p.builder.Build(requirement)
		if err != nil {
			return nil, fmt.Errorf("build planning context: %w", err)
		}
		planningContext = builtContext
	}

	registration, _, err := resolved.registry.Resolve(models.CreatePlanningRunRequest{
		ProviderID: resolvedSelection.ProviderID,
		ModelID:    resolvedSelection.ModelID,
	})
	if err != nil {
		return nil, err
	}
	return registration.provider.Generate(ctx, requirement, planningContext, resolvedSelection)
}

func (p *SettingsBackedPlanner) Options() models.PlanningProviderOptions {
	resolved, err := p.resolve(false, true)
	if err != nil {
		return models.PlanningProviderOptions{
			CredentialMode:    models.CredentialModeShared,
			CanRun:            false,
			UnavailableReason: err.Error(),
		}
	}
	if resolved.registry == nil {
		return models.PlanningProviderOptions{
			CredentialMode:        resolved.credentialMode,
			ResolvedBindingSource: resolved.bindingSource,
			ResolvedBindingLabel:  resolved.bindingLabel,
			CanRun:                resolved.canRun,
			UnavailableReason:     resolved.unavailableReason,
		}
	}

	options := resolved.registry.Options()
	options.CredentialMode = resolved.credentialMode
	options.ResolvedBindingSource = resolved.bindingSource
	options.ResolvedBindingLabel = resolved.bindingLabel
	options.CanRun = resolved.canRun
	options.UnavailableReason = resolved.unavailableReason
	options.AllowModelOverride = resolved.registry.enabledModelCount(options.DefaultSelection.ProviderID) > 1
	return options
}

func (p *SettingsBackedPlanner) resolve(includeSecret, tolerateUnavailable bool) (*planningResolution, error) {
	storedSettings, err := p.settings.Get()
	if err != nil {
		return nil, err
	}

	credentialMode := models.CredentialModeShared
	if storedSettings != nil && storedSettings.CredentialMode != "" {
		credentialMode = storedSettings.CredentialMode
	}

	sharedProviderID := models.PlanningProviderDeterministic
	sharedModelID := models.PlanningProviderModelDeterministic
	sharedBindingSource := models.PlanningBindingSourceSystem
	sharedOpenAICompat := p.baseOpenAICompatConfig()

	if storedSettings != nil {
		if storedSettings.ProviderID != "" {
			sharedProviderID = storedSettings.ProviderID
		}
		if storedSettings.ModelID != "" {
			sharedModelID = storedSettings.ModelID
		}
		if storedSettings.ProviderID == models.PlanningProviderOpenAICompatible {
			sharedBindingSource = models.PlanningBindingSourceShared
			sharedOpenAICompat.Enabled = true
			sharedOpenAICompat.BaseURL = storedSettings.BaseURL
			sharedOpenAICompat.Models = append([]string(nil), storedSettings.ConfiguredModels...)
			if includeSecret && storedSettings.APIKeyCiphertext != "" {
				apiKey, err := p.settings.DecryptAPIKey(storedSettings.APIKeyCiphertext)
				if err != nil {
					return nil, err
				}
				sharedOpenAICompat.APIKey = apiKey
			}
		}
	}

	if p.bindings != nil && p.callerUserID != "" && credentialMode != models.CredentialModeShared {
		binding, err := p.bindings.GetActiveByUserAndProvider(p.callerUserID, models.PlanningProviderOpenAICompatible)
		if err != nil {
			return nil, fmt.Errorf("resolve personal binding: %w", err)
		}
		if binding != nil {
			personalOpenAICompat := p.baseOpenAICompatConfig()
			personalOpenAICompat.Enabled = true
			personalOpenAICompat.BaseURL = binding.BaseURL
			personalOpenAICompat.Models = append([]string(nil), binding.ConfiguredModels...)
			if includeSecret && binding.APIKeyCiphertext != "" {
				apiKey, err := p.bindings.DecryptAPIKey(binding.APIKeyCiphertext)
				if err != nil {
					return nil, err
				}
				personalOpenAICompat.APIKey = apiKey
			}
			registry, err := NewDefaultProviderRegistry(binding.ProviderID, binding.ModelID, personalOpenAICompat)
			if err != nil {
				return nil, err
			}
			return &planningResolution{
				registry:       registry,
				credentialMode: credentialMode,
				bindingSource:  models.PlanningBindingSourcePersonal,
				bindingLabel:   binding.Label,
				canRun:         true,
			}, nil
		}
	}

	if credentialMode == models.CredentialModePersonalRequired {
		previewRegistry, previewErr := NewDefaultProviderRegistry(sharedProviderID, sharedModelID, sharedOpenAICompat)
		if tolerateUnavailable {
			resolution := &planningResolution{
				registry:          previewRegistry,
				credentialMode:    credentialMode,
				bindingSource:     sharedBindingSource,
				canRun:            false,
				unavailableReason: "personal_required is enabled but no active personal binding is configured for this user",
			}
			if previewErr != nil {
				resolution.unavailableReason = previewErr.Error()
			}
			return resolution, nil
		}
		return nil, fmt.Errorf("credential_mode is personal_required but no active personal binding found for user")
	}

	registry, err := NewDefaultProviderRegistry(sharedProviderID, sharedModelID, sharedOpenAICompat)
	if err != nil {
		return nil, err
	}
	return &planningResolution{
		registry:       registry,
		credentialMode: credentialMode,
		bindingSource:  sharedBindingSource,
		canRun:         true,
	}, nil
}

func (p *SettingsBackedPlanner) baseOpenAICompatConfig() OpenAICompatibleProviderConfig {
	return OpenAICompatibleProviderConfig{
		Timeout:          45,
		MaxCandidates:    p.maxCandidates,
		MaxResponseBytes: p.maxResponseBytes,
	}
}
