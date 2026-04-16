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

	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	summary, err := h.store.ComputeSummary(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute summary")
		return
	}
	if err := h.store.SaveDailySnapshot(summary); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save summary snapshot")
		return
	}
	writeSuccess(w, http.StatusOK, summary, nil)
}

func (h *SummaryHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
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

	history, err := h.store.GetHistory(projectID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get summary history")
		return
	}
	writeSuccess(w, http.StatusOK, history, nil)
}
