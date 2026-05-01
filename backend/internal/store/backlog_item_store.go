package store

import (
	"bytes"
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

var (
	ErrBacklogItemAlreadyArchived = errors.New("backlog item is archived")
	ErrBacklogItemInvalidOrigin   = errors.New("backlog item origin does not belong to project")
	ErrBacklogItemNotCommittable  = errors.New("backlog item is not ready to commit")
)

type BacklogItemStore struct {
	db      *sql.DB
	dialect database.Dialect
}

func NewBacklogItemStore(db *sql.DB, dialect database.Dialect) *BacklogItemStore {
	return &BacklogItemStore{db: db, dialect: dialect}
}

const backlogItemColumns = `id, project_id, requirement_id, planning_run_id, backlog_candidate_id, task_id,
	title, description, status, priority, source, rank, labels, acceptance_criteria, blocked_reason,
	created_at, updated_at`

func (s *BacklogItemStore) ListByProject(projectID string, page, perPage int, sort, order string, filters models.BacklogItemListFilters) ([]models.BacklogItem, int, error) {
	whereClause, args, nextPos := buildBacklogItemWhereClause(s.dialect, projectID, filters)

	var total int
	if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM backlog_items %s", whereClause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	validSorts := map[string]bool{
		"rank":       true,
		"created_at": true,
		"updated_at": true,
		"priority":   true,
		"status":     true,
		"title":      true,
	}
	if sort == "" || !validSorts[sort] {
		sort = "rank"
	}
	if order == "" {
		order = "ASC"
	}
	order = strings.ToUpper(order)
	if order != "ASC" && order != "DESC" {
		order = "ASC"
	}

	offset := (page - 1) * perPage
	queryArgs := append(append([]interface{}{}, args...), perPage, offset)
	query := fmt.Sprintf(`
		SELECT %s
		FROM backlog_items
		%s
		ORDER BY %s %s, updated_at DESC, id ASC
		LIMIT $%d OFFSET $%d
	`, backlogItemColumns, whereClause, sort, order, nextPos, nextPos+1)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]models.BacklogItem, 0)
	for rows.Next() {
		item, err := scanBacklogItem(rows)
		if err != nil {
			return nil, 0, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, total, rows.Err()
}

func buildBacklogItemWhereClause(dialect database.Dialect, projectID string, filters models.BacklogItemListFilters) (string, []interface{}, int) {
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
	if filters.Source != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("source = $%d", pos))
		args = append(args, filters.Source)
		pos++
	}
	if filters.Label != "" {
		if dialect.IsSQLite() {
			whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(labels) WHERE value = $%d)", pos))
		} else {
			whereClauses = append(whereClauses, fmt.Sprintf("labels ? $%d", pos))
		}
		args = append(args, filters.Label)
		pos++
	}
	if filters.Query != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(LOWER(title) LIKE LOWER($%d) OR LOWER(description) LIKE LOWER($%d))", pos, pos))
		args = append(args, "%"+filters.Query+"%")
		pos++
	}

	return "WHERE " + strings.Join(whereClauses, " AND "), args, pos
}

func (s *BacklogItemStore) GetByID(id string) (*models.BacklogItem, error) {
	return scanBacklogItem(s.db.QueryRow(
		`SELECT `+backlogItemColumns+` FROM backlog_items WHERE id = $1`, id,
	))
}

