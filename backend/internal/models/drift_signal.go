package models

import "time"

// ChangedFileMeta holds per-file info captured during a sync scan.
type ChangedFileMeta struct {
	Path       string `json:"path"`
	ChangeType string `json:"change_type"` // M | A | D | R
}

// TriggerMeta is the structured payload stored in trigger_meta (JSONB).
// It replaces the old free-text parsing approach and enables sorting / false-positive control.
type TriggerMeta struct {
	// code_change fields
	ChangedFiles []ChangedFileMeta `json:"changed_files,omitempty"`
	Confidence   string            `json:"confidence,omitempty"` // "high" | "medium" | "low"

	// time_decay fields
	DaysStale int `json:"days_stale,omitempty"`
}

// DriftSignal represents a detected mismatch between a document and its linked code.
type DriftSignal struct {
	ID            string       `json:"id"`
	ProjectID     string       `json:"project_id"`
	DocumentID    string       `json:"document_id"`
	DocumentTitle string       `json:"document_title,omitempty"` // joined field
	TriggerType   string       `json:"trigger_type"`             // code_change | time_decay | manual
	TriggerDetail string       `json:"trigger_detail"`
	TriggerMeta   *TriggerMeta `json:"trigger_meta,omitempty"`
	// Severity: 1=low, 2=medium, 3=high — used for sorting and triage ordering.
	Severity   int        `json:"severity"`
	SyncRunID  string     `json:"sync_run_id,omitempty"`
	Status     string     `json:"status"` // open | resolved | dismissed
	ResolvedBy string     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type UpdateDriftSignalRequest struct {
	Status     *string `json:"status,omitempty"`
	ResolvedBy *string `json:"resolved_by,omitempty"`
}

type CreateDriftSignalRequest struct {
	DocumentID    string       `json:"document_id"`
	TriggerType   string       `json:"trigger_type"`
	TriggerDetail string       `json:"trigger_detail"`
	TriggerMeta   *TriggerMeta `json:"trigger_meta,omitempty"`
	// Severity: 1=low, 2=medium, 3=high. Defaults to 1 when omitted.
	Severity  int    `json:"severity,omitempty"`
	SyncRunID string `json:"sync_run_id,omitempty"`
}

var ValidDriftSignalStatuses = map[string]bool{
	"open":      true,
	"resolved":  true,
	"dismissed": true,
}

var ValidTriggerTypes = map[string]bool{
	"code_change": true,
	"time_decay":  true,
	"manual":      true,
}

// ValidSortByDrift is the allowlist for sort_by query params.
var ValidSortByDrift = map[string]string{
	"severity":   "ds.severity DESC, ds.created_at DESC",
	"created_at": "ds.created_at DESC",
}
