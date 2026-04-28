package router

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/screenleon/agent-native-pm/internal/frontend"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
)

type Deps struct {
	ProjectHandler            *handlers.ProjectHandler
	RequirementHandler        *handlers.RequirementHandler
	PlanningRunHandler        *handlers.PlanningRunHandler
	PlanningSettingsHandler   *handlers.PlanningSettingsHandler
	AccountBindingHandler     *handlers.AccountBindingHandler
	LocalConnectorHandler     *handlers.LocalConnectorHandler
	ConnectorActivityHandler  *handlers.ConnectorActivityHandler
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
	AdapterModelsHandler      *handlers.AdapterModelsHandler
	RemoteModelsHandler       *handlers.RemoteModelsHandler
	MetaHandler               *handlers.MetaHandler
	HealthHandler             *handlers.HealthHandler
	RolesHandler              *handlers.RolesHandler
	AuthMiddleware            func(http.Handler) http.Handler
	APIKeyMiddleware          func(http.Handler) http.Handler
	// LocalModeMiddleware, when non-nil, is applied before AuthMiddleware
	// to inject a synthetic admin user on every request (local mode).
	LocalModeMiddleware func(http.Handler) http.Handler
	FrontendDir         string
	// CORSAllowedOrigins is the explicit origin allowlist. When nil/empty
	// the router falls back to safe localhost defaults so existing tests
	// keep working without configuration plumbing.
	CORSAllowedOrigins []string
}

