package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/activity"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// connectorOnlineWindow is how recently a connector must have been seen
// for it to be considered "online". Mirrors LocalConnectorLivenessWindow
// from the store package (90 seconds = 3x the 30s heartbeat interval).
const connectorOnlineWindow = 90 * time.Second

// ConnectorActivityHandler handles Phase 6c PR-4 connector activity endpoints.
type ConnectorActivityHandler struct {
	hub        *activity.Hub
	connectors *store.LocalConnectorStore
	projects   *store.ProjectStore
}

// NewConnectorActivityHandler creates a ConnectorActivityHandler.
func NewConnectorActivityHandler(hub *activity.Hub, connectors *store.LocalConnectorStore, projects *store.ProjectStore) *ConnectorActivityHandler {
	return &ConnectorActivityHandler{
		hub:        hub,
		connectors: connectors,
		projects:   projects,
	}
}

// Report handles POST /api/connector/activity — connector-authenticated.
// The connector body is a models.ConnectorActivity JSON payload.
// Returns 202 Accepted on success.
func (h *ConnectorActivityHandler) Report(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.Header.Get("X-Connector-Token"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "connector token required")
		return
	}
	connector, err := h.connectors.GetByToken(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify connector token")
		return
	}
	if connector == nil || connector.Status == models.LocalConnectorStatusRevoked {
		writeError(w, http.StatusUnauthorized, "connector token is invalid")
		return
	}

	var a models.ConnectorActivity
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Normalize phase to a known value; unknown phases are stored as-is
	// (future phases should be forwards-compatible).
	if strings.TrimSpace(a.Phase) == "" {
		a.Phase = models.ConnectorPhaseIdle
	}
	now := time.Now().UTC()
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	if a.StartedAt.IsZero() {
		a.StartedAt = now
	}

	h.hub.Update(connector.ID, a)
	writeSuccess(w, http.StatusAccepted, nil, nil)
}

// Get handles GET /api/me/local-connectors/:id/activity — user-authenticated,
// polling fallback. Returns a ConnectorActivityResponse.
func (h *ConnectorActivityHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	connectorID := chi.URLParam(r, "id")
	if strings.TrimSpace(connectorID) == "" {
		writeError(w, http.StatusBadRequest, "connector id is required")
		return
	}
	// Ownership check: connector must belong to authenticated user.
	connector, err := h.connectors.GetByID(connectorID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load connector")
		return
	}
	if connector == nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	online := isConnectorOnline(connector)

	// Prefer in-memory hub state; fall back to DB if the hub has no entry
	// (e.g. after server restart before the connector next reports activity).
	a, hasHub := h.hub.Get(connectorID)
	ageSeconds := 0
	var actPtr *models.ConnectorActivity
	if hasHub && a.Phase != "" {
		actPtr = &a
		if !a.UpdatedAt.IsZero() {
			ageSeconds = int(time.Since(a.UpdatedAt).Seconds())
			if ageSeconds < 0 {
				ageSeconds = 0
			}
		}
	} else {
		dbActivity, _, dbErr := h.connectors.GetActivity(connectorID)
		if dbErr == nil && dbActivity != nil {
			actPtr = dbActivity
			if !dbActivity.UpdatedAt.IsZero() {
				ageSeconds = int(time.Since(dbActivity.UpdatedAt).Seconds())
				if ageSeconds < 0 {
					ageSeconds = 0
				}
			}
		}
	}

	resp := models.ConnectorActivityResponse{
		Activity:   actPtr,
		Online:     online,
		AgeSeconds: ageSeconds,
	}
	writeSuccess(w, http.StatusOK, resp, nil)
}

