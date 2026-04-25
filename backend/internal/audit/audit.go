// Package audit is the canonical writer/reader for the actor_audit
// table. It exists because Phase 6c authoring lifecycle (PR-2)
// promotes execution_role to a first-class authored field with a
// who/when/why trail, and that trail is the **single source of
// truth** — backlog_candidates.execution_role itself carries only the
// current value. Other fields and subjects (task status, po_decision,
// connector activity) can adopt this same audit model later without
// schema churn (the table is generic via subject_kind discriminator).
//
// Design contract:
//
//   - Record MUST be called inside the same *sql.Tx that mutates the
//     subject row, so the audit row and the value change land
//     atomically. There is no async / fire-and-forget Record path.
//
//   - actor_kind="router" is reserved for Phase 6d auto-apply. PR-3's
//     suggest endpoint does NOT write router rows — operator confirms
//     before apply, so the audit row is actor_kind="user" with the
//     router result in rationale (e.g. "operator confirmed router
//     suggestion role=X confidence=0.87"). This keeps the trail
//     unambiguous: rows reflect WHO MADE THE CHANGE, not WHO
//     SUGGESTED IT.
//
//   - QueryLatest is the read path for handlers building the
//     candidate response. Frontend reads execution_role authoring
//     metadata via this helper, not via denormalised columns on the
//     subject row.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ActorKind enumerates the kinds of actors that may modify an audited
// field. The string values are wire-stable — they appear in the DB,
// in API responses, and in user-visible audit panels.
type ActorKind string

const (
	ActorUser      ActorKind = "user"      // session-authenticated human operator
	ActorAPIKey    ActorKind = "api_key"   // automation-authenticated request via API key
	ActorRouter    ActorKind = "router"    // 6d auto-apply (LLM router); reserved
	ActorSystem    ActorKind = "system"    // server-side enforcement (e.g. claim-next-task stale-role transition)
	ActorConnector ActorKind = "connector" // connector-initiated change
)

// IsValid reports whether the actor kind is a recognised value.
// Persisting an unrecognised actor_kind is a programming error; the
// table accepts any string for forward-compat but Record validates.
func (k ActorKind) IsValid() bool {
	switch k {
	case ActorUser, ActorAPIKey, ActorRouter, ActorSystem, ActorConnector:
		return true
	}
	return false
}

// SubjectKind enumerates the row types that participate in the audit
// trail. Adding a new subject is a schema-free change — the
// discriminator is the subject_kind column value.
type SubjectKind string

const (
	SubjectBacklogCandidate SubjectKind = "backlog_candidate"
	SubjectTask             SubjectKind = "task"
	SubjectPlanningRun      SubjectKind = "planning_run"
	SubjectConnector        SubjectKind = "connector"
)

// ActorInfo describes who made a change. Kind is required; ID,
// Rationale, and Confidence are optional and depend on Kind:
//
//   - ActorUser:      ID = user_id (typically a UUID)
//   - ActorRouter:    ID = router prompt version, Confidence MUST be set
//   - ActorSystem:    ID = component name (e.g. "claim-next-task")
//   - ActorConnector: ID = connector_id (typically a UUID)
type ActorInfo struct {
	Kind       ActorKind
	ID         string
	Rationale  string
	Confidence *float64 // pointer so 0.0 is distinguishable from "not set"
}

// Entry is one audit row. OldValue and NewValue are nullable —
// "field unset → some value" has OldValue = nil; "value cleared"
// has NewValue = nil.
type Entry struct {
	ID          string
	SubjectKind SubjectKind
	SubjectID   string
	Field       string
	OldValue    *string
	NewValue    *string
	Actor       ActorInfo
	CreatedAt   time.Time
}

// dbQuerier abstracts over *sql.DB and *sql.Tx for QueryLatest's read
// path. Record uses *sql.Tx directly because it MUST be transactional.
type dbQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrInvalidActorKind is returned by Record when the supplied
// ActorInfo.Kind is not in the recognised enum.
var ErrInvalidActorKind = errors.New("audit: invalid actor_kind")

// ErrInvalidConfidence is returned when ActorInfo.Confidence is set
// but outside [0, 1], or when actor_kind=router but Confidence is nil.
var ErrInvalidConfidence = errors.New("audit: confidence must be set in [0,1] for actor_kind=router")