func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(cors.Handler(buildCORSOptions(deps.CORSAllowedOrigins)))
	r.Use(securityHeaders)

	// Local mode: inject synthetic admin before normal auth middlewares.
	if deps.LocalModeMiddleware != nil {
		r.Use(deps.LocalModeMiddleware)
	}

	// Inject auth identity (session + API key) on every request
	if deps.AuthMiddleware != nil {
		r.Use(deps.AuthMiddleware)
	}
	if deps.APIKeyMiddleware != nil {
		r.Use(deps.APIKeyMiddleware)
	}

	// API routes
	r.Route("/api", func(r chi.Router) {
		if deps.HealthHandler != nil {
			r.Get("/health", deps.HealthHandler.Check)
		} else {
			r.Get("/health", handlers.HealthCheck)
		}
		if deps.MetaHandler != nil {
			r.Get("/meta", deps.MetaHandler.Get)
		}
		if deps.AdapterModelsHandler != nil {
			r.Get("/adapter-models", deps.AdapterModelsHandler.Get)
		}
		// Phase 6c PR-2: public role catalog (no auth — same data is
		// in the source tree and shipped embedded in the binary).
		if deps.RolesHandler != nil {
			r.Get("/roles", deps.RolesHandler.List)
		}
		if deps.LocalConnectorHandler != nil {
			r.Post("/connector/pair", deps.LocalConnectorHandler.Pair)
			r.Post("/connector/heartbeat", deps.LocalConnectorHandler.Heartbeat)
			r.Post("/connector/claim-next-run", deps.LocalConnectorHandler.ClaimNextRun)
			r.Post("/connector/planning-runs/{id}/result", deps.LocalConnectorHandler.SubmitPlanningRunResult)
			// Phase 6b: role_dispatch task execution loop.
			r.Post("/connector/claim-next-task", deps.LocalConnectorHandler.ClaimNextTask)
			r.Post("/connector/tasks/{task_id}/execution-result", deps.LocalConnectorHandler.SubmitTaskResult)
		}
		// Phase 6c PR-4: connector activity reporting (connector-token auth,
		// not user auth — connector pushes its current phase to the server).
		if deps.ConnectorActivityHandler != nil {
			r.Post("/connector/activity", deps.ConnectorActivityHandler.Report)
		}

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

			if deps.RequirementHandler != nil {
				r.Get("/projects/{id}/requirements", deps.RequirementHandler.ListByProject)
				r.Post("/projects/{id}/requirements", deps.RequirementHandler.Create)
				r.Get("/requirements/{id}", deps.RequirementHandler.Get)
				r.Patch("/requirements/{id}", deps.RequirementHandler.Update)
				r.Delete("/requirements/{id}", deps.RequirementHandler.Delete)
			}
			if deps.PlanningRunHandler != nil {
				r.Get("/projects/{id}/planning-provider-options", deps.PlanningRunHandler.ProviderOptions)
				r.Get("/projects/{id}/task-lineage", deps.PlanningRunHandler.ListAppliedLineage)
				r.Get("/projects/{id}/backlog-candidates/by-evidence", deps.PlanningRunHandler.ListByEvidence)
				r.Post("/projects/{id}/demo-seed", deps.PlanningRunHandler.DemoSeed)
				r.Post("/requirements/{id}/planning-runs", deps.PlanningRunHandler.Create)
				r.Get("/requirements/{id}/planning-runs", deps.PlanningRunHandler.ListByRequirement)
				r.Get("/planning-runs/{id}", deps.PlanningRunHandler.Get)
				r.Post("/planning-runs/{id}/cancel", deps.PlanningRunHandler.Cancel)
				r.Get("/planning-runs/{id}/context-snapshot", deps.PlanningRunHandler.GetContextSnapshot)
				r.Get("/planning-runs/{id}/backlog-candidates", deps.PlanningRunHandler.ListBacklogCandidates)
				r.Patch("/backlog-candidates/{id}", deps.PlanningRunHandler.UpdateBacklogCandidate)
				r.Post("/backlog-candidates/{id}/apply", deps.PlanningRunHandler.ApplyBacklogCandidate)
				r.Post("/backlog-candidates/{id}/suggest-role", deps.PlanningRunHandler.SuggestRole)
			}

			// Tasks
			r.Get("/projects/{id}/tasks", deps.TaskHandler.ListByProject)
			r.Post("/projects/{id}/tasks", deps.TaskHandler.Create)
			r.Post("/projects/{id}/tasks/batch-update", deps.TaskHandler.BatchUpdate)
			r.Get("/tasks/{id}", deps.TaskHandler.Get)
			r.Patch("/tasks/{id}", deps.TaskHandler.Update)
			r.Delete("/tasks/{id}", deps.TaskHandler.Delete)
			r.Post("/tasks/{id}/requeue-dispatch", deps.TaskHandler.RequeueDispatch)

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
				r.Patch("/repo-mappings/{id}", deps.ProjectRepoMappingHandler.Update)
				r.Delete("/repo-mappings/{id}", deps.ProjectRepoMappingHandler.Delete)
			}
			if deps.DocumentRefreshHandler != nil {
				r.Post("/documents/{id}/refresh-summary", deps.DocumentRefreshHandler.RefreshSummary)
			}

			// Summary
			r.Get("/projects/{id}/summary", deps.SummaryHandler.GetSummary)
			r.Get("/projects/{id}/dashboard-summary", deps.SummaryHandler.GetDashboardSummary)
			r.Get("/projects/{id}/summary/history", deps.SummaryHandler.GetHistory)
			r.Get("/projects/{id}/pending-review-count", deps.SummaryHandler.GetPendingReviewCount)

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
			if deps.PlanningSettingsHandler != nil {
				r.With(middleware.RequireAdmin).Get("/settings/planning", deps.PlanningSettingsHandler.Get)
				r.With(middleware.RequireAdmin).Patch("/settings/planning", deps.PlanningSettingsHandler.Update)
			}
			if deps.AccountBindingHandler != nil {
				r.Get("/me/account-bindings", deps.AccountBindingHandler.List)
				r.Post("/me/account-bindings", deps.AccountBindingHandler.Create)
				r.Patch("/me/account-bindings/{id}", deps.AccountBindingHandler.Update)
				r.Delete("/me/account-bindings/{id}", deps.AccountBindingHandler.Delete)
			}
			if deps.LocalConnectorHandler != nil {
				r.Get("/me/local-connectors", deps.LocalConnectorHandler.List)
				r.Post("/me/local-connectors/pairing-sessions", deps.LocalConnectorHandler.CreatePairingSession)
				r.Delete("/me/local-connectors/{id}", deps.LocalConnectorHandler.Revoke)
				r.Get("/me/local-connectors/run-stats", deps.LocalConnectorHandler.RunStats)
				r.Post("/me/local-connectors/{id}/probe-binding", deps.LocalConnectorHandler.ProbeBinding)
				r.Get("/me/local-connectors/{id}/probe-binding/{probe_id}", deps.LocalConnectorHandler.GetProbeResult)
				// Phase 6a UX-B1: per-connector CLI configs
				r.Get("/me/local-connectors/{id}/cli-configs", deps.LocalConnectorHandler.ListCliConfigs)
				r.Post("/me/local-connectors/{id}/cli-configs", deps.LocalConnectorHandler.CreateCliConfig)
				r.Patch("/me/local-connectors/{id}/cli-configs/{config_id}", deps.LocalConnectorHandler.UpdateCliConfig)
				r.Delete("/me/local-connectors/{id}/cli-configs/{config_id}", deps.LocalConnectorHandler.DeleteCliConfig)
				r.Post("/me/local-connectors/{id}/cli-configs/{config_id}/primary", deps.LocalConnectorHandler.SetPrimaryCliConfig)
			}
			// Phase 6c PR-4: connector activity visibility (user-authenticated).
			if deps.ConnectorActivityHandler != nil {
				r.Get("/me/local-connectors/{id}/activity", deps.ConnectorActivityHandler.Get)
				r.Get("/me/local-connectors/{id}/activity-stream", deps.ConnectorActivityHandler.Stream)
				r.Get("/projects/{id}/active-connectors", deps.ConnectorActivityHandler.ListActive)
			}
			if deps.RemoteModelsHandler != nil {
				r.Post("/me/remote-models", deps.RemoteModelsHandler.Fetch)
				r.Post("/me/probe-model", deps.RemoteModelsHandler.Probe)
			}
			if deps.NotificationHandler != nil {
				r.Get("/notifications", deps.NotificationHandler.List)
				r.Post("/notifications", deps.NotificationHandler.Create)
				r.Patch("/notifications/{id}/read", deps.NotificationHandler.MarkRead)
				r.Patch("/notifications/{id}/unread", deps.NotificationHandler.MarkUnread)
				r.Post("/notifications/read-all", deps.NotificationHandler.MarkAllRead)
				r.Get("/notifications/unread-count", deps.NotificationHandler.UnreadCount)
				r.Get("/notifications/stream", deps.NotificationHandler.Stream)
			}
			if deps.SearchHandler != nil {
				r.Get("/search", deps.SearchHandler.Search)
			}
		})
	})

	// Serve frontend static files: prefer embedded assets, fall back to disk.
	if frontend.HasAssets() {
		if sub, err := frontend.Sub(); err == nil {
			serveSPAEmbedded(r, sub)
		}
	} else if deps.FrontendDir != "" {
		serveSPA(r, deps.FrontendDir)
	}

	return r
}

