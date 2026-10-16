package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Manager owns all active directory-watch goroutines.
//
// It has two users. ctxt's `watch start` runs one over its config's
// directories, with FileRecords in a local state file per dpkms instance
// and an Ingester over HTTP; it has no ConfigStore. dpkms serve runs one
// over its server-side watches (/api/v1/watches), with the configs and
// file records in its own database and the service as the Ingester. The
// two never share records.
type Manager struct {
	configs  ConfigStore
	records  FileRecords
	ingester Ingester

	initialScan bool
	scanDone    func(watchID string)
	// onFSWatcher, when set, gets each fsnotify watcher a watch loop
	// opens; tests use it to feed the watcher's Errors channel.
	onFSWatcher func(*fsnotify.Watcher)

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// serial holds one lock per watch: its files are enqueued and
	// deleted one at a time, so a scan and an event for the same file
	// never both enqueue it.
	serial  map[string]*sync.Mutex
	stopped bool
	running sync.WaitGroup
}

// Option configures a Manager.
type Option func(*Manager)

// WithInitialScan makes every watch walk its directory when it starts
// and hand off each matching file whose content has no record yet: the
// changes made while nothing watched. ctxt uses it; dpkms's server-side
// watches do not.
func WithInitialScan() Option { return func(m *Manager) { m.initialScan = true } }

// WithScanDone calls fn with the watch ID when a watch's initial scan has
// handed off every file it found.
func WithScanDone(fn func(watchID string)) Option { return func(m *Manager) { m.scanDone = fn } }

// NewManager creates a Manager. configs may be nil: watches then live
// only as long as the process. records may be nil (no dedup), for tests.
func NewManager(configs ConfigStore, records FileRecords, ingester Ingester, opts ...Option) *Manager {
	m := &Manager{
		configs:  configs,
		records:  records,
		ingester: ingester,
		cancels:  make(map[string]context.CancelFunc),
		serial:   make(map[string]*sync.Mutex),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start loads all active watches from the config store and begins
// watching. Blocks until ctx is cancelled, then stops all goroutines.
func (m *Manager) Start(ctx context.Context) error {
	if m.configs == nil {
		<-ctx.Done()
		return nil
	}
	watches, err := m.configs.ListWatches(ctx, "active")
	if err != nil {
		return err
	}
	for _, cfg := range watches {
		m.startWatch(ctx, cfg)
	}
	<-ctx.Done()
	m.Stop()
	return nil
}

// Stop cancels all active watch goroutines and waits for them, and for
// any hand-off in flight, to finish. The Manager starts nothing after.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.stopped = true
	for id, cancel := range m.cancels {
		cancel()
		delete(m.cancels, id)
	}
	m.mu.Unlock()
	m.running.Wait()
}

// AddWatch persists a new watch config, when there is a config store,
// and starts watching immediately. A config already in the store is kept
// and watched anyway.
func (m *Manager) AddWatch(ctx context.Context, cfg *WatchConfig) error {
	if m.configs != nil {
		// Ignore duplicate-key errors: the watch may have been pre-created.
		_ = m.configs.CreateWatch(ctx, cfg)
	}
	m.startWatch(ctx, cfg)
	return nil
}

// RemoveWatch stops watching and deletes the config.
func (m *Manager) RemoveWatch(ctx context.Context, id string) error {
	m.cancelWatch(id)
	if m.configs != nil {
		return m.configs.DeleteWatch(ctx, id)
	}
	return nil
}

// PauseWatch stops the goroutine and updates status.
func (m *Manager) PauseWatch(ctx context.Context, id string) error {
	m.cancelWatch(id)
	if m.configs == nil {
		return nil
	}
	cfg, err := m.configs.GetWatch(ctx, id)
	if err != nil {
		return err
	}
	cfg.Status = "paused"
	cfg.UpdatedAt = time.Now()
	return m.configs.UpdateWatch(ctx, cfg)
}

// ResumeWatch restarts a paused watch.
func (m *Manager) ResumeWatch(ctx context.Context, id string) error {
	if m.configs == nil {
		return nil
	}
	cfg, err := m.configs.GetWatch(ctx, id)
	if err != nil {
		return err
	}
	cfg.Status = "active"
	cfg.UpdatedAt = time.Now()
	if err := m.configs.UpdateWatch(ctx, cfg); err != nil {
		return err
	}
	m.startWatch(ctx, cfg)
	return nil
}

func (m *Manager) cancelWatch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancels[id]; ok {
		cancel()
		delete(m.cancels, id)
	}
}

