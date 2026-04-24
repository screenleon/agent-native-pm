package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type SummaryHandler struct {
	store        *store.SummaryStore
	projectStore *store.ProjectStore
}

func NewSummaryHandler(s *store.SummaryStore, ps *store.ProjectStore) *SummaryHandler {
	return &SummaryHandler{store: s, projectStore: ps}
}

func (h *SummaryHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	if ok := h.ensureProject(w, projectID); !ok {
		return
	}

	summary, err := h.store.ComputeCurrentSummary(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute summary")
		return
	}
	writeSuccess(w, http.StatusOK, summary, nil)
}

func (h *SummaryHandler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	if ok := h.ensureProject(w, projectID); !ok {
		return
	}

	dashboard, err := h.store.ComputeDashboardSummary(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute dashboard summary")
		return
	}
	writeSuccess(w, http.StatusOK, dashboard, nil)
}

func (h *SummaryHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	if ok := h.ensureProject(w, projectID); !ok {
		return
	}

	history, err := h.store.GetHistory(projectID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get summary history")
		return
	}
	writeSuccess(w, http.StatusOK, history, nil)
}

// GetPendingReviewCount returns the number of backlog candidates in draft
// status for a project. Used by the Dashboard to show "N decisions pending".
func (h *SummaryHandler) GetPendingReviewCount(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if ok := h.ensureProject(w, projectID); !ok {
		return
	}
	count, err := h.store.CountPendingReviewByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count pending review")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]int{"count": count}, nil)
}

func (h *SummaryHandler) ensureProject(w http.ResponseWriter, projectID string) bool {
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return false
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	return true
}
