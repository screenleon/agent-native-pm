package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type DocumentLinkStore struct {
	db *sql.DB
}

func NewDocumentLinkStore(db *sql.DB) *DocumentLinkStore {
	return &DocumentLinkStore{db: db}
}

func (s *DocumentLinkStore) Create(documentID string, req models.CreateDocumentLinkRequest) (*models.DocumentLink, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO document_links (id, document_id, code_path, link_type, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, documentID, req.CodePath, req.LinkType, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *DocumentLinkStore) GetByID(id string) (*models.DocumentLink, error) {
	var dl models.DocumentLink
	err := s.db.QueryRow(`
		SELECT id, document_id, code_path, link_type, created_at
		FROM document_links WHERE id=$1
	`, id).Scan(&dl.ID, &dl.DocumentID, &dl.CodePath, &dl.LinkType, &dl.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &dl, nil
}

func (s *DocumentLinkStore) ListByDocument(documentID string) ([]models.DocumentLink, error) {
	rows, err := s.db.Query(`
		SELECT id, document_id, code_path, link_type, created_at
		FROM document_links WHERE document_id=$1 ORDER BY created_at ASC
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []models.DocumentLink
	for rows.Next() {
		var dl models.DocumentLink
		if err := rows.Scan(&dl.ID, &dl.DocumentID, &dl.CodePath, &dl.LinkType, &dl.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, dl)
	}
	if links == nil {
		links = []models.DocumentLink{}
	}
	return links, rows.Err()
}

func (s *DocumentLinkStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM document_links WHERE id=$1`, id)
	return err
}

// FindDocumentsForFile returns document IDs whose links match a given file path.
func (s *DocumentLinkStore) FindDocumentsForFile(codePath string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT document_id FROM document_links WHERE code_path=$1
	`, codePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
