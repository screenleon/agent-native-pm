package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type fakePlanningRunStore struct {
	run              *models.PlanningRun
	markRunningCalls int
	completeCalls    int
	failCalls        []string
	completeErr      error
}

func (f *fakePlanningRunStore) CreateWithBinding(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection, snapshot *models.PlanningRunBindingSnapshot) (*models.PlanningRun, error) {
	run, err := f.Create(projectID, requirementID, requestedByUserID, request, selection)
	if err != nil || run == nil {
		return run, err
	}
	if snapshot != nil {
		run.ConnectorCliInfo = &models.PlanningRunCliInfo{BindingSnapshot: snapshot}
	}
	return run, nil
}

func (f *fakePlanningRunStore) Create(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection) (*models.PlanningRun, error) {
	executionMode := models.PlanningExecutionModeDeterministic
	if request.ExecutionMode != "" {
		executionMode = request.ExecutionMode
	}
	dispatchStatus := models.PlanningDispatchStatusNotRequired
	if executionMode == models.PlanningExecutionModeLocalConnector {
		dispatchStatus = models.PlanningDispatchStatusQueued
	}
	f.run = &models.PlanningRun{
		ID:                "planning-1",
		ProjectID:         projectID,
		RequirementID:     requirementID,
		Status:            models.PlanningRunStatusQueued,
		TriggerSource:     request.TriggerSource,
		ProviderID:        selection.ProviderID,
		ModelID:           selection.ModelID,
		SelectionSource:   selection.SelectionSource,
		RequestedByUserID: requestedByUserID,
		ExecutionMode:     executionMode,
		DispatchStatus:    dispatchStatus,
	}
	if f.run.TriggerSource == "" {
		f.run.TriggerSource = "manual"
	}
	return f.run, nil
}

func (f *fakePlanningRunStore) MarkRunning(id string) error {
	f.markRunningCalls++
	f.run.Status = models.PlanningRunStatusRunning
	return nil
}

func (f *fakePlanningRunStore) Complete(id string) error {
	f.completeCalls++
	if f.completeErr != nil {
		return f.completeErr
	}
	f.run.Status = models.PlanningRunStatusCompleted
	return nil
}

func (f *fakePlanningRunStore) Fail(id, errorMessage string) error {
	f.failCalls = append(f.failCalls, errorMessage)
	f.run.Status = models.PlanningRunStatusFailed
	f.run.ErrorMessage = errorMessage
	return nil
}

func (f *fakePlanningRunStore) GetByID(id string) (*models.PlanningRun, error) {
	return f.run, nil
}

type fakeBacklogCandidateStore struct {
	createErr   error
	deleteCalls int
	createdRuns []string
}

func (f *fakeBacklogCandidateStore) CreateDraftsForPlanningRun(requirement *models.Requirement, planningRunID string, drafts []models.BacklogCandidateDraft) ([]models.BacklogCandidate, error) {
	f.createdRuns = append(f.createdRuns, planningRunID)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return []models.BacklogCandidate{{ID: "candidate-1", PlanningRunID: planningRunID, Rank: 1}}, nil
}

func (f *fakeBacklogCandidateStore) DeleteByPlanningRun(planningRunID string) error {
	f.deleteCalls++
	return nil
}

type fakeAgentRunStore struct {
	run           *models.AgentRun
	createCalls   int
	updateHistory []models.UpdateAgentRunRequest
	createErr     error
}

func (f *fakeAgentRunStore) CreateOrGetByIdempotency(projectID string, req models.CreateAgentRunRequest) (*models.AgentRun, bool, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, false, f.createErr
	}
	f.run = &models.AgentRun{
		ID:         "agent-1",
		ProjectID:  projectID,
		AgentName:  req.AgentName,
		ActionType: req.ActionType,
		Status:     models.AgentRunStatusRunning,
		Summary:    req.Summary,
	}
	return f.run, false, nil
}

func (f *fakeAgentRunStore) Update(id string, req models.UpdateAgentRunRequest) (*models.AgentRun, error) {
	f.updateHistory = append(f.updateHistory, req)
	if req.Status != "" {
		f.run.Status = req.Status
	}
	if req.Summary != nil {
		f.run.Summary = *req.Summary
	}
	if req.ErrorMessage != nil {
		f.run.ErrorMessage = *req.ErrorMessage
	}
	return f.run, nil
}

type fakeCandidateGenerator struct {
	err              error
	drafts           []models.BacklogCandidateDraft
	selection        models.PlanningProviderSelection
	resolveErr       error
	resolvedRequests []models.CreatePlanningRunRequest
}

func (f *fakeCandidateGenerator) ResolveSelection(request models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error) {
	f.resolvedRequests = append(f.resolvedRequests, request)
	if f.resolveErr != nil {
		return models.PlanningProviderSelection{}, f.resolveErr
	}
	selection := f.selection
	if selection.ProviderID == "" {
		selection.ProviderID = models.PlanningProviderDeterministic
	}
	if selection.ModelID == "" {
		selection.ModelID = models.PlanningProviderModelDeterministic
	}
	if selection.SelectionSource == "" {
		selection.SelectionSource = models.PlanningSelectionSourceServerDefault
	}
	return selection, nil
}

