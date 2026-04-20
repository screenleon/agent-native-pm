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
	LastSeenAt    *time.Time             `json:"last_seen_at"`
	LastError     string                 `json:"last_error"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
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
}

type PairLocalConnectorResponse struct {
	Connector      LocalConnector `json:"connector"`
	ConnectorToken string         `json:"connector_token"`
}

type LocalConnectorHeartbeatRequest struct {
	Capabilities map[string]interface{} `json:"capabilities,omitempty"`
	LastError    string                 `json:"last_error,omitempty"`
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
}

type LocalConnectorClaimNextRunResponse struct {
	Run             *PlanningRun            `json:"run"`
	Requirement     *Requirement            `json:"requirement"`
	Project         *Project                `json:"project,omitempty"`
	PlanningContext *wire.PlanningContextV1 `json:"planning_context,omitempty"`
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
}

// ConnectorRunStats holds planning run counts across different time windows for a user.
type ConnectorRunStats struct {
	RunsToday int `json:"runs_today"`
	RunsWeek  int `json:"runs_week"`
	RunsMonth int `json:"runs_month"`
	RunsTotal int `json:"runs_total"`
}
