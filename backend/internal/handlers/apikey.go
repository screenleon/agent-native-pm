package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// APIKeyHandler handles Phase 3 API key management.
type APIKeyHandler struct {
	store *store.APIKeyStore
}

func NewAPIKeyHandler(s *store.APIKeyStore) *APIKeyHandler {
	return &APIKeyHandler{store: s}
}

// List GET /api/keys?project_id=...
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	pidStr := r.URL.Query().Get("project_id")
	var pid *string
	if pidStr != "" {
		pid = &pidStr
	}
	keys, err := h.store.List(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list API keys")
		return
	}
	writeSuccess(w, http.StatusOK, keys, nil)
}

// Create POST /api/keys
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	result, err := h.store.Create(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create API key")
		return
	}
	writeSuccess(w, http.StatusCreated, result, nil)
}

// Revoke DELETE /api/keys/{id}
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.Revoke(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke API key")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"revoked": true}, nil)
}

// DocumentRefreshHandler handles Phase 3 document summary refresh.
type DocumentRefreshHandler struct {
	docStore   *store.DocumentStore
	driftStore *store.DriftSignalStore
}

func NewDocumentRefreshHandler(docStore *store.DocumentStore, driftStore *store.DriftSignalStore) *DocumentRefreshHandler {
	return &DocumentRefreshHandler{docStore: docStore, driftStore: driftStore}
}

// RefreshSummary POST /api/documents/{id}/refresh-summary
func (h *DocumentRefreshHandler) RefreshSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	doc, err := h.docStore.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if !requestAllowsProject(r, doc.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	if err := h.docStore.RefreshSummary(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refresh document summary")
		return
	}

	caller := "api_key"
	if u := middleware.UserFromContext(r.Context()); u != nil {
		caller = u.ID
	}
	if _, err := h.driftStore.ResolveOpenByDocumentID(id, caller); err != nil {
		log.Printf("refresh-summary: drift resolve for doc %s: %v", id, err)
	}

	updated, err := h.docStore.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch updated document")
		return
	}
	writeSuccess(w, http.StatusOK, updated, nil)
}