func (f *fakeCandidateGenerator) Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	_ = ctx
	f.selection = selection
	if f.err != nil {
		return nil, f.err
	}
	if len(f.drafts) > 0 {
		return f.drafts, nil
	}
	return []models.BacklogCandidateDraft{{Title: requirement.Title, Description: "Primary", Rationale: "Why now", PriorityScore: 82, Confidence: 79, Rank: 1}}, nil
}

func TestOrchestratorRunCompletesPlanningAndAgentRun(t *testing.T) {
	planningRuns := &fakePlanningRunStore{}
	candidates := &fakeBacklogCandidateStore{}
	agentRuns := &fakeAgentRunStore{}
	generator := &fakeCandidateGenerator{}
	orchestrator := NewOrchestrator(planningRuns, agentRuns, candidates, generator)

	requirement := &models.Requirement{ID: "req-1", ProjectID: "project-1", Title: "Plan sync recovery"}
	run, err := orchestrator.Run(context.Background(), requirement, models.CreatePlanningRunRequest{TriggerSource: "manual"}, "")
	if err != nil {
		t.Fatalf("run planning orchestrator: %v", err)
	}
	if run.Status != models.PlanningRunStatusCompleted {
		t.Fatalf("expected completed planning run, got %s", run.Status)
	}
	if planningRuns.markRunningCalls != 1 {
		t.Fatalf("expected mark running once, got %d", planningRuns.markRunningCalls)
	}
	if planningRuns.completeCalls != 1 {
		t.Fatalf("expected complete once, got %d", planningRuns.completeCalls)
	}
	if agentRuns.createCalls != 1 {
		t.Fatalf("expected one agent run create, got %d", agentRuns.createCalls)
	}
	if run.ProviderID != models.PlanningProviderDeterministic || run.ModelID != models.PlanningProviderModelDeterministic {
		t.Fatalf("expected resolved provider selection on run, got provider=%s model=%s", run.ProviderID, run.ModelID)
	}
	if len(agentRuns.updateHistory) != 1 || agentRuns.run.Status != models.AgentRunStatusCompleted {
		t.Fatalf("expected completed agent run update, got history=%d status=%s", len(agentRuns.updateHistory), agentRuns.run.Status)
	}
	if candidates.deleteCalls != 0 {
		t.Fatalf("expected no candidate cleanup, got %d", candidates.deleteCalls)
	}
}

func TestOrchestratorRunFailsAndCleansUpCandidates(t *testing.T) {
	planningRuns := &fakePlanningRunStore{}
	candidates := &fakeBacklogCandidateStore{createErr: errors.New("boom")}
	agentRuns := &fakeAgentRunStore{}
	generator := &fakeCandidateGenerator{}
	orchestrator := NewOrchestrator(planningRuns, agentRuns, candidates, generator)

	requirement := &models.Requirement{ID: "req-1", ProjectID: "project-1", Title: "Plan sync recovery"}
	_, err := orchestrator.Run(context.Background(), requirement, models.CreatePlanningRunRequest{TriggerSource: "manual"}, "")
	if !errors.Is(err, ErrPersistDraftCandidates) {
		t.Fatalf("expected ErrPersistDraftCandidates, got %v", err)
	}
	if planningRuns.run.Status != models.PlanningRunStatusFailed {
		t.Fatalf("expected failed planning run, got %s", planningRuns.run.Status)
	}
	if candidates.deleteCalls != 1 {
		t.Fatalf("expected candidate cleanup once, got %d", candidates.deleteCalls)
	}
	if len(agentRuns.updateHistory) != 1 {
		t.Fatalf("expected one failed agent update, got %d", len(agentRuns.updateHistory))
	}
	if agentRuns.run.Status != models.AgentRunStatusFailed {
		t.Fatalf("expected failed agent run, got %s", agentRuns.run.Status)
	}
	if agentRuns.run.ErrorMessage != ErrPersistDraftCandidates.Error() {
		t.Fatalf("expected agent error message %q, got %q", ErrPersistDraftCandidates.Error(), agentRuns.run.ErrorMessage)
	}
}

func TestOrchestratorRunFailsWhenCandidateGenerationFails(t *testing.T) {
	planningRuns := &fakePlanningRunStore{}
	candidates := &fakeBacklogCandidateStore{}
	agentRuns := &fakeAgentRunStore{}
	generator := &fakeCandidateGenerator{err: errors.New("generator failed")}
	orchestrator := NewOrchestrator(planningRuns, agentRuns, candidates, generator)

	requirement := &models.Requirement{ID: "req-1", ProjectID: "project-1", Title: "Plan sync recovery"}
	_, err := orchestrator.Run(context.Background(), requirement, models.CreatePlanningRunRequest{TriggerSource: "manual"}, "")
	if !errors.Is(err, ErrGenerateDraftCandidates) {
		t.Fatalf("expected ErrGenerateDraftCandidates, got %v", err)
	}
	if planningRuns.run.Status != models.PlanningRunStatusFailed {
		t.Fatalf("expected failed planning run, got %s", planningRuns.run.Status)
	}
	if candidates.deleteCalls != 1 {
		t.Fatalf("expected candidate cleanup once, got %d", candidates.deleteCalls)
	}
	if agentRuns.run.Status != models.AgentRunStatusFailed {
		t.Fatalf("expected failed agent run, got %s", agentRuns.run.Status)
	}
}
