package store

import (
	"testing"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// setupContextSnapshotStore creates a project + requirement + planning_run so
// the FK constraint on planning_context_snapshots.planning_run_id is satisfied.
func setupContextSnapshotStore(t *testing.T) (*ContextSnapshotStore, string, string) {
	t.Helper()

	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	requirementStore := NewRequirementStore(db)
	planningRunStore := NewPlanningRunStore(db, testutil.TestDialect())
	snapshotStore := NewContextSnapshotStore(db)

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Snapshot Test Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	requirement, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{Title: "Snapshot Test Requirement"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	run, err := planningRunStore.Create(project.ID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}

	return snapshotStore, run.ID, run.ContextPackID
}

func TestContextSnapshotStoreSaveAndGetByRunID(t *testing.T) {
	store, runID, packID := setupContextSnapshotStore(t)

	snap := ContextSnapshot{
		ID:            uuid.New().String(),
		PackID:        packID,
		PlanningRunID: runID,
		SchemaVersion: "context.v2",
		Snapshot:      `{"schema_version":"context.v2","pack_id":"` + packID + `"}`,
		SourcesBytes:  1024,
		DroppedCounts: `{"tasks":2}`,
	}

	if err := store.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetByRunID(runID)
	if err != nil {
		t.Fatalf("GetByRunID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByRunID returned nil, want snapshot")
	}

	if got.ID != snap.ID {
		t.Errorf("ID = %q, want %q", got.ID, snap.ID)
	}
	if got.PackID != packID {
		t.Errorf("PackID = %q, want %q", got.PackID, packID)
	}
	if got.PlanningRunID != runID {
		t.Errorf("PlanningRunID = %q, want %q", got.PlanningRunID, runID)
	}
	if got.SchemaVersion != "context.v2" {
		t.Errorf("SchemaVersion = %q, want %q", got.SchemaVersion, "context.v2")
	}
	if got.SourcesBytes != 1024 {
		t.Errorf("SourcesBytes = %d, want 1024", got.SourcesBytes)
	}
	if got.DroppedCounts != `{"tasks":2}` {
		t.Errorf("DroppedCounts = %q, want %q", got.DroppedCounts, `{"tasks":2}`)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestContextSnapshotStoreGetByRunID_NotFound(t *testing.T) {
	store, _, _ := setupContextSnapshotStore(t)

	got, err := store.GetByRunID("non-existent-run-id")
	if err != nil {
		t.Fatalf("GetByRunID: unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("GetByRunID: expected nil for missing run, got %+v", got)
	}
}

func TestContextSnapshotStoreSave_AutoGeneratesID(t *testing.T) {
	store, runID, packID := setupContextSnapshotStore(t)

	snap := ContextSnapshot{
		// ID intentionally empty — store should generate one.
		PackID:        packID,
		PlanningRunID: runID,
		SchemaVersion: "context.v2",
		Snapshot:      `{}`,
		SourcesBytes:  0,
		DroppedCounts: "{}",
	}

	if err := store.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetByRunID(runID)
	if err != nil {
		t.Fatalf("GetByRunID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByRunID returned nil")
	}
	if got.ID == "" {
		t.Error("auto-generated ID should be non-empty")
	}
}

func TestPlanningRunStore_ContextPackIDSetOnCreate(t *testing.T) {
	// Verify that CreateWithBinding generates and persists a non-empty pack_id.
	planningRunStore, requirementStore, requirementID := setupPlanningRunStore(t)
	requirement, err := requirementStore.GetByID(requirementID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}

	run, err := planningRunStore.Create(requirement.ProjectID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testPlanningSelection)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if run.ContextPackID == "" {
		t.Error("ContextPackID should be non-empty after Create")
	}

	// Verify it's a valid UUID-like string (just non-empty and round-trips through GetByID).
	fetched, err := planningRunStore.GetByID(run.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.ContextPackID != run.ContextPackID {
		t.Errorf("ContextPackID mismatch after GetByID: got %q, want %q", fetched.ContextPackID, run.ContextPackID)
	}
}
