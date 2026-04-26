package handlers_test

// connector_dispatch_test.go covers the Phase 6b task dispatch endpoints:
//   POST /api/connector/claim-next-task
//   POST /api/connector/tasks/:task_id/execution-result
//
// Test IDs are prefixed T-6b to match the Phase 6b DoD.

import (
	"bytes"
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

	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// hashConnectorToken mimics the store's hashSecret function for test token insertion.
func hashConnectorToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// dispatchFixture wires up all the stores needed to test the dispatch endpoints.
type dispatchFixture struct {
	srv               http.Handler
	db                *sql.DB
	dialect           database.Dialect
	taskStore         *store.TaskStore
	localConnectorStore *store.LocalConnectorStore
	requirementStore  *store.RequirementStore
	candidateStore    *store.BacklogCandidateStore
	projectStore      *store.ProjectStore
	// ownerUserID is the project/task owner (linked to connector).
	ownerUserID string
	// otherUserID owns nothing.
	otherUserID string
	projectID   string
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	dialect := testutil.TestDialect()

	// Seed two users.
	mustExec(t, db, `INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('owner-user', 'owner', 'owner@test.com', '', 'member', TRUE)`)
	mustExec(t, db, `INSERT INTO users (id, username, email, password_hash, role, is_active)
		VALUES ('other-user', 'other', 'other@test.com', '', 'member', TRUE)`)

	// Owner creates the project, then is added as a project_member.
	projects := store.NewProjectStore(db)
	project, err := projects.Create(models.CreateProjectRequest{Name: "Dispatch Test Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	// Add owner-user as project member (project_members table controls ownership).
	memberID := fmt.Sprintf("pm-%s", project.ID)
	mustExec(t, db,
		`INSERT INTO project_members (id, project_id, user_id, role) VALUES ($1, $2, $3, 'owner')`,
		memberID, project.ID, "owner-user",
	)

	rs := store.NewRequirementStore(db)
	ts := store.NewTaskStoreWithDialect(db, dialect)
	bcs := store.NewBacklogCandidateStore(db, dialect)
	connectors := store.NewLocalConnectorStore(db, dialect)
	agentRuns := store.NewAgentRunStore(db)

	planningRuns := store.NewPlanningRunStore(db, dialect)
	planner := stubPlanner{}
	planningHandler := handlers.NewPlanningRunHandler(planningRuns, bcs, projects, rs, agentRuns, planner).
		WithLocalConnectorStore(connectors)

	localConnHandler := handlers.NewLocalConnectorHandler(connectors, planningRuns, rs, bcs, agentRuns).
		WithTaskStore(ts)

	srv := router.New(router.Deps{
		PlanningRunHandler:  planningHandler,
		LocalConnectorHandler: localConnHandler,
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
		LocalModeMiddleware: middleware.InjectLocalAdmin,
	})

	return &dispatchFixture{
		srv:                 srv,
		db:                  db,
		dialect:             dialect,
		taskStore:           ts,
		localConnectorStore: connectors,
		requirementStore:    rs,
		candidateStore:      bcs,
		projectStore:        projects,
		ownerUserID:         "owner-user",
		otherUserID:         "other-user",
		projectID:           project.ID,
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("mustExec %q: %v", query, err)
	}
}

// seedConnectorForUser inserts a local connector with the given plaintext token for the given user.
// The token is stored as sha256 hash matching the store's hashSecret function.
func (fx *dispatchFixture) seedConnectorForUser(t *testing.T, connectorID, userID, token string) {
	t.Helper()
	now := time.Now().UTC()
	mustExec(t, fx.db,
		`INSERT INTO local_connectors (id, user_id, label, platform, client_version, status, capabilities, protocol_version, token_hash, last_seen_at, last_error, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '', $4, '{}', 1, $5, $6, '', $6, $6)`,
		connectorID, userID, "test-connector", models.LocalConnectorStatusOnline, hashConnectorToken(token), now,
	)
}

// seedQueuedTask inserts a role_dispatch task in the queued state for the owner's project.
func (fx *dispatchFixture) seedQueuedTask(t *testing.T, title, roleID string) string {
	t.Helper()
	return fx.seedQueuedTaskWithSource(t, title, "role_dispatch:"+roleID)
}

// seedQueuedTaskWithSource inserts a queued task with an arbitrary
// source string. Used by tests that need to exercise malformed sources
// (e.g. legacy "role_dispatch" with no colon suffix).
func (fx *dispatchFixture) seedQueuedTaskWithSource(t *testing.T, title, source string) string {
	t.Helper()
	id := fmt.Sprintf("task-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	mustExec(t, fx.db,
		`INSERT INTO tasks (id, project_id, title, description, status, priority, assignee, source, dispatch_status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'desc', 'todo', 'medium', '', $4, $5, $6, $6)`,
		id, fx.projectID, title, source, models.TaskDispatchStatusQueued, now,
	)
	return id
}

func (fx *dispatchFixture) doClaimTask(connectorToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/connector/claim-next-task", nil)
	req.Header.Set("X-Connector-Token", connectorToken)
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

func (fx *dispatchFixture) doSubmitResult(connectorToken, taskID string, body interface{}) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/connector/tasks/"+taskID+"/execution-result",
		bytes.NewReader(raw),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Connector-Token", connectorToken)
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	return rec
}

// T-6b-1: claim with empty queue returns 200 task:null.
func TestClaimNextTask_EmptyQueue(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-owner", fx.ownerUserID, "tok-owner")

	rec := fx.doClaimTask("tok-owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data handlers.ClaimNextTaskResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Task != nil {
		t.Errorf("expected task=null, got task.ID=%s", env.Data.Task.ID)
	}
}

// T-6b-2: connector owned by the project owner claims a queued task → dispatch_status=running.
func TestClaimNextTask_OwnerClaimsTask(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-owner2", fx.ownerUserID, "tok-owner2")
	taskID := fx.seedQueuedTask(t, "Implement API", "backend-architect")

	rec := fx.doClaimTask("tok-owner2")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data handlers.ClaimNextTaskResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Task == nil {
		t.Fatal("expected a task, got null")
	}
	if env.Data.Task.ID != taskID {
		t.Errorf("expected task %s, got %s", taskID, env.Data.Task.ID)
	}

	// Verify dispatch_status updated in DB.
	task, err := fx.taskStore.GetByID(taskID)
	if err != nil || task == nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.DispatchStatus != models.TaskDispatchStatusRunning {
		t.Errorf("expected dispatch_status=running, got %s", task.DispatchStatus)
	}
}

// T-6b-3: connector owned by a different user → 200 task:null (no ownership).
func TestClaimNextTask_NonMemberGetsNull(t *testing.T) {
	fx := newDispatchFixture(t)
	// other-user has no project.
	fx.seedConnectorForUser(t, "conn-other", fx.otherUserID, "tok-other")
	fx.seedQueuedTask(t, "Implement UI", "ui-scaffolder")

	rec := fx.doClaimTask("tok-other")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data handlers.ClaimNextTaskResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Task != nil {
		t.Errorf("expected task=null for non-member connector, got task.ID=%s", env.Data.Task.ID)
	}
}

// T-6b-4: submit success result → dispatch_status=completed.
func TestSubmitTaskResult_Success(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-s4", fx.ownerUserID, "tok-s4")
	taskID := fx.seedQueuedTask(t, "Write tests", "test-writer")

	// Claim first.
	claimRec := fx.doClaimTask("tok-s4")
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", claimRec.Code, claimRec.Body.String())
	}

	result := map[string]interface{}{"files": []string{"foo_test.go"}}
	rec := fx.doSubmitResult("tok-s4", taskID, handlers.SubmitTaskResultRequest{
		Success: true,
		Result:  mustMarshal(t, result),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	task, _ := fx.taskStore.GetByID(taskID)
	if task.DispatchStatus != models.TaskDispatchStatusCompleted {
		t.Errorf("expected completed, got %s", task.DispatchStatus)
	}
	if len(task.ExecutionResult) == 0 {
		t.Error("expected execution_result to be non-empty")
	}
}

// T-6b-5: submit failure → dispatch_status=failed.
func TestSubmitTaskResult_Failure(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-s5", fx.ownerUserID, "tok-s5")
	taskID := fx.seedQueuedTask(t, "Review code", "code-reviewer")

	claimRec := fx.doClaimTask("tok-s5")
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", claimRec.Code, claimRec.Body.String())
	}

	rec := fx.doSubmitResult("tok-s5", taskID, handlers.SubmitTaskResultRequest{
		Success:      false,
		ErrorMessage: "CLI crashed",
		ErrorKind:    "unknown",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	task, _ := fx.taskStore.GetByID(taskID)
	if task.DispatchStatus != models.TaskDispatchStatusFailed {
		t.Errorf("expected failed, got %s", task.DispatchStatus)
	}
}

// T-6b-6: submit result for task not in running state → 400.
func TestSubmitTaskResult_NotRunningReturns400(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-s6", fx.ownerUserID, "tok-s6")
	// Insert task with dispatch_status=queued (not running) directly.
	taskID := fx.seedQueuedTask(t, "DB schema", "db-schema-designer")
	// Do NOT claim it — task stays queued.

	rec := fx.doSubmitResult("tok-s6", taskID, handlers.SubmitTaskResultRequest{
		Success: true,
		Result:  json.RawMessage(`{"ok":true}`),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// T-6b-7: invalid connector token → 401.
func TestClaimNextTask_InvalidToken(t *testing.T) {
	fx := newDispatchFixture(t)
	rec := fx.doClaimTask("invalid-token-xyz")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// Phase 6c PR-2 T-6c-C1-S3: claim-next-task with a stale role
// (source references a role no longer in the catalog) MUST transition
// the task queued → failed and return task:null. The connector should
// see this as "queue empty" and not be exposed to the stale task.
func TestClaimNextTask_StaleRoleTransitionsToFailed(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-stale", fx.ownerUserID, "tok-stale")
	staleTaskID := fx.seedQueuedTask(t, "Stale role task", "no-longer-in-catalog")

	rec := fx.doClaimTask("tok-stale")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data handlers.ClaimNextTaskResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Task != nil {
		t.Errorf("expected task=null (stale role drained), got task.ID=%s", env.Data.Task.ID)
	}

	// Verify the stale task was transitioned to failed with the right
	// error_kind, and an actor_audit row was written.
	task, err := fx.taskStore.GetByID(staleTaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.DispatchStatus != models.TaskDispatchStatusFailed {
		t.Errorf("expected failed, got %s", task.DispatchStatus)
	}
	if task.ExecutionResult == nil {
		t.Fatal("expected execution_result populated with role_not_found")
	}
	var er map[string]string
	_ = json.Unmarshal(task.ExecutionResult, &er)
	if er["error_kind"] != models.ErrorKindRoleNotFound {
		t.Errorf("expected error_kind=role_not_found, got %s", er["error_kind"])
	}

	// Audit row should be present.
	var auditCount int
	if err := fx.db.QueryRow(
		`SELECT COUNT(*) FROM actor_audit WHERE subject_kind = $1 AND subject_id = $2 AND field = $3 AND actor_kind = $4`,
		"task", staleTaskID, "dispatch_status", "system",
	).Scan(&auditCount); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("expected 1 audit row for stale-role transition, got %d", auditCount)
	}
}

// Phase 6c PR-2 (Copilot review #4): a queued task whose source is
// missing the role suffix entirely (legacy "role_dispatch" without a
// colon, or "role_dispatch:" with empty payload) MUST transition to
// failed with error_kind=role_dispatch_malformed — distinct from
// role_not_found which means "well-formed id, absent from catalog".
// Operators rely on the kind to decide whether to inspect the task
// source field or the role catalog.
func TestClaimNextTask_MalformedSourceTransitionsToFailed(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "no_colon", source: "role_dispatch"},
		{name: "empty_suffix", source: "role_dispatch:"},
		{name: "whitespace_suffix", source: "role_dispatch:   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newDispatchFixture(t)
			fx.seedConnectorForUser(t, "conn-mal-"+tc.name, fx.ownerUserID, "tok-mal-"+tc.name)
			taskID := fx.seedQueuedTaskWithSource(t, "Malformed source", tc.source)

			rec := fx.doClaimTask("tok-mal-" + tc.name)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var env struct {
				Data handlers.ClaimNextTaskResponse `json:"data"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
			if env.Data.Task != nil {
				t.Errorf("expected task=null (malformed task drained), got task.ID=%s", env.Data.Task.ID)
			}

			task, err := fx.taskStore.GetByID(taskID)
			if err != nil {
				t.Fatalf("get task: %v", err)
			}
			if task.DispatchStatus != models.TaskDispatchStatusFailed {
				t.Errorf("expected failed, got %s", task.DispatchStatus)
			}
			if task.ExecutionResult == nil {
				t.Fatal("expected execution_result populated")
			}
			var er map[string]string
			_ = json.Unmarshal(task.ExecutionResult, &er)
			if er["error_kind"] != models.ErrorKindRoleDispatchMalformed {
				t.Errorf("expected error_kind=role_dispatch_malformed, got %s", er["error_kind"])
			}
			if er["error_message"] == "" {
				t.Error("expected non-empty error_message")
			}
			// Audit row should be present and record the malformed-source rationale.
			var auditCount int
			if err := fx.db.QueryRow(
				`SELECT COUNT(*) FROM actor_audit
				 WHERE subject_kind = $1 AND subject_id = $2 AND field = $3 AND actor_kind = $4`,
				"task", taskID, "dispatch_status", "system",
			).Scan(&auditCount); err != nil {
				t.Fatalf("audit count: %v", err)
			}
			if auditCount != 1 {
				t.Errorf("expected 1 audit row, got %d", auditCount)
			}
		})
	}
}

// Phase 6c PR-2 T-6c-C1-S4: claim drains stale-role tasks and surfaces
// the next valid task. Two tasks queued, the first has a stale role.
func TestClaimNextTask_DrainsStaleRoleThenClaimsNext(t *testing.T) {
	fx := newDispatchFixture(t)
	fx.seedConnectorForUser(t, "conn-drain", fx.ownerUserID, "tok-drain")
	staleTaskID := fx.seedQueuedTask(t, "Stale", "ghost-role")
	// Pause to ensure the next task has a strictly later created_at so
	// ORDER BY ASC picks the stale one first.
	time.Sleep(10 * time.Millisecond)
	validTaskID := fx.seedQueuedTask(t, "Valid", "backend-architect")

	rec := fx.doClaimTask("tok-drain")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data handlers.ClaimNextTaskResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Data.Task == nil {
		t.Fatal("expected to claim valid task after draining stale")
	}
	if env.Data.Task.ID != validTaskID {
		t.Errorf("expected to claim %s, got %s", validTaskID, env.Data.Task.ID)
	}
	staleAfter, _ := fx.taskStore.GetByID(staleTaskID)
	if staleAfter.DispatchStatus != models.TaskDispatchStatusFailed {
		t.Errorf("stale task should be failed, got %s", staleAfter.DispatchStatus)
	}
}

// Phase 6c PR-2 T-6c-C1-E1: GET /api/roles returns 6 roles, all
// category=role. Meta-roles (none today; PR-3 adds dispatcher) are
// excluded by the handler-side filter.
func TestRolesEndpoint_ReturnsCatalog(t *testing.T) {
	fx := newDispatchFixture(t)
	srv := buildServerWithRolesHandler(fx)

	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data []handlers.RoleResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 6 {
		t.Errorf("expected 6 roles, got %d (catalog drift?)", len(env.Data))
	}
	for _, r := range env.Data {
		if r.Category != "role" {
			t.Errorf("role %s leaked category=%q (must be 'role')", r.ID, r.Category)
		}
		if r.DefaultTimeoutSec <= 0 {
			t.Errorf("role %s has invalid DefaultTimeoutSec=%d", r.ID, r.DefaultTimeoutSec)
		}
	}
}

// buildServerWithRolesHandler wires a minimal router exposing only the
// /api/roles endpoint (plus whatever the dispatch fixture already
// added). Used to test the public roles endpoint without the full
// server bootstrap.
func buildServerWithRolesHandler(fx *dispatchFixture) http.Handler {
	return router.New(router.Deps{
		RolesHandler: handlers.NewRolesHandler(),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return next
		},
		LocalModeMiddleware: middleware.InjectLocalAdmin,
	})
}

// --- helpers ---

func mustMarshal(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return json.RawMessage(b)
}

// Ensure planning.DraftPlanner is satisfied — stubPlanner is defined in
// handlers_test.go in this same package.
var _ planning.DraftPlanner = stubPlanner{}
