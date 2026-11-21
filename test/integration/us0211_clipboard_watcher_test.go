package integration

// US-0211: Passive Clipboard Watcher (re-grounded in ADR-066).
//
// Drives the real ambient substrate in-process: a clipboard.Source polling a
// fake OS clipboard (the only stub; the OS boundary), the ambient.Runner, the
// fingerprint dedup cache and the HTTP enqueuer, against the in-process dpkms
// server for the end-to-end case. No OS clipboard, no daemon, no ports other
// than loopback httptest/in-process listeners.
//
// Not covered here (unbuilt): no binary constructs an ambient.Runner and
// `ctxt capture --ambient` returns "not yet implemented", so pause/resume and
// the foreground-app deny-list have nothing to drive.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
	"github.com/ideacrafterslabs/ctxt/internal/ambient/clipboard"
	"github.com/ideacrafterslabs/ctxt/internal/ambient/dedup"
	"github.com/ideacrafterslabs/ctxt/internal/ambient/enqueue"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeClipboard is the OS boundary: tests set what "the clipboard" holds.
type fakeClipboard struct {
	mu   sync.Mutex
	text string
}

func (f *fakeClipboard) set(s string) {
	f.mu.Lock()
	f.text = s
	f.mu.Unlock()
}

func (f *fakeClipboard) ReadText(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, nil
}

// recordingEnqueuer records events and optionally forwards to a real enqueuer.
type recordingEnqueuer struct {
	mu   sync.Mutex
	evs  []ambient.RawEvent
	next ambient.Enqueuer
}

func (r *recordingEnqueuer) Enqueue(ctx context.Context, ev ambient.RawEvent) error {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
	if r.next != nil {
		return r.next.Enqueue(ctx, ev)
	}
	return nil
}

func (r *recordingEnqueuer) snapshot() []ambient.RawEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ambient.RawEvent(nil), r.evs...)
}

// clipboardRig is a running Runner with a clipboard source.
type clipboardRig struct {
	clip *fakeClipboard
	enq  *recordingEnqueuer

	mu      sync.Mutex
	topics  []string
	stopRun func()
}

func (c *clipboardRig) topicCount(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, tp := range c.topics {
		if tp == topic {
			n++
		}
	}
	return n
}

func startClipboardRig(t *testing.T, next ambient.Enqueuer) *clipboardRig {
	t.Helper()
	rig := &clipboardRig{clip: &fakeClipboard{}, enq: &recordingEnqueuer{next: next}}

	b := bus.New()
	b.Subscribe("ctxt.ambient.#", func(_ context.Context, e bus.Event) error {
		rig.mu.Lock()
		rig.topics = append(rig.topics, string(e.Topic))
		rig.mu.Unlock()
		return nil
	})

	src, err := clipboard.New(rig.clip, clipboard.Config{PollInterval: 10 * time.Millisecond})
	require.NoError(t, err)
	runner, err := ambient.NewRunner(ambient.RunnerOptions{
		Bus:      b,
		Dedup:    dedup.NewCache(),
		Enqueuer: rig.enq,
	})
	require.NoError(t, err)
	require.NoError(t, runner.Register(src))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.Start(ctx)
	}()
	rig.stopRun = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runner did not stop")
		}
	}
	t.Cleanup(rig.stopRun)
	return rig
}

func waitEvents(t *testing.T, rig *clipboardRig, n int) []ambient.RawEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if evs := rig.enq.snapshot(); len(evs) >= n {
			return evs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %d enqueued events, got %d", n, len(rig.enq.snapshot()))
	return nil
}

const longClip = "Quarterly planning notes: the migration to the new queue backend " +
	"finishes in March, the rollback plan is documented, and the owners are Dana and Priya."

