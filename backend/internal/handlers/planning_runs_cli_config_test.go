package handlers_test

// Phase 6a UX-B3: planning run create resolves CLI selection from the
// new connector-owned cli_configs metadata when the caller supplies
// connector_id + cli_config_id. Legacy account_binding_id still works.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

type cliConfigRunFixture struct {
	srv             http.Handler
	db              *sql.DB
	projectStore    *store.ProjectStore
	requirements    *store.RequirementStore
	connectors      *store.LocalConnectorStore
	accountBindings *store.AccountBindingStore
	userID          string
	projectID       string
	requirementID   string
}

func newCliConfigRunFixture(t *testing.T) cliConfigRunFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)

	if _, err := db.Exec(`INSERT INTO users (id, username, email, password_hash, role, is_active) VALUES ('local-admin', 'local', 'local@example.com', '', 'admin', TRUE)`); err != nil {
		t.Fatalf("seed local-admin: %v", err)
	}

	projectStore := store.NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "CliCfgProj"})
	if err != nil {
		t.Fatal(err)
	}
	reqStore := store.NewRequirementStore(db)
	req, err := reqStore.Create(project.ID, models.CreateRequirementRequest{Title: "r", Source: "human"})
	if err != nil {
		t.Fatal(err)
	}

	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	syncRunStore := store.NewSyncRunStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	planningRunStore := store.NewPlanningRunStore(db, testutil.TestDialect())
	candidateStore := store.NewBacklogCandidateStore(db, testutil.TestDialect())
	planningSettingsStore := store.NewPlanningSettingsStore(db, nil)
	accountBindingStore := store.NewAccountBindingStore(db, nil)
	connectorStore := store.NewLocalConnectorStore(db, testutil.TestDialect())

	planner := planning.NewSettingsBackedPlanner(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore, planningSettingsStore, 0)
	handler := handlers.NewPlanningRunHandler(planningRunStore, candidateStore, projectStore, reqStore, agentRunStore, planner).
		WithAccountBindings(accountBindingStore).
		WithLocalConnectorStore(connectorStore)

	srv := router.New(router.Deps{
		PlanningRunHandler:  handler,
		LocalModeMiddleware: middleware.InjectLocalAdmin,
		AuthMiddleware:      func(next http.Handler) http.Handler { return next },
	})

	return cliConfigRunFixture{
		srv:             srv,
		db:              db,
		projectStore:    projectStore,
		requirements:    reqStore,
		connectors:      connectorStore,
		accountBindings: accountBindingStore,
		userID:          "local-admin",
		projectID:       project.ID,
		requirementID:   req.ID,
	}
}

