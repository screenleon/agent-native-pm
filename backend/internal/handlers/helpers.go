package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/screenleon/agent-native-pm/internal/models"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeSuccess(w http.ResponseWriter, status int, data interface{}, meta interface{}) {
	writeJSON(w, status, models.SuccessResponse(data, meta))
}

// writeSuccessWithWarnings emits a 2xx envelope that carries non-fatal
// advisories alongside the data payload (e.g. Path B S2 stale-CLI-health
// or connector-outdated signals). When `warnings` is empty the response
// shape is byte-identical to writeSuccess.
func writeSuccessWithWarnings(w http.ResponseWriter, status int, data interface{}, meta interface{}, warnings []models.EnvelopeWarning) {
	writeJSON(w, status, models.SuccessResponseWithWarnings(data, meta, warnings))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse(message))
}

func parsePagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return page, perPage
}
