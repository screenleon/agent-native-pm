package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

func TestDeterministicProviderGenerateUsesRichContextSignals(t *testing.T) {
	provider := NewDeterministicProvider()
	now := time.Now().UTC()

	drafts, err := provider.Generate(
		context.Background(),
		&models.Requirement{
			ProjectID:   "project-1",
			Title:       "Improve sync recovery UX",
			Summary:     "Expose recovery options before creating tasks",
			Description: "Users should be able to review ranked suggestions before tasks are created.",
		},
		PlanningContext{
			OpenTasks:        []models.Task{{ID: "task-1", Title: "Improve sync recovery UX", Status: "todo"}},
			RecentDocuments:  []models.Document{{ID: "doc-1", Title: "Sync Recovery Guide", IsStale: true, UpdatedAt: now}},
			OpenDriftSignals: []models.DriftSignal{{ID: "drift-1", DocumentTitle: "Sync Recovery Guide", TriggerType: "code_change", Severity: 3}},
			LatestSyncRun:    &models.SyncRun{ID: "sync-1", Status: "failed", ErrorMessage: "unknown revision"},
			RecentAgentRuns:  []models.AgentRun{{ID: "agent-1", AgentName: "agent:sync", ActionType: "sync", Status: models.AgentRunStatusFailed}},
		},
		models.PlanningProviderSelection{ProviderID: models.PlanningProviderDeterministic, ModelID: models.PlanningProviderModelDeterministic, SelectionSource: models.PlanningSelectionSourceServerDefault},
	)
	if err != nil {
		t.Fatalf("generate drafts: %v", err)
	}
	if len(drafts) != 3 {
		t.Fatalf("expected 3 drafts, got %d", len(drafts))
	}

	var implementation models.BacklogCandidateDraft
	var integration models.BacklogCandidateDraft
	var validation models.BacklogCandidateDraft
	for _, draft := range drafts {
		switch draft.SuggestionType {
		case "implementation":
			implementation = draft
		case "integration":
			integration = draft
		case "validation":
			validation = draft
		}
	}
	if len(implementation.DuplicateTitles) != 1 {
		t.Fatalf("expected implementation duplicate detection, got %+v", implementation.DuplicateTitles)
	}
	if !containsEvidence(validation.Evidence, "Open drift signals relevant to planning") {
		t.Fatalf("expected validation evidence to mention drift signals, got %+v", validation.Evidence)
	}
	if !containsEvidence(validation.Evidence, "Latest sync failed") {
		t.Fatalf("expected validation evidence to mention sync failure, got %+v", validation.Evidence)
	}
	if !containsEvidence(integration.Evidence, "Related project context from documents") {
		t.Fatalf("expected integration evidence to mention documents, got %+v", integration.Evidence)
	}
	if validation.PriorityScore <= integration.PriorityScore {
		t.Fatalf("expected validation score to outrank integration when drift and failures exist, got validation=%v integration=%v", validation.PriorityScore, integration.PriorityScore)
	}
}

func containsEvidence(evidence []string, fragment string) bool {
	for _, item := range evidence {
		if strings.Contains(item, fragment) {
			return true
		}
	}
	return false
}
