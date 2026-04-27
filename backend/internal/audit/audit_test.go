package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/audit"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// recordTx is a helper that wraps audit.Record in its own one-shot
// transaction so tests don't have to repeat the boilerplate. Real
// callers compose Record into a larger transaction that mutates the
// subject row in the same Begin/Commit pair — this helper exists for
// audit-only test cases.
func recordTx(t *testing.T, db *sql.DB, sk audit.SubjectKind, sid, field string, oldV, newV *string, actor audit.ActorInfo) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := audit.Record(tx, sk, sid, field, oldV, newV, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func TestRecordAndQueryLatest(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	v1 := "code-reviewer"
	if err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-1", "execution_role",
		nil, &v1,
		audit.ActorInfo{Kind: audit.ActorUser, ID: "user-42", Rationale: "manual selection"},
	); err != nil {
		t.Fatalf("record 1: %v", err)
	}

	v2 := "backend-architect"
	if err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-1", "execution_role",
		&v1, &v2,
		audit.ActorInfo{Kind: audit.ActorUser, ID: "user-42", Rationale: "operator changed mind"},
	); err != nil {
		t.Fatalf("record 2: %v", err)
	}

	got, err := audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "cand-1", "execution_role")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}
	if got.NewValue == nil || *got.NewValue != "backend-architect" {
		t.Errorf("NewValue = %v, want backend-architect", got.NewValue)
	}
	if got.OldValue == nil || *got.OldValue != "code-reviewer" {
		t.Errorf("OldValue = %v, want code-reviewer", got.OldValue)
	}
	if got.Actor.Kind != audit.ActorUser {
		t.Errorf("Actor.Kind = %q, want user", got.Actor.Kind)
	}
	if got.Actor.Rationale != "operator changed mind" {
		t.Errorf("Actor.Rationale = %q", got.Actor.Rationale)
	}
}

func TestQueryLatestNoRowsIsErrNoRows(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	_, err := audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "missing", "execution_role")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestRecordRouterRequiresConfidence(t *testing.T) {
	db := testutil.OpenTestDB(t)

	v := "x"
	err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-2", "execution_role",
		nil, &v,
		audit.ActorInfo{Kind: audit.ActorRouter, ID: "dispatcher_v1"}, // no confidence
	)
	if !errors.Is(err, audit.ErrInvalidConfidence) {
		t.Errorf("expected ErrInvalidConfidence, got %v", err)
	}
}

func TestRecordConfidenceOutOfRange(t *testing.T) {
	db := testutil.OpenTestDB(t)

	v := "x"
	bad := 1.5
	err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-3", "execution_role",
		nil, &v,
		audit.ActorInfo{Kind: audit.ActorRouter, ID: "dispatcher_v1", Confidence: &bad},
	)
	if !errors.Is(err, audit.ErrInvalidConfidence) {
		t.Errorf("expected ErrInvalidConfidence, got %v", err)
	}
}

func TestRecordNonRouterRejectsConfidence(t *testing.T) {
	// Copilot review #6: non-router actors that pass Confidence must
	// produce ErrConfidenceNotAllowed (NOT the router-specific
	// ErrInvalidConfidence). Loosely: the sentinel describes the
	// actual failure mode so log readers are not misled.
	db := testutil.OpenTestDB(t)

	v := "x"
	conf := 0.5
	for _, kind := range []audit.ActorKind{audit.ActorUser, audit.ActorAPIKey, audit.ActorSystem, audit.ActorConnector} {
		err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-non-router", "execution_role",
			nil, &v,
			audit.ActorInfo{Kind: kind, ID: "actor", Confidence: &conf},
		)
		if !errors.Is(err, audit.ErrConfidenceNotAllowed) {
			t.Errorf("kind=%s: expected ErrConfidenceNotAllowed, got %v", kind, err)
		}
		if errors.Is(err, audit.ErrInvalidConfidence) {
			t.Errorf("kind=%s: should NOT be ErrInvalidConfidence (router-specific), got %v", kind, err)
		}
	}
}

