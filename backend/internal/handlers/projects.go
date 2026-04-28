package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type ProjectHandler struct {
	store            *store.ProjectStore
	repoMappingStore *store.ProjectRepoMappingStore
}

func NewProjectHandler(s *store.ProjectStore, rms *store.ProjectRepoMappingStore) *ProjectHandler {
	return &ProjectHandler{store: s, repoMappingStore: rms}
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	var (
		projects []models.Project
		err      error
	)
	if user != nil && user.Role == "admin" {
		projects, err = h.store.List()
	} else if user != nil {
		projects, err = h.store.ListForUser(user.ID)
	} else {
		projects, err = h.store.List()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	if projects == nil {
		projects = []models.Project{}
	}
	writeSuccess(w, http.StatusOK, projects, nil)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !projectAllowedForUser(r, h.store, project.ID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeSuccess(w, http.StatusOK, project, nil)
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ownerUserID := ""
	if user := middleware.UserFromContext(r.Context()); user != nil {
		ownerUserID = user.ID
	}
	project, err := h.store.CreateWithOwner(req, ownerUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	if req.InitialRepoMapping != nil && h.repoMappingStore != nil {
		normalizedReq, err := normalizeCreateRepoMappingRequest(*req.InitialRepoMapping)
		if err != nil {
			_ = h.store.Delete(project.ID)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		normalizedReq.IsPrimary = true
		mapping, err := h.repoMappingStore.Create(project.ID, normalizedReq)
		if err != nil {
			_ = h.store.Delete(project.ID)
			writeError(w, http.StatusInternalServerError, "failed to create initial repo mapping")
			return
		}
		if mapping.IsPrimary {
			syncProjectPrimaryRepoPath(h.store, project.ID, mapping.RepoPath)
		}
		project, err = h.store.GetByID(project.ID)
		if err != nil || project == nil {
			writeError(w, http.StatusInternalServerError, "failed to reload project")
			return
		}
	}

	writeSuccess(w, http.StatusCreated, project, nil)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if !projectAllowedForUser(r, h.store, id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req models.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RepoPath != nil {
		if err := validateRepoPath(*req.RepoPath); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	project, err := h.store.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeSuccess(w, http.StatusOK, project, nil)
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if !projectAllowedForUser(r, h.store, id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}
