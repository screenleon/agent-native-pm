package planning

import (
	"errors"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type fakeTaskContextSource struct {
	tasks []models.Task
	err   error
}

func (f *fakeTaskContextSource) ListByProject(projectID string, page, perPage int, sort, order string, filters models.TaskListFilters) ([]models.Task, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.tasks, len(f.tasks), nil
}

type fakeDocumentContextSource struct {
	documents []models.Document
	err       error
}

func (f *fakeDocumentContextSource) ListByProject(projectID string, page, perPage int) ([]models.Document, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.documents, len(f.documents), nil
}

type fakeDriftContextSource struct {
	signals []models.DriftSignal
	err     error
}

func (f *fakeDriftContextSource) ListByProject(projectID, statusFilter, sortBy string, page, perPage int) ([]models.DriftSignal, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.signals, len(f.signals), nil
}

type fakeSyncContextSource struct {
	run *models.SyncRun
	err error
}

func (f *fakeSyncContextSource) GetLatestByProject(projectID string) (*models.SyncRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.run, nil
}

type fakeAgentRunContextSource struct {
	runs []models.AgentRun
	err  error
}

func (f *fakeAgentRunContextSource) ListRecentByProject(projectID string, limit int) ([]models.AgentRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

func TestProjectContextBuilderBuildCollectsRichContext(t *testing.T) {
	now := time.Now().UTC()
	builder := NewProjectContextBuilder(
		&fakeTaskContextSource{tasks: []models.Task{{ID: "task-open", Title: "Improve sync recovery UX", Status: "todo"}, {ID: "task-done", Title: "Ignore me", Status: "done"}}},
		&fakeDocumentContextSource{documents: []models.Document{{ID: "doc-guide", Title: "Sync Recovery Guide", DocType: "guide", IsStale: true, UpdatedAt: now}, {ID: "doc-api", Title: "API Contract", DocType: "api", UpdatedAt: now.Add(-time.Hour)}}},
		&fakeDriftContextSource{signals: []models.DriftSignal{{ID: "drift-1", DocumentTitle: "Sync Recovery Guide", TriggerType: "code_change", Severity: 3}}},
		&fakeSyncContextSource{run: &models.SyncRun{ID: "sync-1", Status: "failed", ErrorMessage: "unknown revision"}},
		&fakeAgentRunContextSource{runs: []models.AgentRun{{ID: "agent-current", AgentName: PlannerAgentName, ActionType: plannerAction, Status: models.AgentRunStatusRunning}, {ID: "agent-1", AgentName: "agent:sync", ActionType: "sync", Status: models.AgentRunStatusFailed}, {ID: "agent-2", AgentName: "agent:reviewer", ActionType: "review", Status: models.AgentRunStatusCompleted}}},
	)

	context, err := builder.Build(&models.Requirement{ProjectID: "project-1", Title: "Improve sync recovery UX", Summary: "Improve sync recovery"})
	if err != nil {
		t.Fatalf("build context: %v", err)
	}
	if len(context.OpenTasks) != 1 {
		t.Fatalf("expected 1 open task, got %d", len(context.OpenTasks))
	}
	if len(context.RecentDocuments) == 0 || context.RecentDocuments[0].Title != "Sync Recovery Guide" {
		t.Fatalf("expected relevant guide first, got %+v", context.RecentDocuments)
	}
	if len(context.OpenDriftSignals) != 1 {
		t.Fatalf("expected 1 drift signal, got %d", len(context.OpenDriftSignals))
	}
	if context.LatestSyncRun == nil || context.LatestSyncRun.Status != "failed" {
		t.Fatalf("expected failed latest sync, got %+v", context.LatestSyncRun)
	}
	if len(context.RecentAgentRuns) != 2 {
		t.Fatalf("expected current running planning run to be filtered out, got %d agent runs", len(context.RecentAgentRuns))
	}
}

func TestProjectContextBuilderBuildTreatsOptionalSourcesAsBestEffort(t *testing.T) {
	builder := NewProjectContextBuilder(
		&fakeTaskContextSource{tasks: []models.Task{{ID: "task-open", Title: "Improve sync recovery UX", Status: "todo"}}},
		&fakeDocumentContextSource{err: errors.New("documents unavailable")},
		&fakeDriftContextSource{err: errors.New("drift unavailable")},
		&fakeSyncContextSource{err: errors.New("sync unavailable")},
		&fakeAgentRunContextSource{err: errors.New("agent runs unavailable")},
	)

	context, err := builder.Build(&models.Requirement{ProjectID: "project-1", Title: "Improve sync recovery UX"})
	if err != nil {
		t.Fatalf("expected optional source failures to be ignored, got %v", err)
	}
	if len(context.OpenTasks) != 1 {
		t.Fatalf("expected open tasks to still load, got %d", len(context.OpenTasks))
	}
	if len(context.RecentDocuments) != 0 || len(context.OpenDriftSignals) != 0 || context.LatestSyncRun != nil || len(context.RecentAgentRuns) != 0 {
		t.Fatalf("expected best-effort optional context, got %+v", context)
	}
}

func TestProjectContextBuilderBuildReturnsTaskErrors(t *testing.T) {
	builder := NewProjectContextBuilder(
		&fakeTaskContextSource{err: errors.New("task query failed")},
		nil,
		nil,
		nil,
		nil,
	)

	_, err := builder.Build(&models.Requirement{ProjectID: "project-1", Title: "Improve sync recovery UX"})
	if err == nil {
		t.Fatal("expected task source error to fail context build")
	}
}
