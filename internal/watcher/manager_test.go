package watcher_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// fakeIngester records enqueues and deletes. Each Analyze returns a new
// job ID; results maps job IDs to the object they produced.
type fakeIngester struct {
	mu       sync.Mutex
	analyzed []watcher.Request
	deleted  []string
	results  map[string]string
	fail     error
}

func (f *fakeIngester) Analyze(_ context.Context, req watcher.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.analyzed = append(f.analyzed, req)
	return fmt.Sprintf("job-%d", len(f.analyzed)), nil
}

func (f *fakeIngester) JobResult(_ context.Context, jobID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.results[jobID], nil
}

func (f *fakeIngester) DeleteObject(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeIngester) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func (f *fakeIngester) enqueued() []watcher.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]watcher.Request(nil), f.analyzed...)
}

func (f *fakeIngester) deletedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// memRecords is an in-memory FileRecords.
type memRecords struct {
	mu      sync.Mutex
	files   map[string]watcher.FileRecord
	lastErr map[string]string
}

func newMemRecords() *memRecords {
	return &memRecords{files: map[string]watcher.FileRecord{}, lastErr: map[string]string{}}
}

func (m *memRecords) GetFile(_ context.Context, watchID, path string) (watcher.FileRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[watchID+"|"+path]
	return r, ok, nil
}

func (m *memRecords) PutFile(_ context.Context, watchID string, rec watcher.FileRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[watchID+"|"+rec.Path] = rec
	return nil
}

func (m *memRecords) DeleteFile(_ context.Context, watchID, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, watchID+"|"+path)
	return nil
}

func (m *memRecords) SetLastError(_ context.Context, watchID, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErr[watchID] = msg
	return nil
}

func (m *memRecords) get(watchID, path string) (watcher.FileRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[watchID+"|"+path]
	return r, ok
}

