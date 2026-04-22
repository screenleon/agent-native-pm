package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

var testPlanningSelection = models.PlanningProviderSelection{
	ProviderID:      models.PlanningProviderDeterministic,
	ModelID:         models.PlanningProviderModelDeterministic,
	SelectionSource: models.PlanningSelectionSourceServerDefault,
}

func setupPlanningRunStore(t *testing.T) (*PlanningRunStore, *RequirementStore, string) {
	t.Helper()

	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	requirementStore := NewRequirementStore(db)
	planningRunStore := NewPlanningRunStore(db, testutil.TestDialect())

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Planning Run Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	requirement, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{Title: "Planning Run Requirement"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	return planningRunStore, requirementStore, requirement.ID
}

func TestPlanningRunStoreLifecycleAndList(t *testing.T) {
	store, requirementStore, requirementID := setupPlanningRunStore(t)
	requirement, err := requirementStore.GetByID(requirementID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}

	run, err := store.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}
	if run.Status != models.PlanningRunStatusQueued {
		t.Fatalf("expected queued status, got %s", run.Status)
	}
	if run.ProviderID != testPlanningSelection.ProviderID || run.ModelID != testPlanningSelection.ModelID {
		t.Fatalf("expected provider selection to persist, got provider=%s model=%s", run.ProviderID, run.ModelID)
	}

	if err := store.MarkRunning(run.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := store.Complete(run.ID); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	updated, err := store.GetByID(run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status != models.PlanningRunStatusCompleted {
		t.Fatalf("expected completed status, got %s", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Fatal("expected started_at to be set")
	}
	if updated.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}

	runs, total, err := store.ListByRequirement(requirement.ID, 1, 20)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].ID != run.ID {
		t.Fatalf("expected run id %s, got %s", run.ID, runs[0].ID)
	}
}

func TestPlanningRunStoreRejectsActiveDuplicate(t *testing.T) {
	store, requirementStore, requirementID := setupPlanningRunStore(t)
	requirement, err := requirementStore.GetByID(requirementID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}

	run, err := store.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}
	if run.Status != models.PlanningRunStatusQueued {
		t.Fatalf("expected queued status, got %s", run.Status)
	}

	_, err = store.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if !errors.Is(err, ErrActivePlanningRunExists) {
		t.Fatalf("expected ErrActivePlanningRunExists, got %v", err)
	}

	if err := store.Fail(run.ID, "planner failed"); err != nil {
		t.Fatalf("fail run: %v", err)
	}

	secondRun, err := store.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create run after failure: %v", err)
	}
	if secondRun == nil {
		t.Fatal("expected second run to be created")
	}
}

func TestPlanningRunStoreRejectsConcurrentActiveDuplicates(t *testing.T) {
	store, requirementStore, requirementID := setupPlanningRunStore(t)
	requirement, err := requirementStore.GetByID(requirementID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}

	const attempts = 4
	results := make(chan error, attempts)
	var wg sync.WaitGroup

	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, createErr := store.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
			results <- createErr
		}()
	}

	wg.Wait()
	close(results)

	successCount := 0
	conflictCount := 0
	for createErr := range results {
		switch {
		case createErr == nil:
			successCount++
		case errors.Is(createErr, ErrActivePlanningRunExists):
			conflictCount++
		default:
			t.Fatalf("unexpected create error: %v", createErr)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful create, got %d", successCount)
	}
	if conflictCount != attempts-1 {
		t.Fatalf("expected %d active-run conflicts, got %d", attempts-1, conflictCount)
	}

	runs, total, err := store.ListByRequirement(requirement.ID, 1, 20)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("expected exactly 1 stored run, got total=%d len=%d", total, len(runs))
	}
	if runs[0].Status != models.PlanningRunStatusQueued {
		t.Fatalf("expected surviving run to remain queued, got %s", runs[0].Status)
	}
	activeRun, err := store.GetActiveByRequirement(requirement.ID)
	if err != nil {
		t.Fatalf("get active run: %v", err)
	}
	if activeRun == nil {
		t.Fatal("expected active run to exist after concurrent create attempts")
	}
}

