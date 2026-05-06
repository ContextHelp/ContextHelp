package filewatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// drainEventOrFail reads one event from src.Events() or fails the test.
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

type capturingPublisher struct {
	mu     sync.Mutex
	events []map[string]any
}

func (p *capturingPublisher) Publish(_ context.Context, topic, _ string, payload any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec := map[string]any{"topic": topic, "payload": payload}
	p.events = append(p.events, rec)
	return nil
}

func (p *capturingPublisher) HasTopic(topic string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.events {
		if e["topic"] == topic {
			return true
		}
	}
	return false
}

func newSource(t *testing.T, dir string, opts ...func(*Config)) *Source {
	t.Helper()
	cfg := Config{
		Directories:    []string{dir},
		SettleDuration: 5 * time.Millisecond, // fast for tests
		MaxFileSizeMB:  10,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNew_RejectsEmptyDirectories(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no directories: expected error")
	}
}

func TestSource_NameIsConstant(t *testing.T) {
	t.Parallel()
	s := newSource(t, t.TempDir())
	if s.Name() != "filewatch" {
		t.Errorf("Name = %q, want filewatch", s.Name())
	}
}

func TestSource_EmitsEventForExistingFileOnStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pre-existing file at startup → initialScan should emit.
	path := filepath.Join(dir, "existing.md")
	mustWriteFile(t, path, "# This is some content that's long enough to be picked up.")

	s := newSource(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if string(ev.Payload) != path {
		t.Errorf("Payload = %q, want %q", ev.Payload, path)
	}
	if ev.SuggestedPipeline != "text.long" {
		t.Errorf("SuggestedPipeline = %q, want text.long", ev.SuggestedPipeline)
	}
	if ev.Kind != ambient.KindText {
		t.Errorf("Kind = %q, want %q", ev.Kind, ambient.KindText)
	}
	if ev.Fingerprint == "" {
		t.Error("Fingerprint must be populated")
	}
}

func TestSource_EmitsEventForNewFileAfterCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := newSource(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Drop a file after watcher is running.
	time.Sleep(20 * time.Millisecond) // let watcher subscribe
	path := filepath.Join(dir, "new.md")
	mustWriteFile(t, path, "# Some content")

	ev := drainEventOrFail(t, s, 2*time.Second)
	if !strings.HasSuffix(string(ev.Payload), "new.md") {
		t.Errorf("Payload = %q, want path ending new.md", ev.Payload)
	}
}

func TestSource_RoutesByExtension(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ext      string
		pipeline string
		kind     ambient.Kind
	}{
		{".md", "text.long", ambient.KindText},
		{".txt", "text.long", ambient.KindText},
		{".pdf", "text.long", ambient.KindFile},
		{".png", "image.ocr", ambient.KindImage},
		{".jpg", "image.ocr", ambient.KindImage},
		{".heic", "image.ocr", ambient.KindImage},
		{".mp3", "audio.transcribe", ambient.KindFile},
		{".wav", "audio.transcribe", ambient.KindFile},
		{".mp4", "video.full", ambient.KindFile},
		{".mov", "video.full", ambient.KindFile},
		{".unknown", "text.long", ambient.KindFile},
	}
	for _, c := range cases {
		c := c
		t.Run(c.ext, func(t *testing.T) {
			t.Parallel()
			pipeline, kind := routeByExtension("test/file" + c.ext)
			if pipeline != c.pipeline {
				t.Errorf("pipeline = %q, want %q", pipeline, c.pipeline)
			}
			if kind != c.kind {
				t.Errorf("kind = %q, want %q", kind, c.kind)
			}
		})
	}
}

func TestSource_IgnoresHiddenAndSystemFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".DS_Store"), "x")
	mustWriteFile(t, filepath.Join(dir, "Thumbs.db"), "x")
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "x")
	// And a real one to confirm watcher fires.
	mustWriteFile(t, filepath.Join(dir, "real.md"), "this is some real content with enough length")

	s := newSource(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, nil)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	ev := drainEventOrFail(t, s, time.Second)
	if filepath.Base(string(ev.Payload)) != "real.md" {
		t.Errorf("expected only real.md to emit; got %q", ev.Payload)
	}
}

func TestSource_RejectsFilesOverSizeLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pub := &capturingPublisher{}
	// 2-byte cap; any file exceeds.
	s := newSource(t, dir, func(c *Config) { c.MaxFileSizeMB = 0; c.MaxFileSizeMB = 1 })
	// Create a file > 1 MB.
	bigPath := filepath.Join(dir, "big.md")
	bigContent := strings.Repeat("x", 2*1024*1024)
	mustWriteFile(t, bigPath, bigContent)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// No event should arrive within a short window; failed bus event should fire.
	select {
	case ev := <-s.Events():
		t.Errorf("expected no event for over-size file; got %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: skip
	}
	if !pub.HasTopic("ctxt.ambient.source.failed") {
		t.Error("expected ctxt.ambient.source.failed for over-size file")
	}
}

func TestSource_PublishesReadyOnStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pub := &capturingPublisher{}
	s := newSource(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = s.Start(ctx, pub)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// give async lifecycle a moment
	time.Sleep(10 * time.Millisecond)
	if !pub.HasTopic("ctxt.ambient.source.ready") {
		t.Error("expected ctxt.ambient.source.ready on Start")
	}
}

func TestSource_StartTwiceRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := newSource(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if err := s.Start(ctx, nil); err == nil {
		t.Error("second Start: expected error")
	}
}

func TestSource_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := newSource(t, dir)
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

func TestSource_MoveToProcessedRespectsConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := newSource(t, dir, func(c *Config) { c.MoveAfterEnqueue = true })

	path := filepath.Join(dir, "moveme.md")
	mustWriteFile(t, path, "content")

	dst, err := s.MoveToProcessed(path)
	if err != nil {
		t.Fatalf("MoveToProcessed: %v", err)
	}
	if !strings.Contains(dst, "processed") {
		t.Errorf("dst = %q, expected to contain 'processed'", dst)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("original path still exists after move: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("dst missing after move: %v", err)
	}
}

func TestSource_MoveToProcessedNoOpWhenDisabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := newSource(t, dir) // MoveAfterEnqueue defaults to false (zero value)
	path := filepath.Join(dir, "leaveme.md")
	mustWriteFile(t, path, "content")

	dst, err := s.MoveToProcessed(path)
	if err != nil {
		t.Fatalf("MoveToProcessed: %v", err)
	}
	if dst != path {
		t.Errorf("when MoveAfterEnqueue=false, dst should equal input; got %q", dst)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("original missing: %v", err)
	}
}

func TestFingerprint_DistinctFilesHaveDistinctFingerprints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	mustWriteFile(t, a, "hello a")
	mustWriteFile(t, b, "hello b")

	fpA, err := fingerprintFile(a)
	if err != nil {
		t.Fatalf("fingerprintFile(a): %v", err)
	}
	fpB, err := fingerprintFile(b)
	if err != nil {
		t.Fatalf("fingerprintFile(b): %v", err)
	}
	if fpA == fpB {
		t.Error("distinct files must have distinct fingerprints")
	}
}

func TestFingerprint_SameContentSameFingerprint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	mustWriteFile(t, a, "same content")
	mustWriteFile(t, b, "same content")

	fpA, _ := fingerprintFile(a)
	fpB, _ := fingerprintFile(b)
	if fpA != fpB {
		t.Error("identical content must yield identical fingerprints")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// Compile-time check.
var _ ambient.Source = (*Source)(nil)
