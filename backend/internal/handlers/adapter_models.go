package handlers

import "net/http"

type AdapterModelsHandler struct {
	claudeModels []string
	codexModels  []string
}

func NewAdapterModelsHandler(claudeModels, codexModels []string) *AdapterModelsHandler {
	return &AdapterModelsHandler{
		claudeModels: claudeModels,
		codexModels:  codexModels,
	}
}

type adapterModelsResponse struct {
	Claude []string `json:"claude"`
	Codex  []string `json:"codex"`
}

func (h *AdapterModelsHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, http.StatusOK, adapterModelsResponse{
		Claude: h.claudeModels,
		Codex:  h.codexModels,
	}, nil)
}
