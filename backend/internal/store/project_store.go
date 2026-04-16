package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type ProjectStore struct {
	db *sql.DB
}

func NewProjectStore(db *sql.DB) *ProjectStore {
	return &ProjectStore{db: db}
}

func (s *ProjectStore) List() ([]models.Project, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, repo_url, repo_path, default_branch, last_sync_at, created_at, updated_at
		FROM projects ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.RepoURL, &p.RepoPath, &p.DefaultBranch, &p.LastSyncAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *ProjectStore) GetByID(id string) (*models.Project, error) {
	var p models.Project
	err := s.db.QueryRow(`
		SELECT id, name, description, repo_url, repo_path, default_branch, last_sync_at, created_at, updated_at
		FROM projects WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.RepoURL, &p.RepoPath, &p.DefaultBranch, &p.LastSyncAt, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProjectStore) Create(req models.CreateProjectRequest) (*models.Project, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	branch := req.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	_, err := s.db.Exec(`
		INSERT INTO projects (id, name, description, repo_url, repo_path, default_branch, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, req.Name, req.Description, req.RepoURL, req.RepoPath, branch, now, now)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

func (s *ProjectStore) Update(id string, req models.UpdateProjectRequest) (*models.Project, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", pos))
		args = append(args, *req.Name)
		pos++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", pos))
		args = append(args, *req.Description)
		pos++
	}
	if req.RepoURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("repo_url = $%d", pos))
		args = append(args, *req.RepoURL)
		pos++
	}
	if req.RepoPath != nil {
		setClauses = append(setClauses, fmt.Sprintf("repo_path = $%d", pos))
		args = append(args, *req.RepoPath)
		pos++
	}
	if req.DefaultBranch != nil {
		setClauses = append(setClauses, fmt.Sprintf("default_branch = $%d", pos))
		args = append(args, *req.DefaultBranch)
		pos++
	}

	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	query := fmt.Sprintf("UPDATE projects SET %s WHERE id = $%d", strings.Join(setClauses, ", "), pos)
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

func (s *ProjectStore) Delete(id string) error {
	_, err := s.db.Exec("DELETE FROM projects WHERE id = $1", id)
	return err
}

func (s *ProjectStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&count)
	return count, err
}

// UpdateLastSyncAt stamps the project with the current time.
func (s *ProjectStore) UpdateLastSyncAt(id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE projects SET last_sync_at=$1, updated_at=$2 WHERE id=$3`, now, now, id)
	return err
}
