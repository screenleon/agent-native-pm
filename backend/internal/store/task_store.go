package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type TaskStore struct {
	db      *sql.DB
	dialect database.Dialect
}

var ErrTaskBatchNotFound = errors.New("one or more tasks not found in project")
var ErrTaskBatchEmpty = errors.New("task batch update requires at least one task id")

// ErrDispatchOwnership is returned when a connector tries to operate on a task
// it does not own (via the project membership check).
var ErrDispatchOwnership = errors.New("connector does not have ownership over this task")

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
}

// NewTaskStoreWithDialect creates a TaskStore with an explicit dialect, required
// for the transaction-based dispatch methods.
func NewTaskStoreWithDialect(db *sql.DB, dialect database.Dialect) *TaskStore {
	return &TaskStore{db: db, dialect: dialect}
}

// taskColumns is the canonical column list for scanning a full Task row.
// Keep in sync with scanTask / scanTaskFull.
const taskColumns = `id, project_id, title, description, status, priority, assignee, source,
	dispatch_status, execution_result, created_at, updated_at`

// scanTaskFull scans all columns including the Phase 6b dispatch fields.
func scanTaskFull(row interface {
	Scan(dest ...interface{}) error
}) (*models.Task, error) {
	var t models.Task
	var executionResultRaw sql.NullString
	err := row.Scan(
		&t.ID, &t.ProjectID, &t.Title, &t.Description,
		&t.Status, &t.Priority, &t.Assignee, &t.Source,
		&t.DispatchStatus, &executionResultRaw,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if executionResultRaw.Valid && executionResultRaw.String != "" {
		t.ExecutionResult = json.RawMessage(executionResultRaw.String)
	}
	if t.DispatchStatus == "" {
		t.DispatchStatus = models.TaskDispatchStatusNone
	}
	return &t, nil
}

func (s *TaskStore) ListByProject(projectID string, page, perPage int, sort, order string, filters models.TaskListFilters) ([]models.Task, int, error) {
	whereClause, filterArgs, nextPos := buildTaskListWhereClause(projectID, filters)

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tasks %s", whereClause)
	err := s.db.QueryRow(countQuery, filterArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Validate sort column (whitelist to prevent SQL injection)
	validSorts := map[string]bool{
		"created_at": true,
		"updated_at": true,
		"priority":   true,
		"status":     true,
		"title":      true,
	}
	if sort == "" {
		sort = "created_at"
	}
	if !validSorts[sort] {
		sort = "created_at"
	}

	// Validate order direction
	validOrders := map[string]bool{
		"ASC":  true,
		"DESC": true,
		"asc":  true,
		"desc": true,
	}
	if order == "" {
		order = "DESC"
	}
	if !validOrders[order] {
		order = "DESC"
	}

	// Normalize order to uppercase for SQL
	if order == "asc" {
		order = "ASC"
	} else if order == "desc" {
		order = "DESC"
	}

	offset := (page - 1) * perPage
	orderClause := fmt.Sprintf("ORDER BY %s %s", sort, order)
	queryArgs := append(append([]interface{}{}, filterArgs...), perPage, offset)
	query := fmt.Sprintf(`
		SELECT %s
		FROM tasks %s %s LIMIT $%d OFFSET $%d
	`, taskColumns, whereClause, orderClause, nextPos, nextPos+1)
	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		t, err := scanTaskFull(rows)
		if err != nil {
			return nil, 0, err
		}
		if t != nil {
			tasks = append(tasks, *t)
		}
	}
	return tasks, total, rows.Err()
}

func buildTaskListWhereClause(projectID string, filters models.TaskListFilters) (string, []interface{}, int) {
	whereClauses := []string{"project_id = $1"}
	args := []interface{}{projectID}
	pos := 2

	if filters.Status != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", pos))
		args = append(args, filters.Status)
		pos++
	}
	if filters.Priority != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("priority = $%d", pos))
		args = append(args, filters.Priority)
		pos++
	}
	if filters.Assignee != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("assignee = $%d", pos))
		args = append(args, filters.Assignee)
		pos++
	}

	return "WHERE " + strings.Join(whereClauses, " AND "), args, pos
}

