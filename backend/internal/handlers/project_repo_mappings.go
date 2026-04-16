package handlers

import (
	"fmt"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/git"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

var repoMappingAliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func repoMappingRoot() string {
	root := strings.TrimSpace(os.Getenv("REPO_MAPPING_ROOT"))
	if root == "" {
		root = "/mirrors"
	}
	return filepath.Clean(root)
}

type ProjectRepoMappingHandler struct {
	store        *store.ProjectRepoMappingStore
	projectStore *store.ProjectStore
}

func NewProjectRepoMappingHandler(s *store.ProjectRepoMappingStore, ps *store.ProjectStore) *ProjectRepoMappingHandler {
	return &ProjectRepoMappingHandler{store: s, projectStore: ps}
}

func normalizeCreateRepoMappingRequest(req models.CreateProjectRepoMappingRequest) (models.CreateProjectRepoMappingRequest, error) {
	if strings.TrimSpace(req.Alias) == "" {
		return req, fmt.Errorf("alias is required")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return req, fmt.Errorf("repo_path is required")
	}
	if !repoMappingAliasPattern.MatchString(strings.TrimSpace(req.Alias)) {
		return req, fmt.Errorf("alias must use lowercase letters, numbers, dots, underscores, or hyphens")
	}
	cleanRepoPath := filepath.Clean(strings.TrimSpace(req.RepoPath))
	root := repoMappingRoot()
	if !filepath.IsAbs(cleanRepoPath) {
		return req, fmt.Errorf("repo_path must be an absolute path under the configured mirror root")
	}
	if cleanRepoPath != root && !strings.HasPrefix(cleanRepoPath, root+string(filepath.Separator)) {
		return req, fmt.Errorf("repo_path must stay under the configured mirror root")
	}
	if !git.IsGitRepo(cleanRepoPath) {
		return req, fmt.Errorf("repo_path must point to a readable git repository")
	}
	req.Alias = strings.TrimSpace(req.Alias)
	req.RepoPath = cleanRepoPath
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	return req, nil
}

func syncProjectPrimaryRepoPath(projectStore *store.ProjectStore, projectID string, repoPath string) {
	_, _ = projectStore.Update(projectID, models.UpdateProjectRequest{RepoPath: &repoPath})
}

func (h *ProjectRepoMappingHandler) Discover(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if !requestAllowsProject(r, projectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	discovery, err := git.DiscoverMirrorRepos(repoMappingRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to discover mirror repos")
		return
	}

	if projectID != "" {
		mappings, err := h.store.ListByProject(projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load project repo mappings")
			return
		}
		mappingsByPath := make(map[string]models.ProjectRepoMapping, len(mappings))
		for _, mapping := range mappings {
			mappingsByPath[mapping.RepoPath] = mapping
		}
		for index := range discovery.Repos {
			mapping, ok := mappingsByPath[discovery.Repos[index].RepoPath]
			if !ok {
				continue
			}
			discovery.Repos[index].IsMappedToProject = true
			discovery.Repos[index].IsPrimaryForProject = mapping.IsPrimary
			if mapping.Alias != "" {
				discovery.Repos[index].SuggestedAlias = mapping.Alias
			}
			if mapping.DefaultBranch != "" {
				discovery.Repos[index].DetectedDefaultBranch = mapping.DefaultBranch
			}
		}
	}

	writeSuccess(w, http.StatusOK, discovery, nil)
}

func (h *ProjectRepoMappingHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !requestAllowsProject(r, projectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}
	mappings, err := h.store.ListByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repo mappings")
		return
	}
	writeSuccess(w, http.StatusOK, mappings, nil)
}

func (h *ProjectRepoMappingHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	project, err := h.projectStore.GetByID(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, projectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	var req models.CreateProjectRepoMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Alias) == "" {
		writeError(w, http.StatusBadRequest, "alias is required")
		return
	}
	normalizedReq, err := normalizeCreateRepoMappingRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	mapping, err := h.store.Create(projectID, normalizedReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create repo mapping")
		return
	}

	if mapping.IsPrimary {
		syncProjectPrimaryRepoPath(h.projectStore, projectID, mapping.RepoPath)
	}

	writeSuccess(w, http.StatusCreated, mapping, nil)
}

func (h *ProjectRepoMappingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check repo mapping")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "repo mapping not found")
		return
	}
	if !requestAllowsProject(r, existing.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete repo mapping")
		return
	}

	if existing.IsPrimary {
		nextPrimary, err := h.store.PromoteFirstRemaining(existing.ProjectID)
		if err == nil {
			if nextPrimary != nil {
				syncProjectPrimaryRepoPath(h.projectStore, existing.ProjectID, nextPrimary.RepoPath)
			} else {
				syncProjectPrimaryRepoPath(h.projectStore, existing.ProjectID, "")
			}
		}
	}

	writeSuccess(w, http.StatusOK, nil, nil)
}
