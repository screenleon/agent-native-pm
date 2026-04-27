package planning

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// SnapshotSaver persists a context snapshot after a planning context is built.
// Implementations must be safe to call concurrently. Save is fire-and-forget
// from the orchestrator's perspective: a failure must not abort the run.
type SnapshotSaver interface {
	Save(snap store.ContextSnapshot) error
}

const (
	PlannerAgentName = "agent:planning-orchestrator"
	plannerAction    = "review"
)

var (
	ErrStartPlanningRun        = errors.New("failed to start planning run")
	ErrCreatePlanningAgentRun  = errors.New("failed to create planning agent run")
	ErrGenerateDraftCandidates = errors.New("failed to generate draft backlog candidates")
	ErrPersistDraftCandidates  = errors.New("failed to persist draft backlog candidates")
	ErrFinalizePlanningAgent   = errors.New("failed to finalize planning agent run")
	ErrCompletePlanningRun     = errors.New("failed to complete planning run")
)

type planningRunStore interface {
	Create(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection) (*models.PlanningRun, error)
	CreateWithBinding(projectID, requirementID, requestedByUserID string, request models.CreatePlanningRunRequest, selection models.PlanningProviderSelection, snapshot *models.PlanningRunBindingSnapshot) (*models.PlanningRun, error)
	MarkRunning(id string) error
	Complete(id string) error
	Fail(id, errorMessage string) error
	GetByID(id string) (*models.PlanningRun, error)
}

type backlogCandidateStore interface {
	CreateDraftsForPlanningRun(requirement *models.Requirement, planningRunID string, drafts []models.BacklogCandidateDraft) ([]models.BacklogCandidate, error)
	DeleteByPlanningRun(planningRunID string) error
}

type candidateGenerator interface {
	ResolveSelection(request models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error)
	Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error)
}

type agentRunStore interface {
	CreateOrGetByIdempotency(projectID string, req models.CreateAgentRunRequest) (*models.AgentRun, bool, error)
	Update(id string, req models.UpdateAgentRunRequest) (*models.AgentRun, error)
}

type Orchestrator struct {
	planningRuns    planningRunStore
	agentRuns       agentRunStore
	candidates      backlogCandidateStore
	generator       candidateGenerator
	snapshotSaver   SnapshotSaver
}

func NewOrchestrator(planningRuns planningRunStore, agentRuns agentRunStore, candidates backlogCandidateStore, generator candidateGenerator) *Orchestrator {
	return &Orchestrator{
		planningRuns: planningRuns,
		agentRuns:    agentRuns,
		candidates:   candidates,
		generator:    generator,
	}
}

// WithSnapshotSaver attaches a SnapshotSaver so the orchestrator can persist
// a PlanningContextV2 snapshot after context is built. When nil (default)
// snapshot saving is silently skipped.
func (o *Orchestrator) WithSnapshotSaver(s SnapshotSaver) *Orchestrator {
	o.snapshotSaver = s
	return o
}

func (o *Orchestrator) Run(ctx context.Context, requirement *models.Requirement, request models.CreatePlanningRunRequest, requestedByUserID string) (*models.PlanningRun, error) {
	return o.RunWithBindingSnapshot(ctx, requirement, request, requestedByUserID, nil)
}

