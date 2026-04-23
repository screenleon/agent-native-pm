package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type AccountBindingHandler struct {
	store *store.AccountBindingStore
	// localMode mirrors config.Config.LocalMode. When false, the handler
	// rejects any request whose provider_id is `cli:*` with 403 (Path B
	// Slice S1, design §5 D8 / §6.2 rule 2). The CLI bridge is a
	// subprocess-execution surface; multi-user server mode would let one
	// user's cli_command run as the connector host's identity.
	localMode bool
}

func NewAccountBindingHandler(s *store.AccountBindingStore) *AccountBindingHandler {
	return &AccountBindingHandler{store: s}
}

// WithLocalMode wires the runtime LocalMode flag so the handler can enforce
// the D8 gate on cli:* providers. Returns the same handler for fluent
// composition in main.go.
func (h *AccountBindingHandler) WithLocalMode(localMode bool) *AccountBindingHandler {
	if h == nil {
		return nil
	}
	h.localMode = localMode
	return h
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

	// D8 gate: cli:* providers require local mode. Enforced in the handler
	// (not the store) because LocalMode is a runtime mode flag that does not
	// belong in the persistence layer. Handlers are also where 403 vs 400
	// distinction is meaningful.
	if models.IsCLIAccountBindingProvider(req.ProviderID) && !h.localMode {
		writeError(w, http.StatusForbidden, "cli:* providers are only available in local-mode deployments")
		return
	}

	binding, err := h.store.Create(user.ID, req)
	if err != nil {
		writeAccountBindingError(w, err)
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

	// D8 gate also applies on Update: an existing cli:* binding cannot be
	// patched in server mode. We resolve the existing row's provider_id
	// rather than trusting any caller-supplied field — Update never lets
	// the operator change provider_id.
	if !h.localMode {
		existing, err := h.store.GetByID(id, user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load account binding")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "account binding not found")
			return
		}
		if models.IsCLIAccountBindingProvider(existing.ProviderID) {
			writeError(w, http.StatusForbidden, "cli:* providers are only available in local-mode deployments")
			return
		}
	}

	binding, err := h.store.Update(id, user.ID, req)
	if err != nil {
		writeAccountBindingError(w, err)
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
		writeAccountBindingError(w, err)
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}

// writeAccountBindingError maps store sentinel errors to HTTP status codes
// per design §6.2 rule 8 (uniqueness conflicts surface as 409, not 500/400).
func writeAccountBindingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrAccountBindingValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAccountBindingForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrAccountBindingNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrAccountBindingActiveConflict),
		errors.Is(err, store.ErrAccountBindingPrimaryConflict),
		errors.Is(err, store.ErrAccountBindingDuplicateLabel):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
