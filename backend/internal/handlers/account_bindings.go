package handlers

import (
	"encoding/json"
	"errors"
	"log"
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
	// connectors is optional; when set the Delete path scrubs any pending
	// or completed CLI-binding probe entries across all user connectors.
	// Wired in main.go via WithLocalConnectorStore. Added in Phase 4 (P4-4).
	connectors *store.LocalConnectorStore
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

// WithLocalConnectorStore enables cleanup of CLI-binding probe entries when a
// binding is deleted (P4-4). Optional: without it, delete still succeeds and
// stale probe rows are eventually GC'd by the 24h retention sweep.
func (h *AccountBindingHandler) WithLocalConnectorStore(connectors *store.LocalConnectorStore) *AccountBindingHandler {
	if h == nil {
		return nil
	}
	h.connectors = connectors
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

	// D8 gate: allowlisted cli:* providers require local mode. Enforced in
	// the handler (not the store) because LocalMode is a runtime mode flag
	// that does not belong in the persistence layer.
	//
	// Allowlist precedence: an unrecognized cli:* value (e.g. cli:unknown)
	// must surface as 400 from the store's allowlist check, NOT as 403 from
	// this gate. Otherwise the operator gets a misleading "feature unavailable"
	// when they actually mistyped the provider id. So we only fire the 403
	// gate for cli:* values that are in the allowlist (cli:claude, cli:codex).
	if models.IsCLIAccountBindingProvider(req.ProviderID) &&
		models.AllowedAccountBindingProviderIDs[req.ProviderID] &&
		!h.localMode {
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
	// Best-effort scrub of probe metadata referencing this binding. A failure
	// here does not un-do the delete; the 24h retention sweep will catch
	// leftovers.
	if h.connectors != nil {
		if err := h.connectors.ScrubCliProbesForBinding(user.ID, id); err != nil {
			log.Printf("account_bindings Delete: scrub probe metadata for binding %s: %v", id, err)
		}
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}

// writeAccountBindingError maps store sentinel errors to HTTP status codes
// per design §6.2 rule 8 (uniqueness conflicts surface as 409, not 500/400).
//
// Anything that is NOT a recognised sentinel is treated as an internal error:
// we log the underlying message server-side for ops and return a generic 500
// to the client. This avoids leaking internal details (DB connection state,
// secret-storage misconfiguration, etc.) to API consumers and avoids the
// previous bug where unmapped errors were misclassified as 400 client errors.
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
		log.Printf("account_bindings handler: unmapped store error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
