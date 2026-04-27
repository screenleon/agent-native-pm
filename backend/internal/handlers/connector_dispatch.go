package handlers

// ConnectorDispatchHandler handles the Phase 6b task dispatch API:
//   POST /api/connector/claim-next-task
//   POST /api/connector/tasks/:task_id/execution-result
//
// Both endpoints require X-Connector-Token authentication (same as claim-next-run).
// The handler is wired onto LocalConnectorHandler so it reuses authenticatedConnector.

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// ClaimNextTaskResponse is returned by POST /api/connector/claim-next-task.
type ClaimNextTaskResponse struct {
	Task           *models.Task        `json:"task"`
	Requirement    *RequirementSummary `json:"requirement,omitempty"`
	ProjectContext string              `json:"project_context,omitempty"`
	// RepoPath is the absolute local path to the project's repository. When
	// non-empty, the connector writes the files[] from the execution result to
	// this directory. Omitted when the project has no repo_path configured.
	RepoPath string `json:"repo_path,omitempty"`
}

// RequirementSummary is a slim view of the requirement sent alongside the task.
type RequirementSummary struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// SubmitTaskResultRequest is the request body for POST …/execution-result.
type SubmitTaskResultRequest struct {
	Success      bool            `json:"success"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	ErrorKind    string          `json:"error_kind,omitempty"`
}

// ClaimNextTask implements POST /api/connector/claim-next-task.
func (h *LocalConnectorHandler) ClaimNextTask(w http.ResponseWriter, r *http.Request) {
	connector, ok := h.authenticatedConnector(w, r)
	if !ok {
		return
	}

	if h.taskStore == nil {
		writeError(w, http.StatusInternalServerError, "task dispatch store not configured")
		return
	}

	task, req, err := h.taskStore.ClaimNextDispatchTask(connector.ID, connector.UserID)
	if err != nil {
		log.Printf("claim-next-task: error for connector %s: %v", connector.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to claim next dispatch task")
		return
	}
	if task == nil {
		// Queue empty — return {"task": null}.
		writeSuccess(w, http.StatusOK, ClaimNextTaskResponse{}, nil)
		return
	}

	resp := ClaimNextTaskResponse{Task: task}
	if req != nil {
		resp.Requirement = &RequirementSummary{
			ID:      req.ID,
			Title:   req.Title,
			Summary: req.Summary,
		}
		resp.ProjectContext = buildDispatchProjectContext(req)
	}

	// Populate repo_path from the project so the connector can apply files.
	if h.projects != nil && strings.TrimSpace(task.ProjectID) != "" {
		if project, projErr := h.projects.GetByID(task.ProjectID); projErr != nil {
			log.Printf("claim-next-task: failed to load project %s: %v", task.ProjectID, projErr)
		} else if project != nil && strings.TrimSpace(project.RepoPath) != "" {
			resp.RepoPath = strings.TrimSpace(project.RepoPath)
		}
	}

	writeSuccess(w, http.StatusOK, resp, nil)
}

// SubmitTaskResult implements POST /api/connector/tasks/:task_id/execution-result.
func (h *LocalConnectorHandler) SubmitTaskResult(w http.ResponseWriter, r *http.Request) {
	connector, ok := h.authenticatedConnector(w, r)
	if !ok {
		return
	}

	taskID := chi.URLParam(r, "task_id")
	if strings.TrimSpace(taskID) == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	if h.taskStore == nil {
		writeError(w, http.StatusInternalServerError, "task dispatch store not configured")
		return
	}

	var req SubmitTaskResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify connector has ownership over this task.
	task, err := h.taskStore.GetTaskForConnector(taskID, connector.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify task ownership")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Validate that the task is currently running (not already settled).
	if task.DispatchStatus != models.TaskDispatchStatusRunning {
		writeError(w, http.StatusBadRequest, "task is not in running state")
		return
	}

	// Normalize error_kind (same allowlist as planning-runs).
	errorKind := strings.TrimSpace(req.ErrorKind)
	if errorKind != "" && !models.AllowedErrorKinds[errorKind] {
		errorKind = models.ErrorKindUnknown
	}

	if req.Success {
		if err := h.taskStore.CompleteDispatchTask(taskID, connector.UserID, req.Result); err != nil {
			if errors.Is(err, store.ErrDispatchOwnership) {
				writeError(w, http.StatusForbidden, "connector does not own this task")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to complete task")
			return
		}
	} else {
		errMsg := strings.TrimSpace(req.ErrorMessage)
		if errMsg == "" {
			errMsg = "execution failed"
		}
		if errorKind != "" && errorKind != models.ErrorKindUnknown {
			errMsg = errMsg + " [" + errorKind + "]"
		}
		if err := h.taskStore.FailDispatchTask(taskID, connector.UserID, errMsg); err != nil {
			if errors.Is(err, store.ErrDispatchOwnership) {
				writeError(w, http.StatusForbidden, "connector does not own this task")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to fail task")
			return
		}
	}

	// Return the updated task.
	updated, err := h.taskStore.GetByID(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload task")
		return
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}

// buildDispatchProjectContext produces a compact text summary of the requirement
// that the role prompt can inject as PROJECT_CONTEXT.
func buildDispatchProjectContext(req *models.Requirement) string {
	if req == nil {
		return ""
	}
	var parts []string
	if t := strings.TrimSpace(req.Title); t != "" {
		parts = append(parts, "Requirement: "+t)
	}
	if d := strings.TrimSpace(req.Description); d != "" {
		parts = append(parts, "Description: "+d)
	}
	if a := strings.TrimSpace(req.Audience); a != "" {
		parts = append(parts, "Audience: "+a)
	}
	if s := strings.TrimSpace(req.SuccessCriteria); s != "" {
		parts = append(parts, "Success criteria: "+s)
	}
	return strings.Join(parts, "\n")
}
