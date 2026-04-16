package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/git"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// SyncHandler handles Phase 2 repo sync endpoints.
type SyncHandler struct {
	syncStore    *store.SyncRunStore
	syncService  *git.SyncService
	projectStore *store.ProjectStore
}

func NewSyncHandler(
	syncStore *store.SyncRunStore,
	syncService *git.SyncService,
	projectStore *store.ProjectStore,
) *SyncHandler {
	return &SyncHandler{
		syncStore:    syncStore,
		syncService:  syncService,
		projectStore: projectStore,
	}
}

// TriggerSync POST /api/projects/{id}/sync
func (h *SyncHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	syncRun, err := h.syncService.Run(projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, syncRun, nil)
}

// ListSyncRuns GET /api/projects/{id}/sync-runs
func (h *SyncHandler) ListSyncRuns(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	page, perPage := parsePagination(r)

	runs, total, err := h.syncStore.ListByProject(projectID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sync runs")
		return
	}
	writeSuccess(w, http.StatusOK, runs, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

// AgentRunHandler handles Phase 2 agent activity endpoints.
type AgentRunHandler struct {
	store        *store.AgentRunStore
	projectStore *store.ProjectStore
}

func NewAgentRunHandler(s *store.AgentRunStore, projectStore *store.ProjectStore) *AgentRunHandler {
	return &AgentRunHandler{store: s, projectStore: projectStore}
}

// Create POST /api/agent-runs
func (h *AgentRunHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		models.CreateAgentRunRequest
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	if req.AgentName == "" {
		writeError(w, http.StatusBadRequest, "agent_name is required")
		return
	}
	if !h.apiKeyAllowsProject(r, req.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}
	if req.ActionType == "" {
		req.ActionType = "update"
	}
	if !models.ValidAgentActionTypes[req.ActionType] {
		writeError(w, http.StatusBadRequest, "invalid action_type")
		return
	}

	run, err := h.store.Create(req.ProjectID, req.CreateAgentRunRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent run")
		return
	}
	writeSuccess(w, http.StatusCreated, run, nil)
}

// Update PATCH /api/agent-runs/{id}
func (h *AgentRunHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent run")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	if !h.apiKeyAllowsProject(r, existing.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req models.UpdateAgentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "" && !models.ValidAgentRunStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if req.Status == "running" && existing.Status != "running" {
		writeError(w, http.StatusBadRequest, "cannot transition completed or failed run back to running")
		return
	}

	run, err := h.store.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent run")
		return
	}
	writeSuccess(w, http.StatusOK, run, nil)
}

// ListByProject GET /api/projects/{id}/agent-runs
func (h *AgentRunHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !h.apiKeyAllowsProject(r, projectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}
	page, perPage := parsePagination(r)

	runs, total, err := h.store.ListByProject(projectID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent runs")
		return
	}
	writeSuccess(w, http.StatusOK, runs, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

// Get GET /api/agent-runs/{id}
func (h *AgentRunHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent run")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	if !h.apiKeyAllowsProject(r, run.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}
	writeSuccess(w, http.StatusOK, run, nil)
}

func (h *AgentRunHandler) apiKeyAllowsProject(r *http.Request, projectID string) bool {
	return requestAllowsProject(r, projectID) && middleware.APIKeyFromContext(r.Context()) != nil
}