func (s *TaskStore) GetByID(id string) (*models.Task, error) {
	return scanTaskFull(s.db.QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id,
	))
}

func (s *TaskStore) Create(projectID string, req models.CreateTaskRequest) (*models.Task, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	status := req.Status
	if status == "" {
		status = "todo"
	}
	priority := req.Priority
	if priority == "" {
		priority = "medium"
	}

	_, err := s.db.Exec(`
		INSERT INTO tasks (id, project_id, title, description, status, priority, assignee, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, id, projectID, req.Title, req.Description, status, priority, req.Assignee, req.Source, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *TaskStore) Update(id string, req models.UpdateTaskRequest) (*models.Task, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", pos))
		args = append(args, *req.Title)
		pos++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", pos))
		args = append(args, *req.Description)
		pos++
	}
	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", pos))
		args = append(args, *req.Status)
		pos++
	}
	if req.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", pos))
		args = append(args, *req.Priority)
		pos++
	}
	if req.Assignee != nil {
		setClauses = append(setClauses, fmt.Sprintf("assignee = $%d", pos))
		args = append(args, *req.Assignee)
		pos++
	}

	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = $%d", strings.Join(setClauses, ", "), pos)
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return nil, err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, nil
	}
	return s.GetByID(id)
}

func (s *TaskStore) Delete(id string) error {
	_, err := s.db.Exec("DELETE FROM tasks WHERE id = $1", id)
	return err
}

func (s *TaskStore) BatchUpdate(projectID string, taskIDs []string, changes models.BatchUpdateTaskChanges) ([]models.Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	normalizedIDs := normalizeTaskIDs(taskIDs)
	if len(normalizedIDs) == 0 {
		return nil, ErrTaskBatchEmpty
	}
	countQuery, countArgs := buildTaskBatchCountQuery(projectID, normalizedIDs)
	var matched int
	if err := tx.QueryRow(countQuery, countArgs...).Scan(&matched); err != nil {
		return nil, err
	}
	if matched != len(normalizedIDs) {
		return nil, ErrTaskBatchNotFound
	}

	now := time.Now().UTC()
	setClauses, updateArgs, nextPos := buildBatchUpdateSetClauses(changes, now)
	updateArgs = append(updateArgs, projectID)
	projectPos := nextPos
	nextPos++
	for _, id := range normalizedIDs {
		updateArgs = append(updateArgs, id)
	}
	idPlaceholders := buildPositionalPlaceholders(nextPos, len(normalizedIDs))
	updateQuery := fmt.Sprintf(
		"UPDATE tasks SET %s WHERE project_id = $%d AND id IN (%s)",
		strings.Join(setClauses, ", "),
		projectPos,
		idPlaceholders,
	)
	if _, err := tx.Exec(updateQuery, updateArgs...); err != nil {
		return nil, err
	}

	selectQuery, selectArgs := buildTaskBatchSelectQuery(projectID, normalizedIDs)
	rows, err := tx.Query(selectQuery, selectArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[string]models.Task, len(normalizedIDs))
	for rows.Next() {
		t, err := scanTaskFull(rows)
		if err != nil {
			return nil, err
		}
		if t != nil {
			byID[t.ID] = *t
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	updatedTasks := make([]models.Task, 0, len(normalizedIDs))
	for _, id := range normalizedIDs {
		task, ok := byID[id]
		if !ok {
			return nil, ErrTaskBatchNotFound
		}
		updatedTasks = append(updatedTasks, task)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updatedTasks, nil
}

func (s *TaskStore) CountByProjectAndStatus(projectID string) (map[string]int, error) {
	rows, err := s.db.Query("SELECT status, COUNT(*) FROM tasks WHERE project_id = $1 GROUP BY status", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{
		"todo":        0,
		"in_progress": 0,
		"done":        0,
		"cancelled":   0,
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// ─── Phase 6b: dispatch methods ──────────────────────────────────────────────

// ClaimNextDispatchTask atomically finds the next queued role_dispatch task
// belonging to a project the connector's user is a member of, marks it as
// "running", and returns the task together with its requirement.
//
// Ownership check: the task's project_id must appear in project_members where
// user_id = connectorUserID. A SQLite write-lock is acquired via a no-op
// UPDATE before the SELECT so concurrent claim attempts serialise.
//
// Returns (nil, nil, nil) when the queue is empty for this connector's user.
func (s *TaskStore) ClaimNextDispatchTask(connectorID, connectorUserID string) (*models.Task, *models.Requirement, error) {
	var tx *sql.Tx
	var err error
	if s.dialect.IsSQLite() {
		tx, err = s.db.Begin()
	} else {
		tx, err = s.db.Begin()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// On SQLite force an immediate write lock before the SELECT so concurrent
	// claim attempts serialise (same pattern as lockCandidateApplyKey).
	if s.dialect.IsSQLite() {
		if _, err := tx.Exec(`UPDATE tasks SET updated_at = updated_at WHERE 1 = 0`); err != nil {
			return nil, nil, fmt.Errorf("sqlite write lock: %w", err)
		}
	}

	// Find the oldest queued role_dispatch task in any project the connector's
	// user is a member of (via project_members).
	forUpdate := s.dialect.ForUpdate()
	skipLocked := s.dialect.SkipLocked()
	query := fmt.Sprintf(`
		SELECT t.id, t.project_id, t.title, t.description, t.status, t.priority,
		       t.assignee, t.source, t.dispatch_status, t.execution_result,
		       t.created_at, t.updated_at
		FROM tasks t
		INNER JOIN project_members pm ON pm.project_id = t.project_id
		WHERE t.dispatch_status = $1
		  AND pm.user_id = $2
		  AND t.source LIKE $3
		ORDER BY t.created_at ASC, t.id ASC
		LIMIT 1
		%s%s
	`, forUpdate, skipLocked)

	row := tx.QueryRow(query, models.TaskDispatchStatusQueued, connectorUserID, "role_dispatch%")
	task, err := scanTaskFull(row)
	if err != nil {
		return nil, nil, fmt.Errorf("query queued task: %w", err)
	}
	if task == nil {
		// Queue empty — commit and return nil.
		_ = tx.Commit()
		return nil, nil, nil
	}

	// Mark as running.
	now := time.Now().UTC()
	if _, err := tx.Exec(
		`UPDATE tasks SET dispatch_status = $1, updated_at = $2 WHERE id = $3`,
		models.TaskDispatchStatusRunning, now, task.ID,
	); err != nil {
		return nil, nil, fmt.Errorf("mark task running: %w", err)
	}
	task.DispatchStatus = models.TaskDispatchStatusRunning
	task.UpdatedAt = now

	// Load the requirement via task_lineage so we can return context.
	var req *models.Requirement
	req, err = getRequirementForTask(tx, task.ID)
	if err != nil {
		// Non-fatal: we can proceed without requirement context.
		req = nil
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit claim: %w", err)
	}
	return task, req, nil
}

// getRequirementForTask joins task_lineage → requirements to find the
// requirement associated with the task.
func getRequirementForTask(tx *sql.Tx, taskID string) (*models.Requirement, error) {
	var req models.Requirement
	var summary, description, source, audience, successCriteria sql.NullString
	err := tx.QueryRow(`
		SELECT r.id, r.project_id, COALESCE(r.title,''), COALESCE(r.summary,''),
		       COALESCE(r.description,''), COALESCE(r.status,''), COALESCE(r.source,''),
		       COALESCE(r.audience,''), COALESCE(r.success_criteria,''),
		       r.created_at, r.updated_at
		FROM requirements r
		INNER JOIN task_lineage tl ON tl.requirement_id = r.id
		WHERE tl.task_id = $1
		ORDER BY tl.created_at ASC
		LIMIT 1
	`, taskID).Scan(
		&req.ID, &req.ProjectID, &req.Title, &summary,
		&description, &req.Status, &source,
		&audience, &successCriteria,
		&req.CreatedAt, &req.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if summary.Valid {
		req.Summary = summary.String
	}
	if description.Valid {
		req.Description = description.String
	}
	if source.Valid {
		req.Source = source.String
	}
	if audience.Valid {
		req.Audience = audience.String
	}
	if successCriteria.Valid {
		req.SuccessCriteria = successCriteria.String
	}
	return &req, nil
}

// CompleteDispatchTask marks a task as completed and stores the result JSON.
// The connectorUserID parameter ensures cross-user protection via the project
// ownership check.
func (s *TaskStore) CompleteDispatchTask(taskID, connectorUserID string, result json.RawMessage) error {
	return s.updateDispatchStatus(taskID, connectorUserID, models.TaskDispatchStatusCompleted, "", result)
}

// FailDispatchTask marks a task as failed and records an error message in the
// result JSON.
func (s *TaskStore) FailDispatchTask(taskID, connectorUserID, errorMsg string) error {
	errJSON, _ := json.Marshal(map[string]string{
		"error_message": errorMsg,
		"error_kind":    "dispatch_failed",
	})
	return s.updateDispatchStatus(taskID, connectorUserID, models.TaskDispatchStatusFailed, "", json.RawMessage(errJSON))
}

func (s *TaskStore) updateDispatchStatus(taskID, connectorUserID, status, _ string, result json.RawMessage) error {
	now := time.Now().UTC()
	var resultStr *string
	if len(result) > 0 {
		str := string(result)
		resultStr = &str
	}
	res, err := s.db.Exec(`
		UPDATE tasks
		SET dispatch_status = $1,
		    execution_result = $2,
		    updated_at = $3
		WHERE id = $4
		  AND project_id IN (
		      SELECT project_id FROM project_members WHERE user_id = $5
		  )
	`, status, resultStr, now, taskID, connectorUserID)
	if err != nil {
		return fmt.Errorf("update dispatch status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDispatchOwnership
	}
	return nil
}

// GetTaskForConnector returns a task only if it is owned by the connector's
// user (via project_members membership). Used by the execution-result handler
// to verify the connector has rights before accepting a result.
func (s *TaskStore) GetTaskForConnector(taskID, connectorUserID string) (*models.Task, error) {
	return scanTaskFull(s.db.QueryRow(`
		SELECT t.id, t.project_id, t.title, t.description, t.status, t.priority,
		       t.assignee, t.source, t.dispatch_status, t.execution_result,
		       t.created_at, t.updated_at
		FROM tasks t
		INNER JOIN project_members pm ON pm.project_id = t.project_id
		WHERE t.id = $1 AND pm.user_id = $2
	`, taskID, connectorUserID))
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func normalizeTaskIDs(taskIDs []string) []string {
	seen := make(map[string]bool, len(taskIDs))
	normalized := make([]string, 0, len(taskIDs))
	for _, rawID := range taskIDs {
		trimmed := strings.TrimSpace(rawID)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func buildTaskBatchCountQuery(projectID string, taskIDs []string) (string, []interface{}) {
	args := []interface{}{projectID}
	for _, id := range taskIDs {
		args = append(args, id)
	}
	return fmt.Sprintf(
		"SELECT COUNT(*) FROM tasks WHERE project_id = $1 AND id IN (%s)",
		buildPositionalPlaceholders(2, len(taskIDs)),
	), args
}

func buildBatchUpdateSetClauses(changes models.BatchUpdateTaskChanges, updatedAt time.Time) ([]string, []interface{}, int) {
	setClauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 4)
	pos := 1

	if changes.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", pos))
		args = append(args, *changes.Status)
		pos++
	}
	if changes.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", pos))
		args = append(args, *changes.Priority)
		pos++
	}
	if changes.Assignee != nil {
		setClauses = append(setClauses, fmt.Sprintf("assignee = $%d", pos))
		args = append(args, *changes.Assignee)
		pos++
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, updatedAt)
	pos++

	return setClauses, args, pos
}

func buildTaskBatchSelectQuery(projectID string, taskIDs []string) (string, []interface{}) {
	args := []interface{}{projectID}
	for _, id := range taskIDs {
		args = append(args, id)
	}
	return fmt.Sprintf(`
		SELECT `+taskColumns+`
		FROM tasks WHERE project_id = $1 AND id IN (%s)
	`, buildPositionalPlaceholders(2, len(taskIDs))), args
}

func buildPositionalPlaceholders(start, count int) string {
	placeholders := make([]string, 0, count)
	for offset := 0; offset < count; offset++ {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+offset))
	}
	return strings.Join(placeholders, ", ")
}
