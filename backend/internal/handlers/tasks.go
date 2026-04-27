package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type TaskHandler struct {
	store        *store.TaskStore
	projectStore *store.ProjectStore
}

const taskBatchUpdateLimit = 100

func NewTaskHandler(s *store.TaskStore, ps *store.ProjectStore) *TaskHandler {
	return &TaskHandler{store: s, projectStore: ps}
}

func (h *TaskHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	page, perPage := parsePagination(r)
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	filters := models.TaskListFilters{
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
		Assignee: strings.TrimSpace(r.URL.Query().Get("assignee")),
	}

	if filters.Status != "" && !models.ValidTaskStatuses[filters.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if filters.Priority != "" && !models.ValidTaskPriorities[filters.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}

	tasks, total, err := h.store.ListByProject(projectID, page, perPage, sort, order, filters)
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

func (h *TaskHandler) BatchUpdate(w http.ResponseWriter, r *http.Request) {
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

	var req models.BatchUpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.TaskIDs = normalizeTaskIDs(req.TaskIDs)
	if len(req.TaskIDs) == 0 {
		writeError(w, http.StatusBadRequest, "task_ids must include at least one task")
		return
	}
	if len(req.TaskIDs) > taskBatchUpdateLimit {
		writeError(w, http.StatusBadRequest, "task_ids exceeds batch update limit")
		return
	}
	if !req.Changes.HasChanges() {
		writeError(w, http.StatusBadRequest, "changes must include at least one updatable field")
		return
	}
	if req.Changes.Status != nil && !models.ValidTaskStatuses[*req.Changes.Status] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}
	if req.Changes.Priority != nil && !models.ValidTaskPriorities[*req.Changes.Priority] {
		writeError(w, http.StatusBadRequest, "invalid priority value")
		return
	}

	tasks, err := h.store.BatchUpdate(projectID, req.TaskIDs, req.Changes)
	if err != nil {
		if errors.Is(err, store.ErrTaskBatchNotFound) {
			writeError(w, http.StatusNotFound, "one or more tasks not found in project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to batch update tasks")
		return
	}

	writeSuccess(w, http.StatusOK, models.BatchUpdateTaskResponse{
		UpdatedCount: len(tasks),
		Tasks:        tasks,
	}, nil)
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

// RequeueDispatch implements POST /api/tasks/:id/requeue-dispatch.
// Resets a failed role_dispatch task back to queued so the connector
// can re-attempt it. Returns 404 when the task is not found, 409 when
// the task is not in a failed state or belongs to another user's project.
func (h *TaskHandler) RequeueDispatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	userID := ""
	if user := middleware.UserFromContext(r.Context()); user != nil {
		userID = user.ID
	}

	task, err := h.store.RequeueDispatchTask(id, userID)
	if err != nil {
		if errors.Is(err, store.ErrDispatchOwnership) {
			writeError(w, http.StatusConflict, "task cannot be requeued: not failed, not a role_dispatch task, or not owned by this user")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to requeue task")
		return
	}
	writeSuccess(w, http.StatusOK, task, nil)
}

func normalizeTaskIDs(taskIDs []string) []string {
	seen := make(map[string]bool, len(taskIDs))
	normalized := make([]string, 0, len(taskIDs))
	for _, rawID := range taskIDs {
		trimmed := strings.TrimSpace(rawID)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	return normalized
}
