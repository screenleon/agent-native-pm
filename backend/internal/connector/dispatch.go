package connector

// dispatch.go defines the Phase 6b role_dispatch wire types used by the
// connector client (client.go) and the connector service (service.go).
// They mirror the handler types in internal/handlers/connector_dispatch.go but
// live in this package so the connector binary compiles without importing the
// HTTP handler layer.

import (
	"encoding/json"

	"github.com/screenleon/agent-native-pm/internal/models"
)

// ClaimNextTaskResponse is the payload returned by POST /api/connector/claim-next-task.
// Task is nil when the queue is empty.
type ClaimNextTaskResponse struct {
	Task           *models.Task                 `json:"task"`
	Requirement    *ConnectorRequirementSummary `json:"requirement,omitempty"`
	ProjectContext string                       `json:"project_context,omitempty"`
	// RepoPath is the absolute local path to the project's repository, if the
	// project has one configured. When non-empty, the connector writes the
	// files[] from the execution result to this directory.
	RepoPath string `json:"repo_path,omitempty"`
}

// ConnectorRequirementSummary is the slim requirement view included in the
// claim-next-task response so the connector can build the role prompt context.
type ConnectorRequirementSummary struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// SubmitTaskResultRequest is the request body for
// POST /api/connector/tasks/:task_id/execution-result.
type SubmitTaskResultRequest struct {
	Success      bool            `json:"success"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	ErrorKind    string          `json:"error_kind,omitempty"`
}
