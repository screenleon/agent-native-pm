package handlers_test

// Test matrix for Phase 3-B-1 — Evidence cross-links endpoint.
// T-3B1-1 through T-3B1-7 (HTTP-layer tests; store-layer tests live in
// internal/store/backlog_candidate_store_test.go).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

var testEvidenceSelection = models.PlanningProviderSelection{
	ProviderID:      models.PlanningProviderDeterministic,
	ModelID:         models.PlanningProviderModelDeterministic,
	SelectionSource: models.PlanningSelectionSourceServerDefault,
	BindingSource:   models.PlanningBindingSourceSystem,
}

type evidenceFixture struct {
	srv       http.Handler
	projectID string
}

func newEvidenceFixture(t *testing.T) *evidenceFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	if _, err := db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('local-admin', 'local', 'local@example.com', '', 'admin', TRUE)
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	projectStore := store.NewProjectStore(db)
	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Evidence Test Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	requirementStore := store.NewRequirementStore(db)
	req, err := requirementStore.Create(project.ID, models.CreateRequirementRequest{Title: "Test requirement"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	planningRunStore := store.NewPlanningRunStore(db, dialect)
	run, err := planningRunStore.Create(project.ID, req.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, testEvidenceSelection)
	if err != nil {
		t.Fatalf("create planning run: %v", err)
	}

	candidateStore := store.NewBacklogCandidateStore(db, dialect)
	_, err = candidateStore.CreateDraftsForPlanningRun(req, run.ID, []models.BacklogCandidateDraft{
		{
			SuggestionType: "implementation",
			Title:          "Candidate with evidence",
			PriorityScore:  80,
			Confidence:     70,
			Rank:           1,
			EvidenceDetail: models.PlanningEvidenceDetail{
				Documents: []models.PlanningDocumentEvidence{{
					DocumentID: "doc-evidence-1",
					Title:      "Guide",
				}},
				DriftSignals: []models.PlanningDriftSignalEvidence{{
					DriftSignalID: "ds-evidence-1",
					Severity:      2,
				}},
			},
		},
		{
			SuggestionType: "validation",
			Title:          "Candidate without evidence",
			PriorityScore:  60,
			Confidence:     60,
			Rank:           2,
			EvidenceDetail: models.PlanningEvidenceDetail{},
		},
	})
	if err != nil {
		t.Fatalf("create candidates: %v", err)
	}

	agentRunStore := store.NewAgentRunStore(db)
	planningRunHandler := handlers.NewPlanningRunHandler(
		planningRunStore, candidateStore, projectStore, requirementStore, agentRunStore, stubPlanner{},
	)

	srv := router.New(router.Deps{
		PlanningRunHandler:  planningRunHandler,
		LocalModeMiddleware: middleware.InjectLocalAdmin,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})

	return &evidenceFixture{srv: srv, projectID: project.ID}
}

// T-3B1-1: GET by-evidence?document_id=X returns candidates referencing that doc.
func Test3B1_1ByDocumentReturnsMatch(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/"+f.projectID+"/backlog-candidates/by-evidence?document_id=doc-evidence-1", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []models.CandidateEvidenceSummary `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("want 1 result, got %d", len(resp.Data))
	}
	if resp.Data[0].Title != "Candidate with evidence" {
		t.Fatalf("unexpected title: %s", resp.Data[0].Title)
	}
}

// T-3B1-2: GET by-evidence?drift_signal_id=Y returns candidates referencing that drift signal.
func Test3B1_2ByDriftSignalReturnsMatch(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/"+f.projectID+"/backlog-candidates/by-evidence?drift_signal_id=ds-evidence-1", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []models.CandidateEvidenceSummary `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("want 1 result for ds-evidence-1, got %d", len(resp.Data))
	}
}

// T-3B1-3: Document referenced by zero candidates → empty list.
func Test3B1_3NoMatchReturnsEmpty(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/"+f.projectID+"/backlog-candidates/by-evidence?document_id=doc-never-referenced", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []models.CandidateEvidenceSummary `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("want 0 results, got %d", len(resp.Data))
	}
}

// T-3B1-5: Both document_id and drift_signal_id → 400.
func Test3B1_5BothParamsReturns400(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/"+f.projectID+"/backlog-candidates/by-evidence?document_id=x&drift_signal_id=y", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// T-3B1-6: Neither param → 400.
func Test3B1_6NeitherParamReturns400(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/"+f.projectID+"/backlog-candidates/by-evidence", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// T-3B1-7: Wrong project ID → 404 (project not found).
func Test3B1_7WrongProjectReturnsNotFound(t *testing.T) {
	f := newEvidenceFixture(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/projects/wrong-project-id/backlog-candidates/by-evidence?document_id=doc-evidence-1", nil)
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}
