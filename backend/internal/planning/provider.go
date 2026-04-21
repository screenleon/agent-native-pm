package planning

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
)

const (
	planningTaskContextLimit     = 100
	planningDocumentContextLimit = 8
	planningDriftContextLimit    = 6
	planningAgentRunContextLimit = 6
	planningEvidenceSampleLimit  = 3
	planningFailureSampleLimit   = 2
)

var (
	ErrUnknownPlanningProvider     = errors.New("unknown planning provider")
	ErrUnknownPlanningModel        = errors.New("unknown planning model")
	ErrPlanningProviderUnavailable = errors.New("planning provider is unavailable")
)

type PlanningContext struct {
	OpenTasks        []models.Task
	RecentDocuments  []models.Document
	OpenDriftSignals []models.DriftSignal
	LatestSyncRun    *models.SyncRun
	RecentAgentRuns  []models.AgentRun
}

type Provider interface {
	Generate(ctx context.Context, requirement *models.Requirement, context PlanningContext, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error)
}

type ContextBuilder interface {
	Build(requirement *models.Requirement) (PlanningContext, error)
}

type DraftPlanner interface {
	ResolveSelection(request models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error)
	Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error)
	Options() models.PlanningProviderOptions
}

type RegisteredProvider struct {
	Descriptor     models.PlanningProviderDescriptor
	Implementation Provider
}

type providerRegistration struct {
	descriptor models.PlanningProviderDescriptor
	provider   Provider
}

type ProviderRegistry struct {
	defaultProviderID string
	defaultModelID    string
	providers         map[string]providerRegistration
}

type OpenAICompatibleProviderConfig struct {
	Enabled           bool
	BaseURL           string
	APIKey            string
	Models            []string
	Timeout           int64
	MaxCandidates     int
	MaxResponseBytes  int64
	DefaultProviderID string
	DefaultModelID    string
}

func NewProviderRegistry(defaultProviderID, defaultModelID string, providers ...RegisteredProvider) (*ProviderRegistry, error) {
	registry := &ProviderRegistry{
		defaultProviderID: strings.TrimSpace(defaultProviderID),
		defaultModelID:    strings.TrimSpace(defaultModelID),
		providers:         map[string]providerRegistration{},
	}
	for _, provider := range providers {
		id := strings.TrimSpace(provider.Descriptor.ID)
		if id == "" {
			return nil, fmt.Errorf("planning provider id is required")
		}
		if provider.Implementation == nil {
			return nil, fmt.Errorf("planning provider %q requires an implementation", id)
		}
		if strings.TrimSpace(provider.Descriptor.DefaultModelID) == "" && len(provider.Descriptor.Models) > 0 {
			provider.Descriptor.DefaultModelID = provider.Descriptor.Models[0].ID
		}
		registry.providers[id] = providerRegistration{descriptor: provider.Descriptor, provider: provider.Implementation}
	}
	if len(registry.providers) == 0 {
		return nil, fmt.Errorf("at least one planning provider must be registered")
	}
	if registry.defaultProviderID == "" {
		for id := range registry.providers {
			registry.defaultProviderID = id
			break
		}
	}
	registration, ok := registry.providers[registry.defaultProviderID]
	if !ok {
		return nil, fmt.Errorf("default planning provider %q: %w", registry.defaultProviderID, ErrUnknownPlanningProvider)
	}
	if registry.defaultModelID == "" {
		registry.defaultModelID = registration.descriptor.DefaultModelID
	}
	if !registry.hasModel(registration.descriptor, registry.defaultModelID) {
		return nil, fmt.Errorf("default planning model %q: %w", registry.defaultModelID, ErrUnknownPlanningModel)
	}
	return registry, nil
}

func (r *ProviderRegistry) Resolve(request models.CreatePlanningRunRequest) (providerRegistration, models.PlanningProviderSelection, error) {
	if r == nil {
		return providerRegistration{}, models.PlanningProviderSelection{}, fmt.Errorf("planning provider registry is required")
	}
	providerID := strings.TrimSpace(request.ProviderID)
	modelID := strings.TrimSpace(request.ModelID)
	selectionSource := models.PlanningSelectionSourceServerDefault
	if providerID == "" {
		providerID = r.defaultProviderID
	} else {
		selectionSource = models.PlanningSelectionSourceRequestOverride
	}
	if modelID != "" {
		selectionSource = models.PlanningSelectionSourceRequestOverride
	}
	if modelID == "" {
		if selectionSource == models.PlanningSelectionSourceServerDefault {
			modelID = r.defaultModelID
		}
	}
	registration, ok := r.providers[providerID]
	if !ok {
		return providerRegistration{}, models.PlanningProviderSelection{}, fmt.Errorf("%w: %s", ErrUnknownPlanningProvider, providerID)
	}
	if modelID == "" {
		modelID = registration.descriptor.DefaultModelID
	}
	if !r.hasModel(registration.descriptor, modelID) {
		return providerRegistration{}, models.PlanningProviderSelection{}, fmt.Errorf("%w: %s", ErrUnknownPlanningModel, modelID)
	}
	return registration, models.PlanningProviderSelection{
		ProviderID:      providerID,
		ModelID:         modelID,
		SelectionSource: selectionSource,
	}, nil
}

