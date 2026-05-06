package foreground

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// fakeReader is a controllable Reader: tests Push focus changes.
type fakeReader struct {
	mu       sync.Mutex
	onChange func(Window)
	subErr   error
	cancelled bool
}

func (f *fakeReader) Subscribe(ctx context.Context, onChange func(Window)) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subErr != nil {
		return nil, f.subErr
	}
	f.onChange = onChange
	return func() {
		f.mu.Lock()
		f.cancelled = true
		f.onChange = nil
		f.mu.Unlock()
	}, nil
}

func (f *fakeReader) Push(w Window) {
	f.mu.Lock()
	cb := f.onChange
	f.mu.Unlock()
	if cb != nil {
		cb(w)
	}
}

func (f *fakeReader) Cancelled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelled
}

type capturingPub struct {
	mu     sync.Mutex
	topics []string
}

func (p *capturingPub) Publish(_ context.Context, topic, _ string, _ any) error {
	p.mu.Lock()
	p.topics = append(p.topics, topic)
	p.mu.Unlock()
	return nil
}

func (p *capturingPub) HasTopic(t string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.topics {
		if s == t {
			return true
		}
	}
	return false
}

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

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{})
	if s.Name() != "foreground" {
		t.Errorf("Name = %q, want foreground", s.Name())
	}
}

func TestSource_EmitsRawEventOnFocusChange(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{CaptureWindowTitle: true})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	r.Push(Window{
		BundleID:    "us.zoom.xos",
		AppName:     "zoom.us",
		WindowTitle: "Q3 Planning",
		FocusedAt:   time.Now(),
	})

	ev := drainEventOrFail(t, s, time.Second)
	if ev.Kind != ambient.KindWindowFocus {
		t.Errorf("Kind = %q, want %q", ev.Kind, ambient.KindWindowFocus)
	}
	if string(ev.Payload) != "us.zoom.xos" {
		t.Errorf("Payload = %q, want bundle_id us.zoom.xos", ev.Payload)
	}
	if ev.Metadata["bundle_id"] != "us.zoom.xos" {
		t.Errorf("Metadata[bundle_id] = %v, want us.zoom.xos", ev.Metadata["bundle_id"])
	}
	if ev.Metadata["app_name"] != "zoom.us" {
		t.Errorf("Metadata[app_name] = %v, want zoom.us", ev.Metadata["app_name"])
	}
	if ev.Metadata["window_title"] != "Q3 Planning" {
		t.Errorf("Metadata[window_title] = %v, want Q3 Planning", ev.Metadata["window_title"])
	}
	// Critical privacy assertion: NO ax-tree, focused-element, visible-text,
	// or keystroke fields.
	if _, has := ev.Metadata["ax_tree"]; has {
		t.Error("RawEvent.Metadata MUST NOT carry ax_tree")
	}
	if _, has := ev.Metadata["focused_element"]; has {
		t.Error("RawEvent.Metadata MUST NOT carry focused_element")
	}
	if _, has := ev.Metadata["visible_text"]; has {
		t.Error("RawEvent.Metadata MUST NOT carry visible_text")
	}
}

func TestSource_OmitsWindowTitleWhenDisabled(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{CaptureWindowTitle: false})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	r.Push(Window{
		BundleID:    "com.apple.Safari",
		AppName:     "Safari",
		WindowTitle: "Sensitive Bank Account",
	})

	ev := drainEventOrFail(t, s, time.Second)
	if _, has := ev.Metadata["window_title"]; has {
		t.Error("window_title leaked when CaptureWindowTitle=false")
	}
	if ev.Metadata["bundle_id"] != "com.apple.Safari" {
		t.Errorf("bundle_id should still be present")
	}
}

func TestSource_DebouncesRapidBursts(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{DebounceWindow: 200 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Rapid burst of same bundle within debounce window: only one event.
	for range 5 {
		r.Push(Window{BundleID: "us.zoom.xos", AppName: "zoom"})
	}

	_ = drainEventOrFail(t, s, time.Second)
	select {
	case ev := <-s.Events():
		t.Errorf("debounce failed: got second emission %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSource_EmitsAgainAfterDebounceExpires(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{DebounceWindow: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	r.Push(Window{BundleID: "com.example.app"})
	_ = drainEventOrFail(t, s, time.Second)

	time.Sleep(80 * time.Millisecond)
	r.Push(Window{BundleID: "com.example.app"})
	_ = drainEventOrFail(t, s, time.Second)
}

func TestSource_EmitsImmediatelyOnAppSwitch(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{DebounceWindow: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	r.Push(Window{BundleID: "us.zoom.xos"})
	_ = drainEventOrFail(t, s, time.Second)

	r.Push(Window{BundleID: "com.apple.Safari"})
	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != "com.apple.Safari" {
		t.Errorf("after switch, Payload = %q, want com.apple.Safari", ev.Payload)
	}
}

func TestSource_ExcludeBundlesDropsBeforeEmission(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{
		ExcludeBundles: []string{"com.1password.*"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	r.Push(Window{BundleID: "com.1password.macos"})

	select {
	case ev := <-s.Events():
		t.Errorf("excluded bundle should not produce event; got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMatchBundle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, bundle string
		want            bool
	}{
		{"com.example.app", "com.example.app", true},
		{"com.example.app", "com.other.app", false},
		{"com.example.*", "com.example.app", true},
		{"com.example.*", "com.example.subapp.helper", true},
		{"com.example.*", "com.other.app", false},
		{"*.helper", "com.example.helper", true},
		{"com.*.app", "com.example.app", true},
		{"com.*.app", "com.example.helper", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.pattern+"_"+c.bundle, func(t *testing.T) {
			t.Parallel()
			if got := matchBundle(c.pattern, c.bundle); got != c.want {
				t.Errorf("matchBundle(%q, %q) = %v, want %v", c.pattern, c.bundle, got, c.want)
			}
		})
	}
}

func TestSource_PublishesReadyOnStart(t *testing.T) {
	t.Parallel()
	pub := &capturingPub{}
	s, _ := New(&fakeReader{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if !pub.HasTopic("ctxt.ambient.source.ready") {
		t.Error("expected ctxt.ambient.source.ready on Start")
	}
}

func TestSource_PublishesFailedWhenSubscribeReturnsErrNotSupported(t *testing.T) {
	t.Parallel()
	r := &fakeReader{subErr: ErrNotSupported}
	pub := &capturingPub{}
	s, _ := New(r, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	err := s.Start(ctx, pub)
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("Start: expected ErrNotSupported, got %v", err)
	}
	if !pub.HasTopic("ctxt.ambient.source.failed") {
		t.Error("expected ctxt.ambient.source.failed when reader returns ErrNotSupported")
	}
}

func TestSource_StopCancelsSubscription(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	s, _ := New(r, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	_ = s.Stop(context.Background())
	if !r.Cancelled() {
		t.Error("Stop did not invoke Reader cancel")
	}
}

func TestSource_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	s, _ := New(&fakeReader{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Errorf("second Stop should be idempotent: %v", err)
	}
}

func TestFingerprint_BucketsToSecond(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 5, 5, 14, 30, 5, 100000000, time.UTC)
	t2 := time.Date(2026, 5, 5, 14, 30, 5, 900000000, time.UTC)
	if a, b := fingerprint("com.example", t1), fingerprint("com.example", t2); a != b {
		t.Errorf("same second should yield same fingerprint: %s vs %s", a, b)
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
