package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func setupTestServer(t *testing.T) http.Handler {
	t.Helper()
	db := testutil.OpenTestDB(t)

	ps := store.NewProjectStore(db)
	ts := store.NewTaskStore(db)
	ds := store.NewDocumentStore(db)
	rms := store.NewProjectRepoMappingStore(db)
	ss := store.NewSummaryStore(db, ts, ds)

	return router.New(router.Deps{
		ProjectHandler:            handlers.NewProjectHandler(ps, rms),
		TaskHandler:               handlers.NewTaskHandler(ts, ps),
		DocumentHandler:           handlers.NewDocumentHandler(ds, ps, rms),
		SummaryHandler:            handlers.NewSummaryHandler(ss, ps),
		ProjectRepoMappingHandler: handlers.NewProjectRepoMappingHandler(rms, ps),
	})
}

func setupGitRepoForHandlers(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	commands := [][]string{
		{"git", "-C", repo, "init"},
		{"git", "-C", repo, "config", "user.email", "test@example.com"},
		{"git", "-C", repo, "config", "user.name", "test"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("run %v: %v (%s)", args, err, string(out))
		}
	}
}

func TestHealthCheck(t *testing.T) {
	srv := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", body["status"])
	}
}

func TestProjectCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Create
	body := `{"name":"Test Project","description":"A test","repo_url":"https://github.com/example/test.git","repo_path":"/tmp/test","default_branch":"main"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createResp models.Envelope
	json.NewDecoder(w.Body).Decode(&createResp)
	projectData := createResp.Data.(map[string]interface{})
	projectID := projectData["id"].(string)

	if projectData["name"] != "Test Project" {
		t.Fatalf("expected name 'Test Project', got '%s'", projectData["name"])
	}
	if projectData["repo_url"] != "https://github.com/example/test.git" {
		t.Fatalf("expected repo_url to round-trip, got '%v'", projectData["repo_url"])
	}

	// List
	req = httptest.NewRequest("GET", "/api/projects", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	// Get
	req = httptest.NewRequest("GET", "/api/projects/"+projectID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}

	// Update
	body = `{"name":"Updated Project"}`
	req = httptest.NewRequest("PATCH", "/api/projects/"+projectID, strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/projects/"+projectID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("delete: expected 200, got %d", w.Code)
	}

	// Verify gone
	req = httptest.NewRequest("GET", "/api/projects/"+projectID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

func TestProjectCreateWithInitialRepoMapping(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	primaryRepo := filepath.Join(mirrorRoot, "agent-native-pm")
	setupGitRepoForHandlers(t, primaryRepo)

	body := `{"name":"Mirror Project","default_branch":"main","initial_repo_mapping":{"alias":"app","repo_path":"` + strings.ReplaceAll(primaryRepo, "\\", "\\\\") + `","default_branch":"main"}}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project with initial mapping: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)
	if projectResp.Data.(map[string]interface{})["repo_path"] != primaryRepo {
		t.Fatalf("expected project repo_path to sync to initial mapping, got %v", projectResp.Data.(map[string]interface{})["repo_path"])
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/repo-mappings", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list repo mappings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	mappings := listResp.Data.([]interface{})
	if len(mappings) != 1 {
		t.Fatalf("expected 1 repo mapping, got %d", len(mappings))
	}
	if mappings[0].(map[string]interface{})["is_primary"] != true {
		t.Fatalf("expected initial mapping to be primary, got %v", mappings[0].(map[string]interface{})["is_primary"])
	}
}

func TestTaskCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Create project first
	body := `{"name":"Task Test Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var projResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projResp)
	projectID := projResp.Data.(map[string]interface{})["id"].(string)

	// Create task
	body = `{"title":"Test Task","priority":"high","source":"human"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var taskResp models.Envelope
	json.NewDecoder(w.Body).Decode(&taskResp)
	taskID := taskResp.Data.(map[string]interface{})["id"].(string)

	// Update status
	body = `{"status":"in_progress"}`
	req = httptest.NewRequest("PATCH", "/api/tasks/"+taskID, strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("update task: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List tasks
	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/tasks", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list tasks: expected 200, got %d", w.Code)
	}

	// Invalid status
	body = `{"status":"invalid"}`
	req = httptest.NewRequest("PATCH", "/api/tasks/"+taskID, strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("invalid status: expected 400, got %d", w.Code)
	}
}

