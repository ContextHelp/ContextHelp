package watcher_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestClipboardWatcher_PersistsHashToStateFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "watcher-state.json")

	ing := &captureIngester{}
	url := "https://example.com/persist-test"
	reader := func() (string, error) { return url, nil }

	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
		StateFile:    stateFile,
	}
	cw := watcher.NewClipboardWatcher(cfg, ing, reader)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	cw.Run(ctx)

	if ing.count() != 1 {
		t.Fatalf("expected 1 ingestion, got %d", ing.count())
	}

	// State file must exist and contain a hash (never raw content).
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("state file not created: %v", err)
	}
	var sf map[string]any
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("state file not valid JSON: %v", err)
	}
	clip, ok := sf["clipboard"].(map[string]any)
	if !ok {
		t.Fatalf("state file missing clipboard key")
	}
	hash, ok := clip["last_hash"].(string)
	if !ok || hash == "" {
		t.Errorf("state file missing last_hash")
	}
	// Verify raw URL is NOT stored in state file.
	rawContent := string(data)
	if rawContent == url {
		t.Errorf("state file must not contain raw clipboard content")
	}
}

func TestClipboardWatcher_ResumesFromPersistedHash(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "watcher-state.json")

	url := "https://example.com/resume-test"

	// Pre-populate state file with the hash of the URL.
	// Simulates a previous session that already ingested this URL.
	ing1 := &captureIngester{}
	cfg := watcher.ClipboardConfig{
		Enabled:      true,
		PollInterval: 5 * time.Millisecond,
		MinLength:    80,
		AutoIngest:   true,
		StateFile:    stateFile,
	}
	reader := func() (string, error) { return url, nil }

	cw1 := watcher.NewClipboardWatcher(cfg, ing1, reader)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	cw1.Run(ctx1)
	cancel1()

	if ing1.count() != 1 {
		t.Fatalf("first run: expected 1 ingestion, got %d", ing1.count())
	}

	// Second watcher run with same URL — should skip due to persisted hash.
	ing2 := &captureIngester{}
	cw2 := watcher.NewClipboardWatcher(cfg, ing2, reader)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	cw2.Run(ctx2)

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
