package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/store"
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

// projectAllowedForUser returns true if the requesting user may access the project.
// Admins bypass the membership check. Returns false on any store error (fail-closed).
func projectAllowedForUser(r *http.Request, ps *store.ProjectStore, projectID string) bool {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	ok, err := ps.IsUserMember(projectID, user.ID)
	if err != nil {
		return false
	}
	return ok
}

// validateRepoPath ensures a repo_path value is an absolute path and not a
// filesystem root, preventing obvious misconfigurations via the PATCH endpoint.
func validateRepoPath(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("repo_path must be an absolute path")
	}
	if clean == "/" {
		return fmt.Errorf("repo_path must not be the filesystem root")
	}
	return nil
}
