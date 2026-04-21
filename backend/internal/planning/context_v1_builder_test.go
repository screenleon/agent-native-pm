package planning

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

func TestBuildContextV1AttachesSanitizedContext(t *testing.T) {
	now := time.Now().UTC()
	requirement := &models.Requirement{
		ID:          "req-1",
		ProjectID:   "proj-1",
		Title:       "Improve sync recovery UX",
		Description: "Align sync behavior with the recovery guide.",
	}
	syncRun := &models.SyncRun{
		ID:           "sync-1",
		Status:       "failed",
		StartedAt:    now,
		ErrorMessage: "Authorization: Bearer leakedsecrettoken-1234567890",
	}
	builder := NewProjectContextBuilder(
		&fakeTaskContextSource{tasks: []models.Task{
			{ID: "t1", Title: "Fix sync retry", Status: "todo", Priority: "high", UpdatedAt: now},
		}},
		&fakeDocumentContextSource{documents: []models.Document{
			{ID: "d1", Title: "Sync Recovery Guide", FilePath: "docs/recovery.md", DocType: "guide", IsStale: true, UpdatedAt: now},
		}},
		&fakeDriftContextSource{signals: []models.DriftSignal{
			{ID: "drift-1", DocumentTitle: "Recovery Guide", TriggerType: "code_change", Severity: 3, CreatedAt: now},
		}},
		&fakeSyncContextSource{run: syncRun},
		&fakeAgentRunContextSource{runs: []models.AgentRun{
			{ID: "a1", AgentName: "agent:sync", ActionType: "sync", Status: models.AgentRunStatusFailed, StartedAt: now, Summary: "token=sk-abcdefghijklmnopqrstuvwxyz0123"},
		}},
	)

	ctx, err := builder.BuildContextV1(requirement)
	if err != nil {
		t.Fatalf("BuildContextV1 returned error: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil PlanningContextV1")
	}
	if ctx.SchemaVersion != wire.ContextSchemaV1 {
		t.Fatalf("schema_version mismatch: %q", ctx.SchemaVersion)
	}
	if ctx.GeneratedBy != wire.GeneratedByServer {
		t.Fatalf("generated_by mismatch: %q", ctx.GeneratedBy)
	}
	if ctx.SanitizerVersion != wire.SanitizerVersion {
		t.Fatalf("sanitizer_version mismatch: %q", ctx.SanitizerVersion)
	}
	if len(ctx.Sources.OpenTasks) != 1 || ctx.Sources.OpenTasks[0].Priority != "high" {
		t.Fatalf("unexpected open_tasks: %+v", ctx.Sources.OpenTasks)
	}
	if len(ctx.Sources.OpenDriftSignals) != 1 || ctx.Sources.OpenDriftSignals[0].Severity != "high" {
		t.Fatalf("expected severity label 'high'; got %+v", ctx.Sources.OpenDriftSignals)
	}
	if ctx.Sources.LatestSyncRun == nil {
		t.Fatal("expected latest_sync_run to be present")
	}
	if !strings.Contains(ctx.Sources.LatestSyncRun.ErrorMessage, "[REDACTED]") {
		t.Fatalf("expected sync run error redacted; got %q", ctx.Sources.LatestSyncRun.ErrorMessage)
	}
	if len(ctx.Sources.RecentAgentRuns) != 1 {
		t.Fatalf("expected 1 agent run; got %d", len(ctx.Sources.RecentAgentRuns))
	}
	if !strings.Contains(ctx.Sources.RecentAgentRuns[0].Summary, "[REDACTED]") {
		t.Fatalf("expected agent run summary redacted; got %q", ctx.Sources.RecentAgentRuns[0].Summary)
	}
	if ctx.Meta.SourcesBytes <= 0 {
		t.Fatalf("expected positive sources_bytes; got %d", ctx.Meta.SourcesBytes)
	}
	if _, ok := ctx.Meta.DroppedCounts["open_tasks"]; !ok {
		t.Fatalf("expected dropped_counts to include all sources; got %+v", ctx.Meta.DroppedCounts)
	}
	if ctx.Meta.Ranking["documents"] != wire.RankingDocuments {
		t.Fatalf("expected ranking taxonomy in meta; got %+v", ctx.Meta.Ranking)
	}

	// Sanity-check that JSON marshaling works end-to-end.
	if _, err := json.Marshal(ctx); err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Input sync run must not be mutated by the sanitizer.
	if syncRun.ErrorMessage == "[REDACTED]" {
		t.Fatalf("original SyncRun.ErrorMessage was mutated")
	}
}

func TestBuildContextV1NilRequirementReturnsEmptyContext(t *testing.T) {
	builder := NewProjectContextBuilder(
		&fakeTaskContextSource{},
		&fakeDocumentContextSource{},
		&fakeDriftContextSource{},
		&fakeSyncContextSource{},
		&fakeAgentRunContextSource{},
	)
	ctx, err := builder.BuildContextV1(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if len(ctx.Sources.OpenTasks) != 0 || len(ctx.Sources.RecentDocuments) != 0 {
		t.Fatalf("expected empty sources for nil requirement")
	}
}