func (s *BacklogItemStore) Create(projectID string, req models.CreateBacklogItemRequest) (*models.BacklogItem, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	if err := s.validateOriginProject(projectID, req); err != nil {
		return nil, err
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = models.BacklogItemStatusTriage
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "medium"
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = models.BacklogItemSourceHuman
	}
	labelsJSON, err := marshalBacklogLabels(req.Labels)
	if err != nil {
		return nil, err
	}

	_, err = s.db.Exec(`
		INSERT INTO backlog_items (
			id, project_id, requirement_id, planning_run_id, backlog_candidate_id, title, description,
			status, priority, source, rank, labels, acceptance_criteria, blocked_reason, created_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, id, projectID, req.RequirementID, req.PlanningRunID, req.BacklogCandidateID, strings.TrimSpace(req.Title),
		req.Description, status, priority, source, req.Rank, string(labelsJSON), req.AcceptanceCriteria, req.BlockedReason, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *BacklogItemStore) validateOriginProject(projectID string, req models.CreateBacklogItemRequest) error {
	checks := []struct {
		table string
		id    string
	}{
		{table: "requirements", id: strings.TrimSpace(req.RequirementID)},
		{table: "planning_runs", id: strings.TrimSpace(req.PlanningRunID)},
		{table: "backlog_candidates", id: strings.TrimSpace(req.BacklogCandidateID)},
	}
	for _, check := range checks {
		if check.id == "" {
			continue
		}
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = $1 AND project_id = $2", check.table)
		if err := s.db.QueryRow(query, check.id, projectID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return ErrBacklogItemInvalidOrigin
		}
	}
	return nil
}

func (s *BacklogItemStore) Update(id string, req models.UpdateBacklogItemRequest) (*models.BacklogItem, error) {
	setClauses := make([]string, 0)
	args := make([]interface{}, 0)
	pos := 1

	if req.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", pos))
		args = append(args, strings.TrimSpace(*req.Title))
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
	if req.Rank != nil {
		setClauses = append(setClauses, fmt.Sprintf("rank = $%d", pos))
		args = append(args, *req.Rank)
		pos++
	}
	if req.Labels != nil {
		labelsJSON, err := marshalBacklogLabels(*req.Labels)
		if err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", pos))
		args = append(args, string(labelsJSON))
		pos++
	}
	if req.AcceptanceCriteria != nil {
		setClauses = append(setClauses, fmt.Sprintf("acceptance_criteria = $%d", pos))
		args = append(args, *req.AcceptanceCriteria)
		pos++
	}
	if req.BlockedReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("blocked_reason = $%d", pos))
		args = append(args, *req.BlockedReason)
		pos++
	}
	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	query := fmt.Sprintf("UPDATE backlog_items SET %s WHERE id = $%d", strings.Join(setClauses, ", "), pos)
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

func (s *BacklogItemStore) CommitToTask(id string) (*models.CommitBacklogItemResponse, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := s.lockBacklogItemForCommit(tx, id); err != nil {
		return nil, err
	}

	item, err := s.getByIDForUpdate(tx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	if item.Status == models.BacklogItemStatusArchived {
		return nil, ErrBacklogItemAlreadyArchived
	}
	if item.TaskID != "" {
		task, err := getTaskByID(tx, item.TaskID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			lineage, err := getTaskLineageByBacklogItemID(tx, item.ID)
			if err != nil {
				return nil, err
			}
			if lineage == nil {
				lineage, err = createBacklogItemTaskLineage(tx, item, task.ID)
				if err != nil {
					return nil, err
				}
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &models.CommitBacklogItemResponse{BacklogItem: *item, Task: *task, Lineage: *lineage, AlreadyApplied: true}, nil
		}
	}
	if item.Status != models.BacklogItemStatusTriage && item.Status != models.BacklogItemStatusReady {
		return nil, ErrBacklogItemNotCommittable
	}

	task, err := createBacklogItemTask(tx, item)
	if err != nil {
		return nil, err
	}
	lineage, err := createBacklogItemTaskLineage(tx, item, task.ID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`
		UPDATE backlog_items
		SET status = $1, task_id = $2, updated_at = $3
		WHERE id = $4
	`, models.BacklogItemStatusCommitted, task.ID, now, item.ID); err != nil {
		return nil, err
	}
	item.Status = models.BacklogItemStatusCommitted
	item.TaskID = task.ID
	item.UpdatedAt = now

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &models.CommitBacklogItemResponse{BacklogItem: *item, Task: *task, Lineage: *lineage}, nil
}

func (s *BacklogItemStore) lockBacklogItemForCommit(tx *sql.Tx, id string) error {
	if !s.dialect.IsSQLite() {
		return nil
	}
	_, err := tx.Exec(`UPDATE backlog_items SET updated_at = updated_at WHERE id = $1`, id)
	return err
}

func (s *BacklogItemStore) getByIDForUpdate(tx *sql.Tx, id string) (*models.BacklogItem, error) {
	query := `SELECT ` + backlogItemColumns + ` FROM backlog_items WHERE id = $1 ` + s.dialect.ForUpdate()
	return scanBacklogItem(tx.QueryRow(query, id))
}

func scanBacklogItem(row rowScanner) (*models.BacklogItem, error) {
	var item models.BacklogItem
	var requirementID, planningRunID, backlogCandidateID, taskID sql.NullString
	var labelsRaw []byte
	err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&requirementID,
		&planningRunID,
		&backlogCandidateID,
		&taskID,
		&item.Title,
		&item.Description,
		&item.Status,
		&item.Priority,
		&item.Source,
		&item.Rank,
		&labelsRaw,
		&item.AcceptanceCriteria,
		&item.BlockedReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if requirementID.Valid {
		item.RequirementID = requirementID.String
	}
	if planningRunID.Valid {
		item.PlanningRunID = planningRunID.String
	}
	if backlogCandidateID.Valid {
		item.BacklogCandidateID = backlogCandidateID.String
	}
	if taskID.Valid {
		item.TaskID = taskID.String
	}
	item.Labels = unmarshalBacklogLabels(labelsRaw)
	return &item, nil
}

func createBacklogItemTask(tx *sql.Tx, item *models.BacklogItem) (*models.Task, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	source := "backlog:" + item.ID
	if len(source) > 80 {
		source = source[:80]
	}
	_, err := tx.Exec(`
		INSERT INTO tasks (id, project_id, title, description, status, priority, assignee, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, '', $7, $8, $9)
	`, id, item.ProjectID, item.Title, item.Description, "todo", item.Priority, source, now, now)
	if err != nil {
		return nil, err
	}
	return getTaskByID(tx, id)
}

func createBacklogItemTaskLineage(tx *sql.Tx, item *models.BacklogItem, taskID string) (*models.TaskLineage, error) {
	lineage := buildBacklogItemLineage(item, taskID, time.Now().UTC())
	_, err := tx.Exec(`
		INSERT INTO task_lineage (
			id, project_id, task_id, requirement_id, planning_run_id, backlog_candidate_id, backlog_item_id, lineage_kind, created_at
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9)
	`, lineage.ID, lineage.ProjectID, lineage.TaskID, lineage.RequirementID, lineage.PlanningRunID,
		lineage.BacklogCandidateID, lineage.BacklogItemID, lineage.LineageKind, lineage.CreatedAt)
	if err != nil {
		return nil, err
	}
	return lineage, nil
}

func buildBacklogItemLineage(item *models.BacklogItem, taskID string, createdAt time.Time) *models.TaskLineage {
	return &models.TaskLineage{
		ID:                 uuid.New().String(),
		ProjectID:          item.ProjectID,
		TaskID:             taskID,
		RequirementID:      item.RequirementID,
		PlanningRunID:      item.PlanningRunID,
		BacklogCandidateID: item.BacklogCandidateID,
		BacklogItemID:      item.ID,
		LineageKind:        models.TaskLineageKindBacklogItem,
		CreatedAt:          createdAt,
	}
}

func getTaskLineageByBacklogItemID(tx *sql.Tx, backlogItemID string) (*models.TaskLineage, error) {
	return scanTaskLineage(
		tx.QueryRow(`
			SELECT id, project_id, task_id, requirement_id, planning_run_id, backlog_candidate_id, backlog_item_id, lineage_kind, created_at
			FROM task_lineage
			WHERE backlog_item_id = $1
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		`, backlogItemID),
	)
}

func marshalBacklogLabels(values []string) ([]byte, error) {
	return json.Marshal(normalizeBacklogLabels(values))
}

func unmarshalBacklogLabels(raw []byte) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []string{}
	}
	values := []string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return normalizeBacklogLabels(values)
}

func normalizeBacklogLabels(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}
