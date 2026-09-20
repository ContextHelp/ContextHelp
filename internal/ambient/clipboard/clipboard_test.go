package clipboard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// fakeReader is a controllable Reader: tests Set the next ReadText result.
type fakeReader struct {
	mu    sync.Mutex
	text  string
	err   error
	reads atomic.Int64
}

func (f *fakeReader) Set(text string) {
	f.mu.Lock()
	f.text = text
	f.err = nil
	f.mu.Unlock()
}

func (f *fakeReader) SetError(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *fakeReader) ReadText(_ context.Context) (string, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.err
}

// fakePublisher captures Publish calls.
type fakePublisher struct {
	mu     sync.Mutex
	topics []string
}

func (p *fakePublisher) Publish(_ context.Context, topic, _ string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	return nil
}

func (p *fakePublisher) HasTopic(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.topics {
		if s == t {
			return true
		}
	}
	return false
}

// drainEventOrFail reads one event from src.Events() within the deadline,
// or fails the test.
func drainEventOrFail(t *testing.T, src *Source, deadline time.Duration) ambient.RawEvent {
	t.Helper()
	select {
	case ev := <-src.Events():
		return ev
	case <-time.After(deadline):
		t.Fatalf("did not receive event within %s", deadline)
	}
	return ambient.RawEvent{}
}

func TestNew_RequiresReader(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, Config{}); err == nil {
		t.Fatal("New(nil): expected error")
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	t.Parallel()
	s, err := New(&fakeReader{}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.cfg.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %s, want %s", s.cfg.PollInterval, DefaultPollInterval)
	}
	if s.cfg.MinLength != DefaultMinLength {
		t.Errorf("MinLength = %d, want %d", s.cfg.MinLength, DefaultMinLength)
	}
	if s.cfg.RouteUrlsTo != "url.generic" {
		t.Errorf("RouteUrlsTo = %q, want url.generic", s.cfg.RouteUrlsTo)
	}
	if s.cfg.RouteTextTo != "text.short" {
		t.Errorf("RouteTextTo = %q, want text.short", s.cfg.RouteTextTo)
	}
}

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{})
	if s.Name() != "clipboard" {
		t.Errorf("Name = %q, want clipboard", s.Name())
	}
}

func TestSource_EmitsURLAsURLKind(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Set("https://example.com/article")

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Kind != ambient.KindURL {
		t.Errorf("Kind = %q, want %q", ev.Kind, ambient.KindURL)
	}
	if ev.SuggestedPipeline != "url.generic" {
		t.Errorf("SuggestedPipeline = %q, want url.generic", ev.SuggestedPipeline)
	}
	if string(ev.Payload) != "https://example.com/article" {
		t.Errorf("Payload = %q", ev.Payload)
	}
	if ev.Fingerprint == "" {
		t.Error("Fingerprint must be populated")
	}
}

func TestSource_EmitsCodeBlockAsTextKind(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Set("```go\npackage main\n```")

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Kind != ambient.KindText {
		t.Errorf("Kind = %q, want %q (code block)", ev.Kind, ambient.KindText)
	}
	if ev.SuggestedPipeline != "text.short" {
		t.Errorf("SuggestedPipeline = %q, want text.short", ev.SuggestedPipeline)
	}
}

func TestSource_EmitsLongTextAsTextKind(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	long := "This is some longer text content that should trigger the length-based heuristic for the clipboard source."
	r.Set(long)

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond, MinLength: 80})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Kind != ambient.KindText {
		t.Errorf("Kind = %q, want %q", ev.Kind, ambient.KindText)
	}
}

func TestSource_SkipsShortText(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Set("hi")

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond, MinLength: 80})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case ev := <-s.Events():
		t.Errorf("expected no event for short text; got %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected: no emission
	}
}

func TestSource_SkipsEmptyClipboard(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Set("")

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case ev := <-s.Events():
		t.Errorf("expected no event for empty clipboard; got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSource_SuppressesRepeatedReads(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	url := "https://example.com/article"
	r.Set(url)

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// First emit
	_ = drainEventOrFail(t, s, time.Second)

	// No second emit even though reader keeps returning same content.
	select {
	case ev := <-s.Events():
		t.Errorf("expected suppression of repeated read; got second emission %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSource_EmitsAgainWhenContentChanges(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Set("https://example.com/one")

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	first := drainEventOrFail(t, s, time.Second)
	if string(first.Payload) != "https://example.com/one" {
		t.Errorf("first event payload = %q", first.Payload)
	}

	r.Set("https://example.com/two")
	second := drainEventOrFail(t, s, time.Second)
	if string(second.Payload) != "https://example.com/two" {
		t.Errorf("second event payload = %q", second.Payload)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Error("distinct payloads must produce distinct fingerprints")
	}
}

func TestSource_PublishesReadyTopicOnStart(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	pub := &fakePublisher{}

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, pub); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if !pub.HasTopic("ctxt.ambient.source.ready") {
		t.Error("missing ctxt.ambient.source.ready on Start")
	}
}

func TestSource_PublishesFailedOnReaderError(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.SetError(errors.New("os denied"))
	pub := &fakePublisher{}

	s, _ := New(r, Config{PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Wait for at least one tick to attempt a read.
	deadline := time.After(time.Second)
	for {
		if pub.HasTopic("ctxt.ambient.source.failed") {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("ctxt.ambient.source.failed not emitted within deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSource_StartTwiceRejected(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if err := s.Start(ctx, nil); err == nil {
		t.Error("second Start: expected error, got nil")
	}
}

func TestSource_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Errorf("second Stop: %v (should be idempotent)", err)
	}
}

func TestSource_RouteHeuristics(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{MinLength: 10})
	cases := []struct {
		name     string
		input    string
		wantOK   bool
		wantKind ambient.Kind
	}{
		{"http url", "http://example.com/path", true, ambient.KindURL},
		{"https url", "https://example.com/path", true, ambient.KindURL},
		{"url with spaces (rejected as URL)", "http://example.com hello", true, ambient.KindText}, // falls through to length check; this string is long enough
		{"code block ``` no lang", "```\nx\n```", true, ambient.KindText},
		{"code block with lang", "```python\nprint(1)\n```", true, ambient.KindText},
		{"long text", "this is a longer text content here", true, ambient.KindText},
		{"short text", "hi", false, ""},
		{"single word", "abc", false, ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			kind, _, ok := s.route(c.input)
			if ok != c.wantOK {
				t.Errorf("route(%q) ok = %v, want %v", c.input, ok, c.wantOK)
			}
			if ok && kind != c.wantKind {
				t.Errorf("route(%q) kind = %q, want %q", c.input, kind, c.wantKind)
			}
		})
	}
}

func TestFingerprint_DeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	a := fingerprint([]byte("hello"))
	b := fingerprint([]byte("hello"))
	c := fingerprint([]byte("world"))
	if a != b {
		t.Error("identical inputs must yield identical fingerprints")
	}
	if a == c {
		t.Error("distinct inputs must yield distinct fingerprints")
	}
	if len(a) != 64 { // 32 bytes hex-encoded
		t.Errorf("fingerprint length = %d, want 64", len(a))
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
