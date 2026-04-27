package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// ContextSnapshot represents one row in the planning_context_snapshots table.
// Snapshot and DroppedCounts are JSON blobs stored as TEXT (SQLite-compatible).
type ContextSnapshot struct {
	ID            string    `json:"id"`
	PackID        string    `json:"pack_id"`
	PlanningRunID string    `json:"planning_run_id"`
	SchemaVersion string    `json:"schema_version"`
	Snapshot      string    `json:"snapshot"`       // JSON blob of PlanningContextV2
	SourcesBytes  int       `json:"sources_bytes"`
	DroppedCounts string    `json:"dropped_counts"` // JSON blob e.g. {"tasks":2}
	CreatedAt     time.Time `json:"created_at"`
}

// ContextSnapshotStore persists and retrieves planning context snapshots.
type ContextSnapshotStore struct {
	db *sql.DB
}

// NewContextSnapshotStore returns a new store backed by db.
func NewContextSnapshotStore(db *sql.DB) *ContextSnapshotStore {
	return &ContextSnapshotStore{db: db}
}

// Save persists snap to the planning_context_snapshots table.
// If snap.ID is empty a new UUID is generated.
// Returns error on any database failure.
func (s *ContextSnapshotStore) Save(snap ContextSnapshot) error {
	if snap.ID == "" {
		snap.ID = uuid.New().String()
	}
	if snap.SchemaVersion == "" {
		snap.SchemaVersion = "context.v2"
	}
	if snap.DroppedCounts == "" {
		snap.DroppedCounts = "{}"
	}
	_, err := s.db.Exec(`
		INSERT INTO planning_context_snapshots
		    (id, pack_id, planning_run_id, schema_version, snapshot, sources_bytes, dropped_counts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, snap.ID, snap.PackID, snap.PlanningRunID, snap.SchemaVersion, snap.Snapshot, snap.SourcesBytes, snap.DroppedCounts, time.Now().UTC())
	return err
}

// GetByRunID returns the snapshot for the given planning run, or nil if none
// exists. Returns an error on any database failure other than "not found".
func (s *ContextSnapshotStore) GetByRunID(planningRunID string) (*ContextSnapshot, error) {
	row := s.db.QueryRow(`
		SELECT id, pack_id, planning_run_id, schema_version, snapshot, sources_bytes, dropped_counts, created_at
		FROM planning_context_snapshots
		WHERE planning_run_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, planningRunID)

	var snap ContextSnapshot
	err := row.Scan(
		&snap.ID,
		&snap.PackID,
		&snap.PlanningRunID,
		&snap.SchemaVersion,
		&snap.Snapshot,
		&snap.SourcesBytes,
		&snap.DroppedCounts,
		&snap.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snap, nil
}
