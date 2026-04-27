// Package activity provides in-process pub/sub for connector activity state.
// The Hub maintains the latest ConnectorActivity for each connector in memory,
// broadcasts updates to all current subscribers, and persists snapshots via a
// Persister interface so state survives across client reconnects and can be
// restored after a server restart.
package activity

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

// maxSSEPerUser is the maximum number of concurrent SSE subscriptions a single
// user may hold. Exceeding this limit causes SubscribeWithCap to return
// ErrSSECapExceeded and the caller should respond 503.
// DECISIONS.md 2026-04-25 §(g): "per-user concurrent SSE connections capped at 3".
const maxSSEPerUser = 3

// idlePurgeTTL controls how long an idle activity is retained before the
// background purge goroutine evicts it from the in-memory states map.
// DECISIONS.md 2026-04-25 §(g): "idle activities retained 5 min before purge".
const idlePurgeTTL = 5 * time.Minute

// ErrSSECapExceeded is returned by SubscribeWithCap when the caller has
// reached maxSSEPerUser concurrent SSE subscriptions.
var ErrSSECapExceeded = errors.New("too many concurrent SSE connections for this user")

// Persister is a store-level interface for persisting activity snapshots.
// Implemented by LocalConnectorStore.PersistActivity.
type Persister interface {
	PersistActivity(connectorID string, a models.ConnectorActivity) error
}

// Hub is an in-process fan-out registry for connector activity state.
// Safe for concurrent use by multiple goroutines.
type Hub struct {
	mu           sync.RWMutex
	states       map[string]models.ConnectorActivity
	subscribers  map[string][]chan models.ConnectorActivity
	userSubCount map[string]int // userID → active SSE subscription count
	persister    Persister
}

// NewHub creates a Hub backed by the given Persister. persister may be nil
// (useful in tests that don't need DB persistence).
func NewHub(p Persister) *Hub {
	return &Hub{
		states:       make(map[string]models.ConnectorActivity),
		subscribers:  make(map[string][]chan models.ConnectorActivity),
		userSubCount: make(map[string]int),
		persister:    p,
	}
}

// StartPurge starts a background goroutine that evicts idle activity entries
// older than idlePurgeTTL. It runs every minute and stops when ctx is
// cancelled. Call this once from main after hub creation.
func (h *Hub) StartPurge(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.PurgeIdle()
			}
		}
	}()
}

// PurgeIdle evicts state entries where Phase == idle and UpdatedAt is older
// than idlePurgeTTL. Exported so tests can invoke it directly without waiting
// for the background ticker.
func (h *Hub) PurgeIdle() {
	cutoff := time.Now().UTC().Add(-idlePurgeTTL)
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, a := range h.states {
		if a.Phase == models.ConnectorPhaseIdle && !a.UpdatedAt.IsZero() && a.UpdatedAt.Before(cutoff) {
			delete(h.states, id)
		}
	}
}

// Update stores the latest activity for connectorID in memory, broadcasts it
// to all active subscribers, and calls the persister asynchronously (fire and
// forget — a failed persist is logged but never blocks callers).
//
// Channels are never closed by Update; subscribers are removed from the map by
// their unsub() callback. This avoids a send-on-closed-channel panic that would
// occur if a subscriber unsubscribed (and its channel got closed) between the
// moment Update copies the subscriber list and the moment it sends on each
// channel.
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
// than blocking the publisher. Channels are never closed; callers exit via
// context cancellation rather than channel-close detection.
//
// For SSE handlers that need a per-user cap, use SubscribeWithCap instead.
func (h *Hub) Subscribe(connectorID string) (initial models.ConnectorActivity, ch <-chan models.ConnectorActivity, unsub func()) {
	initial, ch, unsub, _ = h.subscribeInternal(connectorID, "", 0)
	return
}

// SubscribeWithCap is like Subscribe but enforces a per-user SSE cap. Returns
// ErrSSECapExceeded if the user already has maxSSEPerUser active subscriptions.
// The caller should respond 503 in that case.
func (h *Hub) SubscribeWithCap(connectorID, userID string) (initial models.ConnectorActivity, ch <-chan models.ConnectorActivity, unsub func(), err error) {
	return h.subscribeInternal(connectorID, userID, maxSSEPerUser)
}

func (h *Hub) subscribeInternal(connectorID, userID string, cap int) (initial models.ConnectorActivity, ch <-chan models.ConnectorActivity, unsub func(), err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cap > 0 && userID != "" && h.userSubCount[userID] >= cap {
		return models.ConnectorActivity{}, nil, nil, ErrSSECapExceeded
	}

	current := h.states[connectorID]
	c := make(chan models.ConnectorActivity, 8)
	h.subscribers[connectorID] = append(h.subscribers[connectorID], c)
	if userID != "" {
		h.userSubCount[userID]++
	}

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.subscribers[connectorID]
		for i, s := range subs {
			if s == c {
				h.subscribers[connectorID] = append(subs[:i], subs[i+1:]...)
				// Do NOT close c here. Update() may have already copied c into its
				// local subs slice and will attempt a non-blocking send after we
				// return. Closing c would cause a send-on-closed-channel panic.
				// The SSE handler exits via r.Context().Done(), not via ch close.
				break
			}
		}
		if userID != "" && h.userSubCount[userID] > 0 {
			h.userSubCount[userID]--
			if h.userSubCount[userID] == 0 {
				delete(h.userSubCount, userID)
			}
		}
	}
	return current, c, unsubscribe, nil
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
