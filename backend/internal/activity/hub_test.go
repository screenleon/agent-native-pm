package activity_test

import (
	"sync"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/activity"
	"github.com/screenleon/agent-native-pm/internal/models"
)

// mockPersister is a no-op persister for unit tests.
type mockPersister struct {
	mu    sync.Mutex
	calls []persistCall
	err   error
}

type persistCall struct {
	connectorID string
	activity    models.ConnectorActivity
}

func (m *mockPersister) PersistActivity(connectorID string, a models.ConnectorActivity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, persistCall{connectorID: connectorID, activity: a})
	return m.err
}

func (m *mockPersister) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// TestUpdate_BroadcastsToMultipleSubscribers verifies that Update delivers
// the activity to all currently subscribed channels.
func TestUpdate_BroadcastsToMultipleSubscribers(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	_, ch1, unsub1 := hub.Subscribe("conn-1")
	defer unsub1()
	_, ch2, unsub2 := hub.Subscribe("conn-1")
	defer unsub2()

	a := models.ConnectorActivity{
		Phase:     models.ConnectorPhasePlanning,
		UpdatedAt: time.Now().UTC(),
	}
	hub.Update("conn-1", a)

	// Both channels should receive the activity.
	select {
	case got := <-ch1:
		if got.Phase != models.ConnectorPhasePlanning {
			t.Errorf("ch1: expected phase %q, got %q", models.ConnectorPhasePlanning, got.Phase)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch1: timed out waiting for activity")
	}

	select {
	case got := <-ch2:
		if got.Phase != models.ConnectorPhasePlanning {
			t.Errorf("ch2: expected phase %q, got %q", models.ConnectorPhasePlanning, got.Phase)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("ch2: timed out waiting for activity")
	}
}

// TestUpdate_SlowSubscriberDropped verifies that a full channel is not sent to
// (non-blocking drop) and does not block the publisher.
func TestUpdate_SlowSubscriberDropped(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	// Subscribe but never consume.
	_, _, unsub := hub.Subscribe("conn-slow")
	defer unsub()

	// Fill the channel buffer (size 8) so the next Update would block a
	// synchronous sender.
	for i := 0; i < 10; i++ {
		a := models.ConnectorActivity{Phase: models.ConnectorPhaseIdle, UpdatedAt: time.Now().UTC()}
		done := make(chan struct{})
		go func() {
			hub.Update("conn-slow", a)
			close(done)
		}()
		select {
		case <-done:
			// Good — did not block.
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Update blocked on iteration %d (slow subscriber not dropped)", i)
		}
	}
}

// TestRestoreFromDB_PrePopulatesState verifies that RestoreFromDB seeds the
// hub's in-memory map and that Get returns the restored value.
func TestRestoreFromDB_PrePopulatesState(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	activities := map[string]models.ConnectorActivity{
		"conn-a": {Phase: models.ConnectorPhasePlanning, UpdatedAt: time.Now().UTC()},
		"conn-b": {Phase: models.ConnectorPhaseIdle, UpdatedAt: time.Now().UTC()},
	}
	hub.RestoreFromDB(activities)

	got, ok := hub.Get("conn-a")
	if !ok {
		t.Fatal("Get(conn-a): expected ok=true after RestoreFromDB")
	}
	if got.Phase != models.ConnectorPhasePlanning {
		t.Errorf("conn-a phase: expected %q, got %q", models.ConnectorPhasePlanning, got.Phase)
	}

	got, ok = hub.Get("conn-b")
	if !ok {
		t.Fatal("Get(conn-b): expected ok=true after RestoreFromDB")
	}
	if got.Phase != models.ConnectorPhaseIdle {
		t.Errorf("conn-b phase: expected %q, got %q", models.ConnectorPhaseIdle, got.Phase)
	}
}

// TestSubscribe_ReturnsInitialState verifies that Subscribe returns the
// current in-memory state immediately, even without a prior Update call
// (after a RestoreFromDB).
func TestSubscribe_ReturnsInitialState(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	expected := models.ConnectorActivity{
		Phase:     models.ConnectorPhaseDispatching,
		SubjectID: "task-123",
		UpdatedAt: time.Now().UTC(),
	}
	hub.RestoreFromDB(map[string]models.ConnectorActivity{"conn-x": expected})

	initial, _, unsub := hub.Subscribe("conn-x")
	defer unsub()

	if initial.Phase != models.ConnectorPhaseDispatching {
		t.Errorf("initial phase: expected %q, got %q", models.ConnectorPhaseDispatching, initial.Phase)
	}
	if initial.SubjectID != "task-123" {
		t.Errorf("initial SubjectID: expected %q, got %q", "task-123", initial.SubjectID)
	}
}

// TestSubscribe_NoInitialState returns a zero-value activity when the connector
// has no recorded state.
func TestSubscribe_NoInitialState(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	initial, _, unsub := hub.Subscribe("conn-unknown")
	defer unsub()

	if initial.Phase != "" {
		t.Errorf("expected empty phase for unknown connector, got %q", initial.Phase)
	}
}

// TestUpdate_PersistsAsync verifies that Update calls the persister (async).
func TestUpdate_PersistsAsync(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	_, _, unsub := hub.Subscribe("conn-p")
	defer unsub()

	a := models.ConnectorActivity{Phase: models.ConnectorPhaseIdle, UpdatedAt: time.Now().UTC()}
	hub.Update("conn-p", a)

	// Give the async goroutine time to fire.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.CallCount() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("persister was not called within 200ms of Update")
}

// TestRestoreFromDB_DoesNotOverwriteExisting verifies that RestoreFromDB skips
// connectors that already have in-memory state (set by a concurrent Update
// before restore runs).
func TestRestoreFromDB_DoesNotOverwriteExisting(t *testing.T) {
	p := &mockPersister{}
	hub := activity.NewHub(p)

	live := models.ConnectorActivity{Phase: models.ConnectorPhasePlanning, UpdatedAt: time.Now().UTC()}
	hub.Update("conn-z", live)

	stale := models.ConnectorActivity{Phase: models.ConnectorPhaseIdle, UpdatedAt: time.Now().Add(-10 * time.Minute)}
	hub.RestoreFromDB(map[string]models.ConnectorActivity{"conn-z": stale})

	got, _ := hub.Get("conn-z")
	if got.Phase != models.ConnectorPhasePlanning {
		t.Errorf("RestoreFromDB should not overwrite live state; got phase %q", got.Phase)
	}
}