// track registers one unit of work that Stop waits for. It reports false
// once the Manager is stopped; the caller then does nothing.
func (m *Manager) track() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return false
	}
	m.running.Add(1)
	return true
}

func (m *Manager) startWatch(ctx context.Context, cfg *WatchConfig) {
	if !m.track() {
		return
	}
	watchCtx, cancel := context.WithCancel(ctx) // #nosec G118 -- cancel is stored in m.cancels and invoked by cancelWatch/Stop
	m.mu.Lock()
	if prev, ok := m.cancels[cfg.ID]; ok {
		prev()
	}
	m.cancels[cfg.ID] = cancel
	if m.serial[cfg.ID] == nil {
		m.serial[cfg.ID] = &sync.Mutex{}
	}
	m.mu.Unlock()
	go func() {
		defer m.running.Done()
		m.watchLoop(watchCtx, cfg)
	}()
}

// matches reports whether a file at relPath (slash-separated, relative
// to the watched directory) is in the watch.
func matches(cfg *WatchConfig, relPath string) bool {
	if !MatchAny(cfg.IncludePatterns, relPath) {
		return false
	}
	return len(cfg.ExcludePatterns) == 0 || !MatchAny(cfg.ExcludePatterns, relPath)
}

func relTo(cfg *WatchConfig, absPath string) string {
	return filepath.ToSlash(strings.TrimPrefix(absPath, cfg.Path+string(os.PathSeparator)))
}

func (m *Manager) watchLoop(ctx context.Context, cfg *WatchConfig) {
	// Check if polling is forced via env var.
	if os.Getenv("CTXT_WATCH_POLLING") == "1" {
		m.pollLoop(ctx, cfg)
		return
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		// Fall back to polling on init failure.
		m.pollLoop(ctx, cfg)
		return
	}
	// The loop is done with the watcher; a close error leaves nothing to undo.
	defer func() { _ = fsw.Close() }()
	if m.onFSWatcher != nil {
		m.onFSWatcher(fsw)
	}

	watchTree(fsw, cfg, cfg.Path, nil)

	// The initial scan runs once the tree is registered, so a file
	// written during the scan is caught by either the scan or its event;
	// the per-watch lock and the records keep it to one hand-off.
	if m.initialScan && m.track() {
		go func() {
			defer m.running.Done()
			m.scan(ctx, cfg)
		}()
	}

	debounceDelay := time.Duration(cfg.DebounceMS) * time.Millisecond
	if debounceDelay <= 0 {
		debounceDelay = 500 * time.Millisecond
	}
	debounce := newDebouncer(debounceDelay, func(absPath string) { m.handOff(ctx, cfg, absPath) })
	defer debounce.stop()
	schedule := debounce.schedule

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-fsw.Events:
			if !ok {
				return
			}
			absPath := event.Name

			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				// New directory: add it to the watcher and walk it.
				// Walking covers the race window between MkdirAll and any
				// child file Create events fired before fsw.Add lands —
				// fsnotify only delivers events for paths registered at
				// the time the kernel emits them.
				if fi, err := os.Stat(absPath); err == nil && fi.IsDir() {
					watchTree(fsw, cfg, absPath, schedule)
					continue
				}
				if matches(cfg, relTo(cfg, absPath)) {
					schedule(absPath)
				}

			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				debounce.cancel(absPath)
				m.remove(ctx, cfg, absPath)
			}

		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			// The loop keeps running: an overflow drops events, and a
			// file whose event was dropped is picked up on its next change.
			slog.Warn("watcher: file event error", "watch", cfg.ID, "err", err)
		}
	}
}

// debouncer delays each path's hand-off until no event has touched it
// for delay. It is used from the watch loop's goroutine only; fire runs
// on the timer's.
type debouncer struct {
	delay  time.Duration
	fire   func(absPath string)
	timers map[string]*time.Timer
}

