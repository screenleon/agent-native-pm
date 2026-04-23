package models

import "time"

const (
	RequirementStatusDraft    = "draft"
	RequirementStatusPlanned  = "planned"
	RequirementStatusArchived = "archived"

	PlanningProviderDeterministic      = "deterministic"
	PlanningProviderOpenAICompatible   = "openai-compatible"
	PlanningProviderModelDeterministic = "deterministic-v1"

	PlanningSelectionSourceServerDefault   = "server_default"
	PlanningSelectionSourceRequestOverride = "request_override"

	PlanningBindingSourceSystem   = "system"
	PlanningBindingSourceShared   = "shared"
	PlanningBindingSourcePersonal = "personal"

	PlanningExecutionModeDeterministic  = "deterministic"
	PlanningExecutionModeServerProvider = "server_provider"
	PlanningExecutionModeLocalConnector = "local_connector"

	PlanningDispatchStatusNotRequired = "not_required"
	PlanningDispatchStatusQueued      = "queued"
	PlanningDispatchStatusLeased      = "leased"
	PlanningDispatchStatusReturned    = "returned"
	PlanningDispatchStatusExpired     = "expired"

	PlanningRunStatusQueued    = "queued"
	PlanningRunStatusRunning   = "running"
	PlanningRunStatusCompleted = "completed"
	PlanningRunStatusFailed    = "failed"
	PlanningRunStatusCancelled = "cancelled"

	BacklogCandidateStatusDraft    = "draft"
	BacklogCandidateStatusApproved = "approved"
	BacklogCandidateStatusRejected = "rejected"
	BacklogCandidateStatusApplied  = "applied"

	TaskLineageKindAppliedCandidate  = "applied_candidate"
	TaskLineageKindManualRequirement = "manual_requirement"
	TaskLineageKindMergedRequirement = "merged_requirement"
)

var ValidRequirementStatuses = map[string]bool{
	RequirementStatusDraft:    true,
	RequirementStatusPlanned:  true,
	RequirementStatusArchived: true,
}

var ValidPlanningRunStatuses = map[string]bool{
	PlanningRunStatusQueued:    true,
	PlanningRunStatusRunning:   true,
	PlanningRunStatusCompleted: true,
	PlanningRunStatusFailed:    true,
	PlanningRunStatusCancelled: true,
}

var ValidPlanningExecutionModes = map[string]bool{
	PlanningExecutionModeDeterministic:  true,
	PlanningExecutionModeServerProvider: true,
	PlanningExecutionModeLocalConnector: true,
}

var ValidPlanningDispatchStatuses = map[string]bool{
	PlanningDispatchStatusNotRequired: true,
	PlanningDispatchStatusQueued:      true,
	PlanningDispatchStatusLeased:      true,
	PlanningDispatchStatusReturned:    true,
	PlanningDispatchStatusExpired:     true,
}

var ValidBacklogCandidateReviewStatuses = map[string]bool{
	BacklogCandidateStatusDraft:    true,
	BacklogCandidateStatusApproved: true,
	BacklogCandidateStatusRejected: true,
}

