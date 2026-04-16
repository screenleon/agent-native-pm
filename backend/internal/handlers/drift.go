package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// DriftSignalHandler handles Phase 2 drift signal endpoints.
type DriftSignalHandler struct {
	store        *store.DriftSignalStore
	docStore     *store.DocumentStore
	projectStore *store.ProjectStore
}

func NewDriftSignalHandler(
	s *store.DriftSignalStore,
	docStore *store.DocumentStore,
	projectStore *store.ProjectStore,
) *DriftSignalHandler {
	return &DriftSignalHandler{store: s, docStore: docStore, projectStore: projectStore}
}

// ListByProject GET /api/projects/{id}/drift-signals
// Query params:
//
//	status   = open | resolved | dismissed (empty = all)
//	sort_by  = severity | created_at  (default: created_at)
func (h *DriftSignalHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	statusFilter := r.URL.Query().Get("status")
	sortBy := r.URL.Query().Get("sort_by")
	page, perPage := parsePagination(r)

	signals, total, err := h.store.ListByProject(projectID, statusFilter, sortBy, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list drift signals")
		return
	}
	writeSuccess(w, http.StatusOK, signals, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

// Create POST /api/projects/{id}/drift-signals
func (h *DriftSignalHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")

	var req models.CreateDriftSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DocumentID == "" {
		writeError(w, http.StatusBadRequest, "document_id is required")
		return
	}
	if req.TriggerType == "" {
		req.TriggerType = "manual"
	}
	if !models.ValidTriggerTypes[req.TriggerType] {
		writeError(w, http.StatusBadRequest, "invalid trigger_type")
		return
	}

	signal, err := h.store.Create(projectID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create drift signal")
		return
	}
	writeSuccess(w, http.StatusCreated, signal, nil)
}

// Update PATCH /api/drift-signals/{id}
func (h *DriftSignalHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateDriftSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != nil && !models.ValidDriftSignalStatuses[*req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	signal, err := h.store.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update drift signal")
		return
	}
	if signal == nil {
		writeError(w, http.StatusNotFound, "drift signal not found")
		return
	}
	writeSuccess(w, http.StatusOK, signal, nil)
}

// BulkResolveByProject POST /api/projects/{id}/drift-signals/resolve-all
func (h *DriftSignalHandler) BulkResolveByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var req struct {
		ResolvedBy string `json:"resolved_by"`
	}
	// ignore decode error — fall back to default
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ResolvedBy == "" {
		req.ResolvedBy = "human"
	}
	count, err := h.store.BulkResolveByProject(projectID, req.ResolvedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bulk resolve drift signals")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]int{"resolved": count}, nil)
}

// DocumentLinkHandler handles document-to-code-file link endpoints.
type DocumentLinkHandler struct {
	store    *store.DocumentLinkStore
	docStore *store.DocumentStore
}

func NewDocumentLinkHandler(s *store.DocumentLinkStore, docStore *store.DocumentStore) *DocumentLinkHandler {
	return &DocumentLinkHandler{store: s, docStore: docStore}
}

// List GET /api/documents/{id}/links
func (h *DocumentLinkHandler) List(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")
	links, err := h.store.ListByDocument(docID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list document links")
		return
	}
	writeSuccess(w, http.StatusOK, links, nil)
}

// Create POST /api/documents/{id}/links
func (h *DocumentLinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")

	doc, err := h.docStore.GetByID(docID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	var req models.CreateDocumentLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CodePath == "" {
		writeError(w, http.StatusBadRequest, "code_path is required")
		return
	}
	if req.LinkType == "" {
		req.LinkType = "covers"
	}
	if !models.ValidLinkTypes[req.LinkType] {
		writeError(w, http.StatusBadRequest, "invalid link_type")
		return
	}

	link, err := h.store.Create(docID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create document link")
		return
	}
	writeSuccess(w, http.StatusCreated, link, nil)
}

// Delete DELETE /api/document-links/{id}
func (h *DocumentLinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete document link")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"deleted": true}, nil)
}
