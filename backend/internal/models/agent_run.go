package models

import "time"

// AgentRun records a single agent activity event.
type AgentRun struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	AgentName        string     `json:"agent_name"`
	ActionType       string     `json:"action_type"` // create | update | review | sync
	Status           string     `json:"status"`      // running | completed | failed
	Summary          string     `json:"summary"`
	FilesAffected    []string   `json:"files_affected"` // stored as JSON in DB
	NeedsHumanReview bool       `json:"needs_human_review"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	ErrorMessage     string     `json:"error_message"`
	IdempotencyKey   string     `json:"idempotency_key,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type CreateAgentRunRequest struct {
	AgentName        string   `json:"agent_name"`
	ActionType       string   `json:"action_type"`
	Summary          string   `json:"summary"`
	FilesAffected    []string `json:"files_affected"`
	NeedsHumanReview bool     `json:"needs_human_review"`
	IdempotencyKey   string   `json:"idempotency_key,omitempty"`
}

type UpdateAgentRunRequest struct {
	Status           string    `json:"status,omitempty"`
	Summary          *string   `json:"summary,omitempty"`
	FilesAffected    *[]string `json:"files_affected,omitempty"`
	NeedsHumanReview *bool     `json:"needs_human_review,omitempty"`
	ErrorMessage     *string   `json:"error_message,omitempty"`
}

var ValidAgentActionTypes = map[string]bool{
	"create": true,
	"update": true,
	"review": true,
	"sync":   true,
}

var ValidAgentRunStatuses = map[string]bool{
	"running":   true,
	"completed": true,
	"failed":    true,
}
