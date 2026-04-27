package handlers_test

// Phase 6c PR-3: handler-level tests for POST /api/backlog-candidates/:id/suggest-role.
// Tests cover: nil suggester → 503, missing candidate → 404, LLM error → 200 with
// error_kind in body (API-008 advisory LLM contract), and a successful suggestion.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/connector"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// suggestFixture extends applyFixture with a wired role suggester.
type suggestFixture struct {
	applyFixture
}

func newSuggestFixture(t *testing.T, suggester func(ctx context.Context, title, desc, req, proj string, cliSel *connector.AdapterCliSelection) connector.SuggestRoleResult) suggestFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if _, err := db.Exec(`INSERT INTO users (id, username, email, password_hash, role, is_active) VALUES ('local-admin', 'local', 'local@example.com', '', 'admin', TRUE)`); err != nil {
		t.Fatalf("seed local-admin: %v", err)
	}

	projectStore := store.NewProjectStore(db)
	requirementStore := store.NewRequirementStore(db)
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	syncRunStore := store.NewSyncRunStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	planningRunStore := store.NewPlanningRunStore(db, testutil.TestDialect())
	candidateStore := store.NewBacklogCandidateStore(db, testutil.TestDialect())
	planningSettingsStore := store.NewPlanningSettingsStore(db, nil)

	planner := planning.NewSettingsBackedPlanner(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore, planningSettingsStore, 0)
	planningRunHandler := handlers.NewPlanningRunHandler(planningRunStore, candidateStore, projectStore, requirementStore, agentRunStore, planner)
	if suggester != nil {
		planningRunHandler = planningRunHandler.WithRoleSuggester(suggester)
	}

	srv := router.New(router.Deps{
		PlanningRunHandler:  planningRunHandler,
		LocalModeMiddleware: middleware.InjectLocalAdmin,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})
	return suggestFixture{applyFixture: applyFixture{
		srv:          srv,
		projectStore: projectStore,
		requirements: requirementStore,
		runs:         planningRunStore,
		candidates:   candidateStore,
	}}
}

// TestSuggestRole_NilSuggester_Returns503 verifies that the endpoint returns
// 503 when no role suggester is configured.
func TestSuggestRole_NilSuggester_Returns503(t *testing.T) {
	fx := newSuggestFixture(t, nil)
	c := fx.seedApprovedCandidate(t, "")

	req := httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/"+c.ID+"/suggest-role", nil)
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when no suggester configured, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSuggestRole_MissingCandidate_Returns404 verifies that a non-existent
// candidate ID produces a 404.
func TestSuggestRole_MissingCandidate_Returns404(t *testing.T) {
	alwaysSuccess := func(_ context.Context, _, _, _, _ string, _ *connector.AdapterCliSelection) connector.SuggestRoleResult {
		return connector.SuggestRoleResult{RoleID: "backend-engineer", Confidence: 0.9}
	}
	fx := newSuggestFixture(t, alwaysSuccess)

	req := httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/does-not-exist/suggest-role", nil)
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404 for missing candidate, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSuggestRole_LLMError_Returns200WithErrorBody verifies that a suggester
// returning an error result still produces HTTP 200 with error_kind in the
// response body (API-008: advisory LLM endpoints never use 4xx for LLM
// failures — errors are expressed in the payload).
func TestSuggestRole_LLMError_Returns200WithErrorBody(t *testing.T) {
	failSuggester := func(_ context.Context, _, _, _, _ string, _ *connector.AdapterCliSelection) connector.SuggestRoleResult {
		return connector.SuggestRoleResult{
			ErrorKind:    models.ErrorKindCliNotFound,
			ErrorMessage: "claude not found on PATH",
		}
	}
	fx := newSuggestFixture(t, failSuggester)
	c := fx.seedApprovedCandidate(t, "")

	req := httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/"+c.ID+"/suggest-role", nil)
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for LLM error (advisory endpoint), got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data connector.SuggestRoleResult `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.ErrorKind == "" {
		t.Error("want non-empty error_kind in response body for LLM failure")
	}
	if resp.Data.RoleID != "" {
		t.Errorf("want empty role_id on failure, got %q", resp.Data.RoleID)
	}
}

// TestSuggestRole_Success_Returns200WithRoleID verifies that a successful
// suggestion produces HTTP 200 with role_id, confidence, and reasoning.
func TestSuggestRole_Success_Returns200WithRoleID(t *testing.T) {
	successSuggester := func(_ context.Context, _, _, _, _ string, _ *connector.AdapterCliSelection) connector.SuggestRoleResult {
		return connector.SuggestRoleResult{
			RoleID:     "backend-engineer",
			Confidence: 0.92,
			Reasoning:  "task involves Go backend changes",
		}
	}
	fx := newSuggestFixture(t, successSuggester)
	c := fx.seedApprovedCandidate(t, "")

	req := httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/"+c.ID+"/suggest-role", nil)
	rr := httptest.NewRecorder()
	fx.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for success, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data connector.SuggestRoleResult `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.RoleID != "backend-engineer" {
		t.Errorf("want role_id 'backend-engineer', got %q", resp.Data.RoleID)
	}
	if resp.Data.ErrorKind != "" {
		t.Errorf("want empty error_kind on success, got %q", resp.Data.ErrorKind)
	}
	if resp.Data.Confidence < 0.9 {
		t.Errorf("want confidence >= 0.9, got %v", resp.Data.Confidence)
	}
}
