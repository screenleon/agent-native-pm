package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/screenleon/agent-native-pm/internal/audit"
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
	}, audit.ActorInfo{Kind: audit.ActorUser, ID: "test-user"})
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
	}, audit.ActorInfo{Kind: audit.ActorUser, ID: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ExecutionRole != nil {
		t.Fatalf("want cleared, got %v", *cleared.ExecutionRole)
	}
}

// Phase 6c PR-2: ApplyToTaskWithMode now requires the execution_role
// payload argument and validates it against the catalog. Unknown roles
// return ErrBacklogCandidateUnknownExecutionRole; missing roles return
// ErrBacklogCandidateMissingExecutionRole. This replaces the Phase 5
// "rune-aware truncation defends against non-ASCII roles" test —
// catalog enforcement makes that defense moot because all roles are
// catalogue-controlled ASCII identifiers.
func TestApplyToTaskWithMode_RoleDispatchRequiresKnownRole(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "EnforceCatalogProj"})
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
		TriggerSource: "manual", ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID: "deterministic", ModelID: "deterministic", SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatal(err)
	}

	candStore := NewBacklogCandidateStore(db, testutil.TestDialect())
	created, err := candStore.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{Title: "t", Rank: 1, PriorityScore: 10, Confidence: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved := "approved"
	if _, err := candStore.Update(created[0].ID, models.UpdateBacklogCandidateRequest{Status: &approved}, audit.ActorInfo{}); err != nil {
		t.Fatal(err)
	}

	actor := audit.ActorInfo{Kind: audit.ActorUser, ID: "test-user"}

	// Empty role for role_dispatch → typed error.
	if _, err := candStore.ApplyToTaskWithMode(created[0].ID, models.ApplyExecutionModeRoleDispatch, "", actor); !errors.Is(err, ErrBacklogCandidateMissingExecutionRole) {
		t.Fatalf("expected ErrBacklogCandidateMissingExecutionRole, got %v", err)
	}

	// Unknown role for role_dispatch → typed error.
	if _, err := candStore.ApplyToTaskWithMode(created[0].ID, models.ApplyExecutionModeRoleDispatch, "not-in-catalog", actor); !errors.Is(err, ErrBacklogCandidateUnknownExecutionRole) {
		t.Fatalf("expected ErrBacklogCandidateUnknownExecutionRole, got %v", err)
	}

	// Multi-byte non-catalog role → still rejected (catalog enforcement
	// is the gate, rune-aware truncation is no longer the defense).
	multiByteBogus := strings.Repeat("テストロール", 20)
	if _, err := candStore.ApplyToTaskWithMode(created[0].ID, models.ApplyExecutionModeRoleDispatch, multiByteBogus, actor); !errors.Is(err, ErrBacklogCandidateUnknownExecutionRole) {
		t.Fatalf("multi-byte non-catalog role: expected ErrBacklogCandidateUnknownExecutionRole, got %v", err)
	}

	// Valid catalog role → succeeds; task.source is "role_dispatch:<role>".
	resp, err := candStore.ApplyToTaskWithMode(created[0].ID, models.ApplyExecutionModeRoleDispatch, "backend-architect", actor)
	if err != nil {
		t.Fatalf("valid role: %v", err)
	}
	wantSource := "role_dispatch:backend-architect"
	if resp.Task.Source != wantSource {
		t.Errorf("task.source = %q, want %q", resp.Task.Source, wantSource)
	}
	if !utf8.ValidString(resp.Task.Source) {
		t.Errorf("task.source is not valid UTF-8: %q", resp.Task.Source)
	}
}

// Phase 6c PR-2: PATCH /backlog-candidates/:id rejects unknown roles
// (catalog enforcement). Replaces the Phase 5 "unknown roles accepted"
// test which pinned the inverse contract.
func TestUpdateCandidate_RejectsUnknownExecutionRole(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "RejectUnknownRoleProj"})
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
	if _, err := candStore.Update(created[0].ID, models.UpdateBacklogCandidateRequest{
		ExecutionRole: &bogus,
	}, audit.ActorInfo{Kind: audit.ActorUser, ID: "test-user"}); !errors.Is(err, ErrBacklogCandidateUnknownExecutionRole) {
		t.Fatalf("expected ErrBacklogCandidateUnknownExecutionRole, got %v", err)
	}
}
