package repl

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/stretchr/testify/assert"
)

func TestJobWatcher_CompletedEvent(t *testing.T) {
	bus := events.NewLocalBus()
	var buf bytes.Buffer
	w := NewJobWatcher(bus, "ctxt> ", &buf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	ev, _ := events.NewEvent("test", "job.completed", map[string]any{"job_id": "test-job-123"})
	bus.Publish(context.Background(), ev)

	time.Sleep(50 * time.Millisecond)
	w.Stop()

	out := buf.String()
	assert.Contains(t, out, "test-job-123")
	assert.Contains(t, out, "completed")
}

func TestJobWatcher_FailedEvent(t *testing.T) {
	bus := events.NewLocalBus()
	var buf bytes.Buffer
	w := NewJobWatcher(bus, "ctxt> ", &buf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	ev, _ := events.NewEvent("test", "job.failed", map[string]any{"job_id": "fail-job-456"})
	bus.Publish(context.Background(), ev)

	time.Sleep(50 * time.Millisecond)
	w.Stop()

	out := buf.String()
	assert.Contains(t, out, "fail-job-456")
	assert.True(t, strings.Contains(out, "failed") || strings.Contains(out, "fail"))
}

func TestJobWatcher_PromptReprinted(t *testing.T) {
	bus := events.NewLocalBus()
	var buf bytes.Buffer
	prompt := "ctxt> "
	w := NewJobWatcher(bus, prompt, &buf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	ev, _ := events.NewEvent("test", "job.completed", map[string]any{"job_id": "x"})
	bus.Publish(context.Background(), ev)

	time.Sleep(50 * time.Millisecond)
	w.Stop()

	assert.Contains(t, buf.String(), prompt)
}
