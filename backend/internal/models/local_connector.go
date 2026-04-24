package models

import (
	"time"

	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

const (
	LocalConnectorStatusPending = "pending"
	LocalConnectorStatusOnline  = "online"
	LocalConnectorStatusOffline = "offline"
	LocalConnectorStatusRevoked = "revoked"

	ConnectorPairingStatusPending   = "pending"
	ConnectorPairingStatusClaimed   = "claimed"
	ConnectorPairingStatusExpired   = "expired"
	ConnectorPairingStatusCancelled = "cancelled"
)


type LocalConnector struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Label         string                 `json:"label"`
	Platform      string                 `json:"platform"`
	ClientVersion string                 `json:"client_version"`
	Status        string                 `json:"status"`
	Capabilities  map[string]interface{} `json:"capabilities"`
	// ProtocolVersion identifies which wire-protocol revision the connector
	// understands. 0 = pre-Path-B (does not parse `cli_binding` in claim
	// response). 1 = Path B / S2-aware. Set by the connector at pair time
	// (Path B S2). See migration 023 and design §6.2 / §6.5 R3.
	ProtocolVersion int                    `json:"protocol_version"`
	// Metadata holds operational data such as CLI health probe results
	// (path: metadata.cli_health.<binding_id>). Added in migration 025.
	Metadata        map[string]interface{} `json:"metadata"`
	LastSeenAt      *time.Time             `json:"last_seen_at"`
	LastError       string                 `json:"last_error"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type ConnectorPairingSession struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Label       string    `json:"label"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	ConnectorID string    `json:"connector_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateLocalConnectorPairingSessionRequest struct {
	Label string `json:"label,omitempty"`
}

type CreateLocalConnectorPairingSessionResponse struct {
	Session     ConnectorPairingSession `json:"session"`
	PairingCode string                  `json:"pairing_code"`
}

type PairLocalConnectorRequest struct {
	PairingCode   string                 `json:"pairing_code"`
	Label         string                 `json:"label,omitempty"`
	Platform      string                 `json:"platform,omitempty"`
	ClientVersion string                 `json:"client_version,omitempty"`
	Capabilities  map[string]interface{} `json:"capabilities,omitempty"`
	// ProtocolVersion is sent by Path-B-aware connectors (S2+). Old
	// connectors that omit the field default to 0 server-side, which the
	// claim dispatcher treats as "cannot receive cli_binding". See design
	// §6.2 and migration 023.
	ProtocolVersion int `json:"protocol_version,omitempty"`
}

type PairLocalConnectorResponse struct {
	Connector      LocalConnector `json:"connector"`
	ConnectorToken string         `json:"connector_token"`
}

type LocalConnectorHeartbeatRequest struct {
	Capabilities     map[string]interface{} `json:"capabilities,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	// LastCliHealthyAt is set by the connector when a CLI health probe
	// succeeded since the previous heartbeat. The server overwrites
	// metadata.cli_last_healthy_at with this value (single timestamp,
	// no per-binding accumulation). Path B S5b.
	LastCliHealthyAt *time.Time `json:"last_cli_healthy_at,omitempty"`
	// CliProbeResults carries outcomes of any CLI-binding probe requests the
	// connector processed since its previous heartbeat. Added in Phase 4
	// (P4-4). The server pops matching entries from
	// metadata.pending_cli_probe_requests and stores the outcome under
	// metadata.cli_probe_results.<probe_id>.
	CliProbeResults []CliProbeResult `json:"cli_probe_results,omitempty"`
}

// PendingCliProbeRequest is stored in LocalConnector.Metadata under the key
// "pending_cli_probe_requests" (array). Each entry represents an operator
// click of "Test on connector" that has not been delivered and completed yet.
// The connector sees it in the heartbeat response, runs the built-in adapter
// with a minimal prompt, and reports a matching CliProbeResult in the next
// heartbeat.
type PendingCliProbeRequest struct {
	ProbeID     string    `json:"probe_id"`
	BindingID   string    `json:"binding_id"`
	ProviderID  string    `json:"provider_id"`
	ModelID     string    `json:"model_id"`
	CliCommand  string    `json:"cli_command,omitempty"`
	Label       string    `json:"label,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

// CliProbeResult is the outcome of a single probe. The server stores it
// under metadata.cli_probe_results.<probe_id>; the UI polls for it by
// probe_id. Retained for 24h before GC.
type CliProbeResult struct {
	ProbeID      string    `json:"probe_id"`
	BindingID    string    `json:"binding_id,omitempty"`
	OK           bool      `json:"ok"`
	LatencyMS    int64     `json:"latency_ms"`
	Content      string    `json:"content,omitempty"`
	ErrorKind    string    `json:"error_kind,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CompletedAt  time.Time `json:"completed_at"`
}