// securityHeaders adds standard defensive HTTP headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// buildCORSOptions returns a CORS configuration based on the supplied
// allowlist. A literal "*" entry disables credentialed CORS because the
// browser rejects the wildcard + credentials combination; any other entry
// list keeps cookies/Authorization enabled. An empty allowlist falls back
// to safe localhost defaults so existing tests run without configuration.
func buildCORSOptions(allowed []string) cors.Options {
	cleaned := make([]string, 0, len(allowed))
	wildcard := false
	for _, origin := range allowed {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			wildcard = true
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if wildcard && len(cleaned) == 0 {
		return cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-API-Key", "X-Connector-Token"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: false,
			MaxAge:           300,
		}
	}
	if len(cleaned) == 0 {
		cleaned = []string{
			"http://localhost:5173",
			"http://localhost:18765",
			"http://127.0.0.1:5173",
			"http://127.0.0.1:18765",
		}
	}
	return cors.Options{
		AllowedOrigins:   cleaned,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-API-Key", "X-Connector-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}
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

// serveSPAEmbedded serves the React SPA from an embedded fs.FS.
func serveSPAEmbedded(r chi.Router, assets fs.FS) {
	fileServer := http.FileServer(http.FS(assets))

	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if f, err := assets.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, req)
			return
		}
		// Fall back to index.html for SPA client-side routing.
		http.ServeFileFS(w, req, assets, "index.html")
	})
}
