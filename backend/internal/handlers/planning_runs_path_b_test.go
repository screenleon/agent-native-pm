package handlers_test

// Test matrix for Path B Slice S2 — per-run CLI selection plumbing,
// connector protocol-version gating, decoder discipline, and envelope cap.
// Each test is named to match the design DoD ID exactly (T-S2-1 through
// T-S2-13). Design at docs/path-b-subscription-cli-bridge-design.md §8.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/connector"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// pathBFixture wires the planning-run handler against a real SQLite (or
// PostgreSQL) database with the account-bindings + local-connectors stores
// attached. Each test owns a fresh DB; tests run independently.
type pathBFixture struct {
	srv               http.Handler
	db                *sql.DB
	planningRunStore  *store.PlanningRunStore
	requirementStore  *store.RequirementStore
	bindingStore      *store.AccountBindingStore
	localConnectorStore *store.LocalConnectorStore
	notificationStore *store.NotificationStore
	userID            string
	otherUserID       string
	projectID         string
	requirementID     string
}

func newPathBFixture(t *testing.T) *pathBFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	// Seed two users so cross-user-binding tests can reuse the same fixture.
	if _, err := db.Exec(`INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('local-admin', 'local', 'local@example.com', '', 'admin', TRUE)`); err != nil {
		t.Fatalf("seed local-admin: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('user-bravo', 'bravo', 'bravo@example.com', '', 'member', TRUE)`); err != nil {
		t.Fatalf("seed bravo: %v", err)
	}

	projects := store.NewProjectStore(db)
	project, err := projects.Create(models.CreateProjectRequest{Name: "Path B Project", Description: "S2 fixture"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	requirements := store.NewRequirementStore(db)
	requirement, err := requirements.Create(project.ID, models.CreateRequirementRequest{
		Title: "Improve sync recovery UX", Summary: "fixture", Description: "fixture",
	})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	bindings := store.NewAccountBindingStore(db, nil)
	planningRuns := store.NewPlanningRunStore(db, dialect)
	candidates := store.NewBacklogCandidateStore(db, dialect)
	agentRuns := store.NewAgentRunStore(db)
	connectors := store.NewLocalConnectorStore(db, dialect)
	notifications := store.NewNotificationStore(db)

	planner := stubPlanner{}
	handler := handlers.NewPlanningRunHandler(planningRuns, candidates, projects, requirements, agentRuns, planner).
		WithLocalConnectorStore(connectors).
		WithAccountBindings(bindings).
		WithNotifications(notifications).
		WithPlannerFactory(func(userID string) planning.DraftPlanner { return planner })

	srv := router.New(router.Deps{
		PlanningRunHandler: handler,
		LocalModeMiddleware: middleware.InjectLocalAdmin,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})

	return &pathBFixture{
		srv:                 srv,
		db:                  db,
		planningRunStore:    planningRuns,
		requirementStore:    requirements,
		bindingStore:        bindings,
		localConnectorStore: connectors,
		notificationStore:   notifications,
		userID:              "local-admin",
		otherUserID:         "user-bravo",
		projectID:           project.ID,
		requirementID:       requirement.ID,
	}
}

// stubPlanner satisfies planning.DraftPlanner with a single deterministic
// candidate. Avoids pulling in the full settings-backed planner (which
// would also try to load planning_settings rows the fixture doesn't seed).
type stubPlanner struct{}

func (stubPlanner) ResolveSelection(req models.CreatePlanningRunRequest) (models.PlanningProviderSelection, error) {
	return models.PlanningProviderSelection{
		ProviderID:      models.PlanningProviderDeterministic,
		ModelID:         models.PlanningProviderModelDeterministic,
		SelectionSource: models.PlanningSelectionSourceServerDefault,
		BindingSource:   models.PlanningBindingSourceSystem,
	}, nil
}

func (stubPlanner) Generate(ctx context.Context, requirement *models.Requirement, selection models.PlanningProviderSelection) ([]models.BacklogCandidateDraft, error) {
	return []models.BacklogCandidateDraft{{Title: "stub", Rank: 1}}, nil
}

func (stubPlanner) Options() models.PlanningProviderOptions {
	return models.PlanningProviderOptions{}
}

// seedConnector creates a paired-and-online connector for the named user
// at the requested protocol_version. Bypasses the pair flow (which would
// also burn a pairing-code session row).
func (fx *pathBFixture) seedConnector(t *testing.T, userID, label string, protocolVersion int) *models.LocalConnector {
	t.Helper()
	id := "connector-" + label
	now := time.Now().UTC()
	tokenHash := "th-" + label
	if _, err := fx.db.Exec(`
		INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, '', '', $4, '{}', $5, $6, $7, '', $7, $7)`,
		id, userID, label, models.LocalConnectorStatusOnline, protocolVersion, tokenHash, now); err != nil {
		t.Fatalf("seed connector: %v", err)
	}
	conn, err := fx.localConnectorStore.GetByID(id, userID)
	if err != nil || conn == nil {
		t.Fatalf("re-fetch connector: %v", err)
	}
	return conn
}

// createCliBinding inserts a cli:* binding directly via the store (the
// LocalMode gate is enforced at the handler layer; the store accepts the
// row when the request shape is valid).
func (fx *pathBFixture) createCliBinding(t *testing.T, userID, label, providerID, modelID string, primary bool) *models.AccountBinding {
	t.Helper()
	primaryFlag := primary
	binding, err := fx.bindingStore.Create(userID, models.CreateAccountBindingRequest{
		ProviderID: providerID,
		Label:      label,
		ModelID:    modelID,
		IsPrimary:  &primaryFlag,
		CliCommand: "/usr/local/bin/" + strings.TrimPrefix(providerID, "cli:"),
	})
	if err != nil {
		t.Fatalf("create cli binding: %v", err)
	}
	return binding
}

func (fx *pathBFixture) postPlanningRun(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/requirements/"+fx.requirementID+"/planning-runs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

// pathBEnvelope is the response envelope shape that includes warnings.
type pathBEnvelope struct {
	Data     models.PlanningRun       `json:"data"`
	Error    *string                  `json:"error"`
	Warnings []models.EnvelopeWarning `json:"warnings"`
}

// T-S2-1 (migration smoke): both drivers apply 022 + 023 cleanly. Implicit
// in OpenTestDB which calls RunMigrations end-to-end; the explicit assertion
// is that the two new columns exist.
func TestPathBS2_T_S2_1_MigrationsApplyCleanly(t *testing.T) {
	fx := newPathBFixture(t)
	var n int
	if err := fx.db.QueryRow(`SELECT COUNT(*) FROM planning_runs`).Scan(&n); err != nil {
		t.Fatalf("expected planning_runs table to exist: %v", err)
	}
	// account_binding_id must be readable.
	if _, err := fx.db.Exec(`UPDATE planning_runs SET account_binding_id = NULL WHERE 1=0`); err != nil {
		t.Fatalf("account_binding_id column missing: %v", err)
	}
	// protocol_version must be readable on local_connectors.
	if _, err := fx.db.Exec(`UPDATE local_connectors SET protocol_version = 0 WHERE 1=0`); err != nil {
		t.Fatalf("protocol_version column missing: %v", err)
	}
}

// T-S2-2 (snapshot persistence): create run with account_binding_id →
// snapshot present in connector_cli_info.binding_snapshot AND
// account_binding_id column populated.
func TestPathBS2_T_S2_2_SnapshotPersistence(t *testing.T) {
	fx := newPathBFixture(t)
	fx.seedConnector(t, fx.userID, "primary", 1)
	binding := fx.createCliBinding(t, fx.userID, "Mac Claude", "cli:claude", "claude-sonnet-4-6", true)

	bindingID := binding.ID
	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &bindingID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var env pathBEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.AccountBindingID == nil || *env.Data.AccountBindingID != binding.ID {
		t.Fatalf("expected account_binding_id=%q, got %v", binding.ID, env.Data.AccountBindingID)
	}
	if env.Data.ConnectorCliInfo == nil || env.Data.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("expected binding_snapshot in connector_cli_info, got %+v", env.Data.ConnectorCliInfo)
	}
	snap := env.Data.ConnectorCliInfo.BindingSnapshot
	if snap.ProviderID != "cli:claude" || snap.ModelID != "claude-sonnet-4-6" || snap.Label != "Mac Claude" {
		t.Fatalf("snapshot fields wrong: %+v", snap)
	}
	if env.Data.AdapterType != "cli:claude" {
		t.Fatalf("adapter_type should mirror provider: %q", env.Data.AdapterType)
	}
	if env.Data.ModelOverride != "claude-sonnet-4-6" {
		t.Fatalf("model_override should mirror binding model: %q", env.Data.ModelOverride)
	}
}

// T-S2-3 (primary auto-resolution): run created without account_binding_id
// AND user has a primary cli:claude → primary auto-resolved into snapshot.
func TestPathBS2_T_S2_3_PrimaryAutoResolution(t *testing.T) {
	fx := newPathBFixture(t)
	fx.seedConnector(t, fx.userID, "primary", 1)
	primary := fx.createCliBinding(t, fx.userID, "Primary Claude", "cli:claude", "claude-sonnet-4-6", true)

	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode: models.PlanningExecutionModeLocalConnector,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var env pathBEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.AccountBindingID == nil || *env.Data.AccountBindingID != primary.ID {
		t.Fatalf("expected primary auto-resolved to %q, got %v", primary.ID, env.Data.AccountBindingID)
	}
}

// T-S2-4 (zero-bindings backwards-compat): user has zero cli:* bindings →
// account_binding_id stays NULL, no snapshot, run still creates.
func TestPathBS2_T_S2_4_ZeroBindingsBackwardsCompat(t *testing.T) {
	fx := newPathBFixture(t)
	fx.seedConnector(t, fx.userID, "primary", 1)
	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode: models.PlanningExecutionModeLocalConnector,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 (zero-binding fallback), got %d body=%s", rec.Code, rec.Body.String())
	}
	var env pathBEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.AccountBindingID != nil {
		t.Fatalf("expected nil account_binding_id, got %q", *env.Data.AccountBindingID)
	}
	if env.Data.ConnectorCliInfo != nil && env.Data.ConnectorCliInfo.BindingSnapshot != nil {
		t.Fatalf("expected no snapshot, got %+v", env.Data.ConnectorCliInfo.BindingSnapshot)
	}
}

// T-S2-5 (cross-user binding-id): user A submits run with binding owned by
// user B → 400 (R2 ownership check).
func TestPathBS2_T_S2_5_CrossUserBindingRejected(t *testing.T) {
	fx := newPathBFixture(t)
	fx.seedConnector(t, fx.userID, "primary", 1)
	otherBinding := fx.createCliBinding(t, fx.otherUserID, "Bravo Claude", "cli:claude", "claude-sonnet-4-6", true)

	bindingID := otherBinding.ID
	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &bindingID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-user binding, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "account_binding_id") {
		t.Fatalf("expected error to name account_binding_id: %s", rec.Body.String())
	}
}

