package store

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupSearchStore(t *testing.T) (*SearchStore, string) {
	t.Helper()

	db := testutil.OpenTestDB(t)

	projectStore := NewProjectStore(db)
	taskStore := NewTaskStore(db)
	documentStore := NewDocumentStore(db)
	searchStore := NewSearchStore(db, database.NewDialect("postgres://test"))

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Search Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	_, err = taskStore.Create(project.ID, models.CreateTaskRequest{
		Title:  "alpha done task",
		Status: "done",
	})
	if err != nil {
		t.Fatalf("create done task: %v", err)
	}
	_, err = taskStore.Create(project.ID, models.CreateTaskRequest{
		Title:  "alpha todo task",
		Status: "todo",
	})
	if err != nil {
		t.Fatalf("create todo task: %v", err)
	}

	staleDoc, err := documentStore.Create(project.ID, models.CreateDocumentRequest{
		Title:   "alpha api doc",
		DocType: "api",
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("create stale doc: %v", err)
	}
	if err := documentStore.MarkStale(staleDoc.ID); err != nil {
		t.Fatalf("mark stale doc: %v", err)
	}
	_, err = documentStore.Create(project.ID, models.CreateDocumentRequest{
		Title:   "alpha guide doc",
		DocType: "guide",
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("create fresh doc: %v", err)
	}

	return searchStore, project.ID
}

func TestSearchStore_TaskStatusFilter(t *testing.T) {
	store, projectID := setupSearchStore(t)

	result, err := store.Search("alpha", projectID, "all", "done", "", nil)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}
	if result.Tasks[0].Status != "done" {
		t.Fatalf("expected done task, got %s", result.Tasks[0].Status)
	}
}

func TestSearchStore_DocumentTypeAndStalenessFilter(t *testing.T) {
	store, projectID := setupSearchStore(t)
	staleOnly := true

	result, err := store.Search("alpha", projectID, "documents", "", "api", &staleOnly)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("expected 0 tasks for documents-only search, got %d", len(result.Tasks))
	}
	if len(result.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result.Documents))
	}
	if result.Documents[0].DocType != "api" {
		t.Fatalf("expected api document, got %s", result.Documents[0].DocType)
	}
	if !result.Documents[0].IsStale {
		t.Fatalf("expected stale document")
	}
}