// Copying long text yields a KnowledgeObject through the real dpkms analyze
// path, routed to the text.short pipeline, with a populated fingerprint.
func TestClipboardWatcher_CapturesPlainText(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	client, err := enqueue.New(env.URL)
	require.NoError(t, err)
	rig := startClipboardRig(t, client)

	rig.clip.set(longClip)
	evs := waitEvents(t, rig, 1)
	assert.Equal(t, "clipboard", evs[0].Source)
	assert.Equal(t, ambient.KindText, evs[0].Kind)
	assert.NotEmpty(t, evs[0].Fingerprint, "fingerprint must be populated")

	// The job the runner enqueued lands in dpkms and completes.
	var jobID string
	require.Eventually(t, func() bool {
		jobs, _, err := env.driver.Jobs().List(ownerCtx(), storage.JobFilter{Limit: 10})
		if err != nil || len(jobs) == 0 {
			return false
		}
		jobID = jobs[0].ID
		return true
	}, 10*time.Second, 50*time.Millisecond, "no job created from clipboard event")

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	assert.Equal(t, "text.short", job.Pipeline)
	obj := getObject(t, env.URL, job.ResultID)
	assert.Equal(t, longClip, strings.TrimSpace(obj.TextContent))
}

// Identical content copied again inside the window is dropped by the
// fingerprint dedup at the enqueue boundary and announced on the bus.
func TestClipboardWatcher_DedupeRapidUpdates(t *testing.T) {
	rig := startClipboardRig(t, nil)

	other := longClip + " Second variant."
	rig.clip.set(longClip)
	waitEvents(t, rig, 1)
	rig.clip.set(other)
	waitEvents(t, rig, 2)
	// Source-side suppression only covers consecutive repeats; copying the
	// first text again reaches the substrate dedup.
	rig.clip.set(longClip)
	require.Eventually(t, func() bool {
		return rig.topicCount(string(events.TopicAmbientEventDeduped)) >= 1
	}, 10*time.Second, 10*time.Millisecond, "repeat copy must emit a deduped event")

	assert.Len(t, rig.enq.snapshot(), 2, "the repeat must not be enqueued")
}

// Routing heuristics: URL to url.generic, fenced code and long text to
// text.short, short plain text skipped.
func TestClipboardWatcher_RoutesToTextPipeline(t *testing.T) {
	rig := startClipboardRig(t, nil)

	steps := []struct {
		clip     string
		kind     ambient.Kind
		pipeline string
	}{
		{"https://example.com/some/article", ambient.KindURL, "url.generic"},
		{"```go\nfmt.Println(1)\n```", ambient.KindText, "text.short"},
		{longClip, ambient.KindText, "text.short"},
	}
	// A short plain string must be skipped; copy it first so a leak would
	// show up ahead of the expected events.
	rig.clip.set("too short")
	time.Sleep(100 * time.Millisecond)
	for i, s := range steps {
		rig.clip.set(s.clip)
		waitEvents(t, rig, i+1)
	}

	evs := rig.enq.snapshot()
	require.Len(t, evs, len(steps), "short text must not be enqueued")
	for i, s := range steps {
		assert.Equal(t, s.kind, evs[i].Kind, "step %d kind", i)
		assert.Equal(t, s.pipeline, evs[i].SuggestedPipeline, "step %d pipeline", i)
	}
}

// TestClipboardWatcher_RespectsAppAllowlist: unbuilt. clipboard.Config has no
// deny-list, the Runner has no policy hook and no foreground signal reaches
// clipboard events.
func TestClipboardWatcher_RespectsAppAllowlist(t *testing.T) {
	t.Skip("unbuilt: clipboard.Config has no app deny-list and no foreground signal reaches clipboard events")
}

// TestClipboardWatcher_PauseResume: unbuilt. No binary constructs an
// ambient.Runner and `ctxt capture --ambient` returns "not yet implemented".
func TestClipboardWatcher_PauseResume(t *testing.T) {
	t.Skip("unbuilt: no binary constructs ambient.Runner; `ctxt capture --ambient` is not implemented")
}