// T-S2-6 (pre-Path-B connector + CLI run): a connector with
// protocol_version=0 claims; if there's a queued run with account_binding_id,
// claim returns "no run"; envelope warning fires for the requesting user.
func TestPathBS2_T_S2_6_PrePathBConnectorSkipsCliBoundRun(t *testing.T) {
	fx := newPathBFixture(t)
	old := fx.seedConnector(t, fx.userID, "old", 0)
	binding := fx.createCliBinding(t, fx.userID, "Mac Claude", "cli:claude", "claude-sonnet-4-6", true)

	bindingID := binding.ID
	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &bindingID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var env pathBEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Envelope should carry the connector_outdated warning.
	sawOutdated := false
	for _, w := range env.Warnings {
		if w.Code == "connector_outdated" {
			sawOutdated = true
		}
	}
	if !sawOutdated {
		t.Fatalf("expected connector_outdated envelope warning, got %+v", env.Warnings)
	}

	// Notification should have fired for the user.
	notifs, total, err := fx.notificationStore.ListByUser(fx.userID, false, 1, 50)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if total == 0 {
		t.Fatalf("expected at least 1 notification, got 0")
	}
	sawNotif := false
	for _, n := range notifs {
		if n.Kind == "warning" && strings.Contains(n.Body, "anpm-connector") {
			sawNotif = true
		}
	}
	if !sawNotif {
		t.Fatalf("expected warning notification mentioning anpm-connector, got %+v", notifs)
	}

	// And the lease must refuse to hand the run to the old connector.
	leased, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(fx.userID, old.ID, old.Label, time.Minute, old.ProtocolVersion)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if leased != nil {
		t.Fatalf("expected no lease for protocol=0 connector, got %+v", leased)
	}
}

