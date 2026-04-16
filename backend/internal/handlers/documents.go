package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type DocumentHandler struct {
	store        *store.DocumentStore
	projectStore *store.ProjectStore
	mappingStore *store.ProjectRepoMappingStore
}

type documentContentResponse struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Content   string `json:"content"`
	SizeBytes int    `json:"size_bytes"`
	Truncated bool   `json:"truncated"`
}

const maxDocumentPreviewBytes = 512 * 1024

func NewDocumentHandler(s *store.DocumentStore, ps *store.ProjectStore, ms *store.ProjectRepoMappingStore) *DocumentHandler {
	return &DocumentHandler{store: s, projectStore: ps, mappingStore: ms}
}

func (h *DocumentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	page, perPage := parsePagination(r)

	docs, total, err := h.store.ListByProject(projectID, page, perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	if docs == nil {
		docs = []models.Document{}
	}
	writeSuccess(w, http.StatusOK, docs, models.PaginationMeta{Page: page, PerPage: perPage, Total: total})
}

func (h *DocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	doc, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeSuccess(w, http.StatusOK, doc, nil)
}

func (h *DocumentHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	doc, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	project, err := h.projectStore.GetByID(doc.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !requestAllowsProject(r, doc.ProjectID) {
		writeError(w, http.StatusForbidden, "api key not allowed for this project")
		return
	}

	mappings := []models.ProjectRepoMapping{}
	if h.mappingStore != nil {
		mappings, err = h.mappingStore.ListByProject(doc.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve repo mappings")
			return
		}
	}
	fullPath, raw, err := readDocumentContent(project.RepoPath, mappings, doc.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "document file not found")
			return
		}
		if strings.Contains(err.Error(), "invalid file path") || strings.Contains(err.Error(), "empty") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read document content")
		return
	}

	truncated := false
	if len(raw) > maxDocumentPreviewBytes {
		raw = raw[:maxDocumentPreviewBytes]
		truncated = true
	}

	writeSuccess(w, http.StatusOK, documentContentResponse{
		Path:      fullPath,
		Language:  detectLanguage(doc.FilePath),
		Content:   string(raw),
		SizeBytes: len(raw),
		Truncated: truncated,
	}, nil)
}

func (h *DocumentHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req models.CreateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.DocType != "" && !models.ValidDocTypes[req.DocType] {
		writeError(w, http.StatusBadRequest, "invalid doc_type value")
		return
	}

	doc, err := h.store.Create(projectID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create document")
		return
	}
	writeSuccess(w, http.StatusCreated, doc, nil)
}

func (h *DocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DocType != nil && !models.ValidDocTypes[*req.DocType] {
		writeError(w, http.StatusBadRequest, "invalid doc_type value")
		return
	}

	doc, err := h.store.Update(id, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update document")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	writeSuccess(w, http.StatusOK, doc, nil)
}

func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.store.GetByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check document")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete document")
		return
	}
	writeSuccess(w, http.StatusOK, nil, nil)
}

func readDocumentContent(repoPath string, mappings []models.ProjectRepoMapping, filePath string) (string, []byte, error) {
	resolvedRepoPath := strings.TrimSpace(repoPath)
	resolvedFilePath := filePath
	if len(mappings) > 0 {
		for _, mapping := range mappings {
			prefix := strings.TrimSpace(mapping.Alias) + "/"
			if strings.TrimSpace(mapping.Alias) != "" && strings.HasPrefix(filePath, prefix) {
				resolvedRepoPath = mapping.RepoPath
				resolvedFilePath = strings.TrimPrefix(filePath, prefix)
				break
			}
		}
		if resolvedRepoPath == "" {
			for _, mapping := range mappings {
				if mapping.IsPrimary {
					resolvedRepoPath = mapping.RepoPath
					break
				}
			}
		}
	}
	if strings.TrimSpace(resolvedRepoPath) == "" {
		return "", nil, errors.New("project repo path is empty")
	}
	if strings.TrimSpace(filePath) == "" {
		return "", nil, errors.New("invalid file path: file_path is empty")
	}

	repoAbs, err := filepath.Abs(resolvedRepoPath)
	if err != nil {
		return "", nil, err
	}

	cleanRel := filepath.Clean(resolvedFilePath)
	if filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", nil, errors.New("invalid file path: must be a repo-relative path")
	}

	fullPath := filepath.Join(repoAbs, cleanRel)
	fullAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", nil, err
	}

	prefix := repoAbs + string(os.PathSeparator)
	if fullAbs != repoAbs && !strings.HasPrefix(fullAbs, prefix) {
		return "", nil, errors.New("invalid file path: outside repo root")
	}

	raw, err := os.ReadFile(fullAbs)
	if err != nil {
		return "", nil, err
	}

	return fullAbs, raw, nil
}

func detectLanguage(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".markdown":
		return "markdown"
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".txt":
		return "text"
	default:
		return "plaintext"
	}
}
