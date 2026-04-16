package main

import (
	"log"
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/config"
	"github.com/screenleon/agent-native-pm/internal/database"
	"github.com/screenleon/agent-native-pm/internal/git"
	"github.com/screenleon/agent-native-pm/internal/handlers"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/router"
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
	taskStore := store.NewTaskStore(db)
	documentStore := store.NewDocumentStore(db)
	summaryStore := store.NewSummaryStore(db, taskStore, documentStore)

	// Phase 2 stores
	syncRunStore := store.NewSyncRunStore(db)
	agentRunStore := store.NewAgentRunStore(db)
	driftSignalStore := store.NewDriftSignalStore(db)
	documentLinkStore := store.NewDocumentLinkStore(db)
	repoMappingStore := store.NewProjectRepoMappingStore(db)

	// Phase 3 stores
	apiKeyStore := store.NewAPIKeyStore(db)

	// Phase 4 stores
	userStore := store.NewUserStore(db)
	sessionStore := store.NewSessionStore(db, userStore)
	notificationStore := store.NewNotificationStore(db)
	searchStore := store.NewSearchStore(db)

	// Git sync service
	syncService := git.NewSyncService(syncRunStore, documentLinkStore, driftSignalStore, documentStore, projectStore, repoMappingStore, cfg.StaleDaysThreshold, cfg.RepoRoot)

	// Phase 1 handlers
	projectHandler := handlers.NewProjectHandler(projectStore, repoMappingStore)
	taskHandler := handlers.NewTaskHandler(taskStore, projectStore)
	documentHandler := handlers.NewDocumentHandler(documentStore, projectStore, repoMappingStore)
	summaryHandler := handlers.NewSummaryHandler(summaryStore, projectStore)

	// Phase 2 handlers
	syncHandler := handlers.NewSyncHandler(syncRunStore, syncService, projectStore)
	agentRunHandler := handlers.NewAgentRunHandler(agentRunStore, projectStore)
	driftSignalHandler := handlers.NewDriftSignalHandler(driftSignalStore, documentStore, projectStore)
	documentLinkHandler := handlers.NewDocumentLinkHandler(documentLinkStore, documentStore)
	repoMappingHandler := handlers.NewProjectRepoMappingHandler(repoMappingStore, projectStore)

	// Phase 3 handlers
	apiKeyHandler := handlers.NewAPIKeyHandler(apiKeyStore)
	documentRefreshHandler := handlers.NewDocumentRefreshHandler(documentStore)

	// Phase 4 handlers
	userHandler := handlers.NewUserHandler(userStore, sessionStore)
	notificationHandler := handlers.NewNotificationHandler(notificationStore)
	searchHandler := handlers.NewSearchHandler(searchStore)

	// Auth middleware
	sessionAuthMiddleware := middleware.SessionAuth(sessionStore)
	apiKeyAuthMiddleware := middleware.APIKeyAuth(apiKeyStore)

	r := router.New(router.Deps{
		ProjectHandler:            projectHandler,
		TaskHandler:               taskHandler,
		DocumentHandler:           documentHandler,
		SummaryHandler:            summaryHandler,
		SyncHandler:               syncHandler,
		AgentRunHandler:           agentRunHandler,
		DriftSignalHandler:        driftSignalHandler,
		DocumentLinkHandler:       documentLinkHandler,
		APIKeyHandler:             apiKeyHandler,
		DocumentRefreshHandler:    documentRefreshHandler,
		ProjectRepoMappingHandler: repoMappingHandler,
		UserHandler:               userHandler,
		NotificationHandler:       notificationHandler,
		SearchHandler:             searchHandler,
		AuthMiddleware:            sessionAuthMiddleware,
		APIKeyMiddleware:          apiKeyAuthMiddleware,
		FrontendDir:               cfg.FrontendDir,
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
