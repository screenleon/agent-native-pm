package handlers

import (
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/middleware"
)

func requestAllowsProject(r *http.Request, projectID string) bool {
	apiKey := middleware.APIKeyFromContext(r.Context())
	if apiKey == nil {
		return true
	}
	if apiKey.ProjectID == nil {
		return true
	}
	return *apiKey.ProjectID == projectID
}
