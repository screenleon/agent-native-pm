package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

// coalesceWindow is how long to wait before flushing same-phase step changes.
// Phase changes always flush immediately.
const coalesceWindow = 500 * time.Millisecond

// ActivityReporter sends activity updates to the server asynchronously.
// Phase changes are always enqueued immediately; same-phase step changes
// within the coalesce window are merged into the last queued entry so the
// server is not flooded on tight inner loops.
type ActivityReporter struct {
	client   *Client
	mu       sync.Mutex
	queue    []models.ConnectorActivity
	flushCh  chan struct{}
	done     chan struct{}
	last     models.ConnectorActivity
	lastTime time.Time
}

// NewActivityReporter creates an ActivityReporter backed by client.
func NewActivityReporter(client *Client) *ActivityReporter {
	return &ActivityReporter{
		client:  client,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

// Start launches the background flush goroutine. Call once, before any
// Report calls. The goroutine exits when ctx is cancelled.
func (r *ActivityReporter) Start(ctx context.Context) {
	go r.run(ctx)
}

// Report enqueues an activity update. If the phase differs from the last
// queued entry, or the coalesce window has elapsed, a new entry is appended
// to the queue. Otherwise, the last queued entry is replaced with a
// (same-phase) step change.
func (r *ActivityReporter) Report(a models.ConnectorActivity) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	isPhaseChange := a.Phase != r.last.Phase
	withinCoalesce := now.Sub(r.lastTime) < coalesceWindow

	if !isPhaseChange && withinCoalesce && len(r.queue) > 0 {
		// Merge into the last entry: preserve Phase, update the rest.
		last := r.queue[len(r.queue)-1]
		last.Step = a.Step
		last.SubjectKind = a.SubjectKind
		last.SubjectID = a.SubjectID
		last.SubjectTitle = a.SubjectTitle
		last.RoleID = a.RoleID
		last.UpdatedAt = a.UpdatedAt
		r.queue[len(r.queue)-1] = last
	} else {
		r.queue = append(r.queue, a)
		r.last = a
		r.lastTime = now
	}

	// Signal the flush goroutine (non-blocking).
	select {
	case r.flushCh <- struct{}{}:
	default:
	}
}

// Snapshot returns the last activity that was enqueued (zero value if none).
func (r *ActivityReporter) Snapshot() models.ConnectorActivity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// run is the background flush goroutine.
func (r *ActivityReporter) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining queue before exit (best-effort, 5 s deadline).
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			r.flush(shutdownCtx)
			cancel()
			return
		case <-r.flushCh:
			r.flush(ctx)
		case <-ticker.C:
			r.flush(ctx)
		}
	}
}

// flush drains the queue and POSTs each activity to the server.
// Failures are logged and ignored (fire-and-forget).
func (r *ActivityReporter) flush(ctx context.Context) {
	r.mu.Lock()
	if len(r.queue) == 0 {
		r.mu.Unlock()
		return
	}
	batch := make([]models.ConnectorActivity, len(r.queue))
	copy(batch, r.queue)
	r.queue = r.queue[:0]
	r.mu.Unlock()

	for _, a := range batch {
		if err := r.client.ReportActivity(ctx, a); err != nil {
			fmt.Printf("activity reporter: POST failed: %v\n", err)
		}
	}
}
