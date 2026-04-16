package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type TaskHandler struct {
	store        *store.TaskStore
	projectStore *store.ProjectStore
}

func NewTaskHandler(s *store.TaskStore, ps *store.ProjectStore) *TaskHandler {
	return &TaskHandler{store: s, projectStore: ps}
}

func (h *TaskHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	page, perPage := parsePagination(r)
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")

	tasks, total, err := h.store.ListByProject(projectID, page, perPage, sort, order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}
	writeSuccess(w, http.StatusOK, tasks, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeSuccess(w, http.StatusOK, task, nil)
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	// Verify project exists
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req models.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Status != "" && !models.ValidTaskStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if req.Priority != "" && !models.ValidTaskPriorities[req.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}

	task, err := h.store.Create(projectID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}
	writeSuccess(w, http.StatusCreated, task, nil)
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != nil && !models.ValidTaskStatuses[*req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if req.Priority != nil && !models.ValidTaskPriorities[*req.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}

	task, err := h.store.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeSuccess(w, http.StatusOK, task, nil)
}

func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check task")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete task")
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}
