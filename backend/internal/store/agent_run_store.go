package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/screenleon/agent-native-pm/internal/models"
)

const agentRunIdempotencyConstraint = "agent_runs_idempotency_key_key"

var ErrAgentRunIdempotencyProjectMismatch = errors.New("idempotency key belongs to another project")

type AgentRunStore struct {
	db *sql.DB
}

func NewAgentRunStore(db *sql.DB) *AgentRunStore {
	return &AgentRunStore{db: db}
}

func (s *AgentRunStore) Create(projectID string, req models.CreateAgentRunRequest) (*models.AgentRun, error) {
	filesJSON, err := json.Marshal(req.FilesAffected)
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	needsReview := req.NeedsHumanReview

	ikey := req.IdempotencyKey
	if ikey == "" {
		ikey = id // use the generated ID as a default idempotency key
	}

	_, err = s.db.Exec(`
		INSERT INTO agent_runs
			(id, project_id, agent_name, action_type, status, summary, files_affected, needs_human_review, started_at, completed_at, error_message, idempotency_key, created_at)
		VALUES ($1, $2, $3, $4, 'running', $5, $6, $7, $8, NULL, '', $9, $10)
	`, id, projectID, req.AgentName, req.ActionType, req.Summary, string(filesJSON), needsReview, now, ikey, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *AgentRunStore) CreateOrGetByIdempotency(projectID string, req models.CreateAgentRunRequest) (*models.AgentRun, bool, error) {
	run, err := s.Create(projectID, req)
	if err == nil {
		return run, false, nil
	}
	if !isAgentRunIdempotencyConstraintError(err) || req.IdempotencyKey == "" {
		return nil, false, err
	}
	existing, lookupErr := s.GetByIdempotencyKey(req.IdempotencyKey)
	if lookupErr != nil {
		return nil, false, lookupErr
	}
	if existing == nil {
		return nil, false, err
	}
	if existing.ProjectID != projectID {
		return nil, false, ErrAgentRunIdempotencyProjectMismatch
	}
	return existing, true, nil
}

func isAgentRunIdempotencyConstraintError(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	return string(pqErr.Code) == "23505" && pqErr.Constraint == agentRunIdempotencyConstraint
}

func (s *AgentRunStore) Update(id string, req models.UpdateAgentRunRequest) (*models.AgentRun, error) {
	run, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, nil
	}

	status := run.Status
	if req.Status != "" {
		status = req.Status
	}
	summary := run.Summary
	if req.Summary != nil {
		summary = *req.Summary
	}
	files := run.FilesAffected
	if req.FilesAffected != nil {
		files = *req.FilesAffected
	}
	filesJSON, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}
	needsReview := run.NeedsHumanReview
	if req.NeedsHumanReview != nil {
		needsReview = *req.NeedsHumanReview
	}
	errorMessage := run.ErrorMessage
	if req.ErrorMessage != nil {
		errorMessage = *req.ErrorMessage
	}

	var completedAt interface{}
	if status == "completed" || status == "failed" {
		if run.CompletedAt != nil {
			completedAt = *run.CompletedAt
		} else {
			now := time.Now().UTC()
			completedAt = now
		}
	} else {
		completedAt = nil
	}

	_, err = s.db.Exec(`
		UPDATE agent_runs
		SET status=$1, summary=$2, files_affected=$3, needs_human_review=$4, completed_at=$5, error_message=$6
		WHERE id=$7
	`, status, summary, string(filesJSON), needsReview, completedAt, errorMessage, id)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

func (s *AgentRunStore) GetByID(id string) (*models.AgentRun, error) {
	var r models.AgentRun
	var filesJSON string
	var ikey sql.NullString
	var needsReview bool

	err := s.db.QueryRow(`
		SELECT id, project_id, agent_name, action_type, status, summary, files_affected,
		       needs_human_review, started_at, completed_at, error_message, idempotency_key, created_at
		FROM agent_runs WHERE id=$1
	`, id).Scan(&r.ID, &r.ProjectID, &r.AgentName, &r.ActionType, &r.Status, &r.Summary,
		&filesJSON, &needsReview, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage, &ikey, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.NeedsHumanReview = needsReview
	if ikey.Valid {
		r.IdempotencyKey = ikey.String
	}
	if err := json.Unmarshal([]byte(filesJSON), &r.FilesAffected); err != nil {
		r.FilesAffected = []string{}
	}
	return &r, nil
}

func (s *AgentRunStore) GetByIdempotencyKey(idempotencyKey string) (*models.AgentRun, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM agent_runs WHERE idempotency_key=$1`, idempotencyKey).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *AgentRunStore) ListByProject(projectID string, page, perPage int) ([]models.AgentRun, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_runs WHERE project_id=$1`, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, agent_name, action_type, status, summary, files_affected,
		       needs_human_review, started_at, completed_at, error_message, idempotency_key, created_at
		FROM agent_runs WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`, projectID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var runs []models.AgentRun
	for rows.Next() {
		var r models.AgentRun
		var filesJSON string
		var ikey sql.NullString
		var needsReview bool
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.AgentName, &r.ActionType, &r.Status, &r.Summary,
			&filesJSON, &needsReview, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage, &ikey, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		r.NeedsHumanReview = needsReview
		if ikey.Valid {
			r.IdempotencyKey = ikey.String
		}
		if err := json.Unmarshal([]byte(filesJSON), &r.FilesAffected); err != nil {
			r.FilesAffected = []string{}
		}
		runs = append(runs, r)
	}
	if runs == nil {
		runs = []models.AgentRun{}
	}
	return runs, total, rows.Err()
}

func (s *AgentRunStore) ListRecentByProject(projectID string, limit int) ([]models.AgentRun, error) {
	if limit < 1 {
		limit = 5
	}

	rows, err := s.db.Query(`
		SELECT id, project_id, agent_name, action_type, status, summary, files_affected,
		       needs_human_review, started_at, completed_at, error_message, idempotency_key, created_at
		FROM agent_runs
		WHERE project_id=$1
		ORDER BY created_at DESC
		LIMIT $2
	`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []models.AgentRun
	for rows.Next() {
		var r models.AgentRun
		var filesJSON string
		var ikey sql.NullString
		var needsReview bool
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.AgentName, &r.ActionType, &r.Status, &r.Summary,
			&filesJSON, &needsReview, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage, &ikey, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.NeedsHumanReview = needsReview
		if ikey.Valid {
			r.IdempotencyKey = ikey.String
		}
		if err := json.Unmarshal([]byte(filesJSON), &r.FilesAffected); err != nil {
			r.FilesAffected = []string{}
		}
		runs = append(runs, r)
	}
	if runs == nil {
		runs = []models.AgentRun{}
	}
	return runs, rows.Err()
}
