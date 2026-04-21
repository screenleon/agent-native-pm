package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type AccountBindingHandler struct {
	store *store.AccountBindingStore
}

func NewAccountBindingHandler(s *store.AccountBindingStore) *AccountBindingHandler {
	return &AccountBindingHandler{store: s}
}

// List GET /api/me/account-bindings
func (h *AccountBindingHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	bindings, err := h.store.ListByUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list account bindings")
		return
	}
	writeSuccess(w, http.StatusOK, bindings, nil)
}

// Create POST /api/me/account-bindings
func (h *AccountBindingHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req models.CreateAccountBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	binding, err := h.store.Create(user.ID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusCreated, binding, nil)
}

// Update PATCH /api/me/account-bindings/:id
func (h *AccountBindingHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "binding id is required")
		return
	}
	var req models.UpdateAccountBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	binding, err := h.store.Update(id, user.ID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, binding, nil)
}

// Delete DELETE /api/me/account-bindings/:id
func (h *AccountBindingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "binding id is required")
		return
	}
	if err := h.store.Delete(id, user.ID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}
