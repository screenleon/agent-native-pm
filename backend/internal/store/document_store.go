package store

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type DocumentStore struct {
	db *sql.DB
}

func NewDocumentStore(db *sql.DB) *DocumentStore {
	return &DocumentStore{db: db}
}

func (s *DocumentStore) ListByProject(projectID string, page, perPage int) ([]models.Document, int, error) {
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents WHERE project_id = $1", projectID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.Query(`
		SELECT id, project_id, title, file_path, doc_type, last_updated_at, staleness_days, is_stale, source, created_at, updated_at
		FROM documents WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`, projectID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var docs []models.Document
	for rows.Next() {
		var d models.Document
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Title, &d.FilePath, &d.DocType, &d.LastUpdatedAt, &d.StalenessDays, &d.IsStale, &d.Source, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, err
		}
		docs = append(docs, d)
	}
	return docs, total, rows.Err()
}

func (s *DocumentStore) GetByID(id string) (*models.Document, error) {
	var d models.Document
	err := s.db.QueryRow(`
		SELECT id, project_id, title, file_path, doc_type, last_updated_at, staleness_days, is_stale, source, created_at, updated_at
		FROM documents WHERE id = $1
	`, id).Scan(&d.ID, &d.ProjectID, &d.Title, &d.FilePath, &d.DocType, &d.LastUpdatedAt, &d.StalenessDays, &d.IsStale, &d.Source, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *DocumentStore) Create(projectID string, req models.CreateDocumentRequest) (*models.Document, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	docType := req.DocType
	if docType == "" {
		docType = "general"
	}

	_, err := s.db.Exec(`
		INSERT INTO documents (id, project_id, title, file_path, doc_type, last_updated_at, source, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, projectID, req.Title, req.FilePath, docType, now, req.Source, now, now)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

func (s *DocumentStore) Update(id string, req models.UpdateDocumentRequest) (*models.Document, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", pos))
		args = append(args, *req.Title)
		pos++
	}
	if req.FilePath != nil {
		setClauses = append(setClauses, fmt.Sprintf("file_path = $%d", pos))
		args = append(args, *req.FilePath)
		pos++
	}
	if req.DocType != nil {
		setClauses = append(setClauses, fmt.Sprintf("doc_type = $%d", pos))
		args = append(args, *req.DocType)
		pos++
	}
	if req.Source != nil {
		setClauses = append(setClauses, fmt.Sprintf("source = $%d", pos))
		args = append(args, *req.Source)
		pos++
	}

	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", pos))
	args = append(args, time.Now().UTC())
	pos++
	args = append(args, id)

	query := fmt.Sprintf("UPDATE documents SET %s WHERE id = $%d", strings.Join(setClauses, ", "), pos)
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

func (s *DocumentStore) Delete(id string) error {
	_, err := s.db.Exec("DELETE FROM documents WHERE id = $1", id)
	return err
}

func (s *DocumentStore) CountByProject(projectID string) (int, int, error) {
	var total, stale int
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents WHERE project_id = $1", projectID).Scan(&total)
	if err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM documents WHERE project_id = $1 AND is_stale = TRUE", projectID).Scan(&stale)
	if err != nil {
		return 0, 0, err
	}
	return total, stale, nil
}

// FindIDsByProjectAndFilePath finds documents in a project whose registered file_path matches the given path.
func (s *DocumentStore) FindIDsByProjectAndFilePath(projectID, filePath string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM documents WHERE project_id=$1 AND file_path=$2`, projectID, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FindTimeDecayedDocumentIDs returns document IDs that have not been refreshed within thresholdDays.
func (s *DocumentStore) FindTimeDecayedDocumentIDs(projectID string, thresholdDays int) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT id, last_updated_at, created_at
		FROM documents
		WHERE project_id = $1
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	ids := []string{}
	for rows.Next() {
		var id string
		var lastUpdated sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &lastUpdated, &createdAt); err != nil {
			return nil, err
		}
		reference := createdAt
		if lastUpdated.Valid {
			reference = lastUpdated.Time
		}
		days := int(math.Floor(now.Sub(reference).Hours() / 24))
		if days > thresholdDays {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// MarkStale sets is_stale=1 on a document.
func (s *DocumentStore) MarkStale(id string) error {
	now := time.Now().UTC()
	var lastUpdated sql.NullTime
	err := s.db.QueryRow(`SELECT last_updated_at FROM documents WHERE id=$1`, id).Scan(&lastUpdated)
	if err != nil {
		return err
	}

	stalenessDays := 0
	if lastUpdated.Valid {
		days := int(math.Floor(now.Sub(lastUpdated.Time).Hours() / 24))
		if days > 0 {
			stalenessDays = days
		}
	}

	_, err = s.db.Exec(`UPDATE documents SET is_stale=TRUE, staleness_days=$1, updated_at=$2 WHERE id=$3`, stalenessDays, now, id)
	return err
}

// RefreshSummary updates a document's last_updated_at and clears staleness.
func (s *DocumentStore) RefreshSummary(id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE documents SET last_updated_at=$1, staleness_days=0, is_stale=FALSE, updated_at=$2 WHERE id=$3`,
		now, now, id)
	return err
}
