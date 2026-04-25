// Phase 6c PR-2: buildAuthoringActor maps an authenticated request
// onto the audit.ActorInfo struct that store-layer mutators consume.
// Two auth channels reach role-authoring endpoints today:
//
//   1. Session middleware (operator at the browser): UserFromContext
//      returns the *models.User → actor_kind=user, actor_id=user_id.
//   2. API-key middleware (automation / scripts): APIKeyFromContext
//      returns the *models.APIKey → actor_kind=api_key,
//      actor_id="api-key:<key id>".
//
// Critic round 1 finding #3 + risk-reviewer M1 flagged that the
// previous code collapsed both channels into actor_kind=user with
// the api-key path leaving actor_id empty. That made the audit
// trail say "a user did X" when in reality an automation did, and
// silently dropped the actor_id when api-key auth was active.
//
// rationale is the caller-supplied free-form reason ("apply backlog
// candidate", "patch backlog candidate"). Stored verbatim in
// actor_audit.rationale.
package handlers

import (
	"net/http"

	"github.com/screenleon/agent-native-pm/internal/audit"
	"github.com/screenleon/agent-native-pm/internal/middleware"
)

func buildAuthoringActor(r *http.Request, rationale string) audit.ActorInfo {
	ctx := r.Context()
	if k := middleware.APIKeyFromContext(ctx); k != nil {
		return audit.ActorInfo{
			Kind:      audit.ActorAPIKey,
			ID:        "api-key:" + k.ID,
			Rationale: rationale,
		}
	}
	if u := middleware.UserFromContext(ctx); u != nil {
		return audit.ActorInfo{
			Kind:      audit.ActorUser,
			ID:        u.ID,
			Rationale: rationale,
		}
	}
	// Defense-in-depth: route is RequireAuth-gated so this branch is
	// unreachable, but if reached we still produce a valid ActorInfo
	// rather than panicking on an empty Kind. actor_id="" surfaces in
	// the audit row as a reviewable anomaly.
	return audit.ActorInfo{Kind: audit.ActorSystem, ID: "unauthenticated", Rationale: rationale}
}
