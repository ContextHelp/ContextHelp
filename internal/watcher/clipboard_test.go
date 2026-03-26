package watcher_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// captureIngester records all calls to Analyze.
type captureIngester struct {
	mu  sync.Mutex
	got []service.AnalyzeRequest
}

func (c *captureIngester) Analyze(_ context.Context, req service.AnalyzeRequest) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, req)
	return "job-" + req.Source, nil
}

func (c *captureIngester) DeleteObject(_ context.Context, _ string) error { return nil }

func (c *captureIngester) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func (c *captureIngester) latest() service.AnalyzeRequest {
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
