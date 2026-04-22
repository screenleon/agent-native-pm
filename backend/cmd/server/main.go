package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/screenleon/agent-native-pm/internal/config"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/events"
	"github.com/screenleon/agent-native-pm/internal/git"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/secrets"
	"github.com/screenleon/agent-native-pm/internal/store"
)

func main() {
	cfg := config.Load()

	// Early port log emitted after FindAvailablePort resolves below.

	isSQLite := database.IsSQLiteDSN(cfg.DatabaseURL)
	dialect := database.NewDialect(cfg.DatabaseURL)

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.RunMigrations(db, isSQLite); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}

	// Phase 1 stores
	projectStore := store.NewProjectStore(db)
	requirementStore := store.NewRequirementStore(db)
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)

	// Phase 2 stores
	planningRunStore := store.NewPlanningRunStore(db, dialect)
	backlogCandidateStore := store.NewBacklogCandidateStore(db, dialect)
	syncRunStore := store.NewSyncRunStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	documentLinkStore := store.NewDocumentLinkStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)
	summaryStore := store.NewSummaryStore(db, taskStore, documentStore, syncRunStore, driftSignalStore, agentRunStore)

	// Phase 3 stores
	apiKeyStore := store.NewAPIKeyStore(db)
	settingsBox, err := secrets.NewBox(cfg.AppSettingsMasterKey)
	if err != nil {
		slog.Error("failed to initialize app settings secret storage", "err", err)
		os.Exit(1)
	}
	planningSettingsStore := store.NewPlanningSettingsStore(db, settingsBox)
	accountBindingStore := store.NewAccountBindingStore(db, settingsBox)
	localConnectorStore := store.NewLocalConnectorStore(db, dialect)

	// Phase 4 stores
	userStore := store.NewUserStore(db)
	sessionStore := store.NewSessionStore(db, userStore)
	notificationStore := store.NewNotificationStore(db)
	searchStore := store.NewSearchStore(db, dialect)

	// Git sync service
	syncService := git.NewSyncService(syncRunStore, documentLinkStore, driftSignalStore, documentStore, projectStore, repoMappingStore, cfg.StaleDaysThreshold, cfg.RepoRoot)

	// Phase 1 handlers
	projectHandler := handlers.NewProjectHandler(projectStore, repoMappingStore)
	requirementHandler := handlers.NewRequirementHandler(requirementStore, projectStore)
	taskHandler := handlers.NewTaskHandler(taskStore, projectStore)
	documentHandler := handlers.NewDocumentHandler(documentStore, projectStore, repoMappingStore)
	summaryHandler := handlers.NewSummaryHandler(summaryStore, projectStore)

	// Phase 2 handlers
	planner := planning.NewSettingsBackedPlanner(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore, planningSettingsStore, cfg.PlanningMaxResponseBytes)
	planningRunHandler := handlers.NewPlanningRunHandler(planningRunStore, backlogCandidateStore, projectStore, requirementStore, agentRunStore, planner).WithPlannerFactory(func(userID string) planning.DraftPlanner {
		return planning.NewSettingsBackedPlannerWithBindings(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore, planningSettingsStore, accountBindingStore, userID, cfg.PlanningMaxResponseBytes)
	}).WithLocalConnectorStore(localConnectorStore)
	planningSettingsHandler := handlers.NewPlanningSettingsHandler(planningSettingsStore)
	syncHandler := handlers.NewSyncHandler(syncRunStore, syncService, projectStore)
	agentRunHandler := handlers.NewAgentRunHandler(agentRunStore, projectStore)
	driftSignalHandler := handlers.NewDriftSignalHandler(driftSignalStore, documentStore, projectStore)
	documentLinkHandler := handlers.NewDocumentLinkHandler(documentLinkStore, documentStore)
	repoMappingHandler := handlers.NewProjectRepoMappingHandler(repoMappingStore, projectStore)

	// Phase 3 handlers
	apiKeyHandler := handlers.NewAPIKeyHandler(apiKeyStore)
	documentRefreshHandler := handlers.NewDocumentRefreshHandler(documentStore, driftSignalStore)
	accountBindingHandler := handlers.NewAccountBindingHandler(accountBindingStore)
	localConnectorHandler := handlers.NewLocalConnectorHandler(localConnectorStore, planningRunStore, requirementStore, backlogCandidateStore, agentRunStore).
		WithProjectStore(projectStore).
		WithNotificationStore(notificationStore).
		WithContextBuilder(planning.NewProjectContextBuilder(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore))

	// Phase 4 handlers
	notificationBroker := events.NewBroker()
	notificationStore.SetBroker(notificationBroker)
	userHandler := handlers.NewUserHandler(userStore, sessionStore)
	notificationHandler := handlers.NewNotificationHandler(notificationStore).
		WithBroker(notificationBroker, sessionStore)
	searchHandler := handlers.NewSearchHandler(searchStore)
	adapterModelsHandler := handlers.NewAdapterModelsHandler(cfg.ClaudeModels, cfg.CodexModels)

	// Health handler with DB reference for diagnostics
	healthHandler := handlers.NewHealthHandler(db)

	// Auth middleware
	sessionAuthMiddleware := middleware.SessionAuth(sessionStore)
	apiKeyAuthMiddleware := middleware.APIKeyAuth(apiKeyStore)

	// Local mode: auto-initialise the single project and bypass auth.
	var metaHandler *handlers.MetaHandler
	var localModeMiddleware func(http.Handler) http.Handler
	if cfg.LocalMode {
		projectID, err := ensureLocalProject(projectStore, cfg.LocalProjectName, cfg.LocalRepoRoot)
		if err != nil {
			slog.Error("failed to ensure local project", "err", err)
			os.Exit(1)
		}
		metaHandler = handlers.NewMetaHandler(true, projectID, cfg.LocalProjectName, cfg.Port)
		localModeMiddleware = middleware.InjectLocalAdmin
		slog.Info("local mode ready", "project", cfg.LocalProjectName, "id", projectID, "port", cfg.Port, "db", cfg.DatabaseURL)
	} else {
		metaHandler = handlers.NewMetaHandler(false, "", "", cfg.Port)
	}

	r := router.New(router.Deps{
		ProjectHandler:            projectHandler,
		RequirementHandler:        requirementHandler,
		PlanningRunHandler:        planningRunHandler,
		TaskHandler:               taskHandler,
		DocumentHandler:           documentHandler,
		SummaryHandler:            summaryHandler,
		SyncHandler:               syncHandler,
		AgentRunHandler:           agentRunHandler,
		DriftSignalHandler:        driftSignalHandler,
		DocumentLinkHandler:       documentLinkHandler,
		APIKeyHandler:             apiKeyHandler,
		DocumentRefreshHandler:    documentRefreshHandler,
		PlanningSettingsHandler:   planningSettingsHandler,
		AccountBindingHandler:     accountBindingHandler,
		LocalConnectorHandler:     localConnectorHandler,
		ProjectRepoMappingHandler: repoMappingHandler,
		UserHandler:               userHandler,
		NotificationHandler:       notificationHandler,
		SearchHandler:             searchHandler,
		AdapterModelsHandler:      adapterModelsHandler,
		MetaHandler:               metaHandler,
		HealthHandler:             healthHandler,
		AuthMiddleware:            sessionAuthMiddleware,
		APIKeyMiddleware:          apiKeyAuthMiddleware,
		LocalModeMiddleware:       localModeMiddleware,
		FrontendDir:               cfg.FrontendDir,
		CORSAllowedOrigins:        cfg.CORSAllowedOrigins,
	})

	// In local mode: find the first available port (starting from the hash-derived
	// one) and persist it to .anpm/port so "anpm status" can discover it.
	// Bind only loopback so the no-auth endpoint is unreachable from other machines.
	bindAddr := ":" + cfg.Port
	if cfg.LocalMode {
		port := config.FindAvailablePort(toInt(cfg.Port), cfg.AnpmDir)
		cfg.Port = fmt.Sprintf("%d", port)
		bindAddr = "127.0.0.1:" + cfg.Port
		// Re-emit port in case it changed due to collision.
		slog.Info("local mode starting", "port", cfg.Port, "db", cfg.DatabaseURL)
	}

	srv := &http.Server{
		Addr:    bindAddr,
		Handler: r,
		// Reasonable timeouts for a local tool.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	slog.Info("server starting", "addr", bindAddr, "frontend", cfg.FrontendDir)
	if cfg.LocalMode {
		slog.Info("local mode active — auth bypassed, single project")
		slog.Info("open in browser", "url", "http://127.0.0.1:"+cfg.Port)
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start listener first so we catch port-in-use errors before the goroutine.
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		slog.Error("failed to listen", "addr", bindAddr, "err", err)
		os.Exit(1)
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("server stopped")
}

func toInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// ensureLocalProject returns the ID of the single local project, creating it
// if the database is empty.
func ensureLocalProject(ps *store.ProjectStore, name, repoRoot string) (string, error) {
	projects, err := ps.List()
	if err != nil {
		return "", err
	}
	if len(projects) > 0 {
		return projects[0].ID, nil
	}
	p, err := ps.Create(models.CreateProjectRequest{
		Name:          name,
		RepoPath:      repoRoot,
		DefaultBranch: "main",
	})
	if err != nil {
		return "", err
	}
	return p.ID, nil
}
