package models

import "time"

const (
	BacklogItemStatusTriage    = "triage"
	BacklogItemStatusReady     = "ready"
	BacklogItemStatusCommitted = "committed"
	BacklogItemStatusBlocked   = "blocked"
	BacklogItemStatusArchived  = "archived"

	BacklogItemSourceHuman            = "human"
	BacklogItemSourcePlanningRun      = "planning_run"
	BacklogItemSourceBacklogCandidate = "backlog_candidate"
	BacklogItemSourceConnector        = "connector"
)

var ValidBacklogItemStatuses = map[string]bool{
	BacklogItemStatusTriage:    true,
	BacklogItemStatusReady:     true,
	BacklogItemStatusCommitted: true,
	BacklogItemStatusBlocked:   true,
	BacklogItemStatusArchived:  true,
}

var ValidBacklogItemSources = map[string]bool{
	BacklogItemSourceHuman:            true,
	BacklogItemSourcePlanningRun:      true,
	BacklogItemSourceBacklogCandidate: true,
	BacklogItemSourceConnector:        true,
}

type BacklogItem struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	RequirementID      string    `json:"requirement_id,omitempty"`
	PlanningRunID      string    `json:"planning_run_id,omitempty"`
	BacklogCandidateID string    `json:"backlog_candidate_id,omitempty"`
	TaskID             string    `json:"task_id,omitempty"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Status             string    `json:"status"`
	Priority           string    `json:"priority"`
	Source             string    `json:"source"`
	Rank               int       `json:"rank"`
	Labels             []string  `json:"labels"`
	AcceptanceCriteria string    `json:"acceptance_criteria"`
	BlockedReason      string    `json:"blocked_reason"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateBacklogItemRequest struct {
	RequirementID      string   `json:"requirement_id,omitempty"`
	PlanningRunID      string   `json:"planning_run_id,omitempty"`
	BacklogCandidateID string   `json:"backlog_candidate_id,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	Source             string   `json:"source"`
	Rank               int      `json:"rank"`
	Labels             []string `json:"labels"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	BlockedReason      string   `json:"blocked_reason"`
}

type UpdateBacklogItemRequest struct {
	Title              *string   `json:"title,omitempty"`
	Description        *string   `json:"description,omitempty"`
	Status             *string   `json:"status,omitempty"`
	Priority           *string   `json:"priority,omitempty"`
	Rank               *int      `json:"rank,omitempty"`
	Labels             *[]string `json:"labels,omitempty"`
	AcceptanceCriteria *string   `json:"acceptance_criteria,omitempty"`
	BlockedReason      *string   `json:"blocked_reason,omitempty"`
}

type BacklogItemListFilters struct {
	Status   string
	Priority string
	Source   string
	Label    string
	Query    string
}

type CommitBacklogItemResponse struct {
	BacklogItem    BacklogItem `json:"backlog_item"`
	Task           Task        `json:"task"`
	Lineage        TaskLineage `json:"lineage"`
	AlreadyApplied bool        `json:"already_applied"`
}
