package connector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
)

func TestExecuteExecJSONSuccess(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "adapter.sh")
	script := "#!/bin/sh\nprintf '{\"candidates\":[{\"title\":\"Candidate A\",\"description\":\"desc\"}]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write adapter script: %v", err)
	}
	result := ExecuteExecJSON(context.Background(), ExecJSONAdapterConfig{
		Command:        scriptPath,
		WorkingDir:     tempDir,
		TimeoutSeconds: 10,
		MaxOutputBytes: 2048,
	}, ExecJSONInput{Run: &models.PlanningRun{ID: "run-1"}, Requirement: &models.Requirement{ID: "req-1", Title: "Do work"}, RequestedMaxCandidates: 3})
	if !result.Success {
		t.Fatalf("expected success result, got %+v", result)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Title != "Candidate A" {
		t.Fatalf("unexpected candidates %+v", result.Candidates)
	}
}

func TestExecuteExecJSONMalformedOutput(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "adapter.sh")
	script := "#!/bin/sh\nprintf 'not-json'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write adapter script: %v", err)
	}
	result := ExecuteExecJSON(context.Background(), ExecJSONAdapterConfig{
		Command:        scriptPath,
		WorkingDir:     tempDir,
		TimeoutSeconds: 10,
		MaxOutputBytes: 2048,
	}, ExecJSONInput{Run: &models.PlanningRun{ID: "run-1"}, Requirement: &models.Requirement{ID: "req-1", Title: "Do work"}, RequestedMaxCandidates: 3})
	if result.Success {
		t.Fatalf("expected failure result, got %+v", result)
	}
	if result.ErrorMessage == "" {
		t.Fatal("expected error message for malformed output")
	}
}