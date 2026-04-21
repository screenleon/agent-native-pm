package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/middleware"
	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type PlanningSettingsHandler struct {
	store *store.PlanningSettingsStore
}

func NewPlanningSettingsHandler(store *store.PlanningSettingsStore) *PlanningSettingsHandler {
	return &PlanningSettingsHandler{store: store}
}

func (h *PlanningSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load planning settings")
		return
	}
	writeSuccess(w, http.StatusOK, models.PlanningSettingsView{
		Settings:           sanitizePlanningSettings(settings),
		SecretStorageReady: h.store.SecretStorageReady(),
	}, nil)
}

func (h *PlanningSettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req models.UpdatePlanningSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := h.store.Upsert(req, user.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, http.StatusOK, models.PlanningSettingsView{
		Settings:           sanitizePlanningSettings(settings),
		SecretStorageReady: h.store.SecretStorageReady(),
	}, nil)
}

func sanitizePlanningSettings(settings *models.StoredPlanningSettings) models.PlanningSettings {
	if settings == nil {
		return models.PlanningSettings{
			ProviderID:       models.PlanningProviderDeterministic,
			ModelID:          models.PlanningProviderModelDeterministic,
			ConfiguredModels: []string{models.PlanningProviderModelDeterministic},
			APIKeyConfigured: false,
			CredentialMode:   models.CredentialModeShared,
		}
	}
	return settings.PlanningSettings
}