// T-S2-7 (Path-B connector + CLI run): connector with protocol_version=1
// claims same run → success.
func TestPathBS2_T_S2_7_PathBConnectorClaimsCliBoundRun(t *testing.T) {
	fx := newPathBFixture(t)
	pathB := fx.seedConnector(t, fx.userID, "pathb", 1)
	binding := fx.createCliBinding(t, fx.userID, "Mac Claude", "cli:claude", "claude-sonnet-4-6", true)
	bindingID := binding.ID

	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &bindingID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", rec.Code, rec.Body.String())
	}

	leased, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(fx.userID, pathB.ID, pathB.Label, time.Minute, pathB.ProtocolVersion)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if leased == nil {
		t.Fatalf("expected leased run for pathB connector, got nil")
	}
	if leased.AccountBindingID == nil || *leased.AccountBindingID != binding.ID {
		t.Fatalf("expected leased run to expose binding id, got %v", leased.AccountBindingID)
	}
	if leased.ConnectorCliInfo == nil || leased.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("expected snapshot on leased run")
	}
}

// T-S2-8 (decoder discipline): connector daemon decodes a claim response
// that contains a future unknown field — does NOT error.
func TestPathBS2_T_S2_8_DecoderToleratesUnknownFields(t *testing.T) {
	// The response shape exposed by the connector client is plain
	// json.Unmarshal — which by definition ignores unknown fields. We
	// assert that explicitly here by feeding the connector's claim-response
	// type a payload with extra keys and confirming no error.
	raw := []byte(`{
		"run": {"id":"r-1","project_id":"p","requirement_id":"req"},
		"requirement": {"id":"req","title":"t"},
		"cli_binding": {"id":"b","provider_id":"cli:claude","model_id":"x","cli_command":"/usr/local/bin/claude","label":"L"},
		"future_field_added_in_v3": {"foo":"bar"},
		"another_future_field": ["a","b","c"]
	}`)
	var out models.LocalConnectorClaimNextRunResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unknown fields must be silently ignored: %v", err)
	}
	if out.CliBinding == nil || out.CliBinding.ProviderID != "cli:claude" {
		t.Fatalf("expected cli_binding decoded, got %+v", out.CliBinding)
	}
}

