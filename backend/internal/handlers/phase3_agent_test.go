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
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

type phase3Fixture struct {
	srv            http.Handler
	documentStore  *store.DocumentStore
	projectStore   *store.ProjectStore
	projectAID     string
	projectBID     string
	globalAPIKey   string
	projectAAPIKey string
}

func setupPhase3Server(t *testing.T) phase3Fixture {
	t.Helper()

	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)
	summaryStore := store.NewSummaryStore(db, taskStore, documentStore)
	agentRunStore := store.NewAgentRunStore(db)
	apiKeyStore := store.NewAPIKeyStore(db)
	userStore := store.NewUserStore(db)
	sessionStore := store.NewSessionStore(db, userStore)

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

	return phase3Fixture{
		srv:            srv,
		documentStore:  documentStore,
		projectStore:   projectStore,
		projectAID:     projectA.ID,
		projectBID:     projectB.ID,
		globalAPIKey:   globalKey.Key,
		projectAAPIKey: projectScopedKey.Key,
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
	fx := setupPhase3Server(t)

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

func TestAgentRunCreateRequiresAPIKey(t *testing.T) {
	fx := setupPhase3Server(t)

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
	fx := setupPhase3Server(t)

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
	fx := setupPhase3Server(t)

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
	fx := setupPhase3Server(t)

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
	fx := setupPhase3Server(t)
	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/projects/"+fx.projectBID+"/repo-mappings", nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched repo mapping project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRepoMappingDiscoveryScopeBlocksOtherProject(t *testing.T) {
	fx := setupPhase3Server(t)
	resp := doJSONRequest(t, fx.srv, http.MethodGet, "/api/repo-mappings/discover?project_id="+fx.projectBID, nil, fx.projectAAPIKey)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched repo mapping discovery project scope, got %d: %s", resp.Code, resp.Body.String())
	}
}
