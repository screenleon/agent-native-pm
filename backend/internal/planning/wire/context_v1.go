// Package wire defines the pure DTO types, sanitizer, and byte-cap reducer
// used to serialize planning context to local connector adapters over the
// `context.v1` wire contract.
//
// This package is a leaf: it imports only the Go standard library. It is
// intentionally decoupled from internal/models and internal/planning so that
// both the server and the connector binary can import it without pulling in
// server-only code and without creating an import cycle with internal/models
// (which references wire types in its local connector response struct).
//
// See docs/local-connector-context.md for the full contract specification.
package wire

import "time"

// Schema and version constants.
const (
	ContextSchemaV1  = "context.v1"
	SanitizerVersion = "v1"
	GeneratedByServer = "server"
)

// Default per-source limits for Phase A. These mirror the constants already
// used by planning.ProjectContextBuilder.Build so Phase A cannot regress
// existing server-provider behavior.
const (
	DefaultMaxOpenTasks         = 100
	DefaultMaxRecentDocuments   = 8
	DefaultMaxOpenDriftSignals  = 6
	DefaultMaxRecentAgentRuns   = 6
	DefaultIncludeLatestSyncRun = true
	DefaultMaxSourcesBytes      = 256 * 1024 // 256 KiB
)

// Truncation caps for sanitized free-form fields.
const (
	MaxAgentRunSummaryChars   = 180
	MaxSyncRunErrorChars      = 240
)

// Ranking taxonomy values (wire-stable; renaming internal Go funcs must not
// change these strings).
const (
	RankingDocuments    = "relevance_v1"
	RankingTasks        = "updated_at_desc"
	RankingDriftSignals = "severity_desc_then_opened_at_desc"
	RankingAgentRuns    = "started_at_desc_excluding_self_planner"
)

// PlanningContextV1 is the serialized planning context delivered to local
// connector adapters. Field names, JSON tags, and types are part of the
// public wire contract. Adding fields is non-breaking; renaming or removing
// requires a new schema version.
type PlanningContextV1 struct {
	SchemaVersion    string                 `json:"schema_version"`
	GeneratedAt      time.Time              `json:"generated_at"`
	GeneratedBy      string                 `json:"generated_by"`
	SanitizerVersion string                 `json:"sanitizer_version"`
	Limits           PlanningContextLimits  `json:"limits"`
	Sources          PlanningContextSources `json:"sources"`
	Meta             PlanningContextMeta    `json:"meta"`
}

type PlanningContextLimits struct {
	MaxOpenTasks         int  `json:"max_open_tasks"`
	MaxRecentDocuments   int  `json:"max_recent_documents"`
	MaxOpenDriftSignals  int  `json:"max_open_drift_signals"`
	MaxRecentAgentRuns   int  `json:"max_recent_agent_runs"`
	IncludeLatestSyncRun bool `json:"include_latest_sync_run"`
	MaxSourcesBytes      int  `json:"max_sources_bytes"`
}

type PlanningContextSources struct {
	OpenTasks        []WireTask        `json:"open_tasks"`
	RecentDocuments  []WireDocument    `json:"recent_documents"`
	OpenDriftSignals []WireDriftSignal `json:"open_drift_signals"`
	LatestSyncRun    *WireSyncRun      `json:"latest_sync_run,omitempty"`
	RecentAgentRuns  []WireAgentRun    `json:"recent_agent_runs"`
}

type WireTask struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WireDocument struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	FilePath      string `json:"file_path"`
	DocType       string `json:"doc_type"`
	IsStale       bool   `json:"is_stale"`
	StalenessDays int    `json:"staleness_days"`
}

type WireDriftSignal struct {
	ID            string    `json:"id"`
	DocumentTitle string    `json:"document_title"`
	TriggerType   string    `json:"trigger_type"`
	TriggerDetail string    `json:"trigger_detail"`
	Severity      string    `json:"severity"`
	OpenedAt      time.Time `json:"opened_at"`
}

type WireSyncRun struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message"`
}

type WireAgentRun struct {
	ID         string    `json:"id"`
	AgentName  string    `json:"agent_name"`
	ActionType string    `json:"action_type"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	Summary    string    `json:"summary"`
}

type PlanningContextMeta struct {
	Ranking       map[string]string `json:"ranking"`
	DroppedCounts map[string]int    `json:"dropped_counts"`
	SourcesBytes  int               `json:"sources_bytes"`
	// Warnings carries non-fatal degradation notes (e.g. a source store that
	// returned an error and was substituted with an empty slice). Always
	// non-nil after sanitization so adapters can iterate without nil checks.
	Warnings []string `json:"warnings"`
}

// DefaultLimits returns the Phase A default limits.
func DefaultLimits() PlanningContextLimits {
	return PlanningContextLimits{
		MaxOpenTasks:         DefaultMaxOpenTasks,
		MaxRecentDocuments:   DefaultMaxRecentDocuments,
		MaxOpenDriftSignals:  DefaultMaxOpenDriftSignals,
		MaxRecentAgentRuns:   DefaultMaxRecentAgentRuns,
		IncludeLatestSyncRun: DefaultIncludeLatestSyncRun,
		MaxSourcesBytes:      DefaultMaxSourcesBytes,
	}
}

// DefaultRanking returns the wire-stable ranking taxonomy for meta.ranking.
func DefaultRanking() map[string]string {
	return map[string]string{
		"documents":     RankingDocuments,
		"tasks":         RankingTasks,
		"drift_signals": RankingDriftSignals,
		"agent_runs":    RankingAgentRuns,
	}
}
