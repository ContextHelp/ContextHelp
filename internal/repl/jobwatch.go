package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// JobWatcher subscribes to the event bus and prints async job status
// notifications without interrupting the read loop. After printing a
// notification it reprints the prompt so the user can continue typing.
type JobWatcher struct {
	bus    events.Bus
	prompt string
	out    io.Writer
	mu     sync.Mutex
	done   chan struct{}
}

// NewJobWatcher creates a JobWatcher. Call Start to begin watching.
func NewJobWatcher(bus events.Bus, prompt string, out io.Writer) *JobWatcher {
	return &JobWatcher{
		bus:    bus,
		prompt: prompt,
		out:    out,
		done:   make(chan struct{}),
	}
}

// Start subscribes to job.completed and job.failed events on the bus.
func (w *JobWatcher) Start(ctx context.Context) {
	handler := func(_ context.Context, e events.Event) error {
		select {
		case <-w.done:
			return nil
		default:
		}
		jobID := jobIDFromEvent(e)
		w.mu.Lock()
		defer w.mu.Unlock()
		fmt.Fprintf(w.out, "\n[job %s %s]\n%s", jobID, eventVerb(e.Type), w.prompt)
		return nil
	}
	w.bus.Subscribe("job.completed", handler)
	w.bus.Subscribe("job.failed", handler)
}

// Stop signals the watcher to ignore further events.
func (w *JobWatcher) Stop() {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
}

// jobIDFromEvent extracts the job_id from an event's Data field (json.RawMessage).
func jobIDFromEvent(e events.Event) string {
	if e.Data == nil {
		return e.ID
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(e.Data, &payload); err != nil {
		return e.ID
	}
	if id, ok := payload["job_id"]; ok {
		return fmt.Sprintf("%v", id)
	}
	return e.ID
}

// eventVerb maps an event type string to a human-readable past-tense verb.
func eventVerb(eventType string) string {
	switch eventType {
	case "job.completed":
		return "completed"
	case "job.failed":
		return "failed"
	default:
		return eventType
	}
}
