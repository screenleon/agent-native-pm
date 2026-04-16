package handlers

import (
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// SearchHandler handles Phase 4 full-text search.
type SearchHandler struct {
	store *store.SearchStore
}

func NewSearchHandler(s *store.SearchStore) *SearchHandler {
	return &SearchHandler{store: s}
}

// Search GET /api/search?q=...&project_id=...
// Optional filters: type=all|tasks|documents, status=..., doc_type=..., staleness=all|stale|fresh
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	projectID := r.URL.Query().Get("project_id")
	searchType := r.URL.Query().Get("type")
	if searchType == "" {
		searchType = "all"
	}
	if searchType != "all" && searchType != "tasks" && searchType != "documents" {
		writeError(w, http.StatusBadRequest, "invalid type value")
		return
	}

	taskStatus := r.URL.Query().Get("status")
	if taskStatus != "" && !models.ValidTaskStatuses[taskStatus] {
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}

	docType := r.URL.Query().Get("doc_type")
	if docType != "" && !models.ValidDocTypes[docType] {
		writeError(w, http.StatusBadRequest, "invalid doc_type value")
		return
	}

	staleness := r.URL.Query().Get("staleness")
	var staleOnly *bool
	if staleness != "" && staleness != "all" {
		switch staleness {
		case "stale":
			v := true
			staleOnly = &v
		case "fresh":
			v := false
			staleOnly = &v
		default:
			writeError(w, http.StatusBadRequest, "invalid staleness value")
			return
		}
	}

	result, err := h.store.Search(q, projectID, searchType, taskStatus, docType, staleOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeSuccess(w, http.StatusOK, result, nil)
}
