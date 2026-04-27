// Package handlers — Phase 6c PR-2: GET /api/roles surfaces the
// role catalog to the frontend so the apply panel and CandidateRoleEditor
// can render a typed dropdown (with title, version, default timeout
// estimate, use_case tooltip) instead of a free-text input.
//
// The endpoint is deliberately public (no auth gate) because the
// catalog is checked into source control, ships embedded in the
// binary, and contains no sensitive data — it is the same information
// available to anyone reading the repo. Adding auth would only
// introduce friction for the frontend and the catalog drift test.
//
// Filtering: only category="role" entries are returned. The dispatcher
// meta-prompt (PR-3) lives in the same Go catalog with category="meta"
// and MUST NOT surface in the apply dropdown — operators classify
// tasks against execution roles, not against the dispatcher itself.
package handlers

import (
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/roles"
)

// RoleResponse is the JSON shape returned by GET /api/roles. The
// `default_timeout_sec` field lets the apply panel render
// "預估 N 分鐘" inline so the operator can pre-evaluate whether a
// role is appropriate for the task at hand.
type RoleResponse struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Version           int    `json:"version"`
	UseCase           string `json:"use_case"`
	DefaultTimeoutSec int    `json:"default_timeout_sec"`
	Category          string `json:"category"`
}

// RolesHandler serves GET /api/roles.
type RolesHandler struct{}

// NewRolesHandler constructs a RolesHandler. Stateless — the catalog
// is a package-level constant in internal/roles.
func NewRolesHandler() *RolesHandler {
	return &RolesHandler{}
}

// List returns the role catalog filtered to category="role". Meta-roles
// (e.g. dispatcher) are filtered out because they are not directly
// selectable by operators — they are routing helpers, not execution
// targets.
func (h *RolesHandler) List(w http.ResponseWriter, r *http.Request) {
	all := roles.All()
	out := make([]RoleResponse, 0, len(all))
	for _, role := range all {
		if role.Category != roles.CategoryRole {
			continue
		}
		out = append(out, RoleResponse{
			ID:                role.ID,
			Title:             role.Title,
			Version:           role.Version,
			UseCase:           role.UseCase,
			DefaultTimeoutSec: role.DefaultTimeoutSec,
			Category:          role.Category,
		})
	}
	writeSuccess(w, http.StatusOK, out, nil)
}
