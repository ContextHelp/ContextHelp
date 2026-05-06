package screenshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// fakeCapturer captures on-demand; tests configure its return values.
type fakeCapturer struct {
	mu     sync.Mutex
	result CaptureResult
	err    error
	calls  int
}

func (f *fakeCapturer) Capture(_ context.Context, _ CaptureRequest) (CaptureResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return CaptureResult{}, f.err
	}
	return f.result, nil
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

func newSource(t *testing.T, capturer Capturer, cfg Config) *Source {
	t.Helper()
	if cfg.MediaDir == "" {
		cfg.MediaDir = t.TempDir()
	}
	s, err := New(capturer, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func writeFakeImage(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fake-image-bytes"), 0o600); err != nil {
		t.Fatalf("write fake image: %v", err)
	}
	return path
}

func TestNew_RequiresCapturerAndMediaDir(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, Config{MediaDir: "/tmp"}); err == nil {
		t.Error("New(nil capturer): expected error")
	}
	if _, err := New(&fakeCapturer{}, Config{}); err == nil {
		t.Error("New(no MediaDir): expected error")
	}
}

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeCapturer{}, Config{})
	if s.Name() != "screenshot" {
		t.Errorf("Name = %q, want screenshot", s.Name())
	}
}

func TestSource_TriggerEmitsImageEvent(t *testing.T) {
	t.Parallel()
	mediaDir := t.TempDir()
	imgPath := writeFakeImage(t, mediaDir, "shot.png")
	cap := &fakeCapturer{result: CaptureResult{
		Path:           imgPath,
		PerceptualHash: "phash-abc123",
		Width:          1920,
		Height:         1080,
	}}
	s := newSource(t, cap, Config{MediaDir: mediaDir})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	fp, err := s.Trigger(ctx, CaptureRequest{
		Mode:               CaptureActiveWindow,
		Label:              "Q3 dashboard",
		ForegroundBundleID: "com.apple.Safari",
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if fp != "phash-abc123" {
		t.Errorf("returned fingerprint = %q, want phash-abc123", fp)
	}

	select {
	case ev := <-s.Events():
		if ev.Kind != ambient.KindImage {
			t.Errorf("Kind = %q, want KindImage", ev.Kind)
		}
		if string(ev.Payload) != imgPath {
			t.Errorf("Payload = %q, want %q", ev.Payload, imgPath)
		}
		if ev.Fingerprint != "phash-abc123" {
			t.Errorf("Fingerprint = %q, want phash-abc123", ev.Fingerprint)
		}
		if ev.SuggestedPipeline != "image.ocr" {
			t.Errorf("SuggestedPipeline = %q, want image.ocr", ev.SuggestedPipeline)
		}
		if ev.Metadata["label"] != "Q3 dashboard" {
			t.Errorf("Metadata[label] = %v, want Q3 dashboard", ev.Metadata["label"])
		}
		if ev.Metadata["width"] != 1920 {
			t.Errorf("Metadata[width] = %v, want 1920", ev.Metadata["width"])
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event after Trigger")
	}
}

func TestSource_TriggerVetoesExcludedBundleBeforeCapture(t *testing.T) {
	t.Parallel()
	cap := &fakeCapturer{result: CaptureResult{Path: "/tmp/never.png"}}
	pub := &capturingPub{}
	s := newSource(t, cap, Config{
		MediaDir:       t.TempDir(),
		ExcludeBundles: []string{"com.1password.*"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	fp, err := s.Trigger(ctx, CaptureRequest{
		Mode:               CaptureActiveWindow,
		ForegroundBundleID: "com.1password.macos",
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if fp != "" {
		t.Errorf("vetoed Trigger should return empty fingerprint; got %q", fp)
	}
	if cap.calls != 0 {
		t.Errorf("Capturer was invoked %d times; expected 0 (deny BEFORE capture)", cap.calls)
	}
	if !pub.HasTopic("ctxt.ambient.event.filtered") {
		t.Error("expected ctxt.ambient.event.filtered for vetoed bundle")
	}
	select {
	case ev := <-s.Events():
		t.Errorf("unexpected event after veto: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSource_TriggerSurfacesCapturerError(t *testing.T) {
	t.Parallel()
	cap := &fakeCapturer{err: errors.New("permission denied")}
	pub := &capturingPub{}
	s := newSource(t, cap, Config{MediaDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	_, err := s.Trigger(ctx, CaptureRequest{Mode: CaptureFullScreen})
	if err == nil {
		t.Fatal("Trigger with capturer error: expected error")
	}
	if !pub.HasTopic("ctxt.ambient.source.failed") {
		t.Error("expected ctxt.ambient.source.failed on Capturer error")
	}
}

func TestSource_FallbackFingerprintWhenCapturerOmitsPHash(t *testing.T) {
	t.Parallel()
	mediaDir := t.TempDir()
	imgPath := writeFakeImage(t, mediaDir, "shot.png")
	cap := &fakeCapturer{result: CaptureResult{Path: imgPath /* no PerceptualHash */}}
	s := newSource(t, cap, Config{MediaDir: mediaDir})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	fp, err := s.Trigger(ctx, CaptureRequest{Mode: CaptureFullScreen})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if fp == "" {
		t.Error("expected fallback fingerprint when Capturer omits perceptual hash")
	}
	if len(fp) != 64 {
		t.Errorf("fallback fingerprint length = %d, want 64 (sha256 hex)", len(fp))
	}
}

func TestMatchBundle_PatternMatching(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, bundle string
		want            bool
	}{
		{"com.example.app", "com.example.app", true},
		{"com.1password.*", "com.1password.macos", true},
		{"com.1password.*", "com.example.app", false},
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

func TestSource_StartTwiceRejected(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeCapturer{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	if err := s.Start(ctx, nil); err == nil {
		t.Error("second Start: expected error")
	}
}

func TestSource_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newSource(t, &fakeCapturer{}, Config{})
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

func TestHashFile_DeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := writeFakeImage(t, dir, "a.png")
	b := writeFakeImage(t, dir, "b.png")
	// Make b's bytes different.
	if err := os.WriteFile(b, []byte("different bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hashA, err := hashFile(a)
	if err != nil {
		t.Fatalf("hashFile(a): %v", err)
	}
	hashB, err := hashFile(b)
	if err != nil {
		t.Fatalf("hashFile(b): %v", err)
	}
	if hashA == hashB {
		t.Error("distinct files must hash differently")
	}
	hashA2, _ := hashFile(a)
	if hashA != hashA2 {
		t.Error("hashFile must be deterministic")
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