func (m *memRecords) errFor(watchID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr[watchID]
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func dirWatch(dir string) *watcher.WatchConfig {
	return &watcher.WatchConfig{
		ID: dir, Path: dir, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		DebounceMS:      50, Status: "active",
	}
}

// eventually polls cond until it holds or 5s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// settle gives debounce timers and fsnotify time to deliver anything
// still in flight, for assertions that nothing more happens.
func settle() { time.Sleep(400 * time.Millisecond) }

func startManager(t *testing.T, recs watcher.FileRecords, ing watcher.Ingester, cfg *watcher.WatchConfig, opts ...watcher.Option) *watcher.Manager {
	t.Helper()
	m := watcher.NewManager(nil, recs, ing, opts...)
	if err := m.AddWatch(context.Background(), cfg); err != nil {
		t.Fatalf("AddWatch: %v", err)
	}
	t.Cleanup(m.Stop)
	return m
}

func TestManager_NewAndStop(t *testing.T) {
	m := watcher.NewManager(nil, nil, &fakeIngester{})
	m.Stop()
}

// One enqueue per content change: rewriting the same bytes enqueues
// nothing, new bytes enqueue once, and the record keeps the job ID.
func TestManager_OneEnqueuePerContentChange(t *testing.T) {
	dir := t.TempDir()
	recs, ing := newMemRecords(), &fakeIngester{}
	startManager(t, recs, ing, dirWatch(dir), watcher.WithInitialScan())
	settle()

	note := filepath.Join(dir, "note.md")
	if err := os.WriteFile(note, []byte("# one"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "first enqueue", func() bool { return len(ing.enqueued()) == 1 })

	if err := os.WriteFile(note, []byte("# one"), 0o600); err != nil {
		t.Fatal(err)
	}
	settle()
	if n := len(ing.enqueued()); n != 1 {
		t.Fatalf("unchanged rewrite enqueued again: %d enqueues", n)
	}

	if err := os.WriteFile(note, []byte("# two"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "second enqueue", func() bool { return len(ing.enqueued()) == 2 })
	settle()
	if n := len(ing.enqueued()); n != 2 {
		t.Fatalf("one content change gave %d enqueues in total, want 2", n)
	}

	rec, ok := recs.get(dir, note)
	if !ok || rec.ContentHash != hashOf("# two") || rec.JobID == "" {
		t.Fatalf("record = %+v, %v; want hash of the new content and its job", rec, ok)
	}
}

// The initial scan enqueues files that changed while nothing watched,
// and skips the ones already recorded with the same content.
func TestManager_InitialScanSkipsRecordedContent(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same.md")
	changed := filepath.Join(dir, "changed.md")
	fresh := filepath.Join(dir, "sub", "fresh.md")
	for p, body := range map[string]string{same: "same", changed: "changed now", fresh: "fresh"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recs, ing := newMemRecords(), &fakeIngester{}
	ctx := context.Background()
	_ = recs.PutFile(ctx, dir, watcher.FileRecord{Path: same, ContentHash: hashOf("same"), JobID: "job-old"})
	_ = recs.PutFile(ctx, dir, watcher.FileRecord{Path: changed, ContentHash: hashOf("changed before"), JobID: "job-old2"})

	scanned := make(chan string, 1)
	startManager(t, recs, ing, dirWatch(dir), watcher.WithInitialScan(),
		watcher.WithScanDone(func(id string) { scanned <- id }))
	select {
	case <-scanned:
	case <-time.After(5 * time.Second):
		t.Fatal("initial scan never finished")
	}

	got := map[string]bool{}
	for _, r := range ing.enqueued() {
		got[r.Source] = true
	}
	want := map[string]bool{"watch:generic:changed.md": true, "watch:generic:sub/fresh.md": true}
	if len(got) != len(want) || !got["watch:generic:changed.md"] || !got["watch:generic:sub/fresh.md"] {
		t.Fatalf("initial scan enqueued %v; want %v", got, want)
	}
}

// Without WithInitialScan, files already on disk are left alone (dpkms's
// server-side watches keep that behavior).
func TestManager_NoInitialScanByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	ing := &fakeIngester{}
	startManager(t, newMemRecords(), ing, dirWatch(dir))
	settle()
	if n := len(ing.enqueued()); n != 0 {
		t.Fatalf("enqueued %d pre-existing files without an initial scan", n)
	}
}

// Deleting a file deletes the object its job produced, not the job ID.
func TestManager_DeleteFileDeletesResultObject(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	if err := os.WriteFile(note, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs := newMemRecords()
	_ = recs.PutFile(context.Background(), dir, watcher.FileRecord{Path: note, ContentHash: hashOf("body"), JobID: "job-7"})
	ing := &fakeIngester{results: map[string]string{"job-7": "obj-42"}}
	startManager(t, recs, ing, dirWatch(dir))
	settle()

	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	eventually(t, "delete", func() bool { return len(ing.deletedIDs()) > 0 })
	if got := ing.deletedIDs(); len(got) != 1 || got[0] != "obj-42" {
		t.Fatalf("deleted %v; want [obj-42], the job's result", got)
	}
	if _, ok := recs.get(dir, note); ok {
		t.Error("record kept after the file was deleted")
	}
}

// A file deleted before its job produced an object deletes nothing and
// says so in the watch's last error.
func TestManager_DeleteBeforeJobFinishesRecordsError(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	if err := os.WriteFile(note, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs := newMemRecords()
	_ = recs.PutFile(context.Background(), dir, watcher.FileRecord{Path: note, ContentHash: hashOf("body"), JobID: "job-pending"})
	ing := &fakeIngester{}
	startManager(t, recs, ing, dirWatch(dir))
	settle()

	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	eventually(t, "last error", func() bool { return recs.errFor(dir) != "" })
	if got := ing.deletedIDs(); len(got) != 0 {
		t.Fatalf("deleted %v; want nothing while the job has no result", got)
	}
	if _, ok := recs.get(dir, note); ok {
		t.Error("record kept after the file was deleted")
	}
}

// A failed enqueue is not recorded, so the next change (or the next
// start's scan) sends the file again.
func TestManager_FailedEnqueueIsRetriedOnNextChange(t *testing.T) {
	dir := t.TempDir()
	recs, ing := newMemRecords(), &fakeIngester{}
	ing.setFail(errors.New("dpkms unreachable"))
	startManager(t, recs, ing, dirWatch(dir))
	settle()

	note := filepath.Join(dir, "note.md")
	if err := os.WriteFile(note, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "last error", func() bool { return recs.errFor(dir) != "" })
	if _, ok := recs.get(dir, note); ok {
		t.Fatal("a failed enqueue was recorded as handed off")
	}

	ing.setFail(nil)
	if err := os.WriteFile(note, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "retried enqueue", func() bool { return len(ing.enqueued()) == 1 })
}

// TestWatchLoop_NewSubdir_FileIngested: files dropped into a
// subdirectory created after the watcher started are still ingested.
// fsnotify on macOS does not propagate into new subdirectories; the
// watcher must add the subdir and walk it, since the race between
// MkdirAll and Add can swallow the file's own event.
func TestWatchLoop_NewSubdir_FileIngested(t *testing.T) {
	dir := t.TempDir()
	ing := &fakeIngester{}
	startManager(t, newMemRecords(), ing, dirWatch(dir))
	settle()

	sub := filepath.Join(dir, "newsub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "child.md"), []byte("# Child"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "child.md enqueue", func() bool { return len(ing.enqueued()) > 0 })
}

func TestWatchLoop_ExcludedFile_NoAnalyze(t *testing.T) {
	dir := t.TempDir()
	cfg := dirWatch(dir)
	cfg.ExcludePatterns = []string{"**/.git/**"}
	ing := &fakeIngester{}
	startManager(t, newMemRecords(), ing, cfg)

	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD.md"), []byte("ref: refs/heads/main"), 0o600); err != nil {
		t.Fatal(err)
	}
	settle()
	if n := len(ing.enqueued()); n != 0 {
		t.Errorf("expected no Analyze calls for excluded .git file, got %d", n)
	}
}

func TestDetectMode_Obsidian(t *testing.T) {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, ".obsidian"), 0o700)
	if watcher.DetectMode(dir) != "obsidian" {
		t.Error("expected obsidian for dir with .obsidian/")
	}
}

func TestDetectMode_Logseq(t *testing.T) {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "logseq"), 0o700)
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
	if testing.Short() {
		t.Skip("slow polling test")
	}
	dir := t.TempDir()
	t.Setenv("CTXT_WATCH_POLLING", "1") // force polling path
	ing := &fakeIngester{}
	startManager(t, newMemRecords(), ing, dirWatch(dir))

	time.Sleep(time.Second)
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(ing.enqueued()) == 0 {
		time.Sleep(200 * time.Millisecond)
	}
	if len(ing.enqueued()) == 0 {
		t.Fatal("polling loop did not ingest new.md within 10s")
	}
}
