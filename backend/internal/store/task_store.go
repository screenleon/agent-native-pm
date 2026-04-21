package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type TaskStore struct {
	db *sql.DB
}

var ErrTaskBatchNotFound = errors.New("one or more tasks not found in project")
var ErrTaskBatchEmpty = errors.New("task batch update requires at least one task id")

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
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
		SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
		FROM tasks %s %s LIMIT $%d OFFSET $%d
	`, whereClause, orderClause, nextPos, nextPos+1)
	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.Assignee, &t.Source, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
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
	var t models.Task
	err := s.db.QueryRow(`
		SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
		FROM tasks WHERE id = $1
	`, id).Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.Assignee, &t.Source, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
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
		var task models.Task
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.Title, &task.Description, &task.Status, &task.Priority, &task.Assignee, &task.Source, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, err
		}
		byID[task.ID] = task
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
		SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
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