// T-S2-9 (envelope size budget): connector refuses to spawn adapter when
// stdin envelope exceeds 264 KiB. Returns submit-result with success=false
// and an adapter_protocol_error message.
func TestPathBS2_T_S2_9_EnvelopeSizeBudget(t *testing.T) {
	// Build an oversized planning context by stuffing the open_tasks list
	// with a very large title. The full envelope marshal must exceed
	// MaxAdapterStdinBytes (264 KiB).
	bigTitle := strings.Repeat("x", connector.MaxAdapterStdinBytes+1024)
	ctx := &wire.PlanningContextV1{
		SchemaVersion: "context.v1",
		Sources: wire.PlanningContextSources{
			OpenTasks: []wire.WireTask{
				{ID: "task-1", Title: bigTitle},
			},
		},
	}
	input := connector.ExecJSONInput{
		Run:                    &models.PlanningRun{ID: "r-1"},
		Requirement:            &models.Requirement{ID: "req-1", Title: "t"},
		RequestedMaxCandidates: 3,
		PlanningContext:        ctx,
	}
	// Adapter command of `/bin/true` would normally succeed; but the cap
	// check fires before spawn.
	cfg := connector.ExecJSONAdapterConfig{
		Command:        "/bin/true",
		TimeoutSeconds: 5,
		MaxOutputBytes: 1024,
	}
	result := connector.ExecuteExecJSON(context.Background(), cfg, input)
	if result.Success {
		t.Fatalf("expected refusal, got success")
	}
	if !strings.Contains(result.ErrorMessage, "adapter_protocol_error") {
		t.Fatalf("expected adapter_protocol_error hint, got %q", result.ErrorMessage)
	}
	if !strings.Contains(result.ErrorMessage, fmt.Sprintf("%d byte cap", connector.MaxAdapterStdinBytes)) {
		t.Fatalf("expected cap value in error: %q", result.ErrorMessage)
	}
}

// T-S2-10 (stale cli_health warning) — SKIPPED. The cli_health metadata
// surface is added in S5b and not present in the current schema. The S2
// design (§6.2) explicitly stubs this check with the comment
// "stub the check: only fire when local_connectors.metadata->>'cli_health'
// already exists". With nothing to seed against, the warning cannot
// meaningfully fire; the hook lives in resolvePathBBinding and S5b will
// flip it on without further changes here.
func TestPathBS2_T_S2_10_StaleCliHealthWarning(t *testing.T) {
	t.Skip("S5b adds local_connectors.metadata.cli_health; S2 stubs the check per design §6.2")
}