func newDebouncer(delay time.Duration, fire func(absPath string)) *debouncer {
	return &debouncer{delay: delay, fire: fire, timers: make(map[string]*time.Timer)}
}

// schedule (re)starts absPath's timer.
func (d *debouncer) schedule(absPath string) {
	if t, ok := d.timers[absPath]; ok {
		t.Stop()
	}
	d.timers[absPath] = time.AfterFunc(d.delay, func() { d.fire(absPath) })
}

// cancel drops absPath's pending hand-off, if any.
func (d *debouncer) cancel(absPath string) {
	if t, ok := d.timers[absPath]; ok {
		t.Stop()
		delete(d.timers, absPath)
	}
}

// stop drops every pending hand-off.
func (d *debouncer) stop() {
	for _, t := range d.timers {
		t.Stop()
	}
}

// watchTree adds root and every directory beneath it to fsw. With a
// non-nil schedule it also schedules each matching file found, which
// covers files created in a new directory before it was watched.
//
// A per-entry error (unreadable dir, race with a concurrent delete) skips
// that entry only: returning it would abort the whole walk and leave the
// remaining subtree unwatched, which is strictly worse than missing one dir.
func watchTree(fsw *fsnotify.Watcher, cfg *WatchConfig, root string, schedule func(absPath string)) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entry; aborting the walk would leave the subtree unwatched
		}
		if d.IsDir() {
			_ = fsw.Add(p)
			return nil
		}
		if schedule != nil && matches(cfg, relTo(cfg, p)) {
			schedule(p)
		}
		return nil
	})
}

// scan hands off every matching file under the watch whose content has
// no record yet.
func (m *Manager) scan(ctx context.Context, cfg *WatchConfig) {
	_ = filepath.WalkDir(cfg.Path, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entry; the next change or start retries it
		}
		if matches(cfg, relTo(cfg, path)) {
			m.processFile(ctx, cfg, path)
		}
		return nil
	})
	if ctx.Err() == nil && m.scanDone != nil {
		m.scanDone(cfg.ID)
	}
}

func (m *Manager) pollLoop(ctx context.Context, cfg *WatchConfig) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	seen := map[string]struct{}{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := map[string]struct{}{}
			_ = filepath.WalkDir(cfg.Path, func(path string, d fs.DirEntry, err error) error {
				// Poll tick: skip entries we cannot stat this round rather
				// than aborting the sweep — the next tick retries them.
				if err != nil || d.IsDir() {
					return nil //nolint:nilerr // skip unreadable entry; next poll tick retries
				}
				if !matches(cfg, relTo(cfg, path)) {
					return nil
				}
				current[path] = struct{}{}
				m.processFile(ctx, cfg, path)
				return nil
			})
			// Detect deletions.
			for path := range seen {
				if _, ok := current[path]; !ok {
					m.remove(ctx, cfg, path)
				}
			}
			seen = current
		}
	}
}

// handOff runs processFile from a debounce timer, as tracked work.
func (m *Manager) handOff(ctx context.Context, cfg *WatchConfig, absPath string) {
	if !m.track() {
		return
	}
	defer m.running.Done()
	m.processFile(ctx, cfg, absPath)
}

// remove runs deleteFile as tracked work.
func (m *Manager) remove(ctx context.Context, cfg *WatchConfig, absPath string) {
	if !m.track() {
		return
	}
	defer m.running.Done()
	m.deleteFile(ctx, cfg, absPath)
}

