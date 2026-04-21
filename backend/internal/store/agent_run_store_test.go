package store

import (
	"errors"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func TestAgentRunStoreCreateOrGetByIdempotency(t *testing.T) {
	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	agentRunStore := NewAgentRunStore(db)

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Agent Run Store Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	req := models.CreateAgentRunRequest{
		AgentName:      "agent:planning-orchestrator",
		ActionType:     "review",
		Summary:        "started",
		IdempotencyKey: "planning-run:run-1",
	}

	first, existing, err := agentRunStore.CreateOrGetByIdempotency(project.ID, req)
	if err != nil {
		t.Fatalf("create first agent run: %v", err)
	}
	if existing {
		t.Fatal("expected first create to insert a new run")
	}

	second, existing, err := agentRunStore.CreateOrGetByIdempotency(project.ID, req)
	if err != nil {
		t.Fatalf("create second agent run: %v", err)
	}
	if !existing {
		t.Fatal("expected second create to return existing run")
	}
	if second.ID != first.ID {
		t.Fatalf("expected same run id %s, got %s", first.ID, second.ID)
	}

	runs, total, err := agentRunStore.ListByProject(project.ID, 1, 20)
	if err != nil {
		t.Fatalf("list agent runs: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("expected exactly one stored agent run, got total=%d len=%d", total, len(runs))
	}
}

func TestAgentRunStoreRejectsIdempotencyKeyReuseAcrossProjects(t *testing.T) {
	db := testutil.OpenTestDB(t)
	projectStore := NewProjectStore(db)
	agentRunStore := NewAgentRunStore(db)

	projectA, err := projectStore.Create(models.CreateProjectRequest{Name: "Project A"})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := projectStore.Create(models.CreateProjectRequest{Name: "Project B"})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}

	req := models.CreateAgentRunRequest{
		AgentName:      "agent:planning-orchestrator",
		ActionType:     "review",
		IdempotencyKey: "shared-key",
	}
	if _, _, err := agentRunStore.CreateOrGetByIdempotency(projectA.ID, req); err != nil {
		t.Fatalf("create agent run in project A: %v", err)
	}
	if _, _, err := agentRunStore.CreateOrGetByIdempotency(projectB.ID, req); !errors.Is(err, ErrAgentRunIdempotencyProjectMismatch) {
		t.Fatalf("expected ErrAgentRunIdempotencyProjectMismatch, got %v", err)
	}
}
