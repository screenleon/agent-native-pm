package events

import "sync"

// Event is a named payload pushed to subscribed clients.
type Event struct {
	Type string
	Data interface{}
}

// Broker is a simple in-process fan-out pub/sub. Each subscriber gets its
// own buffered channel; slow readers are dropped silently rather than
// blocking publishers.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // keyed by userID
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string][]chan Event)}
}

// Subscribe registers a channel for the given user and returns an unsubscribe
// func that the caller must invoke (typically via defer) to release the channel.
func (b *Broker) Subscribe(userID string) (chan Event, func()) {
	ch := make(chan Event, 8)
	b.mu.Lock()
	b.subscribers[userID] = append(b.subscribers[userID], ch)
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[userID]
		for i, s := range subs {
			if s == ch {
				b.subscribers[userID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, unsubscribe
}

// Publish delivers an event to all active subscribers for the user.
// Non-blocking: subscribers that are too slow to consume are skipped.
func (b *Broker) Publish(userID string, evt Event) {
	b.mu.RLock()
	subs := b.subscribers[userID]
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
		}
	}
}