// lockWatch serializes the hand-offs of one watch.
func (m *Manager) lockWatch(id string) func() {
	m.mu.Lock()
	l := m.serial[id]
	if l == nil {
		l = &sync.Mutex{}
		m.serial[id] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// processFile enqueues absPath unless its content was already handed
// off. Only a successful enqueue is recorded: a failed one is retried by
// the file's next change, or by the next start's initial scan.
func (m *Manager) processFile(ctx context.Context, cfg *WatchConfig, absPath string) {
	if ctx.Err() != nil || m.ingester == nil {
		return
	}
	unlock := m.lockWatch(cfg.ID)
	defer unlock()

	data, err := os.ReadFile(absPath) // #nosec G304 -- a file under a directory the operator chose to watch
	if err != nil {
		return
	}
	newHash := sha256hex(data)

	if m.records != nil {
		existing, ok, err := m.records.GetFile(ctx, cfg.ID, absPath)
		if err != nil {
			m.fail(ctx, cfg, fmt.Errorf("read record for %s: %w", relTo(cfg, absPath), err))
			return
		}
		if ok && existing.ContentHash == newHash {
			return // no change
		}
	}

	req := Request{
		Content:  string(data),
		Type:     detectType(absPath),
		Source:   "watch:" + cfg.Mode + ":" + relTo(cfg, absPath),
		Pipeline: cfg.PipelineOverride,
	}
	if cfg.Mode == ModeObsidian || cfg.Mode == ModeLogseq {
		if parsed, perr := ParseFile(absPath, cfg.Mode, cfg.Path); perr == nil {
			parsed.Pipeline = cfg.PipelineOverride
			req = parsed
		}
	}

	jobID, err := m.ingester.Analyze(ctx, req)
	if err != nil {
		m.fail(ctx, cfg, fmt.Errorf("enqueue %s: %w", relTo(cfg, absPath), err))
		return
	}
	if m.records == nil {
		return
	}
	rec := FileRecord{Path: absPath, ContentHash: newHash, JobID: jobID, LastSeen: time.Now().UTC()}
	if err := m.records.PutFile(ctx, cfg.ID, rec); err != nil {
		m.fail(ctx, cfg, fmt.Errorf("record %s (job %s): %w", relTo(cfg, absPath), jobID, err))
	}
}

// deleteFile deletes the object a removed file's last hand-off produced.
// The record holds the job ID; the object is the job's result. A job
// with no result yet (still pending, or failed) deletes nothing and says
// so in the watch's last error. The record is dropped either way: the
// file is gone.
func (m *Manager) deleteFile(ctx context.Context, cfg *WatchConfig, absPath string) {
	if m.records == nil || ctx.Err() != nil {
		return
	}
	unlock := m.lockWatch(cfg.ID)
	defer unlock()

	rec, ok, err := m.records.GetFile(ctx, cfg.ID, absPath)
	if err != nil || !ok {
		return
	}
	rel := relTo(cfg, absPath)
	defer func() {
		if err := m.records.DeleteFile(ctx, cfg.ID, absPath); err != nil {
			m.fail(ctx, cfg, fmt.Errorf("forget %s: %w", rel, err))
		}
	}()
	if m.ingester == nil {
		return
	}

	objectID := rec.ObjectID
	if objectID == "" && rec.JobID != "" {
		objectID, err = m.ingester.JobResult(ctx, rec.JobID)
		if err != nil {
			m.fail(ctx, cfg, fmt.Errorf("%s deleted: look up job %s: %w; its object was not deleted", rel, rec.JobID, err))
			return
		}
	}
	if objectID == "" {
		m.fail(ctx, cfg, fmt.Errorf("%s deleted before job %s produced an object; nothing was deleted", rel, rec.JobID))
		return
	}
	if err := m.ingester.DeleteObject(ctx, objectID); err != nil {
		m.fail(ctx, cfg, fmt.Errorf("%s deleted: delete object %s: %w", rel, objectID, err))
	}
}

// fail records err as the watch's last error. A cancellation is the
// watch stopping, not a failure.
func (m *Manager) fail(ctx context.Context, cfg *WatchConfig, err error) {
	if m.records == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	_ = m.records.SetLastError(context.WithoutCancel(ctx), cfg.ID, err.Error())
}

// detectType infers content type from file extension.
func detectType(absPath string) string {
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".md", ".markdown":
		return contentTypeText
	case ".pdf":
		return "pdf"
	case ".html", ".htm":
		return "url"
	default:
		return contentTypeText
	}
}

// sha256hex returns the hex-encoded SHA-256 hash of data.
func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// DetectMode infers the watch mode from the directory structure.
func DetectMode(path string) string {
	if fi, err := os.Stat(filepath.Join(path, ".obsidian")); err == nil && fi.IsDir() {
		return ModeObsidian
	}
	if fi, err := os.Stat(filepath.Join(path, "logseq")); err == nil && fi.IsDir() {
		return ModeLogseq
	}
	return ModeGeneric
}
