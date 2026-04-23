package handlers

import (
	"net/http"
	"os"
	"time"
)

type MetaResponse struct {
	LocalMode   bool   `json:"local_mode"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Port        string `json:"port"`
	Version     string `json:"version"`
	DBType      string `json:"db_type"`
	DBPath      string `json:"db_path"`
	DBSizeBytes int64  `json:"db_size_bytes"`
	StartedAt   string `json:"started_at"`
}

type MetaHandler struct {
	resp   MetaResponse
	dbPath string // absolute path; non-empty only in local mode for dynamic size
}

func NewMetaHandler(localMode bool, projectID, projectName, port,
	version, dbType, dbPath string, startedAt time.Time) *MetaHandler {
	return &MetaHandler{
		resp: MetaResponse{
			LocalMode:   localMode,
			ProjectID:   projectID,
			ProjectName: projectName,
			Port:        port,
			Version:     version,
			DBType:      dbType,
			DBPath:      dbPath,
			StartedAt:   startedAt.UTC().Format(time.RFC3339),
		},
		dbPath: dbPath,
	}
}

func (h *MetaHandler) Get(w http.ResponseWriter, r *http.Request) {
	resp := h.resp
	if h.dbPath != "" {
		if info, err := os.Stat(h.dbPath); err == nil {
			resp.DBSizeBytes = info.Size()
		}
	}
	writeSuccess(w, http.StatusOK, resp, map[string]any{})
}