// CliProbeStatusResponse is returned by the poll endpoint
// GET /api/me/local-connectors/:id/probe-binding/:probe_id.
type CliProbeStatusResponse struct {
	Status string          `json:"status"` // "pending" | "completed" | "not_found"
	Result *CliProbeResult `json:"result,omitempty"`
}

// ProbeBindingOnConnectorRequest is the POST body for kicking off a probe.
type ProbeBindingOnConnectorRequest struct {
	BindingID string `json:"binding_id"`
}

// ProbeBindingOnConnectorResponse returns the probe_id the client should poll.
type ProbeBindingOnConnectorResponse struct {
	ProbeID string `json:"probe_id"`
}

type ConnectorBacklogCandidateDraft struct {
	SuggestionType     string   `json:"suggestion_type,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	Rationale          string   `json:"rationale,omitempty"`
	ValidationCriteria string   `json:"validation_criteria,omitempty"`
	PODecision         string   `json:"po_decision,omitempty"`
	PriorityScore      float64  `json:"priority_score,omitempty"`
	Confidence         float64  `json:"confidence,omitempty"`
	Rank               int      `json:"rank,omitempty"`
	Evidence           []string `json:"evidence,omitempty"`
	DuplicateTitles    []string `json:"duplicate_titles,omitempty"`
	// ExecutionRole is an optional hint from the planner about which
	// specialist should execute this candidate. Phase 5 B2; planners do
	// not populate it today. Phase 6 will.
	ExecutionRole string `json:"execution_role,omitempty"`
}

type LocalConnectorClaimNextRunResponse struct {
	Run             *PlanningRun            `json:"run"`
	Requirement     *Requirement            `json:"requirement"`
	Project         *Project                `json:"project,omitempty"`
	PlanningContext *wire.PlanningContextV1 `json:"planning_context,omitempty"`
	// CliBinding is populated when the run was created with an explicit
	// account_binding_id (or auto-resolved to the user's primary CLI
	// binding). Sourced from the run's ConnectorCliInfo.BindingSnapshot
	// rather than a live binding lookup so audit info survives even if the
	// binding row was deleted between create and claim (R10 mitigation;
	// design §6.2 and §6.5). Path B / S2.
	CliBinding *PlanningRunCliBindingPayload `json:"cli_binding,omitempty"`
}

// PlanningRunCliBindingPayload is the shape returned to the connector in the
// claim-next-run response. Mirrors the fields stored in the run's binding
// snapshot but trimmed to what the connector actually needs to invoke the
// adapter (no IsPrimary; that flag is irrelevant after dispatch).
type PlanningRunCliBindingPayload struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id,omitempty"`
	CliCommand string `json:"cli_command,omitempty"`
	Label      string `json:"label,omitempty"`
}

// CliUsageInfo captures which CLI agent and model were actually used during adapter execution.
type CliUsageInfo struct {
	Agent       string `json:"agent"`
	Model       string `json:"model,omitempty"`
	ModelSource string `json:"model_source,omitempty"` // "default" | "override" | "subscription"
}

type LocalConnectorSubmitRunResultRequest struct {
	Success      bool                             `json:"success"`
	ErrorMessage string                           `json:"error_message,omitempty"`
	Candidates   []ConnectorBacklogCandidateDraft `json:"candidates,omitempty"`
	CliInfo      *CliUsageInfo                    `json:"cli_info,omitempty"`
	ErrorKind    string                           `json:"error_kind,omitempty"`
}

// ConnectorRunStats holds planning run counts across different time windows for a user.
type ConnectorRunStats struct {
	RunsToday int `json:"runs_today"`
	RunsWeek  int `json:"runs_week"`
	RunsMonth int `json:"runs_month"`
	RunsTotal int `json:"runs_total"`
}
