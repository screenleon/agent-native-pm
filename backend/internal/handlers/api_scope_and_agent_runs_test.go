package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

type scopedAPIFixture struct {
	srv              http.Handler
	documentStore    *store.DocumentStore
	projectStore     *store.ProjectStore
	requirementStore *store.RequirementStore
	projectAID       string
	projectBID       string
	globalAPIKey     string
	projectAAPIKey   string
}

func setupScopedAPIServer(t *testing.T) scopedAPIFixture {
	t.Helper()

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	requirementStore := store.NewRequirementStore(db)
	planningRunStore := store.NewPlanningRunStore(db, testutil.TestDialect())
	backlogCandidateStore := store.NewBacklogCandidateStore(db, testutil.TestDialect())
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)
	syncRunStore := store.NewSyncRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	summaryStore := store.NewSummaryStore(db, taskStore, documentStore, syncRunStore, driftSignalStore, agentRunStore)
	apiKeyStore := store.NewAPIKeyStore(db)
	userStore := store.NewUserStore(db)
	sessionStore := store.NewSessionStore(db, userStore)
	planner, err := planning.NewDeterministicPlanner(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore, models.PlanningProviderDeterministic, models.PlanningProviderModelDeterministic)
	if err != nil {
		t.Fatalf("create planner: %v", err)
	}

	projectA, err := projectStore.Create(models.CreateProjectRequest{Name: "P1"})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := projectStore.Create(models.CreateProjectRequest{Name: "P2"})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}

	globalKey, err := apiKeyStore.Create(models.CreateAPIKeyRequest{Label: "global"})
	if err != nil {
		t.Fatalf("create global api key: %v", err)
	}
	projectAID := projectA.ID
	projectScopedKey, err := apiKeyStore.Create(models.CreateAPIKeyRequest{Label: "p1", ProjectID: &projectAID})
	if err != nil {
		t.Fatalf("create project scoped api key: %v", err)
	}

	srv := router.New(router.Deps{
		ProjectHandler:            handlers.NewProjectHandler(projectStore, repoMappingStore),
		RequirementHandler:        handlers.NewRequirementHandler(requirementStore, projectStore),
		PlanningRunHandler:        handlers.NewPlanningRunHandler(planningRunStore, backlogCandidateStore, projectStore, requirementStore, agentRunStore, planner),
		TaskHandler:               handlers.NewTaskHandler(taskStore, projectStore),
		DocumentHandler:           handlers.NewDocumentHandler(documentStore, projectStore, repoMappingStore),
		SummaryHandler:            handlers.NewSummaryHandler(summaryStore, projectStore),
		ProjectRepoMappingHandler: handlers.NewProjectRepoMappingHandler(repoMappingStore, projectStore),
		AgentRunHandler:           handlers.NewAgentRunHandler(agentRunStore, projectStore),
		APIKeyHandler:             handlers.NewAPIKeyHandler(apiKeyStore),
		DocumentRefreshHandler:    handlers.NewDocumentRefreshHandler(documentStore),
		AuthMiddleware:            middleware.SessionAuth(sessionStore),
		APIKeyMiddleware:          middleware.APIKeyAuth(apiKeyStore),
	})

	return scopedAPIFixture{
		srv:              srv,
		documentStore:    documentStore,
		projectStore:     projectStore,
		requirementStore: requirementStore,
		projectAID:       projectA.ID,
		projectBID:       projectB.ID,
		globalAPIKey:     globalKey.Key,
		projectAAPIKey:   projectScopedKey.Key,
	}
}

func doJSONRequest(t *testing.T, srv http.Handler, method, path string, body any, apiKey string) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func envelopeData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	return data
}

func TestAgentRunLifecycleUpdate(t *testing.T) {
	fx := setupScopedAPIServer(t)

	createBody := map[string]any{
		"project_id":         fx.projectAID,
		"agent_name":         "agent:test",
		"action_type":        "update",
		"summary":            "started",
		"files_affected":     []string{"a.go"},
		"needs_human_review": false,
	}
	createResp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/agent-runs", createBody, fx.globalAPIKey)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create agent run: expected 201, got %d: %s", createResp.Code, createResp.Body.String())
	}
	createData := envelopeData(t, createResp)
	runID := createData["id"].(string)
	if createData["status"] != "running" {
		t.Fatalf("expected status running, got %v", createData["status"])
	}

	updateBody := map[string]any{
		"status":  "completed",
		"summary": "finished",
	}
	updateResp := doJSONRequest(t, fx.srv, http.MethodPatch, "/api/agent-runs/"+runID, updateBody, fx.globalAPIKey)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update agent run: expected 200, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	updateData := envelopeData(t, updateResp)
	if updateData["status"] != "completed" {
		t.Fatalf("expected status completed, got %v", updateData["status"])
	}
	if updateData["completed_at"] == nil {
		t.Fatalf("expected completed_at to be set")
	}
}

func TestAgentRunCreateIdempotencyReturnsExistingRun(t *testing.T) {
	fx := setupScopedAPIServer(t)

	body := map[string]any{
		"project_id":         fx.projectAID,
		"agent_name":         "agent:test",
		"action_type":        "review",
		"summary":            "started",
		"idempotency_key":    "same-key",
		"files_affected":     []string{"a.go"},
		"needs_human_review": true,
	}
	firstResp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/agent-runs", body, fx.globalAPIKey)
	if firstResp.Code != http.StatusCreated {
		t.Fatalf("first idempotent create: expected 201, got %d: %s", firstResp.Code, firstResp.Body.String())
	}
	firstID := envelopeData(t, firstResp)["id"].(string)

	secondResp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/agent-runs", body, fx.globalAPIKey)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second idempotent create: expected 200, got %d: %s", secondResp.Code, secondResp.Body.String())
	}
	secondID := envelopeData(t, secondResp)["id"].(string)
	if secondID != firstID {
		t.Fatalf("expected idempotent create to return run %s, got %s", firstID, secondID)
	}
}