// T-S2-11 (binding-deleted-mid-flight): create run with account_binding_id,
// delete binding, lease still succeeds (snapshot preserved).
func TestPathBS2_T_S2_11_BindingDeletedMidFlight(t *testing.T) {
	fx := newPathBFixture(t)
	pathB := fx.seedConnector(t, fx.userID, "pathb", 1)
	binding := fx.createCliBinding(t, fx.userID, "Will Be Deleted", "cli:claude", "claude-sonnet-4-6", true)
	bindingID := binding.ID

	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &bindingID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	if err := fx.bindingStore.Delete(binding.ID, fx.userID); err != nil {
		t.Fatalf("delete binding: %v", err)
	}

	leased, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(fx.userID, pathB.ID, pathB.Label, time.Minute, pathB.ProtocolVersion)
	if err != nil {
		t.Fatalf("lease after delete: %v", err)
	}
	if leased == nil {
		t.Fatalf("expected lease after binding delete")
	}
	if leased.ConnectorCliInfo == nil || leased.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("snapshot must survive binding deletion")
	}
	if leased.ConnectorCliInfo.BindingSnapshot.Label != "Will Be Deleted" {
		t.Fatalf("snapshot label drift: %q", leased.ConnectorCliInfo.BindingSnapshot.Label)
	}
}

// T-S2-12 (integration round-trip): full create with two distinct CLI
// bindings produces two distinct adapter invocations (assert via the
// snapshot embedded in each leased run's cli_binding payload).
func TestPathBS2_T_S2_12_TwoBindingsTwoInvocations(t *testing.T) {
	fx := newPathBFixture(t)
	pathB := fx.seedConnector(t, fx.userID, "pathb", 1)

	// Toggle the auto-active-uniqueness behaviour: claude binding active,
	// then codex active (same partial unique index forces single-active per
	// provider — so we keep both around active simultaneously by virtue of
	// having distinct provider_ids).
	claude := fx.createCliBinding(t, fx.userID, "Mac Claude", "cli:claude", "claude-sonnet-4-6", true)
	codex := fx.createCliBinding(t, fx.userID, "Mac Codex", "cli:codex", "gpt-5.4", true)

	// First run pinned to Claude.
	claudeID := claude.ID
	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &claudeID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create claude run: %d %s", rec.Code, rec.Body.String())
	}
	leased1, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(fx.userID, pathB.ID, pathB.Label, time.Minute, pathB.ProtocolVersion)
	if err != nil || leased1 == nil {
		t.Fatalf("lease 1: %v %v", err, leased1)
	}
	// Complete the run so the requirement is free to host another.
	if err := fx.planningRunStore.CompleteLocalConnectorRun(leased1.ID, pathB.ID, ""); err != nil {
		t.Fatalf("complete leased1: %v", err)
	}

	// Second run pinned to Codex.
	codexID := codex.ID
	rec = fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode:    models.PlanningExecutionModeLocalConnector,
		AccountBindingID: &codexID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create codex run: %d %s", rec.Code, rec.Body.String())
	}
	leased2, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(fx.userID, pathB.ID, pathB.Label, time.Minute, pathB.ProtocolVersion)
	if err != nil || leased2 == nil {
		t.Fatalf("lease 2: %v %v", err, leased2)
	}

	if leased1.ConnectorCliInfo == nil || leased1.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("leased1 missing snapshot")
	}
	if leased2.ConnectorCliInfo == nil || leased2.ConnectorCliInfo.BindingSnapshot == nil {
		t.Fatalf("leased2 missing snapshot")
	}
	if leased1.ConnectorCliInfo.BindingSnapshot.ProviderID != "cli:claude" {
		t.Fatalf("leased1 wrong provider: %q", leased1.ConnectorCliInfo.BindingSnapshot.ProviderID)
	}
	if leased2.ConnectorCliInfo.BindingSnapshot.ProviderID != "cli:codex" {
		t.Fatalf("leased2 wrong provider: %q", leased2.ConnectorCliInfo.BindingSnapshot.ProviderID)
	}
}

// T-S2-13 (rollback fixtures) — SKIPPED. Mirroring T-S1-10's reduction
// clause (DECISIONS 2026-04-23 entry): the project still lacks a generic
// rollback test fixture pattern. The two .down.sql files ship and were
// hand-validated; landing the fixture infrastructure is a follow-up.
func TestPathBS2_T_S2_13_RollbackFixtures(t *testing.T) {
	t.Skip("rollback fixture infrastructure not present; sibling .down.sql files ship per design §13")
}

