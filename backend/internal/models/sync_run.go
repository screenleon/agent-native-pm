package models

import "time"

// SyncRun represents a single repository scan operation.
type SyncRun struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	Status         string     `json:"status"` // running | completed | failed
	CommitsScanned int        `json:"commits_scanned"`
	FilesChanged   int        `json:"files_changed"`
	ErrorMessage   string     `json:"error_message,omitempty"`
}

var ValidSyncRunStatuses = map[string]bool{
	"running":   true,
	"completed": true,
	"failed":    true,
}
