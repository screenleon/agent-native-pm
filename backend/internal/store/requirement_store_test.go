package store

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupRequirementStore(t *testing.T) (*RequirementStore, *ProjectStore, string) {
	t.Helper()

	db := testutil.OpenTestDB(t)
	requirementStore := NewRequirementStore(db)
	projectStore := NewProjectStore(db)

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Requirement Store Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return requirementStore, projectStore, project.ID
}

func TestRequirementStoreCreateAndGetByID(t *testing.T) {
	requirementStore, _, projectID := setupRequirementStore(t)

	created, err := requirementStore.Create(projectID, models.CreateRequirementRequest{
		Title:       "Improve planning reliability",
		Summary:     "Reduce intermittent failures",
		Description: "Capture retry strategy and alerting behavior",
	})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	if created == nil {
		t.Fatalf("expected created requirement")
	}
	if created.Source != "human" {
		t.Fatalf("expected default source human, got %q", created.Source)
	}

	loaded, err := requirementStore.GetByID(created.ID)
	if err != nil {
		t.Fatalf("get requirement by id: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected requirement from GetByID")
	}
	if loaded.ID != created.ID {
		t.Fatalf("expected id %s, got %s", created.ID, loaded.ID)
	}
	if loaded.ProjectID != projectID {
		t.Fatalf("expected project id %s, got %s", projectID, loaded.ProjectID)
	}
	if loaded.Title != "Improve planning reliability" {
		t.Fatalf("unexpected title: %q", loaded.Title)
	}
}

func TestRequirementStoreListByProjectPaginationAndTotal(t *testing.T) {
	requirementStore, _, projectID := setupRequirementStore(t)

	for i := 1; i <= 3; i++ {
		_, err := requirementStore.Create(projectID, models.CreateRequirementRequest{
			Title:       "Requirement " + string(rune('A'+i-1)),
			Summary:     "Summary",
			Description: "Description",
			Source:      "agent:application-implementer",
		})
		if err != nil {
			t.Fatalf("create requirement %d: %v", i, err)
		}
	}

	page1, total1, err := requirementStore.ListByProject(projectID, 1, 2)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if total1 != 3 {
		t.Fatalf("expected total 3 on page 1, got %d", total1)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 requirements on page 1, got %d", len(page1))
	}

	page2, total2, err := requirementStore.ListByProject(projectID, 2, 2)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if total2 != 3 {
		t.Fatalf("expected total 3 on page 2, got %d", total2)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 requirement on page 2, got %d", len(page2))
	}
}