// pathBS5aFixture extends pathBFixture with a LocalConnectorHandler wired
// into the router so submit-result HTTP round-trips can be exercised.
type pathBS5aFixture struct {
	pathBFixture
	connectorHandler *handlers.LocalConnectorHandler
}

func newPathBS5aFixture(t *testing.T) *pathBS5aFixture {
	t.Helper()
	base := newPathBFixture(t)

	agentRuns := store.NewAgentRunStore(base.db)
	connectorStore := base.localConnectorStore

	connHandler := handlers.NewLocalConnectorHandler(
		connectorStore,
		base.planningRunStore,
		base.requirementStore,
		store.NewBacklogCandidateStore(base.db, testutil.TestDialect()),
		agentRuns,
	).WithNotificationStore(base.notificationStore)

	projects := store.NewProjectStore(base.db)

	planner := stubPlanner{}
	planningRunHandler := handlers.NewPlanningRunHandler(
		base.planningRunStore,
		store.NewBacklogCandidateStore(base.db, testutil.TestDialect()),
		projects,
		base.requirementStore,
		agentRuns,
		planner,
	).WithLocalConnectorStore(connectorStore).
		WithAccountBindings(base.bindingStore).
		WithNotifications(base.notificationStore).
		WithPlannerFactory(func(userID string) planning.DraftPlanner { return planner })

	srv := router.New(router.Deps{
		PlanningRunHandler:    planningRunHandler,
		LocalConnectorHandler: connHandler,
		LocalModeMiddleware:   middleware.InjectLocalAdmin,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})

	base.srv = srv
	return &pathBS5aFixture{pathBFixture: *base, connectorHandler: connHandler}
}

// submitResult posts to /api/connector/planning-runs/:id/result with the
// given connector token and body.
func (fx *pathBS5aFixture) submitResult(t *testing.T, runID, tokenHash string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/connector/planning-runs/"+runID+"/result", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Connector-Token", tokenHash)
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

