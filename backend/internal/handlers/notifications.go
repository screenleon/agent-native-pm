package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/events"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// NotificationHandler handles Phase 4 in-app notifications.
type NotificationHandler struct {
	store  *store.NotificationStore
	broker *events.Broker
	// sessions is needed to validate ?token= for SSE (EventSource cannot set
	// Authorization headers in the browser).
	sessions *store.SessionStore
}

func NewNotificationHandler(s *store.NotificationStore) *NotificationHandler {
	return &NotificationHandler{store: s}
}

// WithBroker enables the SSE stream endpoint.
func (h *NotificationHandler) WithBroker(b *events.Broker, sessions *store.SessionStore) *NotificationHandler {
	h.broker = b
	h.sessions = sessions
	return h
}

// List GET /api/notifications?user_id=...&unread_only=true
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		user := middleware.UserFromContext(r.Context())
		if user != nil {
			userID = user.ID
		}
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	unreadOnly := r.URL.Query().Get("unread_only") == "true"
	page, perPage := parsePagination(r)

	notes, total, err := h.store.ListByUser(userID, unreadOnly, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	writeSuccess(w, http.StatusOK, notes, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

// Create POST /api/notifications — internal / admin use
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "user_id and title are required")
		return
	}
	note, err := h.store.Create(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notification")
		return
	}
	writeSuccess(w, http.StatusCreated, note, nil)
}

// MarkRead PATCH /api/notifications/{id}/read
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.MarkRead(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark notification as read")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"read": true}, nil)
}

// MarkAllRead POST /api/notifications/read-all?user_id=...
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		user := middleware.UserFromContext(r.Context())
		if user != nil {
			userID = user.ID
		}
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if err := h.store.MarkAllRead(userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all read")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"done": true}, nil)
}

// MarkUnread PATCH /api/notifications/{id}/unread
func (h *NotificationHandler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.MarkUnread(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark notification as unread")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"read": false}, nil)
}

// UnreadCount GET /api/notifications/unread-count?user_id=...
func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		user := middleware.UserFromContext(r.Context())
		if user != nil {
			userID = user.ID
		}
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	count, err := h.store.CountUnread(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count notifications")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]int{"unread": count}, nil)
}

// Stream GET /api/notifications/stream — Server-Sent Events endpoint.
// EventSource cannot set Authorization headers, so the token is accepted
// via a ?token= query parameter validated directly against the session store.
func (h *NotificationHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if h.broker == nil || h.sessions == nil {
		writeError(w, http.StatusNotImplemented, "SSE stream not configured")
		return
	}

	// Authenticate via context (standard Bearer/cookie path) or ?token= param.
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		rawToken := r.URL.Query().Get("token")
		if rawToken != "" {
			u, err := h.sessions.Validate(rawToken)
			if err == nil && u != nil {
				user = u
			}
		}
	}
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
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

	ch, unsub := h.broker.Subscribe(user.ID)
	defer unsub()

	// Send current unread count immediately so the client badge is accurate.
	if count, err := h.store.CountUnread(user.ID); err == nil {
		data, _ := json.Marshal(map[string]int{"unread": count})
		fmt.Fprintf(w, "event: unread-count\ndata: %s\n\n", data)
		flusher.Flush()
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}
