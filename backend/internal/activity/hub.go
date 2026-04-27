// Package activity provides in-process pub/sub for connector activity state.
// The Hub maintains the latest ConnectorActivity for each connector in memory,
// broadcasts updates to all current subscribers, and persists snapshots via a
// Persister interface so state survives across client reconnects and can be
// restored after a server restart.
package activity

import (
	"log"
	"sync"

	"github.com/screenleon/agent-native-pm/internal/models"
)

// Persister is a store-level interface for persisting activity snapshots.
// Implemented by LocalConnectorStore.PersistActivity.
type Persister interface {
	PersistActivity(connectorID string, a models.ConnectorActivity) error
}

// Hub is an in-process fan-out registry for connector activity state.
// Safe for concurrent use by multiple goroutines.
type Hub struct {
	mu          sync.RWMutex
	states      map[string]models.ConnectorActivity
	subscribers map[string][]chan models.ConnectorActivity
	persister   Persister
}

// NewHub creates a Hub backed by the given Persister. persister may be nil
// (useful in tests that don't need DB persistence).
func NewHub(p Persister) *Hub {
	return &Hub{
		states:      make(map[string]models.ConnectorActivity),
		subscribers: make(map[string][]chan models.ConnectorActivity),
		persister:   p,
	}
}

// Update stores the latest activity for connectorID in memory, broadcasts it
// to all active subscribers, and calls the persister asynchronously (fire and
// forget — a failed persist is logged but never blocks callers).
func (h *Hub) Update(connectorID string, a models.ConnectorActivity) {
	h.mu.Lock()
	h.states[connectorID] = a
	subs := make([]chan models.ConnectorActivity, len(h.subscribers[connectorID]))
	copy(subs, h.subscribers[connectorID])
	h.mu.Unlock()

	// Broadcast to subscribers. Non-blocking: slow readers are dropped.
	for _, ch := range subs {
		select {
		case ch <- a:
		default:
		}
	}

	// Persist asynchronously so the hot path (connector POST) is not blocked
	// by a DB write.
	if h.persister != nil {
		go func() {
			if err := h.persister.PersistActivity(connectorID, a); err != nil {
				log.Printf("activity hub: persist failed for connector %s: %v", connectorID, err)
			}
		}()
	}
}

// Subscribe registers a subscriber for connectorID. It returns:
//   - initial: the current in-memory activity (zero value if none)
//   - ch: a channel that receives future updates
//   - unsub: a function the caller must invoke (typically via defer) to
//     release the channel when it is no longer needed
//
// The channel is buffered (size 8). Slow consumers will miss updates rather
// than blocking the publisher.
func (h *Hub) Subscribe(connectorID string) (initial models.ConnectorActivity, ch <-chan models.ConnectorActivity, unsub func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	current := h.states[connectorID]
	c := make(chan models.ConnectorActivity, 8)
	h.subscribers[connectorID] = append(h.subscribers[connectorID], c)

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.subscribers[connectorID]
		for i, s := range subs {
			if s == c {
				h.subscribers[connectorID] = append(subs[:i], subs[i+1:]...)
				close(c)
				return
			}
		}
	}
	return current, c, unsubscribe
}

// Get returns the current in-memory activity for connectorID, or a zero
// ConnectorActivity if none has been recorded. The second return value reports
// whether any activity was found.
func (h *Hub) Get(connectorID string) (models.ConnectorActivity, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	a, ok := h.states[connectorID]
	return a, ok
}

// RestoreFromDB pre-populates the hub's in-memory state from a map of
// persisted activities. Called at server startup so the hub has initial state
// even after a restart. Existing in-memory entries are not overwritten (though
// at startup the map will always be empty).
func (h *Hub) RestoreFromDB(activities map[string]models.ConnectorActivity) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, a := range activities {
		if _, exists := h.states[id]; !exists {
			h.states[id] = a
		}
	}
}
