package main

import (
	"log"
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/config"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/git"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/planning"
	"github.com/screenleon/agent-native-pm/internal/router"
	"github.com/screenleon/agent-native-pm/internal/secrets"
	"github.com/screenleon/agent-native-pm/internal/store"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := database.RunMigrations(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Phase 1 stores
	projectStore := store.NewProjectStore(db)
	requirementStore := store.NewRequirementStore(db)
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)

	// Phase 2 stores
	planningRunStore := store.NewPlanningRunStore(db)
	backlogCandidateStore := store.NewBacklogCandidateStore(db)
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
		log.Fatalf("failed to initialize app settings secret storage: %v", err)
	}
	planningSettingsStore := store.NewPlanningSettingsStore(db, settingsBox)
	accountBindingStore := store.NewAccountBindingStore(db, settingsBox)
	localConnectorStore := store.NewLocalConnectorStore(db)

	// Phase 4 stores
	userStore := store.NewUserStore(db)
	sessionStore := store.NewSessionStore(db, userStore)
	notificationStore := store.NewNotificationStore(db)
	searchStore := store.NewSearchStore(db)

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
	documentRefreshHandler := handlers.NewDocumentRefreshHandler(documentStore)
	accountBindingHandler := handlers.NewAccountBindingHandler(accountBindingStore)
	localConnectorHandler := handlers.NewLocalConnectorHandler(localConnectorStore, planningRunStore, requirementStore, backlogCandidateStore, agentRunStore).
		WithProjectStore(projectStore).
		WithNotificationStore(notificationStore).
		WithContextBuilder(planning.NewProjectContextBuilder(taskStore, documentStore, driftSignalStore, syncRunStore, agentRunStore))

	// Phase 4 handlers
	userHandler := handlers.NewUserHandler(userStore, sessionStore)
	notificationHandler := handlers.NewNotificationHandler(notificationStore)
	searchHandler := handlers.NewSearchHandler(searchStore)
	adapterModelsHandler := handlers.NewAdapterModelsHandler(cfg.ClaudeModels, cfg.CodexModels)

	// Auth middleware
	sessionAuthMiddleware := middleware.SessionAuth(sessionStore)
	apiKeyAuthMiddleware := middleware.APIKeyAuth(apiKeyStore)

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
		AuthMiddleware:            sessionAuthMiddleware,
		APIKeyMiddleware:          apiKeyAuthMiddleware,
		FrontendDir:               cfg.FrontendDir,
		CORSAllowedOrigins:        cfg.CORSAllowedOrigins,
	})

	addr := ":" + cfg.Port
	log.Printf("starting server on %s", addr)
	log.Printf("database configured via DATABASE_URL")
	log.Printf("frontend: %s", cfg.FrontendDir)
	log.Printf("managed repo root: %s", cfg.RepoRoot)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