func (fx cliConfigRunFixture) seedConnectorWithCliConfig(t *testing.T, role, modelID string) (string, string) {
	t.Helper()
	pairing, err := fx.connectors.CreatePairingSession(fx.userID, models.CreateLocalConnectorPairingSessionRequest{Label: "Laptop"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := fx.connectors.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode: pairing.PairingCode, Label: "Laptop", Platform: "linux", ClientVersion: "t", ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Heartbeat so the connector transitions from `pending` to `online`
	// and passes the `usableLocalConnector` gate in the planning-run
	// handler (requires status=online AND last_seen_at within the 90s
	// liveness window).
	if _, err := fx.connectors.HeartbeatByToken(claim.ConnectorToken, models.LocalConnectorHeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := fx.connectors.AddCliConfig(claim.Connector.ID, fx.userID, models.CreateCliConfigRequest{
		ProviderID: role, ModelID: modelID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return claim.Connector.ID, cfg.ID
}

func (fx cliConfigRunFixture) createRun(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/requirements/"+fx.requirementID+"/planning-runs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fx.srv.ServeHTTP(w, req)
	return w
}

// T-6a-B3-1: valid (connector_id, cli_config_id) → 201 + binding snapshot populated.
func TestCreatePlanningRun_CliConfigHappyPath(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	connID, cfgID := fx.seedConnectorWithCliConfig(t, "cli:claude", "claude-sonnet-4-6")

	w := fx.createRun(t, map[string]any{
		"trigger_source":  "manual",
		"execution_mode":  "local_connector",
		"connector_id":    connID,
		"cli_config_id":   cfgID,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ConnectorCliInfo *struct {
				BindingSnapshot *struct {
					ProviderID string `json:"provider_id"`
					ModelID    string `json:"model_id"`
				} `json:"binding_snapshot"`
			} `json:"connector_cli_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Data.ConnectorCliInfo == nil || resp.Data.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("expected binding_snapshot populated; body=%s", w.Body.String())
	}
	if resp.Data.ConnectorCliInfo.BindingSnapshot.ProviderID != "cli:claude" {
		t.Fatalf("provider_id in snapshot: want cli:claude, got %q", resp.Data.ConnectorCliInfo.BindingSnapshot.ProviderID)
	}
	if resp.Data.ConnectorCliInfo.BindingSnapshot.ModelID != "claude-sonnet-4-6" {
		t.Fatalf("model_id in snapshot: want claude-sonnet-4-6, got %q", resp.Data.ConnectorCliInfo.BindingSnapshot.ModelID)
	}
}

// T-6a-B3-2: cli_config_id on a connector that doesn't own it → 400.
func TestCreatePlanningRun_CliConfigMismatch(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	connA, cfgA := fx.seedConnectorWithCliConfig(t, "cli:claude", "m")

	// Pair a second connector; send its id paired with connA's config id.
	pairing, _ := fx.connectors.CreatePairingSession(fx.userID, models.CreateLocalConnectorPairingSessionRequest{Label: "Workstation"})
	claim, _ := fx.connectors.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode: pairing.PairingCode, Label: "Workstation", Platform: "linux", ProtocolVersion: 1,
	})
	_ = connA

	w := fx.createRun(t, map[string]any{
		"trigger_source":  "manual",
		"execution_mode":  "local_connector",
		"connector_id":    claim.Connector.ID, // wrong connector for cfgA
		"cli_config_id":   cfgA,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched (connector, cli_config), got %d body=%s", w.Code, w.Body.String())
	}
}

// T-6a-B3-3: cli_config_id without connector_id → 400.
func TestCreatePlanningRun_CliConfigMissingConnectorID(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	_, cfgID := fx.seedConnectorWithCliConfig(t, "cli:claude", "m")
	w := fx.createRun(t, map[string]any{
		"trigger_source":  "manual",
		"execution_mode":  "local_connector",
		"cli_config_id":   cfgID,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// cli_config_id outside local_connector mode → 400.
func TestCreatePlanningRun_CliConfigOnlyForLocalConnector(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	connID, cfgID := fx.seedConnectorWithCliConfig(t, "cli:claude", "m")
	w := fx.createRun(t, map[string]any{
		"trigger_source":  "manual",
		"execution_mode":  "deterministic",
		"connector_id":    connID,
		"cli_config_id":   cfgID,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cli_config only valid for local_connector), got %d body=%s", w.Code, w.Body.String())
	}
}

// Unknown connector_id + cli_config_id → 400 (regardless of whether they
// belong to someone else or don't exist at all; GetCliConfig user-scopes
// the lookup so cross-user access degrades to not-found).
func TestCreatePlanningRun_CliConfigUnknownIDs(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	w := fx.createRun(t, map[string]any{
		"trigger_source": "manual",
		"execution_mode": "local_connector",
		"connector_id":   "not-a-real-connector",
		"cli_config_id":  "not-a-real-config",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// Cross-user isolation regression test (Critic SHOULD-FIX #1 / Copilot
// review on PR #23). Seeds a real cli_config on user B's connector, then
// POSTs the planning-run create as user A (local-admin) referencing both
// real IDs. Expect 400 "cli_config_id not found" — proving GetCliConfig
// still scopes by user_id and a dropped filter would surface as a 201
// (regression) rather than the not-found mask used by the unknown-IDs test
// above.
func TestCreatePlanningRun_CliConfigCrossUserDenied(t *testing.T) {
	fx := newCliConfigRunFixture(t)

	// Seed user B and pair a connector + cli_config under their user_id
	// directly via the store (bypasses the HTTP layer because
	// InjectLocalAdmin pins requests to user A).
	userB := "user-b"
	if _, err := fx.db.Exec(`INSERT INTO users (id, username, email, password_hash, role, is_active) VALUES ($1, $1, $1 || '@example.com', '', 'admin', TRUE)`, userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	pairing, err := fx.connectors.CreatePairingSession(userB, models.CreateLocalConnectorPairingSessionRequest{Label: "B-Laptop"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := fx.connectors.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode: pairing.PairingCode, Label: "B-Laptop", Platform: "linux", ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfgB, err := fx.connectors.AddCliConfig(claim.Connector.ID, userB, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Request reaches the handler as user A (local-admin) but references
	// user B's real connector + cli_config. Expect 400, NOT 201.
	w := fx.createRun(t, map[string]any{
		"trigger_source": "manual",
		"execution_mode": "local_connector",
		"connector_id":   claim.Connector.ID,
		"cli_config_id":  cfgB.ID,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-user cli_config must 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// adapter_type mismatch (Critic SHOULD-FIX #3 / Copilot review on PR #23).
// When the caller sends an adapter_type that disagrees with the resolved
// cli_config.provider_id, the server must reject with 400 rather than
// silently overwriting one or the other into a self-inconsistent row.
func TestCreatePlanningRun_CliConfigAdapterTypeMismatch(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	connID, cfgID := fx.seedConnectorWithCliConfig(t, "cli:claude", "claude-sonnet-4-6")
	w := fx.createRun(t, map[string]any{
		"trigger_source": "manual",
		"execution_mode": "local_connector",
		"connector_id":   connID,
		"cli_config_id":  cfgID,
		"adapter_type":   "cli:codex", // intentional mismatch
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("adapter_type mismatch must 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// account_binding_id is silently cleared when (connector_id, cli_config_id)
// also resolve (Critic SHOULD-FIX #2 / Copilot review on PR #23). The
// resulting row must NOT carry the caller's account_binding_id.
func TestCreatePlanningRun_CliConfigClearsAccountBindingID(t *testing.T) {
	fx := newCliConfigRunFixture(t)
	connID, cfgID := fx.seedConnectorWithCliConfig(t, "cli:claude", "claude-sonnet-4-6")

	w := fx.createRun(t, map[string]any{
		"trigger_source":     "manual",
		"execution_mode":     "local_connector",
		"connector_id":       connID,
		"cli_config_id":      cfgID,
		"account_binding_id": "some-other-binding-id",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			AccountBindingID  *string `json:"account_binding_id"`
			TargetConnectorID *string `json:"target_connector_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Data.AccountBindingID != nil {
		t.Fatalf("account_binding_id must be cleared on cli_config-authored runs; got %q", *resp.Data.AccountBindingID)
	}
	if resp.Data.TargetConnectorID == nil || *resp.Data.TargetConnectorID != connID {
		t.Fatalf("target_connector_id must equal the requested connector_id (%s); got %v", connID, resp.Data.TargetConnectorID)
	}
}
