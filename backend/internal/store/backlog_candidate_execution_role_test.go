package store

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// T-P5-B2-5: creating a candidate with ExecutionRole="" leaves the DB
// column NULL and the read-back model's ExecutionRole nil pointer.
func TestCreateCandidate_NullExecutionRoleRoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "ProbeRole", Description: ""})
	if err != nil {
		t.Fatal(err)
	}
	reqStore := NewRequirementStore(db)
	req, err := reqStore.Create(project.ID, models.CreateRequirementRequest{Title: "r", Source: "human"})
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewPlanningRunStore(db, testutil.TestDialect())
	run, err := runStore.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID:      "deterministic",
		ModelID:         "deterministic",
		SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatal(err)
	}
	candStore := NewBacklogCandidateStore(db, testutil.TestDialect())
	created, err := candStore.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{Title: "no role draft", Rank: 1, PriorityScore: 10, Confidence: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(created))
	}
	if created[0].ExecutionRole != nil {
		t.Fatalf("want nil ExecutionRole, got %v", *created[0].ExecutionRole)
	}

	// Read-back via GetByID must agree.
	got, err := candStore.GetByID(created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExecutionRole != nil {
		t.Fatalf("GetByID: want nil ExecutionRole, got %v", *got.ExecutionRole)
	}
}

// T-P5-B2-3/5: creating a candidate WITH ExecutionRole persists the value;
// Update can set and clear it.
func TestCreateAndUpdateCandidate_ExecutionRoleRoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "ProbeRole2", Description: ""})
	if err != nil {
		t.Fatal(err)
	}
	reqStore := NewRequirementStore(db)
	req, err := reqStore.Create(project.ID, models.CreateRequirementRequest{Title: "r", Source: "human"})
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewPlanningRunStore(db, testutil.TestDialect())
	run, err := runStore.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID:      "deterministic",
		ModelID:         "deterministic",
		SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatal(err)
	}

	candStore := NewBacklogCandidateStore(db, testutil.TestDialect())
	created, err := candStore.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{Title: "with role", Rank: 1, PriorityScore: 10, Confidence: 10, ExecutionRole: "ui-scaffolder"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[0].ExecutionRole == nil || *created[0].ExecutionRole != "ui-scaffolder" {
		t.Fatalf("want ui-scaffolder, got %v", created[0].ExecutionRole)
	}

	// PATCH to a different role.
	nextRole := "backend-architect"
	updated, err := candStore.Update(created[0].ID, models.UpdateBacklogCandidateRequest{
		ExecutionRole: &nextRole,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ExecutionRole == nil || *updated.ExecutionRole != "backend-architect" {
		t.Fatalf("want backend-architect, got %v", updated.ExecutionRole)
	}

	// PATCH with empty string clears the column.
	empty := ""
	cleared, err := candStore.Update(created[0].ID, models.UpdateBacklogCandidateRequest{
		ExecutionRole: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ExecutionRole != nil {
		t.Fatalf("want cleared, got %v", *cleared.ExecutionRole)
	}
}

// T-P5-B2-4: unknown role strings are accepted today (no catalog
// enforcement — plan §5 B2 / DECISIONS 2026-04-24). This test pins the
// current contract so a future tightening in Phase 6 is a deliberate
// change rather than a silent regression.
func TestUpdateCandidate_UnknownExecutionRoleAcceptedInPhase5(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "ProbeRole3"})
	if err != nil {
		t.Fatal(err)
	}
	reqStore := NewRequirementStore(db)
	req, err := reqStore.Create(project.ID, models.CreateRequirementRequest{Title: "r", Source: "human"})
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewPlanningRunStore(db, testutil.TestDialect())
	run, err := runStore.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID:      "deterministic",
		ModelID:         "deterministic",
		SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatal(err)
	}

	candStore := NewBacklogCandidateStore(db, testutil.TestDialect())
	created, err := candStore.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{Title: "draft", Rank: 1, PriorityScore: 10, Confidence: 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	bogus := "not-a-real-role-name-in-the-catalog"
	updated, err := candStore.Update(created[0].ID, models.UpdateBacklogCandidateRequest{
		ExecutionRole: &bogus,
	})
	if err != nil {
		t.Fatalf("Phase 5 must accept unknown role strings without catalog validation: %v", err)
	}
	if updated.ExecutionRole == nil || *updated.ExecutionRole != bogus {
		t.Fatalf("unknown role not persisted; got %v", updated.ExecutionRole)
	}
}
