package watcher_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// captureIngester records all calls to Analyze.
type captureIngester struct {
	mu   sync.Mutex
	got  []watcher.Request
	fail error
}

func (c *captureIngester) Analyze(_ context.Context, req watcher.Request) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return "", c.fail
	}
	c.got = append(c.got, req)
	return "job-" + req.Source, nil
}

func (c *captureIngester) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func (c *captureIngester) latest() watcher.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[len(c.got)-1]
}

// TestQualifiesForIngest tests the heuristic filter via ClipboardWatcher.
func TestClipboardWatcher_SkipsShortContent(t *testing.T) {
	ing := &captureIngester{}
	seq := []string{"hi", "hello", "short"}
	idx := 0
	reader := func() (string, error) {
		if idx >= len(seq) {
			return "", nil
		}
		s := seq[idx]
		idx++
		return s, nil
	}

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 0 {
		t.Errorf("expected 0 ingestions for short content, got %d", ing.count())
	}
}

func TestClipboardWatcher_IngestsURL(t *testing.T) {
	ing := &captureIngester{}
	url := "https://example.com/some/article"
	calls := 0
	reader := func() (string, error) {
		calls++
		if calls == 1 {
			return url, nil
		}
		return url, nil // same url, no re-ingest
	}

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 1 {
		t.Errorf("expected exactly 1 ingestion for URL, got %d", ing.count())
	}
	if ing.latest().Type != "url" {
		t.Errorf("expected type=url, got %q", ing.latest().Type)
	}
	if ing.latest().Source != "watcher/clipboard" {
		t.Errorf("expected source=watcher/clipboard, got %q", ing.latest().Source)
	}
}

func TestClipboardWatcher_DeduplicatesContent(t *testing.T) {
	ing := &captureIngester{}
	url := "https://example.com/page"
	reader := func() (string, error) { return url, nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	// Same content repeated — should only ingest once.
	if ing.count() != 1 {
		t.Errorf("dedup failed: expected 1, got %d", ing.count())
	}
}

func TestClipboardWatcher_DisabledByConfig(t *testing.T) {
	ing := &captureIngester{}
	reader := func() (string, error) { return "https://example.com/test", nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      false,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 0 {
		t.Errorf("disabled watcher should not ingest, got %d", ing.count())
	}
}

func TestClipboardWatcher_IngestsLongText(t *testing.T) {
	ing := &captureIngester{}
	longText := "This is a long text that exceeds the minimum length threshold for ingestion by the clipboard watcher."
	reader := func() (string, error) { return longText, nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 1 {
		t.Errorf("expected 1 ingestion for long text, got %d", ing.count())
	}
	if ing.latest().Type != "text" {
		t.Errorf("expected type=text, got %q", ing.latest().Type)
	}
}

func TestClipboardWatcher_IngestsFencedCodeBlock(t *testing.T) {
	ing := &captureIngester{}
	code := "```go\nfmt.Println(\"hello\")\n```"
	reader := func() (string, error) { return code, nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    200, // high threshold, should pass via fenced-code path
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 1 {
		t.Errorf("expected 1 ingestion for fenced code, got %d", ing.count())
	}
}

// memDedup is an in-memory ClipboardDedup.
type memDedup struct {
	mu   sync.Mutex
	hash string
	sets int
}

func (d *memDedup) ClipboardHash() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hash
}

func (d *memDedup) SetClipboardHash(h string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hash, d.sets = h, d.sets+1
	return nil
}

func TestClipboardWatcher_PersistsHashAfterEnqueue(t *testing.T) {
	ing := &captureIngester{}
	url := "https://example.com/persist-test"
	dedup := &memDedup{}
	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
		Dedup:        dedup,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, func() (string, error) { return url, nil })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 1 {
		t.Fatalf("expected 1 ingestion, got %d", ing.count())
	}
	if got := dedup.ClipboardHash(); got == "" || got == url {
		t.Errorf("persisted hash = %q; want the content hash, never the content", got)
	}
}

// A failed enqueue persists nothing, so the next start sends the clip.
func TestClipboardWatcher_FailedEnqueueNotPersisted(t *testing.T) {
	ing := &captureIngester{fail: errors.New("dpkms unreachable")}
	dedup := &memDedup{}
	var errs []error
	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
		Dedup:        dedup,
		OnError:      func(err error) { errs = append(errs, err) },
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, func() (string, error) { return "https://example.com/x", nil })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if dedup.sets != 0 {
		t.Errorf("a failed enqueue persisted the hash")
	}
	if len(errs) != 1 {
		t.Errorf("OnError called %d times; want once for the one clip", len(errs))
	}
}

func TestClipboardWatcher_ResumesFromPersistedHash(t *testing.T) {
	url := "https://example.com/resume-test"
	dedup := &memDedup{}
	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
		Dedup:        dedup,
	}
	reader := func() (string, error) { return url, nil }

	ing1 := &captureIngester{}
	ctx1, cancel1 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	watcher.NewClipboardWatcher(cfg, ing1, reader).Run(ctx1)
	cancel1()
	if ing1.count() != 1 {
		t.Fatalf("first run: expected 1 ingestion, got %d", ing1.count())
	}

	// Second run with the same clip: skipped through the persisted hash.
	ing2 := &captureIngester{}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	watcher.NewClipboardWatcher(cfg, ing2, reader).Run(ctx2)
	if ing2.count() != 0 {
		t.Errorf("second run: expected 0 ingestions (hash persisted), got %d", ing2.count())
	}
}

func TestClipboardWatcher_DisabledByEnvVar(t *testing.T) {
	t.Setenv("CTXT_NO_CLIPBOARD", "1")

	ing := &captureIngester{}
	reader := func() (string, error) { return "https://example.com/env-test", nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 0 {
		t.Errorf("CTXT_NO_CLIPBOARD=1 should disable watcher, got %d ingestions", ing.count())
	}
}