func (r *ProviderRegistry) Options() models.PlanningProviderOptions {
	if r == nil {
		return models.PlanningProviderOptions{}
	}
	providers := make([]models.PlanningProviderDescriptor, 0, len(r.providers))
	for _, registration := range r.providers {
		providers = append(providers, registration.descriptor)
	}
	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].ID < providers[j].ID
	})
	return models.PlanningProviderOptions{
		DefaultSelection: models.PlanningProviderSelection{
			ProviderID:      r.defaultProviderID,
			ModelID:         r.defaultModelID,
			SelectionSource: models.PlanningSelectionSourceServerDefault,
		},
		Providers:          providers,
		CanRun:             true,
		AllowModelOverride: r.enabledModelCount(r.defaultProviderID) > 1,
	}
}

func (r *ProviderRegistry) enabledModelCount(providerID string) int {
	if r == nil {
		return 0
	}
	registration, ok := r.providers[providerID]
	if !ok {
		return 0
	}
	count := 0
	for _, model := range registration.descriptor.Models {
		if model.Enabled {
			count++
		}
	}
	return count
}

func (r *ProviderRegistry) hasModel(descriptor models.PlanningProviderDescriptor, modelID string) bool {
	for _, model := range descriptor.Models {
		if model.ID == modelID && model.Enabled {
			return true
		}
	}
	return false
}

type ContextualPlanner struct {
	builder  ContextBuilder
	registry *ProviderRegistry
}

func NewContextualPlanner(builder ContextBuilder, registry *ProviderRegistry) *ContextualPlanner {
	return &ContextualPlanner{builder: builder, registry: registry}
}

func (p *ContextualPlanner) ResolveSelection(request models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error) {
	if p == nil || p.registry == nil {
		return models.PlanningProviderSelection{}, fmt.Errorf("planning provider registry is required")
	}
	_, selection, err := p.registry.Resolve(request)
	if err != nil {
		return models.PlanningProviderSelection{}, err
	}
	return selection, nil
}

func (p *ContextualPlanner) Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	if p == nil || p.registry == nil {
		return nil, fmt.Errorf("planning provider registry is required")
	}
	registration, resolvedSelection, err := p.registry.Resolve(models.CreatePlanningRunRequest{ProviderID: selection.ProviderID, ModelID: selection.ModelID})
	if err != nil {
		return nil, err
	}

	context := PlanningContext{
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
		context = builtContext
	}

	return registration.provider.Generate(ctx, requirement, context, resolvedSelection)
}

func (p *ContextualPlanner) Options() models.PlanningProviderOptions {
	if p == nil || p.registry == nil {
		return models.PlanningProviderOptions{}
	}
	return p.registry.Options()
}

func NewDeterministicProviderRegistry(defaultProviderID, defaultModelID string) (*ProviderRegistry, error) {
	return NewProviderRegistry(
		defaultProviderID,
		defaultModelID,
		RegisteredProvider{
			Descriptor: models.PlanningProviderDescriptor{
				ID:             models.PlanningProviderDeterministic,
				Label:          "Built-in Planning Fallback",
				Kind:           "rule-based",
				Description:    "Internal rule-based fallback used when no external planning provider is configured.",
				DefaultModelID: models.PlanningProviderModelDeterministic,
				Models: []models.PlanningProviderModel{{
					ID:          models.PlanningProviderModelDeterministic,
					Label:       "Built-in Heuristic Engine",
					Description: "Internal heuristic ranking and evidence synthesis, not an external LLM model.",
					Enabled:     true,
				}},
			},
			Implementation: NewDeterministicProvider(),
		},
	)
}

func NewDefaultProviderRegistry(defaultProviderID, defaultModelID string, openAICompat OpenAICompatibleProviderConfig) (*ProviderRegistry, error) {
	providers := []RegisteredProvider{{
		Descriptor: models.PlanningProviderDescriptor{
			ID:             models.PlanningProviderDeterministic,
			Label:          "Built-in Planning Fallback",
			Kind:           "rule-based",
			Description:    "Internal rule-based fallback used when no external planning provider is configured.",
			DefaultModelID: models.PlanningProviderModelDeterministic,
			Models: []models.PlanningProviderModel{{
				ID:          models.PlanningProviderModelDeterministic,
				Label:       "Built-in Heuristic Engine",
				Description: "Internal heuristic ranking and evidence synthesis, not an external LLM model.",
				Enabled:     true,
			}},
		},
		Implementation: NewDeterministicProvider(),
	}}

	registeredOpenAICompat, err := newOpenAICompatibleRegisteredProvider(openAICompat)
	if err != nil {
		return nil, err
	}
	if registeredOpenAICompat != nil {
		providers = append(providers, *registeredOpenAICompat)
	}

	return NewProviderRegistry(defaultProviderID, defaultModelID, providers...)
}

func NewDeterministicPlanner(tasks taskContextSource, documents documentContextSource, driftSignals driftContextSource, syncRuns syncContextSource, agentRuns agentRunContextSource, defaultProviderID, defaultModelID string) (DraftPlanner, error) {
	registry, err := NewDeterministicProviderRegistry(defaultProviderID, defaultModelID)
	if err != nil {
		return nil, err
	}
	return NewContextualPlanner(
		NewProjectContextBuilder(tasks, documents, driftSignals, syncRuns, agentRuns),
		registry,
	), nil
}
