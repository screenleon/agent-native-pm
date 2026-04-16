package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type TaskStore struct {
	db *sql.DB
}

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
}

func (s *TaskStore) ListByProject(projectID string, page, perPage int, sort, order string) ([]models.Task, int, error) {
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE project_id = $1", projectID).Scan(&total)
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
	query := fmt.Sprintf(`
		SELECT id, project_id, title, description, status, priority, assignee, source, created_at, updated_at
		FROM tasks WHERE project_id = $1 %s LIMIT $2 OFFSET $3
	`, orderClause)
	rows, err := s.db.Query(query, projectID, perPage, offset)
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
