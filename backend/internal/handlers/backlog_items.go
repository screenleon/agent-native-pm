package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type BacklogItemHandler struct {
	store        *store.BacklogItemStore
	projectStore *store.ProjectStore
}

func NewBacklogItemHandler(s *store.BacklogItemStore, ps *store.ProjectStore) *BacklogItemHandler {
	return &BacklogItemHandler{store: s, projectStore: ps}
}

func (h *BacklogItemHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !h.projectReadable(w, r, projectID) {
		return
	}

	page, perPage := parsePagination(r)
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	filters := models.BacklogItemListFilters{
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
		Source:   r.URL.Query().Get("source"),
		Label:    strings.TrimSpace(r.URL.Query().Get("label")),
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
	}
	if !validateBacklogItemFilters(w, filters) {
		return
	}

	items, total, err := h.store.ListByProject(projectID, page, perPage, sort, order, filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list backlog items")
		return
	}
	if items == nil {
		items = []models.BacklogItem{}
	}
	writeSuccess(w, http.StatusOK, items, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *BacklogItemHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !h.projectReadable(w, r, projectID) {
		return
	}

	var req models.CreateBacklogItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Status != "" && !models.ValidBacklogItemStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if req.Status == models.BacklogItemStatusCommitted {
		writeError(w, http.StatusBadRequest, "committed status is set by commit-to-task")
		return
	}
	if req.Priority != "" && !models.ValidTaskPriorities[req.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}
	if req.Source != "" && !models.ValidBacklogItemSources[req.Source] {
		writeError(w, http.StatusBadRequest, "invalid source value")
		return
	}

	item, err := h.store.Create(projectID, req)
	if err != nil {
		if errors.Is(err, store.ErrBacklogItemInvalidOrigin) {
			writeError(w, http.StatusBadRequest, "origin fields must belong to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create backlog item")
		return
	}
	writeSuccess(w, http.StatusCreated, item, nil)
}

func (h *BacklogItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, ok := h.itemReadable(w, r)
	if !ok {
		return
	}
	writeSuccess(w, http.StatusOK, item, nil)
}

func (h *BacklogItemHandler) Update(w http.ResponseWriter, r *http.Request) {
	item, ok := h.itemReadable(w, r)
	if !ok {
		return
	}

	var req models.UpdateBacklogItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title cannot be blank")
		return
	}
	if req.Status != nil && !models.ValidBacklogItemStatuses[*req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if req.Status != nil && *req.Status == models.BacklogItemStatusCommitted && (item.Status != models.BacklogItemStatusCommitted || item.TaskID == "") {
		writeError(w, http.StatusBadRequest, "committed status is set by commit-to-task")
		return
	}
	if req.Status != nil && item.TaskID != "" && *req.Status != models.BacklogItemStatusCommitted {
		writeError(w, http.StatusConflict, "committed backlog item status cannot be changed")
		return
	}
	if req.Priority != nil && !models.ValidTaskPriorities[*req.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}
	if item.TaskID != "" && changesCommittedTaskSnapshot(item, req) {
		writeError(w, http.StatusConflict, "committed backlog item task fields cannot be changed")
		return
	}

	updated, err := h.store.Update(item.ID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update backlog item")
		return
	}
	if updated == nil {
		writeError(w, http.StatusNotFound, "backlog item not found")
		return
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}

func (h *BacklogItemHandler) CommitToTask(w http.ResponseWriter, r *http.Request) {
	item, ok := h.itemReadable(w, r)
	if !ok {
		return
	}

	response, err := h.store.CommitToTask(item.ID)
	if err != nil {
		if errors.Is(err, store.ErrBacklogItemAlreadyArchived) {
			writeError(w, http.StatusConflict, "archived backlog item cannot be committed")
			return
		}
		if errors.Is(err, store.ErrBacklogItemNotCommittable) {
			writeError(w, http.StatusConflict, "backlog item must be triage or ready before commit")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to commit backlog item")
		return
	}
	if response == nil {
		writeError(w, http.StatusNotFound, "backlog item not found")
		return
	}
	writeSuccess(w, http.StatusOK, response, nil)
}

func (h *BacklogItemHandler) projectReadable(w http.ResponseWriter, r *http.Request, projectID string) bool {
	if !requestAllowsProject(r, projectID) {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return false
	}
	if project == nil || !projectAllowedForUser(r, h.projectStore, projectID) {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	return true
}

func changesCommittedTaskSnapshot(item *models.BacklogItem, req models.UpdateBacklogItemRequest) bool {
	if req.Title != nil && strings.TrimSpace(*req.Title) != item.Title {
		return true
	}
	if req.Description != nil && *req.Description != item.Description {
		return true
	}
	if req.Priority != nil && *req.Priority != item.Priority {
		return true
	}
	return false
}

func (h *BacklogItemHandler) itemReadable(w http.ResponseWriter, r *http.Request) (*models.BacklogItem, bool) {
	id := chi.URLParam(r, "id")
	item, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get backlog item")
		return nil, false
	}
	if item == nil || !requestAllowsProject(r, item.ProjectID) || !projectAllowedForUser(r, h.projectStore, item.ProjectID) {
		writeError(w, http.StatusNotFound, "backlog item not found")
		return nil, false
	}
	return item, true
}

func validateBacklogItemFilters(w http.ResponseWriter, filters models.BacklogItemListFilters) bool {
	if filters.Status != "" && !models.ValidBacklogItemStatuses[filters.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return false
	}
	if filters.Priority != "" && !models.ValidTaskPriorities[filters.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return false
	}
	if filters.Source != "" && !models.ValidBacklogItemSources[filters.Source] {
		writeError(w, http.StatusBadRequest, "invalid source value")
		return false
	}
	return true
}
