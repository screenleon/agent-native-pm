package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// NotificationHandler handles Phase 4 in-app notifications.
type NotificationHandler struct {
	store *store.NotificationStore
}

func NewNotificationHandler(s *store.NotificationStore) *NotificationHandler {
	return &NotificationHandler{store: s}
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
