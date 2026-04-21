package store

import (
	"errors"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupTaskStore(t *testing.T) (*TaskStore, string) {
	t.Helper()

	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	taskStore := NewTaskStore(db)

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Task Store Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	for _, req := range []models.CreateTaskRequest{
		{Title: "Alpha", Status: "todo", Priority: "high", Assignee: "agent:application-implementer"},
		{Title: "Bravo", Status: "done", Priority: "high", Assignee: "agent:application-implementer"},
		{Title: "Charlie", Status: "todo", Priority: "low", Assignee: "human:pm"},
	} {
		if _, err := taskStore.Create(project.ID, req); err != nil {
			t.Fatalf("create task %s: %v", req.Title, err)
		}
	}

	return taskStore, project.ID
}

func TestTaskStoreListByProjectFiltersAndTotal(t *testing.T) {
	store, projectID := setupTaskStore(t)

	tasks, total, err := store.ListByProject(projectID, 1, 20, "title", "ASC", models.TaskListFilters{
		Status:   "todo",
		Priority: "high",
		Assignee: "agent:application-implementer",
	})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected filtered total 1, got %d", total)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 filtered task, got %d", len(tasks))
	}
	if tasks[0].Title != "Alpha" {
		t.Fatalf("expected Alpha, got %s", tasks[0].Title)
	}
}

func TestTaskStoreListByProjectAssigneeWhitespaceIsIgnoredWhenEmpty(t *testing.T) {
	store, projectID := setupTaskStore(t)

	tasks, total, err := store.ListByProject(projectID, 1, 20, "title", "ASC", models.TaskListFilters{})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestTaskStoreBatchUpdateAtomicRollback(t *testing.T) {
	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	taskStore := NewTaskStore(db)

	projectA, err := projectStore.Create(models.CreateProjectRequest{Name: "Project A"})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := projectStore.Create(models.CreateProjectRequest{Name: "Project B"})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}

	taskA, err := taskStore.Create(projectA.ID, models.CreateTaskRequest{Title: "Alpha", Status: "todo", Priority: "low", Assignee: "agent:a"})
	if err != nil {
		t.Fatalf("create task A: %v", err)
	}
	taskB, err := taskStore.Create(projectB.ID, models.CreateTaskRequest{Title: "Bravo", Status: "todo", Priority: "medium", Assignee: "agent:b"})
	if err != nil {
		t.Fatalf("create task B: %v", err)
	}

	status := "done"
	updatedTasks, err := taskStore.BatchUpdate(projectA.ID, []string{taskA.ID, taskB.ID}, models.BatchUpdateTaskChanges{Status: &status})
	if !errors.Is(err, ErrTaskBatchNotFound) {
		t.Fatalf("expected ErrTaskBatchNotFound, got %v", err)
	}
	if updatedTasks != nil {
		t.Fatalf("expected no updated tasks on rollback, got %v", updatedTasks)
	}

	reloadedTask, err := taskStore.GetByID(taskA.ID)
	if err != nil {
		t.Fatalf("reload task A: %v", err)
	}
	if reloadedTask.Status != "todo" {
		t.Fatalf("expected rollback to keep status todo, got %s", reloadedTask.Status)
	}
}

func TestTaskStoreBatchUpdateClearsAssignee(t *testing.T) {
	store, projectID := setupTaskStore(t)
	tasks, _, err := store.ListByProject(projectID, 1, 20, "title", "ASC", models.TaskListFilters{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	clearAssignee := ""
	updatedTasks, err := store.BatchUpdate(projectID, []string{tasks[0].ID, tasks[1].ID}, models.BatchUpdateTaskChanges{Assignee: &clearAssignee})
	if err != nil {
		t.Fatalf("batch update clear assignee: %v", err)
	}
	if len(updatedTasks) != 2 {
		t.Fatalf("expected 2 updated tasks, got %d", len(updatedTasks))
	}
	for _, task := range updatedTasks {
		if task.Assignee != "" {
			t.Fatalf("expected cleared assignee, got %q", task.Assignee)
		}
	}
}