func TestRecordRouterRoundTripsConfidence(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	v := "code-reviewer"
	conf := 0.87
	if err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-4", "execution_role",
		nil, &v,
		audit.ActorInfo{
			Kind: audit.ActorRouter, ID: "dispatcher_v1",
			Rationale:  "router confidence 0.87",
			Confidence: &conf,
		},
	); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "cand-4", "execution_role")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}
	if got.Actor.Confidence == nil || *got.Actor.Confidence != 0.87 {
		t.Errorf("confidence round-trip: %v, want 0.87", got.Actor.Confidence)
	}
}

func TestRecordInvalidActorKind(t *testing.T) {
	db := testutil.OpenTestDB(t)

	v := "x"
	err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-5", "execution_role",
		nil, &v,
		audit.ActorInfo{Kind: "ninja", ID: "x"},
	)
	if !errors.Is(err, audit.ErrInvalidActorKind) {
		t.Errorf("expected ErrInvalidActorKind, got %v", err)
	}
}

func TestRecordTransactionRollback(t *testing.T) {
	// If the outer transaction rolls back, the audit row MUST NOT be
	// visible — this is the load-bearing invariant of "atomic with the
	// subject mutation". Test by Record+Rollback then query.
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	v := "x"
	if err := audit.Record(tx, audit.SubjectBacklogCandidate, "cand-6", "execution_role",
		nil, &v,
		audit.ActorInfo{Kind: audit.ActorUser, ID: "u"},
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	_, err = audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "cand-6", "execution_role")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("after rollback, QueryLatest must return ErrNoRows; got %v", err)
	}
}

func TestRecordOrderingIsByCreatedAt(t *testing.T) {
	// QueryLatest returns the most recent by created_at. Insert several
	// rows separated by tiny sleeps so the timestamps differ; assert the
	// last write is what QueryLatest returns.
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	values := []string{"a", "b", "c", "d"}
	for _, v := range values {
		val := v
		if err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-7", "execution_role",
			nil, &val,
			audit.ActorInfo{Kind: audit.ActorUser, ID: "u"},
		); err != nil {
			t.Fatalf("record %q: %v", v, err)
		}
		// Ensure created_at advances; SQLite default precision is
		// 1 second so we sleep enough to avoid ties.
		time.Sleep(2 * time.Millisecond)
	}
	got, err := audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "cand-7", "execution_role")
	if err != nil {
		t.Fatalf("QueryLatest: %v", err)
	}
	if got.NewValue == nil || *got.NewValue != "d" {
		t.Errorf("latest NewValue = %v, want d", got.NewValue)
	}
}

func TestConcurrentRecordsAllPersist(t *testing.T) {
	// Concurrent Record calls in separate transactions must all
	// succeed (the table is append-only with no uniqueness constraint
	// on subject/field beyond the audit row's own id) and must all be
	// findable. This pins the "audit is append-only" invariant.
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	const writers = 8
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			v := "v" + string(rune('0'+i))
			if err := recordTx(t, db, audit.SubjectBacklogCandidate, "cand-8", "execution_role",
				nil, &v,
				audit.ActorInfo{Kind: audit.ActorUser, ID: "u"},
			); err != nil {
				t.Errorf("record %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// At least one row must be queryable; we don't assert order here
	// because concurrent goroutines can interleave on most platforms.
	if _, err := audit.QueryLatest(ctx, db, audit.SubjectBacklogCandidate, "cand-8", "execution_role"); err != nil {
		t.Errorf("after %d concurrent writes, QueryLatest err: %v", writers, err)
	}

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actor_audit WHERE subject_kind = $1 AND subject_id = $2`,
		string(audit.SubjectBacklogCandidate), "cand-8",
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != writers {
		t.Errorf("expected %d audit rows, got %d", writers, count)
	}
}
