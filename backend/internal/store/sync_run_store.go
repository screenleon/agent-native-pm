package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type SyncRunStore struct {
	db *sql.DB
}

func NewSyncRunStore(db *sql.DB) *SyncRunStore {
	return &SyncRunStore{db: db}
}

func (s *SyncRunStore) Create(projectID string) (*models.SyncRun, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO sync_runs (id, project_id, started_at, status)
		VALUES ($1, $2, $3, 'running')
	`, id, projectID, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *SyncRunStore) Complete(id string, commitsScanned, filesChanged int) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE sync_runs SET status='completed', completed_at=$1, commits_scanned=$2, files_changed=$3
		WHERE id=$4
	`, now, commitsScanned, filesChanged, id)
	return err
}

func (s *SyncRunStore) Fail(id string, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE sync_runs SET status='failed', completed_at=$1, error_message=$2
		WHERE id=$3
	`, now, errMsg, id)
	return err
}

func (s *SyncRunStore) GetByID(id string) (*models.SyncRun, error) {
	var run models.SyncRun
	var completedAt sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, project_id, started_at, completed_at, status, commits_scanned, files_changed, error_message
		FROM sync_runs WHERE id=$1
	`, id).Scan(&run.ID, &run.ProjectID, &run.StartedAt, &completedAt,
		&run.Status, &run.CommitsScanned, &run.FilesChanged, &run.ErrorMessage)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return &run, nil
}

func (s *SyncRunStore) GetLatestByProject(projectID string) (*models.SyncRun, error) {
	var run models.SyncRun
	var completedAt sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, project_id, started_at, completed_at, status, commits_scanned, files_changed, error_message
		FROM sync_runs
		WHERE project_id=$1
		ORDER BY started_at DESC
		LIMIT 1
	`, projectID).Scan(&run.ID, &run.ProjectID, &run.StartedAt, &completedAt,
		&run.Status, &run.CommitsScanned, &run.FilesChanged, &run.ErrorMessage)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return &run, nil
}

func (s *SyncRunStore) ListByProject(projectID string, page, perPage int) ([]models.SyncRun, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_runs WHERE project_id=$1`, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, started_at, completed_at, status, commits_scanned, files_changed, error_message
		FROM sync_runs WHERE project_id=$1 ORDER BY started_at DESC LIMIT $2 OFFSET $3
	`, projectID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var runs []models.SyncRun
	for rows.Next() {
		var r models.SyncRun
		var ca sql.NullTime
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.StartedAt, &ca,
			&r.Status, &r.CommitsScanned, &r.FilesChanged, &r.ErrorMessage); err != nil {
			return nil, 0, err
		}
		if ca.Valid {
			r.CompletedAt = &ca.Time
		}
		runs = append(runs, r)
	}
	if runs == nil {
		runs = []models.SyncRun{}
	}
	return runs, total, rows.Err()
}