// Record inserts an audit row inside the supplied transaction. The
// caller is responsible for tx lifecycle (commit/rollback). The audit
// row's id is generated as a fresh UUID.
//
// Behaviour:
//   - Validates ActorInfo.Kind is a recognised enum.
//   - For actor_kind=router, requires Confidence to be set in [0,1].
//   - Confidence is persisted only when set (NULL otherwise) so the
//     column stays clean for non-router actors.
//   - Both subject_kind and field are accepted as caller-supplied
//     strings; subject_kind enum is enforced at this layer for
//     consistency with future subjects.
func Record(tx *sql.Tx, subjectKind SubjectKind, subjectID, field string, oldValue, newValue *string, actor ActorInfo) error {
	if !actor.Kind.IsValid() {
		return fmt.Errorf("%w: %q", ErrInvalidActorKind, actor.Kind)
	}
	// Confidence is provenance-typed: it ONLY belongs on rows whose
	// actor_kind is router, where it carries the router's reported
	// classifier confidence. Allowing user/system/connector rows to
	// carry a confidence value would silently corrupt downstream
	// queries that filter on "router decisions with confidence ≥ X"
	// (critic round 1 finding #1).
	if actor.Kind == ActorRouter {
		if actor.Confidence == nil {
			return ErrInvalidConfidence
		}
		if *actor.Confidence < 0 || *actor.Confidence > 1 {
			return fmt.Errorf("%w: got %f", ErrInvalidConfidence, *actor.Confidence)
		}
	} else if actor.Confidence != nil {
		return fmt.Errorf("%w: confidence must be nil for actor_kind=%q", ErrInvalidConfidence, actor.Kind)
	}

	id := uuid.NewString()
	var oldVal, newVal sql.NullString
	if oldValue != nil {
		oldVal = sql.NullString{String: *oldValue, Valid: true}
	}
	if newValue != nil {
		newVal = sql.NullString{String: *newValue, Valid: true}
	}
	var actorID, rationale sql.NullString
	if actor.ID != "" {
		actorID = sql.NullString{String: actor.ID, Valid: true}
	}
	if actor.Rationale != "" {
		rationale = sql.NullString{String: actor.Rationale, Valid: true}
	}
	var confidence sql.NullFloat64
	if actor.Confidence != nil {
		confidence = sql.NullFloat64{Float64: *actor.Confidence, Valid: true}
	}

	// Use Go-side timestamp for deterministic ordering. SQLite's
	// CURRENT_TIMESTAMP has 1-second precision so two rows in the same
	// test (or rapid-fire production updates) get identical created_at
	// and ORDER BY created_at DESC LIMIT 1 returns an unstable row.
	// time.Now() with nanosecond precision avoids the tie.
	now := time.Now().UTC()
	_, err := tx.Exec(
		`INSERT INTO actor_audit (id, subject_kind, subject_id, field, old_value, new_value, actor_kind, actor_id, rationale, confidence, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, string(subjectKind), subjectID, field, oldVal, newVal, string(actor.Kind), actorID, rationale, confidence, now,
	)
	if err != nil {
		return fmt.Errorf("audit: insert row: %w", err)
	}
	return nil
}

// QueryLatest returns the most recent audit row for the given subject
// + field tuple. ErrNoRows is returned when no audit row has been
// written yet — this is a normal condition (e.g. candidate has no
// execution_role authoring history because the field has never been
// set), and callers should treat it as "no authoring metadata
// available" rather than as an error.
func QueryLatest(ctx context.Context, q dbQuerier, subjectKind SubjectKind, subjectID, field string) (*Entry, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, subject_kind, subject_id, field, old_value, new_value,
		        actor_kind, actor_id, rationale, confidence, created_at
		 FROM actor_audit
		 WHERE subject_kind = $1 AND subject_id = $2 AND field = $3
		 ORDER BY created_at DESC
		 LIMIT 1`,
		string(subjectKind), subjectID, field,
	)

	var (
		e          Entry
		oldVal     sql.NullString
		newVal     sql.NullString
		kind       string
		actorID    sql.NullString
		rationale  sql.NullString
		confidence sql.NullFloat64
		createdAt  time.Time
	)
	err := row.Scan(
		&e.ID, &e.SubjectKind, &e.SubjectID, &e.Field,
		&oldVal, &newVal, &kind, &actorID, &rationale, &confidence, &createdAt,
	)
	if err != nil {
		return nil, err // includes sql.ErrNoRows
	}
	if oldVal.Valid {
		s := oldVal.String
		e.OldValue = &s
	}
	if newVal.Valid {
		s := newVal.String
		e.NewValue = &s
	}
	e.Actor = ActorInfo{
		Kind:      ActorKind(kind),
		ID:        actorID.String,
		Rationale: rationale.String,
	}
	if confidence.Valid {
		f := confidence.Float64
		e.Actor.Confidence = &f
	}
	e.CreatedAt = createdAt
	return &e, nil
}