func TestPlanningRunStoreLocalConnectorLifecycle(t *testing.T) {
	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	requirementStore := NewRequirementStore(db)
	planningRunStore := NewPlanningRunStore(db, testutil.TestDialect())
	userStore := NewUserStore(db)
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Connector Planning Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	requirement, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{Title: "Connector Requirement"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	user, err := userStore.Create(models.CreateUserRequest{
		Username: "planning-connector-user",
		Email:    "planning-connector@example.com",
		Password: "password123",
		Role:     "member",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pairing, err := connectorStore.CreatePairingSession(user.ID, models.CreateLocalConnectorPairingSessionRequest{Label: "Workstation"})
	if err != nil {
		t.Fatalf("create pairing session: %v", err)
	}
	claimed, err := connectorStore.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode: pairing.PairingCode,
		Label:       "Workstation",
	})
	if err != nil {
		t.Fatalf("claim pairing session: %v", err)
	}

	run, err := planningRunStore.Create(project.ID, requirement.ID, user.ID, models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: models.PlanningExecutionModeLocalConnector,
	}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create local connector planning run: %v", err)
	}
	if run.ExecutionMode != models.PlanningExecutionModeLocalConnector {
		t.Fatalf("expected local connector execution mode, got %s", run.ExecutionMode)
	}
	if run.DispatchStatus != models.PlanningDispatchStatusQueued {
		t.Fatalf("expected queued dispatch status, got %s", run.DispatchStatus)
	}
	if run.RequestedByUserID != user.ID {
		t.Fatalf("expected requested_by_user_id %s, got %s", user.ID, run.RequestedByUserID)
	}

	leased, err := planningRunStore.LeaseNextLocalConnectorRun(user.ID, claimed.Connector.ID, claimed.Connector.Label, time.Minute)
	if err != nil {
		t.Fatalf("lease next local connector planning run: %v", err)
	}
	if leased == nil {
		t.Fatal("expected leased planning run")
	}
	if leased.ID != run.ID {
		t.Fatalf("expected leased run %s, got %s", run.ID, leased.ID)
	}
	if leased.Status != models.PlanningRunStatusRunning {
		t.Fatalf("expected running status after lease, got %s", leased.Status)
	}
	if leased.DispatchStatus != models.PlanningDispatchStatusLeased {
		t.Fatalf("expected leased dispatch status, got %s", leased.DispatchStatus)
	}
	if leased.ConnectorID != claimed.Connector.ID {
		t.Fatalf("expected connector id %s, got %s", claimed.Connector.ID, leased.ConnectorID)
	}
	if leased.LeaseExpiresAt == nil {
		t.Fatal("expected lease expiry")
	}

	leasedLookup, err := planningRunStore.GetLeasedLocalConnectorRun(run.ID, claimed.Connector.ID)
	if err != nil {
		t.Fatalf("get leased run: %v", err)
	}
	if leasedLookup == nil {
		t.Fatal("expected leased run lookup")
	}

	if err := planningRunStore.CompleteLocalConnectorRun(run.ID, claimed.Connector.ID, ""); err != nil {
		t.Fatalf("complete local connector run: %v", err)
	}
	completed, err := planningRunStore.GetByID(run.ID)
	if err != nil {
		t.Fatalf("get completed run: %v", err)
	}
	if completed == nil {
		t.Fatal("expected completed run")
	}
	if completed.Status != models.PlanningRunStatusCompleted {
		t.Fatalf("expected completed status, got %s", completed.Status)
	}
	if completed.DispatchStatus != models.PlanningDispatchStatusReturned {
		t.Fatalf("expected returned dispatch status, got %s", completed.DispatchStatus)
	}
	if completed.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}
