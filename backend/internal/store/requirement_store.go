package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type RequirementStore struct {
	db *sql.DB
}

func NewRequirementStore(db *sql.DB) *RequirementStore {
	return &RequirementStore{db: db}
}

func (s *RequirementStore) ListByProject(projectID string, page, perPage int) ([]models.Requirement, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE project_id = $1`, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, title, summary, description, status, source, created_at, updated_at
		FROM requirements
		WHERE project_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, projectID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var requirements []models.Requirement
	for rows.Next() {
		var requirement models.Requirement
		if err := rows.Scan(
			&requirement.ID,
			&requirement.ProjectID,
			&requirement.Title,
			&requirement.Summary,
			&requirement.Description,
			&requirement.Status,
			&requirement.Source,
			&requirement.CreatedAt,
			&requirement.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		requirements = append(requirements, requirement)
	}

	return requirements, total, rows.Err()
}

func (s *RequirementStore) GetByID(id string) (*models.Requirement, error) {
	var requirement models.Requirement
	err := s.db.QueryRow(`
		SELECT id, project_id, title, summary, description, status, source, created_at, updated_at
		FROM requirements
		WHERE id = $1
	`, id).Scan(
		&requirement.ID,
		&requirement.ProjectID,
		&requirement.Title,
		&requirement.Summary,
		&requirement.Description,
		&requirement.Status,
		&requirement.Source,
		&requirement.CreatedAt,
		&requirement.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &requirement, nil
}

func (s *RequirementStore) Create(projectID string, req models.CreateRequirementRequest) (*models.Requirement, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "human"
	}

	_, err := s.db.Exec(`
		INSERT INTO requirements (id, project_id, title, summary, description, status, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, projectID, strings.TrimSpace(req.Title), strings.TrimSpace(req.Summary), strings.TrimSpace(req.Description), models.RequirementStatusDraft, source, now, now)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

// UpdateStatus sets a requirement's status and updates updated_at.
func (s *RequirementStore) UpdateStatus(id, status string) (*models.Requirement, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE requirements SET status=$1, updated_at=$2 WHERE id=$3`, status, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

// PromoteToPlannedIfDraft transitions draft → planned; no-op if already planned/archived.
func (s *RequirementStore) PromoteToPlannedIfDraft(id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE requirements SET status='planned', updated_at=$1
		WHERE id=$2 AND status='draft'
	`, now, id)
	return err
}
