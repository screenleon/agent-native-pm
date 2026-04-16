package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
)

type Deps struct {
	ProjectHandler            *handlers.ProjectHandler
	TaskHandler               *handlers.TaskHandler
	DocumentHandler           *handlers.DocumentHandler
	SummaryHandler            *handlers.SummaryHandler
	SyncHandler               *handlers.SyncHandler
	AgentRunHandler           *handlers.AgentRunHandler
	DriftSignalHandler        *handlers.DriftSignalHandler
	DocumentLinkHandler       *handlers.DocumentLinkHandler
	APIKeyHandler             *handlers.APIKeyHandler
	DocumentRefreshHandler    *handlers.DocumentRefreshHandler
	ProjectRepoMappingHandler *handlers.ProjectRepoMappingHandler
	UserHandler               *handlers.UserHandler
	NotificationHandler       *handlers.NotificationHandler
	SearchHandler             *handlers.SearchHandler
	AuthMiddleware            func(http.Handler) http.Handler
	APIKeyMiddleware          func(http.Handler) http.Handler
	FrontendDir               string
}

func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Inject auth identity (session + API key) on every request
	if deps.AuthMiddleware != nil {
		r.Use(deps.AuthMiddleware)
	}
	if deps.APIKeyMiddleware != nil {
		r.Use(deps.APIKeyMiddleware)
	}

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", handlers.HealthCheck)

		// ── Auth (public) ──────────────────────────────────────────────
		if deps.UserHandler != nil {
			r.Get("/auth/needs-setup", deps.UserHandler.NeedsSetup)
			r.Post("/auth/register", deps.UserHandler.Register)
			r.Post("/auth/login", deps.UserHandler.Login)
			r.Post("/auth/logout", deps.UserHandler.Logout)
			r.Get("/auth/me", deps.UserHandler.Me)
		}

		// ── Protected routes ──────────────────────────────────────────
		r.Group(func(r chi.Router) {
			if deps.AuthMiddleware != nil {
				r.Use(middleware.RequireAuth)
			}

			// Projects
			r.Get("/projects", deps.ProjectHandler.List)
			r.Post("/projects", deps.ProjectHandler.Create)
			r.Get("/projects/{id}", deps.ProjectHandler.Get)
			r.Patch("/projects/{id}", deps.ProjectHandler.Update)
			r.Delete("/projects/{id}", deps.ProjectHandler.Delete)

			// Tasks
			r.Get("/projects/{id}/tasks", deps.TaskHandler.ListByProject)
			r.Post("/projects/{id}/tasks", deps.TaskHandler.Create)
			r.Get("/tasks/{id}", deps.TaskHandler.Get)
			r.Patch("/tasks/{id}", deps.TaskHandler.Update)
			r.Delete("/tasks/{id}", deps.TaskHandler.Delete)

			// Documents
			r.Get("/projects/{id}/documents", deps.DocumentHandler.ListByProject)
			r.Post("/projects/{id}/documents", deps.DocumentHandler.Create)
			r.Get("/documents/{id}", deps.DocumentHandler.Get)
			r.Get("/documents/{id}/content", deps.DocumentHandler.GetContent)
			r.Patch("/documents/{id}", deps.DocumentHandler.Update)
			r.Delete("/documents/{id}", deps.DocumentHandler.Delete)

			// Document links (Phase 2)
			if deps.DocumentLinkHandler != nil {
				r.Get("/documents/{id}/links", deps.DocumentLinkHandler.List)
				r.Post("/documents/{id}/links", deps.DocumentLinkHandler.Create)
				r.Delete("/document-links/{id}", deps.DocumentLinkHandler.Delete)
			}
			if deps.ProjectRepoMappingHandler != nil {
				r.Get("/repo-mappings/discover", deps.ProjectRepoMappingHandler.Discover)
				r.Get("/projects/{id}/repo-mappings", deps.ProjectRepoMappingHandler.ListByProject)
				r.Post("/projects/{id}/repo-mappings", deps.ProjectRepoMappingHandler.Create)
				r.Delete("/repo-mappings/{id}", deps.ProjectRepoMappingHandler.Delete)
			}
			if deps.DocumentRefreshHandler != nil {
				r.With(middleware.RequireAPIKey).Post("/documents/{id}/refresh-summary", deps.DocumentRefreshHandler.RefreshSummary)
			}

			// Summary
			r.Get("/projects/{id}/summary", deps.SummaryHandler.GetSummary)
			r.Get("/projects/{id}/summary/history", deps.SummaryHandler.GetHistory)

			// Sync (Phase 2)
			if deps.SyncHandler != nil {
				r.Post("/projects/{id}/sync", deps.SyncHandler.TriggerSync)
				r.Get("/projects/{id}/sync-runs", deps.SyncHandler.ListSyncRuns)
			}
			if deps.AgentRunHandler != nil {
				r.With(middleware.RequireAPIKey).Post("/agent-runs", deps.AgentRunHandler.Create)
				r.Get("/projects/{id}/agent-runs", deps.AgentRunHandler.ListByProject)
				r.Get("/agent-runs/{id}", deps.AgentRunHandler.Get)
				r.With(middleware.RequireAPIKey).Patch("/agent-runs/{id}", deps.AgentRunHandler.Update)
			}
			if deps.DriftSignalHandler != nil {
				r.Get("/projects/{id}/drift-signals", deps.DriftSignalHandler.ListByProject)
				r.Post("/projects/{id}/drift-signals", deps.DriftSignalHandler.Create)
				r.Post("/projects/{id}/drift-signals/resolve-all", deps.DriftSignalHandler.BulkResolveByProject)
				r.Patch("/drift-signals/{id}", deps.DriftSignalHandler.Update)
			}
			if deps.APIKeyHandler != nil {
				r.Get("/keys", deps.APIKeyHandler.List)
				r.Post("/keys", deps.APIKeyHandler.Create)
				r.Delete("/keys/{id}", deps.APIKeyHandler.Revoke)
			}
			if deps.UserHandler != nil {
				r.With(middleware.RequireAdmin).Get("/users", deps.UserHandler.List)
				r.With(middleware.RequireAdmin).Patch("/users/{id}", deps.UserHandler.Update)
			}
			if deps.NotificationHandler != nil {
				r.Get("/notifications", deps.NotificationHandler.List)
				r.Post("/notifications", deps.NotificationHandler.Create)
				r.Patch("/notifications/{id}/read", deps.NotificationHandler.MarkRead)
				r.Patch("/notifications/{id}/unread", deps.NotificationHandler.MarkUnread)
				r.Post("/notifications/read-all", deps.NotificationHandler.MarkAllRead)
				r.Get("/notifications/unread-count", deps.NotificationHandler.UnreadCount)
			}
			if deps.SearchHandler != nil {
				r.Get("/search", deps.SearchHandler.Search)
			}
		})
	})

	// Serve frontend static files
	if deps.FrontendDir != "" {
		serveSPA(r, deps.FrontendDir)
	}

	return r
}

// serveSPA serves the React SPA from the given directory.
// For any route that doesn't match an API path or a static file, it serves index.html.
func serveSPA(r chi.Router, dir string) {
	fs := http.FileServer(http.Dir(dir))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Try to serve the file directly
		fullPath := filepath.Join(dir, path)
		if !strings.HasPrefix(path, "/api") {
			if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
				fs.ServeHTTP(w, r)
				return
			}
		}

		// Fall back to index.html for SPA routing
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