func TestAgentRunCreateRequiresAPIKey(t *testing.T) {
	fx := setupScopedAPIServer(t)

	createBody := map[string]any{
		"project_id":  fx.projectAID,
		"agent_name":  "agent:test",
		"action_type": "update",
	}
	resp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/agent-runs", createBody, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without api key, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAgentRunScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)

	createBody := map[string]any{
		"project_id":  fx.projectBID,
		"agent_name":  "agent:test",
		"action_type": "update",
	}
	createResp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/agent-runs", createBody, fx.globalAPIKey)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create agent run: expected 201, got %d: %s", createResp.Code, createResp.Body.String())
	}
	runID := envelopeData(t, createResp)["id"].(string)

	getResp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/agent-runs/"+runID, nil, fx.projectAAPIKey)
	if getResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched project scope, got %d: %s", getResp.Code, getResp.Body.String())
	}
}

func TestDocumentRefreshScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)

	doc, err := fx.documentStore.Create(fx.projectBID, models.CreateDocumentRequest{
		Title:   "Doc",
		DocType: "general",
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	resp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/documents/"+doc.ID+"/refresh-summary", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched document project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDocumentPreviewScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)

	repoDir := t.TempDir()
	docPath := filepath.Join(repoDir, "docs", "secret.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write doc file: %v", err)
	}
	if _, err := fx.projectStore.Update(fx.projectBID, models.UpdateProjectRequest{RepoPath: &repoDir}); err != nil {
		t.Fatalf("update project repo_path: %v", err)
	}

	doc, err := fx.documentStore.Create(fx.projectBID, models.CreateDocumentRequest{
		Title:    "Secret",
		FilePath: "docs/secret.md",
		DocType:  "guide",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/documents/"+doc.ID+"/content", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched document preview project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRepoMappingScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)
	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/projects/"+fx.projectBID+"/repo-mappings", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched repo mapping project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRepoMappingDiscoveryScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)
	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/repo-mappings/discover?project_id="+fx.projectBID, nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched repo mapping discovery project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRequirementPlanningScopeBlocksOtherProject(t *testing.T) {
	fx := setupScopedAPIServer(t)

	requirement, err := fx.requirementStore.Create(fx.projectBID, models.CreateRequirementRequest{Title: "Secret planning"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/projects/"+fx.projectBID+"/requirements", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched requirement list project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodGet, "/api/requirements/"+requirement.ID, nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched requirement detail project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodPost, "/api/projects/"+fx.projectBID+"/requirements", map[string]any{"title": "Blocked by scope"}, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched requirement create project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodPost, "/api/requirements/"+requirement.ID+"/planning-runs", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched planning run create project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	globalRunResp := doJSONRequest(t, fx.srv, http.MethodPost, "/api/requirements/"+requirement.ID+"/planning-runs", nil, fx.globalAPIKey)
	if globalRunResp.Code != http.StatusCreated {
		t.Fatalf("create planning run with global key: expected 201, got %d: %s", globalRunResp.Code, globalRunResp.Body.String())
	}
	runID := envelopeData(t, globalRunResp)["id"].(string)

	agentRunsResp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/projects/"+fx.projectBID+"/agent-runs", nil, fx.globalAPIKey)
	if agentRunsResp.Code != http.StatusOK {
		t.Fatalf("list agent runs for planning project: expected 200, got %d: %s", agentRunsResp.Code, agentRunsResp.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(agentRunsResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode agent runs response: %v", err)
	}
	runs := payload["data"].([]any)
	if len(runs) != 1 {
		t.Fatalf("expected 1 agent run from planning flow, got %d", len(runs))
	}
	runData := runs[0].(map[string]any)
	if runData["action_type"] != "review" {
		t.Fatalf("expected planning agent action_type review, got %v", runData["action_type"])
	}
	if runData["status"] != models.AgentRunStatusCompleted {
		t.Fatalf("expected planning agent run completed, got %v", runData["status"])
	}

	resp = doJSONRequest(t, fx.srv, http.MethodGet, "/api/requirements/"+requirement.ID+"/planning-runs", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched planning run list project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodGet, "/api/planning-runs/"+runID, nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched planning run detail project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodGet, "/api/planning-runs/"+runID+"/backlog-candidates", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched backlog candidate list project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	candidateListResp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/planning-runs/"+runID+"/backlog-candidates", nil, fx.globalAPIKey)
	if candidateListResp.Code != http.StatusOK {
		t.Fatalf("list backlog candidates with global key: expected 200, got %d: %s", candidateListResp.Code, candidateListResp.Body.String())
	}
	var candidatePayload map[string]any
	if err := json.NewDecoder(candidateListResp.Body).Decode(&candidatePayload); err != nil {
		t.Fatalf("decode candidate list response: %v", err)
	}
	candidates := candidatePayload["data"].([]any)
	if len(candidates) < 3 {
		t.Fatalf("expected ranked candidates for scope test, got %d", len(candidates))
	}
	candidateID := candidates[0].(map[string]any)["id"].(string)

	resp = doJSONRequest(t, fx.srv, http.MethodPatch, "/api/backlog-candidates/"+candidateID, map[string]any{"status": "approved"}, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched backlog candidate update project scope, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, fx.srv, http.MethodPost, "/api/backlog-candidates/"+candidateID+"/apply", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched backlog candidate apply project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}
