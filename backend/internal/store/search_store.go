package store

import (
	"database/sql"
	"fmt"

	"github.com/screenleon/agent-native-pm/internal/models"
)

type SearchStore struct {
	db *sql.DB
}

func NewSearchStore(db *sql.DB) *SearchStore {
	return &SearchStore{db: db}
}

// Search performs a full-text search across tasks and documents for a given project.
// Optional filters:
// - searchType: all | tasks | documents
// - taskStatus: todo | in_progress | done | cancelled
// - docType: api | architecture | guide | adr | general
// - staleOnly: true=only stale, false=only fresh, nil=all
func (s *SearchStore) Search(
	query string,
	projectID string,
	searchType string,
	taskStatus string,
	docType string,
	staleOnly *bool,
) (*models.SearchResult, error) {
	result := &models.SearchResult{
		Tasks:     []models.Task{},
		Documents: []models.Document{},
	}

	if searchType != "documents" {
		rows, err := s.searchTasks(query, projectID, taskStatus)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var t models.Task
			if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description,
				&t.Status, &t.Priority, &t.Assignee, &t.Source, &t.CreatedAt, &t.UpdatedAt); err != nil {
				return nil, err
			}
			result.Tasks = append(result.Tasks, t)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if searchType != "tasks" {
		rows, err := s.searchDocuments(query, projectID, docType, staleOnly)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var d models.Document
			var lastUpdated sql.NullTime
			var isStale bool
			if err := rows.Scan(&d.ID, &d.ProjectID, &d.Title, &d.FilePath, &d.DocType,
				&lastUpdated, &d.StalenessDays, &isStale, &d.Source, &d.CreatedAt, &d.UpdatedAt); err != nil {
				return nil, err
			}
			d.IsStale = isStale
			if lastUpdated.Valid {
				d.LastUpdatedAt = &lastUpdated.Time
			}
			result.Documents = append(result.Documents, d)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (s *SearchStore) searchTasks(query, projectID, taskStatus string) (*sql.Rows, error) {
	args := []interface{}{query}
	where := "to_tsvector('english', t.title || ' ' || COALESCE(t.description, '')) @@ plainto_tsquery('english', $1)"

	if projectID != "" {
		args = append(args, projectID)
		where += fmt.Sprintf(" AND t.project_id = $%d", len(args))
	}
	if taskStatus != "" {
		args = append(args, taskStatus)
		where += fmt.Sprintf(" AND t.status = $%d", len(args))
	}

	querySQL := fmt.Sprintf(`
		SELECT t.id, t.project_id, t.title, t.description, t.status, t.priority,
		       t.assignee, t.source, t.created_at, t.updated_at
		FROM tasks t
		WHERE %s
		ORDER BY ts_rank_cd(to_tsvector('english', t.title || ' ' || COALESCE(t.description, '')),
			plainto_tsquery('english', $1)) DESC,
			t.created_at DESC
		LIMIT 50
	`, where)

	return s.db.Query(querySQL, args...)
}

func (s *SearchStore) searchDocuments(query, projectID, docType string, staleOnly *bool) (*sql.Rows, error) {
	args := []interface{}{query}
	where := "to_tsvector('english', d.title || ' ' || COALESCE(d.description, '')) @@ plainto_tsquery('english', $1)"

	if projectID != "" {
		args = append(args, projectID)
		where += fmt.Sprintf(" AND d.project_id = $%d", len(args))
	}
	if docType != "" {
		args = append(args, docType)
		where += fmt.Sprintf(" AND d.doc_type = $%d", len(args))
	}
	if staleOnly != nil {
		args = append(args, *staleOnly)
		where += fmt.Sprintf(" AND d.is_stale = $%d", len(args))
	}

	querySQL := fmt.Sprintf(`
		SELECT d.id, d.project_id, d.title, d.file_path, d.doc_type,
		       d.last_updated_at, d.staleness_days, d.is_stale, d.source, d.created_at, d.updated_at
		FROM documents d
		WHERE %s
		ORDER BY ts_rank_cd(to_tsvector('english', d.title || ' ' || COALESCE(d.description, '')),
			plainto_tsquery('english', $1)) DESC,
			d.created_at DESC
		LIMIT 50
	`, where)

	return s.db.Query(querySQL, args...)
}
