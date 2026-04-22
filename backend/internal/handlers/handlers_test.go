package handlers_test

import (
	"encoding/json"
	"fmt"
	"github.com/screenleon/agent-native-pm/internal/planning"
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

var handlerTestPlanningSelection = models.PlanningProviderSelection{
	ProviderID:      models.PlanningProviderDeterministic,
	ModelID:         models.PlanningProviderModelDeterministic,
	SelectionSource: models.PlanningSelectionSourceServerDefault,
}

func setupTestServer(t *testing.T) http.Handler {
	t.Helper()
	db := testutil.OpenTestDB(t)

	ps := store.NewProjectStore(db)
	rs := store.NewRequirementStore(db)
	prs := store.NewPlanningRunStore(db, testutil.TestDialect())
	bcs := store.NewBacklogCandidateStore(db, testutil.TestDialect())
	ts := store.NewTaskStore(db)
	ds := store.NewDocumentStore(db)
	srs := store.NewSyncRunStore(db)
	drs := store.NewDriftSignalStore(db)
	ars := store.NewAgentRunStore(db)
	rms := store.NewProjectRepoMappingStore(db)
	ss := store.NewSummaryStore(db, ts, ds, srs, drs, ars)
	planner, err := planning.NewDeterministicPlanner(ts, ds, drs, srs, ars, models.PlanningProviderDeterministic, models.PlanningProviderModelDeterministic)
	if err != nil {
		t.Fatalf("create planner: %v", err)
	}

	return router.New(router.Deps{
		ProjectHandler:            handlers.NewProjectHandler(ps, rms),
		RequirementHandler:        handlers.NewRequirementHandler(rs, ps),
		PlanningRunHandler:        handlers.NewPlanningRunHandler(prs, bcs, ps, rs, ars, planner),
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

func TestTaskListFilters(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"name":"Task Filter Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projResp)
	projectID := projResp.Data.(map[string]interface{})["id"].(string)

	for _, taskBody := range []string{
		`{"title":"Alpha","status":"todo","priority":"high","assignee":"agent:application-implementer","source":"human"}`,
		`{"title":"Bravo","status":"done","priority":"high","assignee":"agent:application-implementer","source":"human"}`,
		`{"title":"Charlie","status":"todo","priority":"low","assignee":"human:pm","source":"human"}`,
	} {
		req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks", strings.NewReader(taskBody))
		w = httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create filtered task: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/tasks?status=todo&priority=high&assignee=agent:application-implementer", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered list tasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	tasks := listResp.Data.([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 filtered task, got %d", len(tasks))
	}
	if tasks[0].(map[string]interface{})["title"] != "Alpha" {
		t.Fatalf("expected Alpha task, got %v", tasks[0].(map[string]interface{})["title"])
	}
	meta := listResp.Meta.(map[string]interface{})
	if meta["total"].(float64) != 1 {
		t.Fatalf("expected filtered total 1, got %v", meta["total"])
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/tasks?status=blocked", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status filter: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/tasks?priority=urgent", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid priority filter: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskBatchUpdate(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"name":"Task Batch Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projResp)
	projectID := projResp.Data.(map[string]interface{})["id"].(string)

	var taskIDs []string
	for _, taskBody := range []string{
		`{"title":"Alpha","status":"todo","priority":"low","assignee":"agent:a"}`,
		`{"title":"Bravo","status":"todo","priority":"low","assignee":"agent:b"}`,
		`{"title":"Charlie","status":"todo","priority":"medium","assignee":"agent:c"}`,
	} {
		req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks", strings.NewReader(taskBody))
		w = httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var taskResp models.Envelope
		json.NewDecoder(w.Body).Decode(&taskResp)
		taskIDs = append(taskIDs, taskResp.Data.(map[string]interface{})["id"].(string))
	}

	body = fmt.Sprintf(`{"task_ids":[%q,%q],"changes":{"status":"done","priority":"high","assignee":""}}`, taskIDs[0], taskIDs[1])
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks/batch-update", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var batchResp models.Envelope
	json.NewDecoder(w.Body).Decode(&batchResp)
	data := batchResp.Data.(map[string]interface{})
	if int(data["updated_count"].(float64)) != 2 {
		t.Fatalf("expected updated_count 2, got %v", data["updated_count"])
	}
	tasks := data["tasks"].([]interface{})
	if len(tasks) != 2 {
		t.Fatalf("expected 2 updated tasks, got %d", len(tasks))
	}
	for _, rawTask := range tasks {
		task := rawTask.(map[string]interface{})
		if task["status"] != "done" {
			t.Fatalf("expected updated status done, got %v", task["status"])
		}
		if task["priority"] != "high" {
			t.Fatalf("expected updated priority high, got %v", task["priority"])
		}
		if task["assignee"] != "" {
			t.Fatalf("expected cleared assignee, got %v", task["assignee"])
		}
	}

	req = httptest.NewRequest("GET", "/api/tasks/"+taskIDs[2], nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get untouched task: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var untouchedResp models.Envelope
	json.NewDecoder(w.Body).Decode(&untouchedResp)
	untouched := untouchedResp.Data.(map[string]interface{})
	if untouched["status"] != "todo" {
		t.Fatalf("expected untouched task to remain todo, got %v", untouched["status"])
	}

	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks/batch-update", strings.NewReader(`{"task_ids":[],"changes":{"status":"done"}}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty task ids: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks/batch-update", strings.NewReader(fmt.Sprintf(`{"task_ids":[%q],"changes":{"status":"blocked"}}`, taskIDs[0])))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid batch status: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskBatchUpdateRollsBackWhenTaskMissingFromProject(t *testing.T) {
	srv := setupTestServer(t)

	createProject := func(name string) string {
		req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(fmt.Sprintf(`{"name":%q}`, name)))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create project %s: expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
		var resp models.Envelope
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Data.(map[string]interface{})["id"].(string)
	}

	createTask := func(projectID, title string) string {
		req := httptest.NewRequest("POST", "/api/projects/"+projectID+"/tasks", strings.NewReader(fmt.Sprintf(`{"title":%q,"status":"todo","priority":"low","assignee":"agent:x"}`, title)))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create task %s: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var resp models.Envelope
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Data.(map[string]interface{})["id"].(string)
	}

	projectAID := createProject("Project A")
	projectBID := createProject("Project B")
	taskAID := createTask(projectAID, "Alpha")
	taskBID := createTask(projectBID, "Bravo")

	body := fmt.Sprintf(`{"task_ids":[%q,%q],"changes":{"status":"done"}}`, taskAID, taskBID)
	req := httptest.NewRequest("POST", "/api/projects/"+projectAID+"/tasks/batch-update", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project batch update: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/tasks/"+taskAID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get original task after rollback: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var taskResp models.Envelope
	json.NewDecoder(w.Body).Decode(&taskResp)
	task := taskResp.Data.(map[string]interface{})
	if task["status"] != "todo" {
		t.Fatalf("expected rollback to preserve todo status, got %v", task["status"])
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

func TestRequirementCRUD(t *testing.T) {
	srv := setupTestServer(t)

	projectReq := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"Planning Project"}`))
	projectResp := httptest.NewRecorder()
	srv.ServeHTTP(projectResp, projectReq)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", projectResp.Code, projectResp.Body.String())
	}

	var projectEnvelope models.Envelope
	json.NewDecoder(projectResp.Body).Decode(&projectEnvelope)
	projectID := projectEnvelope.Data.(map[string]interface{})["id"].(string)

	body := `{"title":"Planning intake foundation","summary":"Add requirement intake","description":"Users can capture draft requirements before tasks exist","source":"agent:application-implementer"}`
	req := httptest.NewRequest("POST", "/api/projects/"+projectID+"/requirements", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create requirement: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createResp models.Envelope
	json.NewDecoder(w.Body).Decode(&createResp)
	requirementData := createResp.Data.(map[string]interface{})
	requirementID := requirementData["id"].(string)
	if requirementData["status"] != models.RequirementStatusDraft {
		t.Fatalf("expected draft status, got %v", requirementData["status"])
	}
	if requirementData["source"] != "agent:application-implementer" {
		t.Fatalf("expected source to round-trip, got %v", requirementData["source"])
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/requirements", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list requirements: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	requirements := listResp.Data.([]interface{})
	if len(requirements) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(requirements))
	}
	if requirements[0].(map[string]interface{})["id"] != requirementID {
		t.Fatalf("expected listed requirement id %s, got %v", requirementID, requirements[0].(map[string]interface{})["id"])
	}
	meta := listResp.Meta.(map[string]interface{})
	if int(meta["total"].(float64)) != 1 {
		t.Fatalf("expected total 1, got %v", meta["total"])
	}

	req = httptest.NewRequest("GET", "/api/requirements/"+requirementID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get requirement: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var detailResp models.Envelope
	json.NewDecoder(w.Body).Decode(&detailResp)
	if detailResp.Data.(map[string]interface{})["title"] != "Planning intake foundation" {
		t.Fatalf("expected detail title to round-trip, got %v", detailResp.Data.(map[string]interface{})["title"])
	}
	if detailResp.Data.(map[string]interface{})["project_id"] != projectID {
		t.Fatalf("expected project id %s, got %v", projectID, detailResp.Data.(map[string]interface{})["project_id"])
	}
	if detailResp.Data.(map[string]interface{})["summary"] != "Add requirement intake" {
		t.Fatalf("expected summary to round-trip, got %v", detailResp.Data.(map[string]interface{})["summary"])
	}
	if detailResp.Data.(map[string]interface{})["description"] != "Users can capture draft requirements before tasks exist" {
		t.Fatalf("expected description to round-trip, got %v", detailResp.Data.(map[string]interface{})["description"])
	}
	if detailResp.Data.(map[string]interface{})["id"] != requirementID {
		t.Fatalf("expected requirement id %s, got %v", requirementID, detailResp.Data.(map[string]interface{})["id"])
	}
}

func TestRequirementCreateValidation(t *testing.T) {
	srv := setupTestServer(t)

	listReq := httptest.NewRequest("GET", "/api/projects/missing/requirements", nil)
	listResp := httptest.NewRecorder()
	srv.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusNotFound {
		t.Fatalf("list requirements missing project: expected 404, got %d: %s", listResp.Code, listResp.Body.String())
	}

	req := httptest.NewRequest("POST", "/api/projects/missing/requirements", strings.NewReader(`{"title":"Missing project"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("create requirement missing project: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	projectReq := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"Requirement Validation"}`))
	projectResp := httptest.NewRecorder()
	srv.ServeHTTP(projectResp, projectReq)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", projectResp.Code, projectResp.Body.String())
	}

	var projectEnvelope models.Envelope
	json.NewDecoder(projectResp.Body).Decode(&projectEnvelope)
	projectID := projectEnvelope.Data.(map[string]interface{})["id"].(string)

	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/requirements", strings.NewReader(`{"title":"   "}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create requirement empty title: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/requirements", strings.NewReader(`{"title":`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create requirement invalid json: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequirementListIsProjectScopedAndDefaultsSource(t *testing.T) {
	srv := setupTestServer(t)

	createProject := func(name string) string {
		req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"`+name+`"}`))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create project %s: expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
		var resp models.Envelope
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Data.(map[string]interface{})["id"].(string)
	}

	projectAID := createProject("Project A")
	projectBID := createProject("Project B")

	for _, item := range []struct {
		projectID string
		title     string
	}{
		{projectID: projectAID, title: "Requirement A"},
		{projectID: projectBID, title: "Requirement B"},
	} {
		req := httptest.NewRequest("POST", "/api/projects/"+item.projectID+"/requirements", strings.NewReader(`{"title":"`+item.title+`"}`))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create requirement %s: expected 201, got %d: %s", item.title, w.Code, w.Body.String())
		}
		var resp models.Envelope
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Data.(map[string]interface{})["source"] != "human" {
			t.Fatalf("expected default source human, got %v", resp.Data.(map[string]interface{})["source"])
		}
	}

	req := httptest.NewRequest("GET", "/api/projects/"+projectAID+"/requirements", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list requirements for project A: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	requirements := listResp.Data.([]interface{})
	if len(requirements) != 1 {
		t.Fatalf("expected 1 project-scoped requirement, got %d", len(requirements))
	}
	if requirements[0].(map[string]interface{})["title"] != "Requirement A" {
		t.Fatalf("expected Requirement A, got %v", requirements[0].(map[string]interface{})["title"])
	}
}

func TestRequirementGetMissing(t *testing.T) {
	srv := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/requirements/missing", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get missing requirement: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlanningRunLifecycle(t *testing.T) {
	srv := setupTestServer(t)

	projectReq := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"Planning Run Project"}`))
	projectResp := httptest.NewRecorder()
	srv.ServeHTTP(projectResp, projectReq)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", projectResp.Code, projectResp.Body.String())
	}

	var projectEnvelope models.Envelope
	json.NewDecoder(projectResp.Body).Decode(&projectEnvelope)
	projectID := projectEnvelope.Data.(map[string]interface{})["id"].(string)

	requirementReq := httptest.NewRequest("POST", "/api/projects/"+projectID+"/requirements", strings.NewReader(`{"title":"Run planner"}`))
	requirementResp := httptest.NewRecorder()
	srv.ServeHTTP(requirementResp, requirementReq)
	if requirementResp.Code != http.StatusCreated {
		t.Fatalf("create requirement: expected 201, got %d: %s", requirementResp.Code, requirementResp.Body.String())
	}

	var requirementEnvelope models.Envelope
	json.NewDecoder(requirementResp.Body).Decode(&requirementEnvelope)
	requirementID := requirementEnvelope.Data.(map[string]interface{})["id"].(string)

	runReq := httptest.NewRequest("POST", "/api/requirements/"+requirementID+"/planning-runs", nil)
	runResp := httptest.NewRecorder()
	srv.ServeHTTP(runResp, runReq)
	if runResp.Code != http.StatusCreated {
		t.Fatalf("create planning run: expected 201, got %d: %s", runResp.Code, runResp.Body.String())
	}

	var runEnvelope models.Envelope
	json.NewDecoder(runResp.Body).Decode(&runEnvelope)
	runData := runEnvelope.Data.(map[string]interface{})
	runID := runData["id"].(string)
	if runData["status"] != models.PlanningRunStatusCompleted {
		t.Fatalf("expected completed planning run, got %v", runData["status"])
	}
	if runData["requirement_id"] != requirementID {
		t.Fatalf("expected requirement id %s, got %v", requirementID, runData["requirement_id"])
	}
	if runData["project_id"] != projectID {
		t.Fatalf("expected project id %s, got %v", projectID, runData["project_id"])
	}
	if runData["started_at"] == nil || runData["completed_at"] == nil {
		t.Fatalf("expected started_at and completed_at, got %v", runData)
	}

	listReq := httptest.NewRequest("GET", "/api/requirements/"+requirementID+"/planning-runs", nil)
	listResp := httptest.NewRecorder()
	srv.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list planning runs: expected 200, got %d: %s", listResp.Code, listResp.Body.String())
	}

	var listEnvelope models.Envelope
	json.NewDecoder(listResp.Body).Decode(&listEnvelope)
	runs := listEnvelope.Data.([]interface{})
	if len(runs) != 1 {
		t.Fatalf("expected 1 planning run, got %d", len(runs))
	}
	if runs[0].(map[string]interface{})["id"] != runID {
		t.Fatalf("expected listed run id %s, got %v", runID, runs[0].(map[string]interface{})["id"])
	}

	detailReq := httptest.NewRequest("GET", "/api/planning-runs/"+runID, nil)
	detailResp := httptest.NewRecorder()
	srv.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("get planning run: expected 200, got %d: %s", detailResp.Code, detailResp.Body.String())
	}

	var detailEnvelope models.Envelope
	json.NewDecoder(detailResp.Body).Decode(&detailEnvelope)
	if detailEnvelope.Data.(map[string]interface{})["id"] != runID {
		t.Fatalf("expected detail run id %s, got %v", runID, detailEnvelope.Data.(map[string]interface{})["id"])
	}

	candidatesReq := httptest.NewRequest("GET", "/api/planning-runs/"+runID+"/backlog-candidates", nil)
	candidatesResp := httptest.NewRecorder()
	srv.ServeHTTP(candidatesResp, candidatesReq)
	if candidatesResp.Code != http.StatusOK {
		t.Fatalf("list backlog candidates: expected 200, got %d: %s", candidatesResp.Code, candidatesResp.Body.String())
	}

	var candidatesEnvelope models.Envelope
	json.NewDecoder(candidatesResp.Body).Decode(&candidatesEnvelope)
	candidates := candidatesEnvelope.Data.([]interface{})
	if len(candidates) < 3 {
		t.Fatalf("expected at least 3 backlog candidates, got %d", len(candidates))
	}
	candidateData := candidates[0].(map[string]interface{})
	if candidateData["planning_run_id"] != runID {
		t.Fatalf("expected candidate planning_run_id %s, got %v", runID, candidateData["planning_run_id"])
	}
	if candidateData["requirement_id"] != requirementID {
		t.Fatalf("expected candidate requirement_id %s, got %v", requirementID, candidateData["requirement_id"])
	}
	if candidateData["status"] != models.BacklogCandidateStatusDraft {
		t.Fatalf("expected candidate status %s, got %v", models.BacklogCandidateStatusDraft, candidateData["status"])
	}
	if candidateData["rank"].(float64) != 1 {
		t.Fatalf("expected top-ranked candidate to have rank 1, got %v", candidateData["rank"])
	}
	if candidateData["priority_score"].(float64) <= 0 {
		t.Fatalf("expected positive priority score, got %v", candidateData["priority_score"])
	}
	if candidateData["confidence"].(float64) <= 0 {
		t.Fatalf("expected positive confidence, got %v", candidateData["confidence"])
	}

	updateReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateData["id"].(string), strings.NewReader(`{"title":"Reviewed candidate","description":"Updated candidate copy","status":"approved"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	srv.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update backlog candidate: expected 200, got %d: %s", updateResp.Code, updateResp.Body.String())
	}

	var updateEnvelope models.Envelope
	json.NewDecoder(updateResp.Body).Decode(&updateEnvelope)
	updatedCandidate := updateEnvelope.Data.(map[string]interface{})
	if updatedCandidate["title"] != "Reviewed candidate" {
		t.Fatalf("expected updated candidate title, got %v", updatedCandidate["title"])
	}
	if updatedCandidate["status"] != models.BacklogCandidateStatusApproved {
		t.Fatalf("expected approved candidate status, got %v", updatedCandidate["status"])
	}

	noChangeReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateData["id"].(string), strings.NewReader(`{"status":"approved"}`))
	noChangeReq.Header.Set("Content-Type", "application/json")
	noChangeResp := httptest.NewRecorder()
	srv.ServeHTTP(noChangeResp, noChangeReq)
	if noChangeResp.Code != http.StatusBadRequest {
		t.Fatalf("update backlog candidate without changes: expected 400, got %d: %s", noChangeResp.Code, noChangeResp.Body.String())
	}

	blankTitleReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateData["id"].(string), strings.NewReader(`{"title":"   "}`))
	blankTitleReq.Header.Set("Content-Type", "application/json")
	blankTitleResp := httptest.NewRecorder()
	srv.ServeHTTP(blankTitleResp, blankTitleReq)
	if blankTitleResp.Code != http.StatusBadRequest {
		t.Fatalf("update backlog candidate blank title: expected 400, got %d: %s", blankTitleResp.Code, blankTitleResp.Body.String())
	}

	invalidStatusReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateData["id"].(string), strings.NewReader(`{"status":"applied"}`))
	invalidStatusReq.Header.Set("Content-Type", "application/json")
	invalidStatusResp := httptest.NewRecorder()
	srv.ServeHTTP(invalidStatusResp, invalidStatusReq)
	if invalidStatusResp.Code != http.StatusBadRequest {
		t.Fatalf("update backlog candidate invalid status: expected 400, got %d: %s", invalidStatusResp.Code, invalidStatusResp.Body.String())
	}

	invalidJSONReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateData["id"].(string), strings.NewReader(`{"status":`))
	invalidJSONReq.Header.Set("Content-Type", "application/json")
	invalidJSONResp := httptest.NewRecorder()
	srv.ServeHTTP(invalidJSONResp, invalidJSONReq)
	if invalidJSONResp.Code != http.StatusBadRequest {
		t.Fatalf("update backlog candidate invalid json: expected 400, got %d: %s", invalidJSONResp.Code, invalidJSONResp.Body.String())
	}
}

func TestPlanningRunValidationAndConflict(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ps := store.NewProjectStore(db)
	rs := store.NewRequirementStore(db)
	prs := store.NewPlanningRunStore(db, testutil.TestDialect())
	bcs := store.NewBacklogCandidateStore(db, testutil.TestDialect())
	ts := store.NewTaskStore(db)
	ds := store.NewDocumentStore(db)
	srs := store.NewSyncRunStore(db)
	drs := store.NewDriftSignalStore(db)
	ars := store.NewAgentRunStore(db)
	rms := store.NewProjectRepoMappingStore(db)
	ss := store.NewSummaryStore(db, ts, ds, srs, drs, ars)
	planner, err := planning.NewDeterministicPlanner(ts, ds, drs, srs, ars, models.PlanningProviderDeterministic, models.PlanningProviderModelDeterministic)
	if err != nil {
		t.Fatalf("create planner: %v", err)
	}

	srv := router.New(router.Deps{
		ProjectHandler:            handlers.NewProjectHandler(ps, rms),
		RequirementHandler:        handlers.NewRequirementHandler(rs, ps),
		PlanningRunHandler:        handlers.NewPlanningRunHandler(prs, bcs, ps, rs, ars, planner),
		TaskHandler:               handlers.NewTaskHandler(ts, ps),
		DocumentHandler:           handlers.NewDocumentHandler(ds, ps, rms),
		SummaryHandler:            handlers.NewSummaryHandler(ss, ps),
		ProjectRepoMappingHandler: handlers.NewProjectRepoMappingHandler(rms, ps),
	})

	req := httptest.NewRequest("POST", "/api/requirements/missing/planning-runs", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("create planning run missing requirement: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	project, err := ps.Create(models.CreateProjectRequest{Name: "Conflict Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	requirement, err := rs.Create(project.ID, models.CreateRequirementRequest{Title: "Conflict Requirement"})
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	activeRun, err := prs.Create(project.ID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, handlerTestPlanningSelection)
	if err != nil {
		t.Fatalf("seed active planning run: %v", err)
	}

	req = httptest.NewRequest("POST", "/api/requirements/"+requirement.ID+"/planning-runs", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("create planning run with active run: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/requirements/missing/planning-runs", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("list planning runs missing requirement: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/planning-runs/missing", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get planning run missing: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/planning-runs/missing/backlog-candidates", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("list backlog candidates missing run: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("PATCH", "/api/backlog-candidates/missing", strings.NewReader(`{"status":"approved"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing backlog candidate: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/backlog-candidates/missing/apply", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("apply missing backlog candidate: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	if err := prs.Fail(activeRun.ID, "release requirement for applied-candidate validation"); err != nil {
		t.Fatalf("fail active planning run seed: %v", err)
	}

	completedRun, err := prs.Create(project.ID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, handlerTestPlanningSelection)
	if err != nil {
		t.Fatalf("create completed planning run seed: %v", err)
	}
	if err := prs.Complete(completedRun.ID); err != nil {
		t.Fatalf("complete planning run seed: %v", err)
	}
	seededCandidates, err := bcs.CreateDraftsForPlanningRun(requirement, completedRun.ID, []models.BacklogCandidateDraft{{Title: requirement.Title, Description: "Primary candidate", Rationale: "Primary", PriorityScore: 82, Confidence: 78, Rank: 1}})
	if err != nil {
		t.Fatalf("seed backlog candidate: %v", err)
	}
	if _, err := db.Exec(`UPDATE backlog_candidates SET status = $1 WHERE id = $2`, models.BacklogCandidateStatusApplied, seededCandidates[0].ID); err != nil {
		t.Fatalf("mark seeded candidate applied: %v", err)
	}

	req = httptest.NewRequest("PATCH", "/api/backlog-candidates/"+seededCandidates[0].ID, strings.NewReader(`{"title":"should fail"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update applied backlog candidate: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	draftRun, err := prs.Create(project.ID, requirement.ID, "", models.CreatePlanningRunRequest{TriggerSource: "manual"}, handlerTestPlanningSelection)
	if err != nil {
		t.Fatalf("create draft planning run seed: %v", err)
	}
	if err := prs.Complete(draftRun.ID); err != nil {
		t.Fatalf("complete draft planning run seed: %v", err)
	}
	draftCandidates, err := bcs.CreateDraftsForPlanningRun(requirement, draftRun.ID, []models.BacklogCandidateDraft{{Title: requirement.Title, Description: "Primary candidate", Rationale: "Primary", PriorityScore: 82, Confidence: 78, Rank: 1}})
	if err != nil {
		t.Fatalf("seed draft candidate: %v", err)
	}

	req = httptest.NewRequest("POST", "/api/backlog-candidates/"+draftCandidates[0].ID+"/apply", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("apply draft backlog candidate: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	approvedTitle := "Approved conflict candidate"
	approvedStatus := models.BacklogCandidateStatusApproved
	if _, err := bcs.Update(draftCandidates[0].ID, models.UpdateBacklogCandidateRequest{Title: &approvedTitle, Status: &approvedStatus}); err != nil {
		t.Fatalf("approve conflict candidate: %v", err)
	}
	if _, err := ts.Create(project.ID, models.CreateTaskRequest{Title: approvedTitle, Status: "todo", Priority: "medium", Source: "human"}); err != nil {
		t.Fatalf("seed duplicate task: %v", err)
	}

	req = httptest.NewRequest("POST", "/api/backlog-candidates/"+draftCandidates[0].ID+"/apply", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("apply duplicate backlog candidate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacklogCandidateApplyCreatesTaskAndReplaysIdempotently(t *testing.T) {
	srv := setupTestServer(t)

	projectReq := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"Apply Planning Project"}`))
	projectResp := httptest.NewRecorder()
	srv.ServeHTTP(projectResp, projectReq)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", projectResp.Code, projectResp.Body.String())
	}

	var projectEnvelope models.Envelope
	json.NewDecoder(projectResp.Body).Decode(&projectEnvelope)
	projectID := projectEnvelope.Data.(map[string]interface{})["id"].(string)

	requirementReq := httptest.NewRequest("POST", "/api/projects/"+projectID+"/requirements", strings.NewReader(`{"title":"Apply reviewed candidate","summary":"Turn approved candidates into tasks","description":"Apply should create a task and lineage record.","source":"human"}`))
	requirementReq.Header.Set("Content-Type", "application/json")
	requirementResp := httptest.NewRecorder()
	srv.ServeHTTP(requirementResp, requirementReq)
	if requirementResp.Code != http.StatusCreated {
		t.Fatalf("create requirement: expected 201, got %d: %s", requirementResp.Code, requirementResp.Body.String())
	}
	var requirementEnvelope models.Envelope
	json.NewDecoder(requirementResp.Body).Decode(&requirementEnvelope)
	requirementID := requirementEnvelope.Data.(map[string]interface{})["id"].(string)

	runReq := httptest.NewRequest("POST", "/api/requirements/"+requirementID+"/planning-runs", nil)
	runResp := httptest.NewRecorder()
	srv.ServeHTTP(runResp, runReq)
	if runResp.Code != http.StatusCreated {
		t.Fatalf("create planning run: expected 201, got %d: %s", runResp.Code, runResp.Body.String())
	}
	var runEnvelope models.Envelope
	json.NewDecoder(runResp.Body).Decode(&runEnvelope)
	runID := runEnvelope.Data.(map[string]interface{})["id"].(string)

	candidateListReq := httptest.NewRequest("GET", "/api/planning-runs/"+runID+"/backlog-candidates", nil)
	candidateListResp := httptest.NewRecorder()
	srv.ServeHTTP(candidateListResp, candidateListReq)
	if candidateListResp.Code != http.StatusOK {
		t.Fatalf("list candidates: expected 200, got %d: %s", candidateListResp.Code, candidateListResp.Body.String())
	}
	var candidateListEnvelope models.Envelope
	json.NewDecoder(candidateListResp.Body).Decode(&candidateListEnvelope)
	candidates := candidateListEnvelope.Data.([]interface{})
	if len(candidates) < 3 {
		t.Fatalf("expected at least 3 candidates, got %d", len(candidates))
	}
	candidateID := candidates[0].(map[string]interface{})["id"].(string)

	approveReq := httptest.NewRequest("PATCH", "/api/backlog-candidates/"+candidateID, strings.NewReader(`{"status":"approved"}`))
	approveReq.Header.Set("Content-Type", "application/json")
	approveResp := httptest.NewRecorder()
	srv.ServeHTTP(approveResp, approveReq)
	if approveResp.Code != http.StatusOK {
		t.Fatalf("approve candidate: expected 200, got %d: %s", approveResp.Code, approveResp.Body.String())
	}

	applyReq := httptest.NewRequest("POST", "/api/backlog-candidates/"+candidateID+"/apply", nil)
	applyResp := httptest.NewRecorder()
	srv.ServeHTTP(applyResp, applyReq)
	if applyResp.Code != http.StatusCreated {
		t.Fatalf("apply candidate: expected 201, got %d: %s", applyResp.Code, applyResp.Body.String())
	}

	var applyEnvelope models.Envelope
	json.NewDecoder(applyResp.Body).Decode(&applyEnvelope)
	applyData := applyEnvelope.Data.(map[string]interface{})
	if applyData["already_applied"] != false {
		t.Fatalf("expected already_applied false, got %v", applyData["already_applied"])
	}
	taskData := applyData["task"].(map[string]interface{})
	if taskData["status"] != "todo" {
		t.Fatalf("expected created task status todo, got %v", taskData["status"])
	}
	if taskData["source"] != "agent:planning-orchestrator" {
		t.Fatalf("expected created task source agent:planning-orchestrator, got %v", taskData["source"])
	}
	candidateData := applyData["candidate"].(map[string]interface{})
	if candidateData["status"] != models.BacklogCandidateStatusApplied {
		t.Fatalf("expected applied candidate status, got %v", candidateData["status"])
	}
	lineageData := applyData["lineage"].(map[string]interface{})
	if lineageData["task_id"] != taskData["id"] {
		t.Fatalf("expected lineage task_id %v, got %v", taskData["id"], lineageData["task_id"])
	}

	replayReq := httptest.NewRequest("POST", "/api/backlog-candidates/"+candidateID+"/apply", nil)
	replayResp := httptest.NewRecorder()
	srv.ServeHTTP(replayResp, replayReq)
	if replayResp.Code != http.StatusOK {
		t.Fatalf("replay apply candidate: expected 200, got %d: %s", replayResp.Code, replayResp.Body.String())
	}
	var replayEnvelope models.Envelope
	json.NewDecoder(replayResp.Body).Decode(&replayEnvelope)
	replayData := replayEnvelope.Data.(map[string]interface{})
	if replayData["already_applied"] != true {
		t.Fatalf("expected replay already_applied true, got %v", replayData["already_applied"])
	}

	tasksReq := httptest.NewRequest("GET", "/api/projects/"+projectID+"/tasks", nil)
	tasksResp := httptest.NewRecorder()
	srv.ServeHTTP(tasksResp, tasksReq)
	if tasksResp.Code != http.StatusOK {
		t.Fatalf("list tasks after apply: expected 200, got %d: %s", tasksResp.Code, tasksResp.Body.String())
	}
	var tasksEnvelope models.Envelope
	json.NewDecoder(tasksResp.Body).Decode(&tasksEnvelope)
	tasks := tasksEnvelope.Data.([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after apply, got %d", len(tasks))
	}

	// Applying a candidate must promote the parent requirement draft→planned.
	reqGetReq := httptest.NewRequest("GET", "/api/requirements/"+requirementID, nil)
	reqGetResp := httptest.NewRecorder()
	srv.ServeHTTP(reqGetResp, reqGetReq)
	if reqGetResp.Code != http.StatusOK {
		t.Fatalf("get requirement after apply: expected 200, got %d: %s", reqGetResp.Code, reqGetResp.Body.String())
	}
	var reqGetEnvelope models.Envelope
	json.NewDecoder(reqGetResp.Body).Decode(&reqGetEnvelope)
	reqData := reqGetEnvelope.Data.(map[string]interface{})
	if reqData["status"] != "planned" {
		t.Fatalf("expected requirement status planned after apply, got %v", reqData["status"])
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

func TestRepoMappingUpdateBranch(t *testing.T) {
	srv := setupTestServer(t)
	mirrorRoot := t.TempDir()
	t.Setenv("REPO_MAPPING_ROOT", mirrorRoot)

	repo := filepath.Join(mirrorRoot, "primary")
	setupGitRepoForHandlers(t, repo)

	body := `{"name":"Update Mapping Project"}`
	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var projectResp models.Envelope
	json.NewDecoder(w.Body).Decode(&projectResp)
	projectID := projectResp.Data.(map[string]interface{})["id"].(string)

	body = `{"alias":"app","repo_path":"` + strings.ReplaceAll(repo, "\\", "\\\\") + `","default_branch":"master","is_primary":true}`
	req = httptest.NewRequest("POST", "/api/projects/"+projectID+"/repo-mappings", strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create mapping: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var mappingResp models.Envelope
	json.NewDecoder(w.Body).Decode(&mappingResp)
	mappingID := mappingResp.Data.(map[string]interface{})["id"].(string)

	body = `{"default_branch":"review/risk-git-fixes"}`
	req = httptest.NewRequest("PATCH", "/api/repo-mappings/"+mappingID, strings.NewReader(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update mapping: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updateResp models.Envelope
	json.NewDecoder(w.Body).Decode(&updateResp)
	updated := updateResp.Data.(map[string]interface{})
	if updated["default_branch"] != "review/risk-git-fixes" {
		t.Fatalf("expected updated branch, got %v", updated["default_branch"])
	}

	req = httptest.NewRequest("GET", "/api/projects/"+projectID+"/repo-mappings", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mappings: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp models.Envelope
	json.NewDecoder(w.Body).Decode(&listResp)
	mappings := listResp.Data.([]interface{})
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(mappings))
	}
	if mappings[0].(map[string]interface{})["default_branch"] != "review/risk-git-fixes" {
		t.Fatalf("expected persisted updated branch, got %v", mappings[0].(map[string]interface{})["default_branch"])
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

func TestProjectDashboardSummary(t *testing.T) {
	db := testutil.OpenTestDB(t)

	projectStore := store.NewProjectStore(db)
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	syncRunStore := store.NewSyncRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)
	summaryStore := store.NewSummaryStore(db, taskStore, documentStore, syncRunStore, driftSignalStore, agentRunStore)

	srv := router.New(router.Deps{
		ProjectHandler:            handlers.NewProjectHandler(projectStore, repoMappingStore),
		TaskHandler:               handlers.NewTaskHandler(taskStore, projectStore),
		DocumentHandler:           handlers.NewDocumentHandler(documentStore, projectStore, repoMappingStore),
		SummaryHandler:            handlers.NewSummaryHandler(summaryStore, projectStore),
		ProjectRepoMappingHandler: handlers.NewProjectRepoMappingHandler(repoMappingStore, projectStore),
	})

	project, err := projectStore.Create(models.CreateProjectRequest{Name: "Dashboard Test"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	for _, req := range []models.CreateTaskRequest{
		{Title: "Todo Task", Status: "todo"},
		{Title: "Done Task", Status: "done"},
	} {
		if _, err := taskStore.Create(project.ID, req); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	doc, err := documentStore.Create(project.ID, models.CreateDocumentRequest{
		Title:    "API Doc",
		FilePath: "docs/api.md",
		DocType:  "api",
		Source:   "human",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if err := documentStore.MarkStale(doc.ID); err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	syncRun, err := syncRunStore.Create(project.ID)
	if err != nil {
		t.Fatalf("create sync run: %v", err)
	}
	if err := syncRunStore.Complete(syncRun.ID, 3, 5); err != nil {
		t.Fatalf("complete sync run: %v", err)
	}

	if _, err := driftSignalStore.Create(project.ID, models.CreateDriftSignalRequest{
		DocumentID:    doc.ID,
		TriggerType:   "manual",
		TriggerDetail: "needs review",
		Severity:      2,
		SyncRunID:     syncRun.ID,
	}); err != nil {
		t.Fatalf("create drift signal: %v", err)
	}

	for i := 0; i < 6; i++ {
		run, err := agentRunStore.Create(project.ID, models.CreateAgentRunRequest{
			AgentName:     "agent:application-implementer",
			ActionType:    "review",
			Summary:       fmt.Sprintf("run-%d", i),
			FilesAffected: []string{"docs/api.md"},
		})
		if err != nil {
			t.Fatalf("create agent run: %v", err)
		}
		summary := fmt.Sprintf("run-%d-complete", i)
		if _, err := agentRunStore.Update(run.ID, models.UpdateAgentRunRequest{Status: "completed", Summary: &summary}); err != nil {
			t.Fatalf("complete agent run: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/projects/"+project.ID+"/dashboard-summary", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dashboardResp models.Envelope
	json.NewDecoder(w.Body).Decode(&dashboardResp)
	dashboard := dashboardResp.Data.(map[string]interface{})
	if dashboard["project_id"] != project.ID {
		t.Fatalf("expected project id %s, got %v", project.ID, dashboard["project_id"])
	}
	if int(dashboard["open_drift_count"].(float64)) != 1 {
		t.Fatalf("expected 1 open drift, got %v", dashboard["open_drift_count"])
	}
	summary := dashboard["summary"].(map[string]interface{})
	if int(summary["total_tasks"].(float64)) != 2 {
		t.Fatalf("expected 2 total tasks, got %v", summary["total_tasks"])
	}
	latestSyncRun := dashboard["latest_sync_run"].(map[string]interface{})
	if latestSyncRun["status"] != "completed" {
		t.Fatalf("expected completed latest sync run, got %v", latestSyncRun["status"])
	}
	recentAgentRuns := dashboard["recent_agent_runs"].([]interface{})
	if len(recentAgentRuns) != 5 {
		t.Fatalf("expected 5 recent agent runs, got %d", len(recentAgentRuns))
	}

	req = httptest.NewRequest("GET", "/api/projects/"+project.ID+"/summary/history", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("summary history after dashboard summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var historyResp models.Envelope
	json.NewDecoder(w.Body).Decode(&historyResp)
	history := historyResp.Data.([]interface{})
	if len(history) != 1 {
		t.Fatalf("expected exactly 1 daily snapshot after dashboard summary, got %d", len(history))
	}
}
