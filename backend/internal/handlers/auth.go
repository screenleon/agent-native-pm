package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

// UserHandler handles Phase 4 user management.
type UserHandler struct {
	userStore    *store.UserStore
	sessionStore *store.SessionStore
}

func NewUserHandler(userStore *store.UserStore, sessionStore *store.SessionStore) *UserHandler {
	return &UserHandler{userStore: userStore, sessionStore: sessionStore}
}

// NeedsSetup GET /api/auth/needs-setup — public, no auth required
func (h *UserHandler) NeedsSetup(w http.ResponseWriter, r *http.Request) {
	total, err := h.userStore.CountAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check setup state")
		return
	}
	writeSuccess(w, http.StatusOK, map[string]bool{"needs_setup": total == 0}, nil)
}

// Register POST /api/auth/register
func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}

	total, err := h.userStore.CountAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check setup state")
		return
	}
	adminCount, err := h.userStore.CountAdmins()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check admin state")
		return
	}

	// Registration endpoint is bootstrap-only.
	// Once setup is complete (users and at least one admin exist), block further open registration.
	if total > 0 && adminCount > 0 {
		writeError(w, http.StatusForbidden, "setup already completed")
		return
	}

	// First user auto-promotes to admin
	if total == 0 {
		req.Role = "admin"
	} else if adminCount == 0 {
		// No admins left — allow re-creation of admin
		req.Role = "admin"
	}

	user, err := h.userStore.Create(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeSuccess(w, http.StatusCreated, user, nil)
}

// Login POST /api/auth/login
func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	totalUsers, err := h.userStore.CountAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication error")
		return
	}
	if totalUsers == 0 {
		writeError(w, http.StatusUnauthorized, "setup required")
		return
	}

	user, err := h.userStore.Authenticate(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication error")
		return
	}
	if user == nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := h.sessionStore.Create(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	writeSuccess(w, http.StatusOK, models.LoginResponse{Token: token, User: *user}, nil)
}

// Logout POST /api/auth/logout
func (h *UserHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		_ = h.sessionStore.Delete(cookie.Value)
	}
	// Also support Bearer token logout
	token := extractBearerToken(r)
	if token != "" {
		_ = h.sessionStore.Delete(token)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1, Path: "/"})
	writeSuccess(w, http.StatusOK, map[string]bool{"logged_out": true}, nil)
}

// Me GET /api/auth/me
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeSuccess(w, http.StatusOK, user, nil)
}

// List GET /api/users — admin-only
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.userStore.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeSuccess(w, http.StatusOK, users, nil)
}

// Update PATCH /api/users/{id} — admin-only
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := h.userStore.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeSuccess(w, http.StatusOK, user, nil)
}

// Helpers

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}