// Stream handles GET /api/me/local-connectors/:id/activity-stream —
// user-authenticated SSE endpoint.
// Sends the current activity immediately on connect, then pushes updates.
// Keepalive comments are sent every 30 seconds.
func (h *ConnectorActivityHandler) Stream(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	connectorID := chi.URLParam(r, "id")
	if strings.TrimSpace(connectorID) == "" {
		writeError(w, http.StatusBadRequest, "connector id is required")
		return
	}
	// Ownership check.
	connector, err := h.connectors.GetByID(connectorID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load connector")
		return
	}
	if connector == nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe with per-user cap. Returns 503 if the user already has
	// maxSSEPerUser concurrent SSE connections (DECISIONS 2026-04-25 §(g)).
	initial, ch, unsub, capErr := h.hub.SubscribeWithCap(connectorID, user.ID)
	if capErr != nil {
		writeError(w, http.StatusServiceUnavailable, "too many concurrent activity streams; close other tabs or wait")
		return
	}
	defer unsub()

	// Re-read the connector for online status.
	online := isConnectorOnline(connector)

	// Send initial state immediately. If the hub has no entry, try DB.
	if initial.Phase != "" {
		sendActivityEvent(w, flusher, &initial, online)
	} else {
		// Try DB fallback so a freshly connected browser sees something.
		dbActivity, _, _ := h.connectors.GetActivity(connectorID)
		sendActivityEvent(w, flusher, dbActivity, online)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case a, ok := <-ch:
			if !ok {
				return
			}
			// Re-check online status on each update (heartbeat may have
			// arrived between the initial check and now). Best-effort: reuse
			// the cached connector if the store call fails.
			if fresh, err := h.connectors.GetByID(connectorID, user.ID); err == nil && fresh != nil {
				connector = fresh
			}
			online = isConnectorOnline(connector)
			sendActivityEvent(w, flusher, &a, online)
		case <-ticker.C:
			// Keepalive comment to prevent proxy/browser timeout.
			fmt.Fprintf(w, ":\n\n")
			flusher.Flush()
		}
	}
}

// ListActive handles GET /api/projects/:id/active-connectors — user-authenticated.
// Returns all connectors belonging to the authenticated user. Connectors are
// not yet project-scoped (Phase 6c); the project ID is validated for existence
// and access but does not filter the connector list.
// TODO(phase7): filter by connectors assigned to the project once connector-
// project assignments are modelled.
func (h *ConnectorActivityHandler) ListActive(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID := chi.URLParam(r, "id")
	if projectID != "" && h.projects != nil {
		if proj, err := h.projects.GetByID(projectID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify project")
			return
		} else if proj == nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}

	connectors, err := h.connectors.ListByUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connectors")
		return
	}

	entries := make([]models.ActiveConnectorEntry, 0, len(connectors))
	for _, c := range connectors {
		if c.Status == models.LocalConnectorStatusRevoked {
			continue
		}
		online := isConnectorOnline(&c)
		a, hasActivity := h.hub.Get(c.ID)
		ageSeconds := 0
		var actPtr *models.ConnectorActivity
		if hasActivity && a.Phase != "" {
			actPtr = &a
			if !a.UpdatedAt.IsZero() {
				ageSeconds = int(time.Since(a.UpdatedAt).Seconds())
				if ageSeconds < 0 {
					ageSeconds = 0
				}
			}
		}
		// Only include connectors that are online or have recent activity.
		if !online && actPtr == nil {
			continue
		}
		entries = append(entries, models.ActiveConnectorEntry{
			ConnectorID: c.ID,
			Label:       c.Label,
			Activity:    actPtr,
			Online:      online,
			AgeSeconds:  ageSeconds,
		})
	}

	writeSuccess(w, http.StatusOK, entries, nil)
}

// isConnectorOnline reports whether the connector's last heartbeat was within
// the online window (90 seconds).
func isConnectorOnline(c *models.LocalConnector) bool {
	if c == nil || c.LastSeenAt == nil {
		return false
	}
	return time.Since(*c.LastSeenAt) <= connectorOnlineWindow
}

// sendActivityEvent writes a named SSE event containing the activity and
// online status. If activity is nil, a zero-activity payload is sent.
func sendActivityEvent(w http.ResponseWriter, flusher http.Flusher, a *models.ConnectorActivity, online bool) {
	type payload struct {
		Activity *models.ConnectorActivity `json:"activity"`
		Online   bool                      `json:"online"`
	}
	data, _ := json.Marshal(payload{Activity: a, Online: online})
	fmt.Fprintf(w, "event: activity\ndata: %s\n\n", data)
	flusher.Flush()
}