// seedAndLeaseRun creates a planning run, seeds an S2-capable connector with
// a known raw token (stored as sha256 in the DB), and leases the run. Returns
// the run ID and the raw token to use as X-Connector-Token.
func (fx *pathBS5aFixture) seedAndLeaseRun(t *testing.T) (runID, rawToken string) {
	t.Helper()
	rawToken = "s5a-test-connector-token"
	tokenHashBytes := sha256.Sum256([]byte(rawToken))
	tokenHashHex := hex.EncodeToString(tokenHashBytes[:])

	connID := "connector-s5a"
	now := time.Now().UTC()
	if _, err := fx.db.Exec(`
		INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, '', '', $4, '{}', $5, $6, $7, '', $7, $7)`,
		connID, fx.userID, "s5a", models.LocalConnectorStatusOnline, 1, tokenHashHex, now); err != nil {
		t.Fatalf("seed s5a connector: %v", err)
	}
	conn, err := fx.localConnectorStore.GetByID(connID, fx.userID)
	if err != nil || conn == nil {
		t.Fatalf("re-fetch s5a connector: %v", err)
	}

	rec := fx.postPlanningRun(t, models.CreatePlanningRunRequest{
		ExecutionMode: models.PlanningExecutionModeLocalConnector,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", rec.Code, rec.Body.String())
	}
	var env pathBEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	runID = env.Data.ID

	if _, err := fx.planningRunStore.LeaseNextLocalConnectorRunForProtocol(
		fx.userID, conn.ID, conn.Label, time.Minute, conn.ProtocolVersion,
	); err != nil {
		t.Fatalf("lease run: %v", err)
	}
	return runID, rawToken
}

// T-S5a-1: submit-result with error_kind=session_expired → run's
// connector_cli_info.error_kind == "session_expired" and remediation_hint non-empty.
func TestPathBS5a_T_S5a_1_SessionExpiredHint(t *testing.T) {
	fx := newPathBS5aFixture(t)
	runID, token := fx.seedAndLeaseRun(t)

	rec := fx.submitResult(t, runID, token, map[string]any{
		"success":      false,
		"error_message": "session expired",
		"error_kind":   "session_expired",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	run, err := fx.planningRunStore.GetByID(runID)
	if err != nil || run == nil {
		t.Fatalf("reload run: %v", err)
	}
	if run.ConnectorCliInfo == nil {
		t.Fatalf("expected connector_cli_info to be set")
	}
	if run.ConnectorCliInfo.ErrorKind != "session_expired" {
		t.Fatalf("expected error_kind=session_expired, got %q", run.ConnectorCliInfo.ErrorKind)
	}
	if run.ConnectorCliInfo.RemediationHint == "" {
		t.Fatalf("expected non-empty remediation_hint for session_expired")
	}
}

// T-S5a-2: submit-result with error_kind=unknown → error_kind=="unknown",
// remediation_hint empty.
func TestPathBS5a_T_S5a_2_UnknownNoHint(t *testing.T) {
	fx := newPathBS5aFixture(t)
	runID, token := fx.seedAndLeaseRun(t)

	rec := fx.submitResult(t, runID, token, map[string]any{
		"success":      false,
		"error_message": "something unknown",
		"error_kind":   "unknown",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	run, err := fx.planningRunStore.GetByID(runID)
	if err != nil || run == nil {
		t.Fatalf("reload run: %v", err)
	}
	if run.ConnectorCliInfo == nil {
		t.Fatalf("expected connector_cli_info to be set")
	}
	if run.ConnectorCliInfo.ErrorKind != "unknown" {
		t.Fatalf("expected error_kind=unknown, got %q", run.ConnectorCliInfo.ErrorKind)
	}
	if run.ConnectorCliInfo.RemediationHint != "" {
		t.Fatalf("expected empty remediation_hint for unknown, got %q", run.ConnectorCliInfo.RemediationHint)
	}
}

// T-S5a-3: submit-result with error_kind not in the allowlist → normalised to "unknown".
func TestPathBS5a_T_S5a_3_UnknownEnumNormalized(t *testing.T) {
	fx := newPathBS5aFixture(t)
	runID, token := fx.seedAndLeaseRun(t)

	rec := fx.submitResult(t, runID, token, map[string]any{
		"success":      false,
		"error_message": "bad enum",
		"error_kind":   "not_in_enum",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	run, err := fx.planningRunStore.GetByID(runID)
	if err != nil || run == nil {
		t.Fatalf("reload run: %v", err)
	}
	if run.ConnectorCliInfo == nil {
		t.Fatalf("expected connector_cli_info to be set")
	}
	if run.ConnectorCliInfo.ErrorKind != "unknown" {
		t.Fatalf("expected normalised error_kind=unknown, got %q", run.ConnectorCliInfo.ErrorKind)
	}
}

// T-S5a-4: submit-result without error_kind field → defaults to "unknown"
// (backwards compatibility).
func TestPathBS5a_T_S5a_4_MissingErrorKindDefaultsToUnknown(t *testing.T) {
	fx := newPathBS5aFixture(t)
	runID, token := fx.seedAndLeaseRun(t)

	rec := fx.submitResult(t, runID, token, map[string]any{
		"success":      false,
		"error_message": "generic failure",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	run, err := fx.planningRunStore.GetByID(runID)
	if err != nil || run == nil {
		t.Fatalf("reload run: %v", err)
	}
	if run.ConnectorCliInfo == nil {
		t.Fatalf("expected connector_cli_info to be set")
	}
	if run.ConnectorCliInfo.ErrorKind != "unknown" {
		t.Fatalf("expected error_kind=unknown (backwards compat), got %q", run.ConnectorCliInfo.ErrorKind)
	}
}

// T-S5a-5: every AllowedErrorKinds member except "unknown" has a
// corresponding entry in ErrorKindRemediations.
func TestPathBS5a_T_S5a_5_RemediationCatalogComplete(t *testing.T) {
	for kind := range models.AllowedErrorKinds {
		if kind == models.ErrorKindUnknown {
			continue
		}
		hint, ok := models.ErrorKindRemediations[kind]
		if !ok {
			t.Errorf("AllowedErrorKinds[%q] has no entry in ErrorKindRemediations", kind)
			continue
		}
		if strings.TrimSpace(hint) == "" {
			t.Errorf("ErrorKindRemediations[%q] is blank", kind)
		}
	}
}

// Defensive build assertion: ensure the database driver constants we
// touched in this file are still importable. Cheap, but it catches the
// case where someone deletes the database package by accident.
var _ = database.IsSQLiteDSN
