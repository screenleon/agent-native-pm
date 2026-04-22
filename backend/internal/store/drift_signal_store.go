package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/screenleon/agent-native-pm/internal/models"
)

type DriftSignalStore struct {
	db *sql.DB
}

func NewDriftSignalStore(db *sql.DB) *DriftSignalStore {
	return &DriftSignalStore{db: db}
}

// marshalMeta serialises TriggerMeta to a JSON []byte; returns "{}" on nil.
func marshalMeta(m *models.TriggerMeta) []byte {
	if m == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// scanMeta deserialises JSON bytes from the DB into a *TriggerMeta.
// Returns nil (not an error) when the value is an empty object.
func scanMeta(raw []byte) *models.TriggerMeta {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	var m models.TriggerMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

func (s *DriftSignalStore) Create(projectID string, req models.CreateDriftSignalRequest) (*models.DriftSignal, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	severity := req.Severity
	if severity < 1 {
		severity = 1
	}

	metaJSON := marshalMeta(req.TriggerMeta)

	syncRunID := sql.NullString{String: req.SyncRunID, Valid: req.SyncRunID != ""}

	_, err := s.db.Exec(`
		INSERT INTO drift_signals
		  (id, project_id, document_id, trigger_type, trigger_detail, trigger_meta, severity, sync_run_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'open', $9)
	`, id, projectID, req.DocumentID, req.TriggerType, req.TriggerDetail, metaJSON, severity, syncRunID, now)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

const selectDriftCols = `
	SELECT ds.id, ds.project_id, ds.document_id, COALESCE(d.title,'') AS doc_title,
	       ds.trigger_type, ds.trigger_detail, ds.trigger_meta, ds.severity,
	       COALESCE(ds.sync_run_id,'') AS sync_run_id,
	       ds.status, ds.resolved_by, ds.resolved_at, ds.created_at
	FROM drift_signals ds
	LEFT JOIN documents d ON d.id = ds.document_id`

func scanDrift(row interface {
	Scan(...interface{}) error
}) (*models.DriftSignal, error) {
	var ds models.DriftSignal
	var resolvedAt sql.NullTime
	var resolvedBy sql.NullString
	var metaRaw []byte
	err := row.Scan(
		&ds.ID, &ds.ProjectID, &ds.DocumentID, &ds.DocumentTitle,
		&ds.TriggerType, &ds.TriggerDetail, &metaRaw, &ds.Severity,
		&ds.SyncRunID, &ds.Status,
		&resolvedBy, &resolvedAt, &ds.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		ds.ResolvedAt = &resolvedAt.Time
	}
	if resolvedBy.Valid {
		ds.ResolvedBy = resolvedBy.String
	}
	ds.TriggerMeta = scanMeta(metaRaw)
	return &ds, nil
}

func (s *DriftSignalStore) GetByID(id string) (*models.DriftSignal, error) {
	row := s.db.QueryRow(selectDriftCols+` WHERE ds.id=$1`, id)
	ds, err := scanDrift(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ds, err
}

// ListByProject lists drift signals for a project.
// statusFilter: "open"|"resolved"|"dismissed"|"" (all)
// sortBy: "severity"|"created_at"|"" (defaults to "created_at")
func (s *DriftSignalStore) ListByProject(projectID, statusFilter, sortBy string, page, perPage int) ([]models.DriftSignal, int, error) {
	// --- total count ---
	countArgs := []interface{}{projectID}
	countQ := `SELECT COUNT(*) FROM drift_signals WHERE project_id=$1`
	if statusFilter != "" {
		countQ += ` AND status=$2`
		countArgs = append(countArgs, statusFilter)
	}
	var total int
	if err := s.db.QueryRow(countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// --- resolve ORDER BY clause ---
	orderClause, ok := models.ValidSortByDrift[sortBy]
	if !ok {
		orderClause = models.ValidSortByDrift["created_at"]
	}

	// --- main query ---
	offset := (page - 1) * perPage
	base := selectDriftCols + ` WHERE ds.project_id=$1`
	var query string
	var args []interface{}

	if statusFilter != "" {
		query = base + ` AND ds.status=$2 ORDER BY ` + orderClause + ` LIMIT $3 OFFSET $4`
		args = []interface{}{projectID, statusFilter, perPage, offset}
	} else {
		query = base + ` ORDER BY ` + orderClause + ` LIMIT $2 OFFSET $3`
		args = []interface{}{projectID, perPage, offset}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var signals []models.DriftSignal
	for rows.Next() {
		ds, err := scanDrift(rows)
		if err != nil {
			return nil, 0, err
		}
		signals = append(signals, *ds)
	}
	if signals == nil {
		signals = []models.DriftSignal{}
	}
	return signals, total, rows.Err()
}

func (s *DriftSignalStore) Update(id string, req models.UpdateDriftSignalRequest) (*models.DriftSignal, error) {
	var setClauses []string
	var args []interface{}
	pos := 1

	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status=$%d", pos))
		args = append(args, *req.Status)
		pos++

		if *req.Status == "resolved" || *req.Status == "dismissed" {
			now := time.Now().UTC()
			setClauses = append(setClauses, fmt.Sprintf("resolved_at=$%d", pos))
			args = append(args, now)
			pos++
		}
	}
	if req.ResolvedBy != nil {
		setClauses = append(setClauses, fmt.Sprintf("resolved_by=$%d", pos))
		args = append(args, *req.ResolvedBy)
		pos++
	}

	if len(setClauses) == 0 {
		return s.GetByID(id)
	}

	query := "UPDATE drift_signals SET " + strings.Join(setClauses, ", ") + fmt.Sprintf(" WHERE id=$%d", pos)
	args = append(args, id)
	if _, err := s.db.Exec(query, args...); err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *DriftSignalStore) CountOpenByProject(projectID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM drift_signals WHERE project_id=$1 AND status='open'`, projectID).Scan(&count)
	return count, err
}

// BulkResolveByProject resolves all open drift signals for a project and returns the count resolved.
func (s *DriftSignalStore) BulkResolveByProject(projectID, resolvedBy string) (int, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		UPDATE drift_signals
		SET status='resolved', resolved_by=$1, resolved_at=$2
		WHERE project_id=$3 AND status='open'
	`, resolvedBy, now, projectID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// ResolveOpenByDocumentID resolves all open drift signals for a specific document and returns the count resolved.
func (s *DriftSignalStore) ResolveOpenByDocumentID(documentID, resolvedBy string) (int, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		UPDATE drift_signals
		SET status='resolved', resolved_by=$1, resolved_at=$2
		WHERE document_id=$3 AND status='open'
	`, resolvedBy, now, documentID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// ListOpenDocumentIDsByProject returns a set of document IDs that currently have open drift signals.
func (s *DriftSignalStore) ListOpenDocumentIDsByProject(projectID string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT document_id FROM drift_signals WHERE project_id=$1 AND status='open'`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docIDs := map[string]bool{}
	for rows.Next() {
		var documentID string
		if err := rows.Scan(&documentID); err != nil {
			return nil, err
		}
		docIDs[documentID] = true
	}
	return docIDs, rows.Err()
}