func TestDocumentCRUD(t *testing.T) {
	srv := setupTestServer(t)

	repoDir := t.TempDir()
	docPath := filepath.Join(repoDir, "docs", "api.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# API\n\nThis is a test document."), 0o644); err != nil {
		t.Fatalf("write doc file: %v", err)
	}

	// Create project first
	body := `{"name":"Doc Test Project","repo_path":"` + strings.ReplaceAll(repoDir, "\\", "\\\\") + `"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var projResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projResp)
	projectID := projResp.Data.(map[string]interface{})["id"].(string)

	// Create document
	body = `{"title":"API Docs","file_path":"docs/api.md","doc_type":"api","source":"human"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/documents", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create doc: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var docResp models.Envelope
	json.NewDecoder(w.Body).Decode(&docResp)
	docID := docResp.Data.(map[string]interface{})["id"].(string)

	// List documents
	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/documents", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list docs: expected 200, got %d", w.Code)
	}

	// Preview content
	req = httptest.NewRequest("GET", "/api/documents/"+docID+"/content", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get doc content: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var contentResp models.Envelope
	json.NewDecoder(w.Body).Decode(&contentResp)
	data := contentResp.Data.(map[string]interface{})
	if !strings.Contains(data["content"].(string), "This is a test document") {
		t.Fatalf("expected document content in response, got: %v", data["content"])
	}

	// Path traversal protection
	body = `{"title":"Bad Path","file_path":"../outside.md","doc_type":"guide","source":"human"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/documents", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create bad path doc: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var badDocResp models.Envelope
	json.NewDecoder(w.Body).Decode(&badDocResp)
	badDocID := badDocResp.Data.(map[string]interface{})["id"].(string)

	req = httptest.NewRequest("GET", "/api/documents/"+badDocID+"/content", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("path traversal guard: expected 400, got %d", w.Code)
	}
}

func TestDocumentPreviewResolvesSecondaryRepoMappingByAlias(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	primaryRepo := t.TempDir()
	secondaryRepo := filepath.Join(mirrorRoot, "docs-repo")
	setupGitRepoForHandlers(t, secondaryRepo)
	secondaryDocPath := filepath.Join(secondaryRepo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(secondaryDocPath), 0o755); err != nil {
		t.Fatalf("mkdir secondary docs dir: %v", err)
	}
	if err := os.WriteFile(secondaryDocPath, []byte("# Secondary Guide\n\nMirror content."), 0o644); err != nil {
		t.Fatalf("write secondary doc file: %v", err)
	}

	body := `{"name":"Mapped Project","repo_path":"` + strings.ReplaceAll(primaryRepo, "\\", "\\\\") + `"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)

	body = `{"alias":"docs-repo","repo_path":"` + strings.ReplaceAll(secondaryRepo, "\\", "\\\\") + `","default_branch":"main"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/repo-mappings", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create repo mapping: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	body = `{"title":"Secondary Guide","file_path":"docs-repo/docs/guide.md","doc_type":"guide","source":"human"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/documents", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create mapped doc: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var docResp models.Envelope
	json.NewDecoder(w.Body).Decode(&docResp)
	docID := docResp.Data.(map[string]interface{})["id"].(string)

	req = httptest.NewRequest("GET", "/api/documents/"+docID+"/content", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get mapped doc content: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var contentResp models.Envelope
	json.NewDecoder(w.Body).Decode(&contentResp)
	data := contentResp.Data.(map[string]interface{})
	if !strings.Contains(data["content"].(string), "Mirror content") {
		t.Fatalf("expected mapped document content in response, got: %v", data["content"])
	}
	if !strings.Contains(data["path"].(string), filepath.Join("docs", "guide.md")) {
		t.Fatalf("expected resolved path to point at mapped repo file, got: %v", data["path"])
	}
}

func TestRepoMappingRejectsPathOutsideMirrorRoot(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	body := `{"name":"Mapping Validation Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)

	body = `{"alias":"docs-repo","repo_path":"/tmp/outside-root","default_branch":"main"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/repo-mappings", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for repo path outside mirror root, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeletingPrimaryRepoMappingPromotesNextMapping(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	primaryRepo := filepath.Join(mirrorRoot, "primary")
	secondaryRepo := filepath.Join(mirrorRoot, "secondary")
	setupGitRepoForHandlers(t, primaryRepo)
	setupGitRepoForHandlers(t, secondaryRepo)

	body := `{"name":"Promotion Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)

	body = `{"alias":"app","repo_path":"` + strings.ReplaceAll(primaryRepo, "\\", "\\\\") + `","default_branch":"main","is_primary":true}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/repo-mappings", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create primary mapping: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var primaryResp models.Envelope
	json.NewDecoder(w.Body).Decode(&primaryResp)
	primaryID := primaryResp.Data.(map[string]interface{})["id"].(string)

	body = `{"alias":"shared","repo_path":"` + strings.ReplaceAll(secondaryRepo, "\\", "\\\\") + `","default_branch":"main"}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/repo-mappings", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create secondary mapping: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("DELETE", "/api/repo-mappings/"+primaryID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("delete primary mapping: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/repo-mappings", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list mappings: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	mappings := listResp.Data.([]interface{})
	if len(mappings) != 1 {
		t.Fatalf("expected 1 remaining mapping, got %d", len(mappings))
	}
	remaining := mappings[0].(map[string]interface{})
	if remaining["is_primary"] != true {
		t.Fatalf("expected remaining mapping to be promoted to primary, got %v", remaining["is_primary"])
	}
}

func TestRepoMappingDiscoveryListsMountedGitRepos(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	primaryRepo := filepath.Join(mirrorRoot, "agent-native-pm")
	secondaryRepo := filepath.Join(mirrorRoot, "docs-repo")
	setupGitRepoForHandlers(t, primaryRepo)
	setupGitRepoForHandlers(t, secondaryRepo)
	if err := os.MkdirAll(filepath.Join(mirrorRoot, "not-a-repo"), 0o755); err != nil {
		t.Fatalf("mkdir non repo: %v", err)
	}

	body := `{"name":"Discovery Project","initial_repo_mapping":{"alias":"app","repo_path":"` + strings.ReplaceAll(primaryRepo, "\\", "\\\\") + `","default_branch":"main"}}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)

	req = httptest.NewRequest("GET", "/api/repo-mappings/discover?project_id="+projectID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("discover mirror repos: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var discoverResp models.Envelope
	json.NewDecoder(w.Body).Decode(&discoverResp)
	data := discoverResp.Data.(map[string]interface{})
	if data["mirror_root"] != mirrorRoot {
		t.Fatalf("expected mirror_root %s, got %v", mirrorRoot, data["mirror_root"])
	}
	repos := data["repos"].([]interface{})
	if len(repos) != 2 {
		t.Fatalf("expected 2 discovered git repos, got %d", len(repos))
	}
	first := repos[0].(map[string]interface{})
	second := repos[1].(map[string]interface{})
	if first["repo_name"] == "not-a-repo" || second["repo_name"] == "not-a-repo" {
		t.Fatalf("non git directories should not be returned: %v", repos)
	}
	var primaryFound bool
	for _, rawRepo := range repos {
		repo := rawRepo.(map[string]interface{})
		if repo["repo_path"] == primaryRepo {
			primaryFound = true
			if repo["is_primary_for_project"] != true {
				t.Fatalf("expected mapped primary repo to be marked primary, got %v", repo["is_primary_for_project"])
			}
		}
	}
	if !primaryFound {
		t.Fatalf("expected primary repo %s in discovery results", primaryRepo)
	}
}

func TestProjectSummary(t *testing.T) {
	srv := setupTestServer(t)

	// Create project
	body := `{"name":"Summary Test"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var projResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projResp)
	projectID := projResp.Data.(map[string]interface{})["id"].(string)

	// Create tasks in various states
	for _, task := range []string{
		`{"title":"T1","status":"todo"}`,
		`{"title":"T2","status":"in_progress"}`,
		`{"title":"T3","status":"done"}`,
	} {
		req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks", strings.NewReader(task))
		w = httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}

	// Get summary
	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/summary", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var summaryResp models.Envelope
	json.NewDecoder(w.Body).Decode(&summaryResp)
	summaryData := summaryResp.Data.(map[string]interface{})

	if int(summaryData["total_tasks"].(float64)) != 3 {
		t.Fatalf("expected 3 total tasks, got %v", summaryData["total_tasks"])
	}
	if int(summaryData["tasks_done"].(float64)) != 1 {
		t.Fatalf("expected 1 done task, got %v", summaryData["tasks_done"])
	}

	// Calling summary again on same day should not duplicate snapshot rows.
	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/summary", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("summary repeat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/summary/history", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("summary history: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var historyResp models.Envelope
	json.NewDecoder(w.Body).Decode(&historyResp)
	history := historyResp.Data.([]interface{})
	if len(history) != 1 {
		t.Fatalf("expected exactly 1 daily snapshot, got %d", len(history))
	}
}
