package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

func TestServiceRunOnceClaimsExecutesAndSubmits(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "adapter.sh")
	script := "#!/bin/sh\nprintf '{\"candidates\":[{\"title\":\"Candidate A\",\"description\":\"desc\"}]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write adapter script: %v", err)
	}
	submitted := models.LocalConnectorSubmitRunResultRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connector/claim-next-run":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.LocalConnectorClaimNextRunResponse{
				Run:         &models.PlanningRun{ID: "run-1"},
				Requirement: &models.Requirement{ID: "req-1", Title: "Do work"},
			}, "error": nil, "meta": nil})
		case "/api/connector/planning-runs/run-1/result":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.PlanningRun{ID: "run-1", Status: models.PlanningRunStatusCompleted}, "error": nil, "meta": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := &Service{
		Client: NewClient(server.URL, "secret-token"),
		State: &State{Adapter: ExecJSONAdapterConfig{
			Command:        scriptPath,
			WorkingDir:     tempDir,
			TimeoutSeconds: 10,
			MaxOutputBytes: 2048,
		}},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	worked, err := service.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !worked {
		t.Fatal("expected claimed work")
	}
	if !submitted.Success || len(submitted.Candidates) != 1 {
		t.Fatalf("unexpected submitted payload %+v", submitted)
	}
	if submitted.Candidates[0].Title != "Candidate A" {
		t.Fatalf("unexpected candidate %+v", submitted.Candidates[0])
	}
}

// TestServiceRunOncePropagatesProjectAndPlanningContext guards that the
// connector forwards the project metadata and planning_context v1 payload
// received from the server into the adapter's stdin, so the agent CLI has
// enough context to produce project-scoped candidates.
func TestServiceRunOncePropagatesProjectAndPlanningContext(t *testing.T) {
	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "input.json")
	scriptPath := filepath.Join(tempDir, "adapter.sh")
	script := "#!/bin/sh\ncat > " + capturePath + "\nprintf '{\"candidates\":[{\"title\":\"T\"}]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write adapter script: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connector/claim-next-run":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.LocalConnectorClaimNextRunResponse{
				Run:         &models.PlanningRun{ID: "run-42", ProjectID: "proj-42"},
				Requirement: &models.Requirement{ID: "req-42", Title: "Plan feature"},
				Project:     &models.Project{ID: "proj-42", Name: "Acme Billing"},
				PlanningContext: &wire.PlanningContextV1{
					SchemaVersion: wire.ContextSchemaV1,
					Sources: wire.PlanningContextSources{
						OpenTasks: []wire.WireTask{{ID: "task-1", Title: "Existing", Status: "open"}},
					},
				},
			}, "error": nil, "meta": nil})
		case "/api/connector/planning-runs/run-42/result":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.PlanningRun{ID: "run-42", Status: models.PlanningRunStatusCompleted}, "error": nil, "meta": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := &Service{
		Client: NewClient(server.URL, "secret-token"),
		State: &State{Adapter: ExecJSONAdapterConfig{
			Command:        scriptPath,
			WorkingDir:     tempDir,
			TimeoutSeconds: 10,
			MaxOutputBytes: 4096,
		}},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured input: %v", err)
	}
	var got ExecJSONInput
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode captured input: %v", err)
	}
	if got.Project == nil || got.Project.Name != "Acme Billing" {
		t.Fatalf("project not propagated: %+v", got.Project)
	}
	if got.PlanningContext == nil || got.PlanningContext.SchemaVersion != wire.ContextSchemaV1 {
		t.Fatalf("planning_context not propagated: %+v", got.PlanningContext)
	}
	if len(got.PlanningContext.Sources.OpenTasks) != 1 {
		t.Fatalf("context sources mangled: %+v", got.PlanningContext.Sources)
	}
}
