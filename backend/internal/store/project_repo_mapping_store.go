package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type ProjectRepoMappingStore struct {
	db *sql.DB
}

func NewProjectRepoMappingStore(db *sql.DB) *ProjectRepoMappingStore {
	return &ProjectRepoMappingStore{db: db}
}

func (s *ProjectRepoMappingStore) Create(projectID string, req models.CreateProjectRepoMappingRequest) (*models.ProjectRepoMapping, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	alias := strings.TrimSpace(req.Alias)
	branch := strings.TrimSpace(req.DefaultBranch)
	repoPath := strings.TrimSpace(req.RepoPath)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM project_repo_mappings WHERE project_id=$1`, projectID).Scan(&count); err != nil {
		return nil, err
	}
	isPrimary := req.IsPrimary || count == 0
	if isPrimary {
		if _, err := tx.Exec(`UPDATE project_repo_mappings SET is_primary=FALSE, updated_at=$1 WHERE project_id=$2`, now, projectID); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO project_repo_mappings (id, project_id, alias, repo_path, default_branch, is_primary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, projectID, alias, repoPath, branch, isPrimary, now, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *ProjectRepoMappingStore) GetByID(id string) (*models.ProjectRepoMapping, error) {
	var mapping models.ProjectRepoMapping
	err := s.db.QueryRow(`
		SELECT id, project_id, alias, repo_path, default_branch, is_primary, created_at, updated_at
		FROM project_repo_mappings WHERE id=$1
	`, id).Scan(&mapping.ID, &mapping.ProjectID, &mapping.Alias, &mapping.RepoPath, &mapping.DefaultBranch, &mapping.IsPrimary, &mapping.CreatedAt, &mapping.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

func (s *ProjectRepoMappingStore) ListByProject(projectID string) ([]models.ProjectRepoMapping, error) {
	rows, err := s.db.Query(`
		SELECT id, project_id, alias, repo_path, default_branch, is_primary, created_at, updated_at
		FROM project_repo_mappings
		WHERE project_id=$1
		ORDER BY is_primary DESC, created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []models.ProjectRepoMapping
	for rows.Next() {
		var mapping models.ProjectRepoMapping
		if err := rows.Scan(&mapping.ID, &mapping.ProjectID, &mapping.Alias, &mapping.RepoPath, &mapping.DefaultBranch, &mapping.IsPrimary, &mapping.CreatedAt, &mapping.UpdatedAt); err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	if mappings == nil {
		mappings = []models.ProjectRepoMapping{}
	}
	return mappings, rows.Err()
}

func (s *ProjectRepoMappingStore) Update(id string, req models.UpdateProjectRepoMappingRequest) (*models.ProjectRepoMapping, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.DefaultBranch != nil {
		setClauses = append(setClauses, fmt.Sprintf("default_branch = $%d", pos))
		args = append(args, strings.TrimSpace(*req.DefaultBranch))
		pos++
	}

	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	query := fmt.Sprintf("UPDATE project_repo_mappings SET %s WHERE id = $%d", strings.Join(setClauses, ", "), pos)
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

func (s *ProjectRepoMappingStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM project_repo_mappings WHERE id=$1`, id)
	return err
}

func (s *ProjectRepoMappingStore) PromoteFirstRemaining(projectID string) (*models.ProjectRepoMapping, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var existingPrimaryID string
	err = tx.QueryRow(`SELECT id FROM project_repo_mappings WHERE project_id=$1 AND is_primary=TRUE LIMIT 1`, projectID).Scan(&existingPrimaryID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetByID(existingPrimaryID)
	}

	var nextID string
	err = tx.QueryRow(`SELECT id FROM project_repo_mappings WHERE project_id=$1 ORDER BY created_at ASC LIMIT 1`, projectID).Scan(&nextID)
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE project_repo_mappings SET is_primary=TRUE, updated_at=$1 WHERE id=$2`, now, nextID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetByID(nextID)
}
