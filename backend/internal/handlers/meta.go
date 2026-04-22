package handlers

import "net/http"

type MetaResponse struct {
	LocalMode   bool   `json:"local_mode"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Port        string `json:"port"`
}

type MetaHandler struct {
	resp MetaResponse
}

func NewMetaHandler(localMode bool, projectID, projectName, port string) *MetaHandler {
	return &MetaHandler{resp: MetaResponse{
		LocalMode:   localMode,
		ProjectID:   projectID,
		ProjectName: projectName,
		Port:        port,
	}}
}

func (h *MetaHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, http.StatusOK, h.resp, map[string]any{})
}