type Requirement struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateRequirementRequest struct {
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type CreatePlanningRunRequest struct {
	TriggerSource string `json:"trigger_source,omitempty"`
	ProviderID    string `json:"provider_id,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	AdapterType   string `json:"adapter_type,omitempty"`
	ModelOverride string `json:"model_override,omitempty"`
	// AccountBindingID names a personal account_bindings row to dispatch
	// this run against. Optional. When execution_mode is local_connector
	// and no non-empty binding ID is provided, the server may resolve the
	// user's primary cli:* binding (Path B S2). When the user has no
	// cli:* bindings the field stays nil on the run and the connector
	// falls back to its env-var default (backwards compatible with
	// pre-Path-B connectors). Explicit empty string is treated identically
	// to absent — there is no "opt out of auto-resolution" sentinel today.
	AccountBindingID *string `json:"account_binding_id,omitempty"`
}

type PlanningProviderModel struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type PlanningProviderDescriptor struct {
	ID             string                  `json:"id"`
	Label          string                  `json:"label"`
	Kind           string                  `json:"kind"`
	Description    string                  `json:"description"`
	DefaultModelID string                  `json:"default_model_id"`
	Models         []PlanningProviderModel `json:"models"`
}

type PlanningProviderSelection struct {
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	SelectionSource string `json:"selection_source"`
	BindingSource   string `json:"binding_source,omitempty"`
	BindingLabel    string `json:"binding_label,omitempty"`
}

type PlanningProviderOptions struct {
	DefaultSelection         PlanningProviderSelection    `json:"default_selection"`
	Providers                []PlanningProviderDescriptor `json:"providers"`
	CredentialMode           string                       `json:"credential_mode"`
	ResolvedBindingSource    string                       `json:"resolved_binding_source,omitempty"`
	ResolvedBindingLabel     string                       `json:"resolved_binding_label,omitempty"`
	AvailableExecutionModes  []string                     `json:"available_execution_modes,omitempty"`
	PairedConnectorAvailable bool                         `json:"paired_connector_available"`
	ActiveConnectorLabel     string                       `json:"active_connector_label,omitempty"`
	CanRun                   bool                         `json:"can_run"`
	UnavailableReason        string                       `json:"unavailable_reason,omitempty"`
	AllowModelOverride       bool                         `json:"allow_model_override"`
}

type PlanningRun struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	RequirementID     string     `json:"requirement_id"`
	Status            string     `json:"status"`
	TriggerSource     string     `json:"trigger_source"`
	ProviderID        string     `json:"provider_id"`
	ModelID           string     `json:"model_id"`
	SelectionSource   string     `json:"selection_source"`
	BindingSource     string     `json:"binding_source"`
	BindingLabel      string     `json:"binding_label,omitempty"`
	RequestedByUserID string     `json:"requested_by_user_id,omitempty"`
	ExecutionMode     string     `json:"execution_mode"`
	DispatchStatus    string     `json:"dispatch_status"`
	ConnectorID       string     `json:"connector_id,omitempty"`
	ConnectorLabel    string     `json:"connector_label,omitempty"`
	LeaseExpiresAt    *time.Time `json:"lease_expires_at"`
	DispatchError     string     `json:"dispatch_error"`
	ErrorMessage      string     `json:"error_message"`
	AdapterType      string        `json:"adapter_type,omitempty"`
	ModelOverride    string        `json:"model_override,omitempty"`
	// AccountBindingID is the account_bindings.id chosen at run-create time
	// (Path B S2). Nullable to distinguish "no binding selected" from
	// "explicitly empty" — exposed as a pointer so JSON omits the key when
	// absent rather than serialising an empty string.
	AccountBindingID *string `json:"account_binding_id,omitempty"`
	// ConnectorCliInfo is the JSON blob backed by the planning_runs.connector_cli_info
	// column. Holds the binding snapshot taken at run creation (Path B S2),
	// the adapter's reported CLI invocation info (S5b), an optional error_kind
	// (S5a), and a dispatch_warning flag the dispatcher may set if a CLI-bound
	// run was skipped due to a pre-Path-B connector (R3 mitigation, design §6.2).
	ConnectorCliInfo *PlanningRunCliInfo `json:"connector_cli_info,omitempty"`
	StartedAt        *time.Time          `json:"started_at"`
	CompletedAt      *time.Time          `json:"completed_at"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// PlanningRunCliInfo is the wider Path-B-aware envelope serialised into the
// planning_runs.connector_cli_info column. The previous CliUsageInfo type
// is preserved as the embedded `Invocation` field; new sub-blocks are
// optional so old rows decode cleanly.
type PlanningRunCliInfo struct {
	BindingSnapshot  *PlanningRunBindingSnapshot `json:"binding_snapshot,omitempty"`
	Invocation       *CliUsageInfo               `json:"cli_invocation,omitempty"`
	ErrorKind        string                      `json:"error_kind,omitempty"`
	DispatchWarning  string                      `json:"dispatch_warning,omitempty"`
}

// PlanningRunBindingSnapshot freezes the fields of an account_bindings row
// at planning-run creation time. Lives inside the existing
// connector_cli_info JSON column (NOT a new column) so a binding deletion
// after the run is queued does not lose the audit trail (R10 mitigation,
// design §6.5). Field set per the S2 ticket: ProviderID, ModelID,
// CliCommand, Label, IsPrimary.
type PlanningRunBindingSnapshot struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id,omitempty"`
	CliCommand string `json:"cli_command,omitempty"`
	Label      string `json:"label,omitempty"`
	IsPrimary  bool   `json:"is_primary"`
}

type PlanningDocumentEvidence struct {
	DocumentID          string   `json:"document_id"`
	Title               string   `json:"title"`
	FilePath            string   `json:"file_path"`
	DocType             string   `json:"doc_type"`
	IsStale             bool     `json:"is_stale"`
	StalenessDays       int      `json:"staleness_days"`
	MatchedKeywords     []string `json:"matched_keywords"`
	ContributionReasons []string `json:"contribution_reasons"`
}

type PlanningDriftSignalEvidence struct {
	DriftSignalID       string   `json:"drift_signal_id"`
	DocumentID          string   `json:"document_id"`
	DocumentTitle       string   `json:"document_title"`
	Severity            int      `json:"severity"`
	TriggerType         string   `json:"trigger_type"`
	TriggerDetail       string   `json:"trigger_detail"`
	ContributionReasons []string `json:"contribution_reasons"`
}

type PlanningSyncRunEvidence struct {
	SyncRunID           string   `json:"sync_run_id"`
	Status              string   `json:"status"`
	CommitsScanned      int      `json:"commits_scanned"`
	FilesChanged        int      `json:"files_changed"`
	ErrorMessage        string   `json:"error_message"`
	ContributionReasons []string `json:"contribution_reasons"`
}

type PlanningAgentRunEvidence struct {
	AgentRunID          string   `json:"agent_run_id"`
	AgentName           string   `json:"agent_name"`
	ActionType          string   `json:"action_type"`
	Status              string   `json:"status"`
	Summary             string   `json:"summary"`
	ErrorMessage        string   `json:"error_message"`
	ContributionReasons []string `json:"contribution_reasons"`
}

type PlanningDuplicateEvidence struct {
	Title               string   `json:"title"`
	ContributionReasons []string `json:"contribution_reasons"`
}

type PlanningScoreBreakdown struct {
	Impact             float64 `json:"impact"`
	Urgency            float64 `json:"urgency"`
	DependencyUnlock   float64 `json:"dependency_unlock"`
	RiskReduction      float64 `json:"risk_reduction"`
	Effort             float64 `json:"effort"`
	ConfidenceSeed     float64 `json:"confidence_seed"`
	EvidenceBonus      float64 `json:"evidence_bonus"`
	DuplicatePenalty   float64 `json:"duplicate_penalty"`
	FinalPriorityScore float64 `json:"final_priority_score"`
	FinalConfidence    float64 `json:"final_confidence"`
}

type PlanningEvidenceDetail struct {
	Summary        []string                      `json:"summary"`
	Documents      []PlanningDocumentEvidence    `json:"documents"`
	DriftSignals   []PlanningDriftSignalEvidence `json:"drift_signals"`
	SyncRun        *PlanningSyncRunEvidence      `json:"sync_run"`
	AgentRuns      []PlanningAgentRunEvidence    `json:"agent_runs"`
	Duplicates     []PlanningDuplicateEvidence   `json:"duplicates"`
	ScoreBreakdown PlanningScoreBreakdown        `json:"score_breakdown"`
}

type BacklogCandidate struct {
	ID                  string                 `json:"id"`
	ProjectID           string                 `json:"project_id"`
	RequirementID       string                 `json:"requirement_id"`
	PlanningRunID       string                 `json:"planning_run_id"`
	ParentCandidateID   string                 `json:"parent_candidate_id,omitempty"`
	SuggestionType      string                 `json:"suggestion_type"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description"`
	Status              string                 `json:"status"`
	Rationale           string                 `json:"rationale"`
	ValidationCriteria  string                 `json:"validation_criteria,omitempty"`
	PODecision          string                 `json:"po_decision,omitempty"`
	PriorityScore       float64                `json:"priority_score"`
	Confidence          float64                `json:"confidence"`
	Rank                int                    `json:"rank"`
	Evidence            []string               `json:"evidence"`
	EvidenceDetail      PlanningEvidenceDetail `json:"evidence_detail"`
	DuplicateTitles     []string               `json:"duplicate_titles"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

type BacklogCandidateDraft struct {
	ParentCandidateID  string
	SuggestionType     string
	Title              string
	Description        string
	Rationale          string
	ValidationCriteria string
	PODecision         string
	PriorityScore      float64
	Confidence         float64
	Rank               int
	Evidence           []string
	EvidenceDetail     PlanningEvidenceDetail
	DuplicateTitles    []string
}

type UpdateBacklogCandidateRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type TaskLineage struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	TaskID             string    `json:"task_id"`
	RequirementID      string    `json:"requirement_id,omitempty"`
	PlanningRunID      string    `json:"planning_run_id,omitempty"`
	BacklogCandidateID string    `json:"backlog_candidate_id,omitempty"`
	LineageKind        string    `json:"lineage_kind"`
	CreatedAt          time.Time `json:"created_at"`
}

type ApplyBacklogCandidateResponse struct {
	Task           Task             `json:"task"`
	Candidate      BacklogCandidate `json:"candidate"`
	Lineage        TaskLineage      `json:"lineage"`
	AlreadyApplied bool             `json:"already_applied"`
}

// AppliedLineageEntry denormalises task_lineage rows with the headings the
// Planning Workspace applied-lineage lane needs to render the
// requirement → run → candidate → task chain. Populated by
// BacklogCandidateStore.ListAppliedLineageByProject and exposed via
// GET /api/projects/:id/task-lineage.
type AppliedLineageEntry struct {
	LineageID            string    `json:"lineage_id"`
	ProjectID            string    `json:"project_id"`
	TaskID               string    `json:"task_id"`
	TaskTitle            string    `json:"task_title"`
	TaskStatus           string    `json:"task_status"`
	RequirementID        string    `json:"requirement_id,omitempty"`
	RequirementTitle     string    `json:"requirement_title,omitempty"`
	PlanningRunID        string    `json:"planning_run_id,omitempty"`
	PlanningRunStatus    string    `json:"planning_run_status,omitempty"`
	BacklogCandidateID   string    `json:"backlog_candidate_id,omitempty"`
	BacklogCandidateTitle string   `json:"backlog_candidate_title,omitempty"`
	LineageKind          string    `json:"lineage_kind"`
	CreatedAt            time.Time `json:"created_at"`
}