// RunWithBindingSnapshot is the Path B S2 entry point: same orchestration as
// Run, but the caller may pass a pre-resolved binding snapshot that gets
// embedded into the run's connector_cli_info column at INSERT time. The
// caller is responsible for the three-way ownership check + active-check
// (handler does this in planning_runs.Create).
func (o *Orchestrator) RunWithBindingSnapshot(ctx context.Context, requirement *models.Requirement, request models.CreatePlanningRunRequest, requestedByUserID string, snapshot *models.PlanningRunBindingSnapshot) (*models.PlanningRun, error) {
	if requirement == nil {
		return nil, fmt.Errorf("%w: requirement is required", ErrStartPlanningRun)
	}
	selection, err := o.generator.ResolveSelection(request)
	if err != nil {
		return nil, errors.Join(ErrStartPlanningRun, err)
	}

	run, err := o.planningRuns.CreateWithBinding(requirement.ProjectID, requirement.ID, requestedByUserID, request, selection, snapshot)
	if err != nil {
		return nil, err
	}
	agentSummary := buildRunningSummary(requirement, selection)
	if run.ExecutionMode == models.PlanningExecutionModeLocalConnector {
		agentSummary = buildQueuedSummary(requirement, selection)
	} else if err := o.planningRuns.MarkRunning(run.ID); err != nil {
		_ = o.planningRuns.Fail(run.ID, ErrStartPlanningRun.Error())
		return nil, fmt.Errorf("%w: %v", ErrStartPlanningRun, err)
	}

	agentRun, _, err := o.agentRuns.CreateOrGetByIdempotency(requirement.ProjectID, models.CreateAgentRunRequest{
		AgentName:        PlannerAgentName,
		ActionType:       plannerAction,
		Summary:          agentSummary,
		FilesAffected:    []string{},
		NeedsHumanReview: true,
		IdempotencyKey:   PlanningAgentRunKey(run.ID),
	})
	if err != nil {
		_ = o.planningRuns.Fail(run.ID, ErrCreatePlanningAgentRun.Error())
		return nil, fmt.Errorf("%w: %v", ErrCreatePlanningAgentRun, err)
	}
	if run.ExecutionMode == models.PlanningExecutionModeLocalConnector {
		queuedRun, err := o.planningRuns.GetByID(run.ID)
		if err != nil {
			return nil, err
		}
		if queuedRun == nil {
			return run, nil
		}
		return queuedRun, nil
	}

	drafts, err := o.generator.Generate(ctx, requirement, selection)
	if err != nil {
		o.failRunPair(run.ID, agentRun.ID, ErrGenerateDraftCandidates.Error(), true)
		return nil, fmt.Errorf("%w: %v", ErrGenerateDraftCandidates, err)
	}

	if _, err := o.candidates.CreateDraftsForPlanningRun(requirement, run.ID, drafts); err != nil {
		o.failRunPair(run.ID, agentRun.ID, ErrPersistDraftCandidates.Error(), true)
		return nil, fmt.Errorf("%w: %v", ErrPersistDraftCandidates, err)
	}

	completedSummary := buildCompletedSummary(requirement, len(drafts), selection)
	if _, err := o.agentRuns.Update(agentRun.ID, models.UpdateAgentRunRequest{
		Status:  models.AgentRunStatusCompleted,
		Summary: &completedSummary,
	}); err != nil {
		o.failRunPair(run.ID, agentRun.ID, ErrFinalizePlanningAgent.Error(), true)
		return nil, fmt.Errorf("%w: %v", ErrFinalizePlanningAgent, err)
	}

	if err := o.planningRuns.Complete(run.ID); err != nil {
		o.failRunPair(run.ID, agentRun.ID, ErrCompletePlanningRun.Error(), true)
		return nil, fmt.Errorf("%w: %v", ErrCompletePlanningRun, err)
	}

	completedRun, err := o.planningRuns.GetByID(run.ID)
	if err != nil {
		return nil, err
	}
	if completedRun == nil {
		return run, nil
	}
	return completedRun, nil
}

func (o *Orchestrator) failRunPair(planningRunID, agentRunID, message string, cleanupCandidates bool) {
	if cleanupCandidates {
		_ = o.candidates.DeleteByPlanningRun(planningRunID)
	}
	_ = o.planningRuns.Fail(planningRunID, message)
	failedSummary := buildFailedSummary(message)
	_, _ = o.agentRuns.Update(agentRunID, models.UpdateAgentRunRequest{
		Status:       models.AgentRunStatusFailed,
		Summary:      &failedSummary,
		ErrorMessage: &message,
	})
}

func PlanningAgentRunKey(planningRunID string) string {
	return "planning-run:" + strings.TrimSpace(planningRunID)
}

func buildQueuedSummary(requirement *models.Requirement, selection models.PlanningProviderSelection) string {
	return fmt.Sprintf(
		"Planning run queued for local connector execution for requirement %q using %s/%s via %s.",
		strings.TrimSpace(requirement.Title),
		strings.TrimSpace(selection.ProviderID),
		strings.TrimSpace(selection.ModelID),
		strings.TrimSpace(selection.BindingSource),
	)
}

func buildRunningSummary(requirement *models.Requirement, selection models.PlanningProviderSelection) string {
	return fmt.Sprintf(
		"Planning run started for requirement %q using %s/%s via %s.",
		strings.TrimSpace(requirement.Title),
		strings.TrimSpace(selection.ProviderID),
		strings.TrimSpace(selection.ModelID),
		strings.TrimSpace(selection.BindingSource),
	)
}

func buildCompletedSummary(requirement *models.Requirement, count int, selection models.PlanningProviderSelection) string {
	return fmt.Sprintf(
		"Planning run completed for requirement %q with %d ranked backlog candidates ready for review using %s/%s via %s.",
		strings.TrimSpace(requirement.Title),
		count,
		strings.TrimSpace(selection.ProviderID),
		strings.TrimSpace(selection.ModelID),
		strings.TrimSpace(selection.BindingSource),
	)
}

func buildFailedSummary(message string) string {
	return fmt.Sprintf("Planning run failed: %s.", strings.TrimSpace(message))
}

var _ planningRunStore = (*store.PlanningRunStore)(nil)
var _ backlogCandidateStore = (*store.BacklogCandidateStore)(nil)
var _ agentRunStore = (*store.AgentRunStore)(nil)
