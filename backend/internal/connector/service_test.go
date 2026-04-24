package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// T-S5b-3: RunOnce tracks CliBinding.ID when the claim response includes a
// cli_binding block, so the health probe loop can probe it at the next heartbeat.
func TestServiceRunOnceTracksCliBinding(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '{\"candidates\":[]}'\n"), 0o755); err != nil {
		t.Fatalf("write adapter: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connector/claim-next-run":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.LocalConnectorClaimNextRunResponse{
				Run:         &models.PlanningRun{ID: "run-x"},
				Requirement: &models.Requirement{ID: "req-x", Title: "Track"},
				CliBinding:  &models.PlanningRunCliBindingPayload{ID: "bind-1", CliCommand: "claude", ProviderID: "cli:claude"},
			}, "error": nil})
		case "/api/connector/planning-runs/run-x/result":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.PlanningRun{ID: "run-x", Status: "completed"}, "error": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc := &Service{
		Client: NewClient(server.URL, "tok"),
		State: &State{Adapter: ExecJSONAdapterConfig{
			Command:        scriptPath,
			WorkingDir:     tempDir,
			TimeoutSeconds: 5,
		}},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	svc.mu.Lock()
	b, ok := svc.knownCliBindings["bind-1"]
	svc.mu.Unlock()
	if !ok {
		t.Fatal("expected bind-1 to be tracked after RunOnce")
	}
	if b.command != "claude" {
		t.Fatalf("unexpected command %q", b.command)
	}
}

// T-S5b-5: probeCliCommand must skip interpreter commands (blocklist).
func TestProbeCliCommandBlocklistInterpreters(t *testing.T) {
	svc := &Service{Stdout: io.Discard, Stderr: io.Discard}
	for _, cmd := range []string{"python3", "python", "node", "ruby"} {
		healthy, _, errMsg, cancelled := svc.probeCliCommand(context.Background(), cmd)
		if healthy {
			t.Errorf("cmd %q: expected blocklist skip, got healthy=true", cmd)
		}
		if cancelled {
			t.Errorf("cmd %q: unexpected cancelled=true for blocklist command", cmd)
		}
		if !strings.Contains(errMsg, "interpreter") {
			t.Errorf("cmd %q: expected interpreter skip message, got %q", cmd, errMsg)
		}
	}
}

// T-S5b-5b: probeCliCommand returns unhealthy for a non-existent command.
func TestProbeCliCommandNotFound(t *testing.T) {
	svc := &Service{Stdout: io.Discard, Stderr: io.Discard}
	healthy, _, errMsg, cancelled := svc.probeCliCommand(context.Background(), "this-binary-does-not-exist-anpm-test")
	if healthy {
		t.Fatalf("expected unhealthy for missing binary, got healthy=true")
	}
	if cancelled {
		t.Fatal("expected cancelled=false for not-found binary")
	}
	if errMsg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !strings.Contains(errMsg, "not found on PATH") {
		t.Errorf("expected 'not found on PATH' in errMsg, got %q", errMsg)
	}
}

// T-S5b-3b: runDueProbes only probes bindings whose last probe is past the interval.
func TestRunDueProbesRespectsCooldown(t *testing.T) {
	var probeCount int
	// Use a write-to-buffer stdout so we can verify log lines were emitted.
	var logBuf strings.Builder
	svc := &Service{
		Stdout: &logBuf,
		Stderr: &logBuf,
		knownCliBindings: map[string]knownCliBinding{
			"fresh": {command: "this-binary-does-not-exist", lastProbedAt: time.Now()},
			"stale": {command: "this-binary-does-not-exist", lastProbedAt: time.Now().Add(-10 * time.Minute)},
		},
	}
	// Instrument: count how many probes actually run.
	before := logBuf.Len()
	svc.runDueProbes(context.Background(), 5*time.Minute)
	_ = before
	probeCount = 0
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "cli health probe") && strings.Contains(line, "stale") {
			probeCount++
		}
		if strings.Contains(line, "cli health probe") && strings.Contains(line, "fresh") {
			t.Errorf("fresh binding should NOT have been probed")
		}
	}
	if probeCount == 0 {
		t.Fatal("expected at least one log line for the stale binding")
	}
}

// T-S5b-4: heartbeat carries last_cli_healthy_at when a prior probe succeeded.
func TestServiceHeartbeatCarriesLastHealthyAt(t *testing.T) {
	var capturedHB models.LocalConnectorHeartbeatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connector/heartbeat":
			if err := json.NewDecoder(r.Body).Decode(&capturedHB); err != nil {
				t.Errorf("decode heartbeat: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models.LocalConnector{Status: "online"}, "error": nil})
		case "/api/connector/claim-next-run":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "error": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	healthyAt := time.Now().UTC().Add(-30 * time.Second)
	svc := &Service{
		Client:            NewClient(server.URL, "tok"),
		State:             &State{},
		HeartbeatInterval: 10 * time.Millisecond,
		PollInterval:      5 * time.Millisecond,
		CliHealthDisabled: true, // probe loop off; we pre-seed lastCliHealthyAt
		Stdout:            io.Discard,
		Stderr:            io.Discard,
		lastCliHealthyAt:  &healthyAt,
	}
	_ = svc.Run(ctx)
	if capturedHB.LastCliHealthyAt == nil {
		t.Fatal("expected heartbeat to carry last_cli_healthy_at")
	}
}
