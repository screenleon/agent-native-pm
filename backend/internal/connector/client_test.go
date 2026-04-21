package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
)

func TestClientPairHeartbeatClaimAndSubmit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connector/pair":
			writeEnvelope(t, w, models.PairLocalConnectorResponse{
				Connector: models.LocalConnector{ID: "connector-1", Label: "Laptop"},
				ConnectorToken: "secret-token",
			})
		case "/api/connector/heartbeat":
			if got := r.Header.Get("X-Connector-Token"); got != "secret-token" {
				t.Fatalf("unexpected connector token %q", got)
			}
			writeEnvelope(t, w, models.LocalConnector{ID: "connector-1", Label: "Laptop", Status: models.LocalConnectorStatusOnline})
		case "/api/connector/claim-next-run":
			writeEnvelope(t, w, models.LocalConnectorClaimNextRunResponse{
				Run:         &models.PlanningRun{ID: "run-1"},
				Requirement: &models.Requirement{ID: "req-1", Title: "Do work"},
			})
		case "/api/connector/planning-runs/run-1/result":
			writeEnvelope(t, w, models.PlanningRun{ID: "run-1", Status: models.PlanningRunStatusCompleted})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "secret-token")
	ctx := context.Background()
	pair, err := client.Pair(ctx, models.PairLocalConnectorRequest{PairingCode: "PAIR-CODE"})
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if pair.ConnectorToken != "secret-token" {
		t.Fatalf("expected token, got %q", pair.ConnectorToken)
	}
	connector, err := client.Heartbeat(ctx, models.LocalConnectorHeartbeatRequest{})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if connector.Status != models.LocalConnectorStatusOnline {
		t.Fatalf("expected online, got %q", connector.Status)
	}
	claim, err := client.ClaimNextRun(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.Run == nil || claim.Run.ID != "run-1" {
		t.Fatalf("unexpected claim %+v", claim)
	}
	run, err := client.SubmitRunResult(ctx, "run-1", models.LocalConnectorSubmitRunResultRequest{Success: true})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if run.Status != models.PlanningRunStatusCompleted {
		t.Fatalf("expected completed run, got %q", run.Status)
	}
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data, "error": nil, "meta": nil}); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
}