package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

type mockIngester struct {
	mu       sync.Mutex
	analyzed []service.AnalyzeRequest
	deleted  []string
}

func (m *mockIngester) Analyze(ctx context.Context, req service.AnalyzeRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.analyzed = append(m.analyzed, req)
	return "job-" + req.Source, nil
}

func (m *mockIngester) DeleteObject(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, id)
	return nil
}

func TestManager_NewAndStop(t *testing.T) {
	mi := &mockIngester{}
	m := watcher.NewManager(nil, mi)
	m.Stop()
}

func TestWatchLoop_CreateFile_CallsAnalyze(t *testing.T) {
	dir := t.TempDir()
	driver := storageutil.NewTestDriver(t)
	mi := &mockIngester{}
	m := watcher.NewManager(driver.Watches(), mi)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &storage.WatchConfig{
		ID: "w1", Path: dir, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		ExcludePatterns: []string{},
		DebounceMS: 100, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	driver.Watches().CreateWatch(ctx, cfg)
	m.AddWatch(ctx, cfg)

	// Give fsnotify time to initialize.
	time.Sleep(200 * time.Millisecond)

	// Write a matching file.
	os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Hello"), 0644)

	// Wait for Analyze to be called.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mi.mu.Lock()
		n := len(mi.analyzed)
		mi.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mi.mu.Lock()
	defer mi.mu.Unlock()
	if len(mi.analyzed) == 0 {
		t.Fatal("expected Analyze to be called for note.md")
	}
}

func TestWatchLoop_ExcludedFile_NoAnalyze(t *testing.T) {
	dir := t.TempDir()
	driver := storageutil.NewTestDriver(t)
	mi := &mockIngester{}
	m := watcher.NewManager(driver.Watches(), mi)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := &storage.WatchConfig{
		ID: "w1", Path: dir, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		ExcludePatterns: []string{"**/.git/**"},
		DebounceMS: 100, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	driver.Watches().CreateWatch(ctx, cfg)
	m.AddWatch(ctx, cfg)

	gitDir := filepath.Join(dir, ".git")
	os.Mkdir(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0644)

	time.Sleep(600 * time.Millisecond)
	mi.mu.Lock()
	defer mi.mu.Unlock()
	if len(mi.analyzed) != 0 {
		t.Errorf("expected no Analyze calls for excluded .git file, got %d", len(mi.analyzed))
	}
}

func TestWatchLoop_DeleteFile_CallsDeleteObject(t *testing.T) {
	dir := t.TempDir()
	driver := storageutil.NewTestDriver(t)
	mi := &mockIngester{}
	m := watcher.NewManager(driver.Watches(), mi)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &storage.WatchConfig{
		ID: "w1", Path: dir, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		ExcludePatterns: []string{},
		DebounceMS: 100, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	driver.Watches().CreateWatch(ctx, cfg)

	// Pre-seed a file record so the watcher knows the object ID to delete.
	// ContentHash matches "# Hello" so processFile skips it (no re-ingest).
	notePath := filepath.Join(dir, "note.md")
	driver.Watches().UpsertFileRecord(ctx, &storage.WatchFileRecord{
		WatchID: "w1", FilePath: notePath,
		ObjectID: "obj-123", ContentHash: "01c8de44e04d2f7a304f50963545a2aff58c33e9c44a1f33fdcb978fb224cb74",
		LastSeen: time.Now(),
	})

	m.AddWatch(ctx, cfg)

	// Give fsnotify time to initialize.
	time.Sleep(200 * time.Millisecond)

	os.WriteFile(notePath, []byte("# Hello"), 0644)
	time.Sleep(400 * time.Millisecond)
	os.Remove(notePath)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mi.mu.Lock()
		n := len(mi.deleted)
		mi.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mi.mu.Lock()
	defer mi.mu.Unlock()
	if len(mi.deleted) == 0 {
		t.Fatal("expected DeleteObject to be called")
	}
	if mi.deleted[0] != "obj-123" {
		t.Errorf("deleted ID: got %q, want obj-123", mi.deleted[0])
	}
}

func TestDetectMode_Obsidian(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".obsidian"), 0755)
	if watcher.DetectMode(dir) != "obsidian" {
		t.Error("expected obsidian for dir with .obsidian/")
	}
}

func TestDetectMode_Logseq(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "logseq"), 0755)
	if watcher.DetectMode(dir) != "logseq" {
		t.Error("expected logseq for dir with logseq/")
	}
}

func TestDetectMode_Generic(t *testing.T) {
	dir := t.TempDir()
	if watcher.DetectMode(dir) != "generic" {
		t.Error("expected generic for plain dir")
	}
}

func TestPollLoop_PicksUpNewFile(t *testing.T) {
	if os.Getenv("CI") == "" {
		t.Skip("slow polling test; set CI=1 to run")
	}
	dir := t.TempDir()
	driver := storageutil.NewTestDriver(t)
	mi := &mockIngester{}

	t.Setenv("CTXT_WATCH_POLLING", "1") // force polling path

	m := watcher.NewManager(driver.Watches(), mi)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := &storage.WatchConfig{
		ID: "wpoll", Path: dir, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		DebounceMS: 100, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	driver.Watches().CreateWatch(ctx, cfg)
	m.AddWatch(ctx, cfg)

	time.Sleep(1 * time.Second)
	os.WriteFile(filepath.Join(dir, "new.md"), []byte("content"), 0644)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mi.mu.Lock()
		n := len(mi.analyzed)
		mi.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	mi.mu.Lock()
	defer mi.mu.Unlock()
	if len(mi.analyzed) == 0 {
		t.Fatal("polling loop did not ingest new.md within 10s")
	}
}
