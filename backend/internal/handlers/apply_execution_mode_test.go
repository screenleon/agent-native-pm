package handlers_test

// Phase 5 B3: backlog-candidate apply now accepts an optional
// `execution_mode` body field. These tests pin the wire contract.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

type applyFixture struct {
	srv           http.Handler
	projectStore  *store.ProjectStore
	requirements  *store.RequirementStore
	runs          *store.PlanningRunStore
	candidates    *store.BacklogCandidateStore
}

func newApplyFixture(t *testing.T) applyFixture {
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

	srv := router.New(router.Deps{
		PlanningRunHandler:  planningRunHandler,
		LocalModeMiddleware: middleware.InjectLocalAdmin,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})
	return applyFixture{
		srv:           srv,
		projectStore:  projectStore,
		requirements:  requirementStore,
		runs:          planningRunStore,
		candidates:    candidateStore,
	}
}

func (fx applyFixture) seedApprovedCandidate(t *testing.T, executionRole string) *models.BacklogCandidate {
	t.Helper()
	project, err := fx.projectStore.Create(models.CreateProjectRequest{Name: "ApplyProj"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	req, err := fx.requirements.Create(project.ID, models.CreateRequirementRequest{Title: "Requirement", Source: "human"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	run, err := fx.runs.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{
		TriggerSource: "manual",
		ExecutionMode: "deterministic",
	}, models.PlanningProviderSelection{
		ProviderID:      "deterministic",
		ModelID:         "deterministic",
		SelectionSource: "server_default",
	})
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}
	drafts, err := fx.candidates.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{
			Title:         "Apply me",
			Description:   "test",
			Rationale:     "test",
			Rank:          1,
			PriorityScore: 10,
			Confidence:    10,
			ExecutionRole: executionRole,
		},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	approved := "approved"
	updated, err := fx.candidates.Update(drafts[0].ID, models.UpdateBacklogCandidateRequest{Status: &approved})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	return updated
}

func (fx applyFixture) apply(t *testing.T, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	}
	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/"+id+"/apply", bodyReader)
	} else {
		req = httptest.NewRequest(http.MethodPost, "/api/backlog-candidates/"+id+"/apply", nil)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fx.srv.ServeHTTP(w, req)
	return w
}

// T-P5-B3-1: apply with NO body continues to work (back-compat).
func TestApplyBacklogCandidate_NoBody_PreservesManualSource(t *testing.T) {
	fx := newApplyFixture(t)
	candidate := fx.seedApprovedCandidate(t, "")
	w := fx.apply(t, candidate.ID, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Task struct {
				Source string `json:"source"`
			} `json:"task"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Task.Source == "" || strings.HasPrefix(resp.Data.Task.Source, "role_dispatch") {
		t.Fatalf("expected manual source, got %q", resp.Data.Task.Source)
	}
}

// T-P5-B3-2: explicit manual matches no-body behaviour.
func TestApplyBacklogCandidate_ExplicitManual(t *testing.T) {
	fx := newApplyFixture(t)
	candidate := fx.seedApprovedCandidate(t, "ui-scaffolder")
	w := fx.apply(t, candidate.ID, map[string]string{"execution_mode": "manual"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Task struct {
				Source string `json:"source"`
			} `json:"task"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if strings.HasPrefix(resp.Data.Task.Source, "role_dispatch") {
		t.Fatalf("manual must not produce role_dispatch source, got %q", resp.Data.Task.Source)
	}
}

// T-P5-B3-3: role_dispatch + role on candidate → task.source carries the role.
func TestApplyBacklogCandidate_RoleDispatchWithRole(t *testing.T) {
	fx := newApplyFixture(t)
	candidate := fx.seedApprovedCandidate(t, "backend-architect")
	w := fx.apply(t, candidate.ID, map[string]string{"execution_mode": "role_dispatch"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Task struct {
				Source string `json:"source"`
			} `json:"task"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	want := "role_dispatch:backend-architect"
	if resp.Data.Task.Source != want {
		t.Fatalf("expected task source %q, got %q", want, resp.Data.Task.Source)
	}
}

// T-P5-B3-4: role_dispatch + no role set on candidate → plain "role_dispatch" source.
func TestApplyBacklogCandidate_RoleDispatchWithoutRole(t *testing.T) {
	fx := newApplyFixture(t)
	candidate := fx.seedApprovedCandidate(t, "")
	w := fx.apply(t, candidate.ID, map[string]string{"execution_mode": "role_dispatch"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Task struct {
				Source string `json:"source"`
			} `json:"task"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Task.Source != "role_dispatch" {
		t.Fatalf("expected bare 'role_dispatch', got %q", resp.Data.Task.Source)
	}
}

// T-P5-B3-5: unknown execution_mode → 400 (validates the enum).
func TestApplyBacklogCandidate_InvalidExecutionMode(t *testing.T) {
	fx := newApplyFixture(t)
	candidate := fx.seedApprovedCandidate(t, "")
	w := fx.apply(t, candidate.ID, map[string]string{"execution_mode": "not-a-mode"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
