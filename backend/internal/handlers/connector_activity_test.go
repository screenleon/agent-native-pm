package handlers_test

// connector_activity_test.go covers Phase 6c PR-4 connector activity endpoints:
//   POST /api/connector/activity       (connector-token auth)
//   GET  /api/me/local-connectors/:id/activity  (user auth, polling)
//   GET  /api/projects/:id/active-connectors    (user auth)

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	activitypkg "github.com/screenleon/agent-native-pm/internal/activity"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// activityFixture wires all stores and handlers needed for activity tests.
type activityFixture struct {
	srv         http.Handler
	db          *sql.DB
	hub         *activitypkg.Hub
	connectors  *store.LocalConnectorStore
	projects    *store.ProjectStore
	ownerUserID string
	connectorID string
	token       string
}

func newActivityFixture(t *testing.T) *activityFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	// Seed the local-admin user that InjectLocalAdmin injects on every request.
	// The connector must belong to this user so ownership checks in user-auth
	// endpoints (Get, Stream) pass.
	mustExec(t, db, `INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('local-admin', 'local-admin', 'local@localhost', '', 'admin', TRUE)`)

	projects := store.NewProjectStore(db)
	connectors := store.NewLocalConnectorStore(db, dialect)

	// Seed connector for local-admin (the injected user in local-mode tests),
	// online (last_seen_at = now).
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '', $4, '{}', 1, $5, $6, '', $6, $6)`,
		"conn-act-1", "local-admin", "activity-test-conn", models.LocalConnectorStatusOnline,
		hashConnectorToken("tok-act-1"), now,
	)

	hub := activitypkg.NewHub(connectors)
	activityHandler := handlers.NewConnectorActivityHandler(hub, connectors, projects)

	srv := router.New(router.Deps{
		ConnectorActivityHandler: activityHandler,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
		LocalModeMiddleware: middleware.InjectLocalAdmin,
	})

	return &activityFixture{
		srv:         srv,
		db:          db,
		hub:         hub,
		connectors:  connectors,
		projects:    projects,
		ownerUserID: "local-admin",
		connectorID: "conn-act-1",
		token:       "tok-act-1",
	}
}

// doReport posts a connector activity payload.
func (fx *activityFixture) doReport(token string, a models.ConnectorActivity) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(a)
	req := httptest.NewRequest(http.MethodPost, "/api/connector/activity", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Connector-Token", token)
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

// doGetActivity fetches the activity polling endpoint.
func (fx *activityFixture) doGetActivity(connectorID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/me/local-connectors/"+connectorID+"/activity", nil)
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReport_AcceptsValidActivity verifies that a connector-authenticated POST
// returns 202 and updates the hub.
func TestReport_AcceptsValidActivity(t *testing.T) {
	fx := newActivityFixture(t)

	a := models.ConnectorActivity{
		Phase:     models.ConnectorPhasePlanning,
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	rec := fx.doReport(fx.token, a)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Give the async persist goroutine a moment.
	time.Sleep(50 * time.Millisecond)

	got, ok := fx.hub.Get(fx.connectorID)
	if !ok || got.Phase != models.ConnectorPhasePlanning {
		t.Errorf("hub state after Report: ok=%v phase=%q", ok, got.Phase)
	}
}

// TestReport_RejectsInvalidToken verifies that a bad connector token returns 401.
func TestReport_RejectsInvalidToken(t *testing.T) {
	fx := newActivityFixture(t)

	a := models.ConnectorActivity{Phase: models.ConnectorPhaseIdle, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	rec := fx.doReport("wrong-token", a)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rec.Code)
	}
}

// TestReport_MissingToken verifies that an absent X-Connector-Token header returns 401.
func TestReport_MissingToken(t *testing.T) {
	fx := newActivityFixture(t)

	a := models.ConnectorActivity{Phase: models.ConnectorPhaseIdle, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(a)
	req := httptest.NewRequest(http.MethodPost, "/api/connector/activity", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally no X-Connector-Token header.
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", rec.Code)
	}
}

// TestGetActivity_ReturnsCurrentState verifies the polling endpoint returns
// the activity stored in the hub for the connector.
func TestGetActivity_ReturnsCurrentState(t *testing.T) {
	fx := newActivityFixture(t)

	// Pre-populate the hub.
	a := models.ConnectorActivity{
		Phase:     models.ConnectorPhaseDispatching,
		SubjectID: "task-999",
		UpdatedAt: time.Now().UTC(),
	}
	fx.hub.Update(fx.connectorID, a)

	rec := fx.doGetActivity(fx.connectorID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Data models.ConnectorActivityResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Activity == nil {
		t.Fatal("expected activity in response, got nil")
	}
	if env.Data.Activity.Phase != models.ConnectorPhaseDispatching {
		t.Errorf("expected phase %q, got %q", models.ConnectorPhaseDispatching, env.Data.Activity.Phase)
	}
	if env.Data.Activity.SubjectID != "task-999" {
		t.Errorf("expected subject_id %q, got %q", "task-999", env.Data.Activity.SubjectID)
	}
	// Connector has recent last_seen_at so should be online.
	if !env.Data.Online {
		t.Error("expected online=true for connector with recent last_seen_at")
	}
}

// TestGetActivity_UnknownConnector returns 404.
func TestGetActivity_UnknownConnector(t *testing.T) {
	fx := newActivityFixture(t)
	rec := fx.doGetActivity("nonexistent-connector")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetActivity_NoActivity returns a nil activity (not error) for a connector
// that exists but has no recorded activity.
func TestGetActivity_NoActivity(t *testing.T) {
	fx := newActivityFixture(t)

	rec := fx.doGetActivity(fx.connectorID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data models.ConnectorActivityResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Activity != nil {
		t.Errorf("expected nil activity for connector with no state, got phase=%q", env.Data.Activity.Phase)
	}
}

// TestPersistActivity_RoundTrip tests PersistActivity and GetActivity on the
// store directly (unit-level, using the test DB with migration 031 applied).
func TestPersistActivity_RoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	// Seed user and connector.
	mustExec(t, db, `INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('u-persist', 'u-persist', 'persist@test.com', '', 'member', TRUE)`)
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '', $4, '{}', 1, $5, $6, '', $6, $6)`,
		"conn-persist", "u-persist", "persist-conn", models.LocalConnectorStatusOnline,
		hashConnectorToken("tok-persist"), now,
	)

	s := store.NewLocalConnectorStore(db, dialect)

	a := models.ConnectorActivity{
		Phase:        models.ConnectorPhasePlanning,
		SubjectKind:  "planning_run",
		SubjectID:    "run-abc",
		SubjectTitle: "Implement something",
		StartedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.PersistActivity("conn-persist", a); err != nil {
		t.Fatalf("PersistActivity: %v", err)
	}

	got, updatedAt, err := s.GetActivity("conn-persist")
	if err != nil {
		t.Fatalf("GetActivity: %v", err)
	}
	if got == nil {
		t.Fatal("GetActivity returned nil after PersistActivity")
	}
	if got.Phase != models.ConnectorPhasePlanning {
		t.Errorf("phase: expected %q, got %q", models.ConnectorPhasePlanning, got.Phase)
	}
	if got.SubjectID != "run-abc" {
		t.Errorf("subject_id: expected %q, got %q", "run-abc", got.SubjectID)
	}
	if updatedAt.IsZero() {
		t.Error("expected non-zero updatedAt")
	}
}

// TestGetActivity_ReturnsNilForEmptyJSON verifies that a connector with an
// empty current_activity_json returns nil (not an error or garbage struct).
func TestGetActivity_ReturnsNilForEmptyJSON(t *testing.T) {
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	mustExec(t, db, `INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('u-empty', 'u-empty', 'empty@test.com', '', 'member', TRUE)`)
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '', $4, $5, 1, $6, $7, '', $7, $7)`,
		"conn-empty", "u-empty", "empty-conn", models.LocalConnectorStatusOnline, "{}",
		hashConnectorToken("tok-empty"), now,
	)

	s := store.NewLocalConnectorStore(db, dialect)
	got, _, err := s.GetActivity("conn-empty")
	if err != nil {
		t.Fatalf("GetActivity: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty JSON, got phase=%q", got.Phase)
	}
}
