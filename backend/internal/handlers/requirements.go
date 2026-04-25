package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type RequirementHandler struct {
	store        *store.RequirementStore
	projectStore *store.ProjectStore
}

func NewRequirementHandler(s *store.RequirementStore, ps *store.ProjectStore) *RequirementHandler {
	return &RequirementHandler{store: s, projectStore: ps}
}

func (h *RequirementHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	page, perPage := parsePagination(r)

	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, project.ID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	requirements, total, err := h.store.ListByProject(projectID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list requirements")
		return
	}
	if requirements == nil {
		requirements = []models.Requirement{}
	}

	writeSuccess(w, http.StatusOK, requirements, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *RequirementHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	requirement, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get requirement")
		return
	}
	if requirement == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	if !requestAllowsProject(r, requirement.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	writeSuccess(w, http.StatusOK, requirement, nil)
}

func (h *RequirementHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, project.ID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req models.CreateRequirementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	requirement, err := h.store.Create(projectID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create requirement")
		return
	}

	writeSuccess(w, http.StatusCreated, requirement, nil)
}

func (h *RequirementHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	req, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify requirement")
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	if !requestAllowsProject(r, req.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}
	hasLineage, err := h.store.HasAppliedTasks(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check requirement lineage")
		return
	}
	if hasLineage {
		writeError(w, http.StatusConflict, "requirement has applied tasks and cannot be deleted; use archive instead")
		return
	}
	hasActive, err := h.store.HasActiveRun(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check active runs")
		return
	}
	if hasActive {
		writeError(w, http.StatusConflict, "requirement has an active planning run; wait for it to complete before deleting")
		return
	}
	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete requirement")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RequirementHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	requirement, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get requirement")
		return
	}
	if requirement == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	if !requestAllowsProject(r, requirement.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req struct {
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == nil {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	if !models.ValidRequirementStatuses[*req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	updated, err := h.store.UpdateStatus(id, *req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update requirement")
		return
	}
	if updated == nil {
		writeError(w, http.StatusNotFound, "requirement not found")
		return
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}
