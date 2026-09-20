package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Ingester is the subset of service.Service used by the Manager.
type Ingester interface {
	Analyze(ctx context.Context, req service.AnalyzeRequest) (string, error)
	DeleteObject(ctx context.Context, id string) error
}

// Manager owns all active watch goroutines.
type Manager struct {
	store    storage.WatchStore
	ingester Ingester
	cancels  map[string]context.CancelFunc
	mu       sync.Mutex
}

// NewManager creates a Manager. store may be nil for unit tests.
func NewManager(store storage.WatchStore, ingester Ingester) *Manager {
	return &Manager{
		store:    store,
		ingester: ingester,
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Start loads all active watches from storage and begins watching.
// Blocks until ctx is cancelled, then stops all goroutines.
func (m *Manager) Start(ctx context.Context) error {
	if m.store == nil {
		<-ctx.Done()
		return nil
	}
	watches, err := m.store.ListWatches(ctx, "active")
	if err != nil {
		return err
	}
	for _, cfg := range watches {
		if err := m.startWatch(ctx, cfg); err != nil {
			_ = err // log and continue
		}
	}
	<-ctx.Done()
	m.Stop()
	return nil
}

// Stop cancels all active watch goroutines.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.cancels {
		cancel()
		delete(m.cancels, id)
	}
}

// AddWatch persists a new watch config and starts watching immediately.
// If the config already exists in storage, it is ignored and watching starts anyway.
func (m *Manager) AddWatch(ctx context.Context, cfg *WatchConfig) error {
	if m.store != nil {
		// Ignore duplicate-key errors: the watch may have been pre-created.
		_ = m.store.CreateWatch(ctx, cfg)
	}
	return m.startWatch(ctx, cfg)
}

// RemoveWatch stops watching and deletes the config.
func (m *Manager) RemoveWatch(ctx context.Context, id string) error {
	m.cancelWatch(id)
	if m.store != nil {
		return m.store.DeleteWatch(ctx, id)
	}
	return nil
}

// PauseWatch stops the goroutine and updates status.
func (m *Manager) PauseWatch(ctx context.Context, id string) error {
	m.cancelWatch(id)
	if m.store == nil {
		return nil
	}
	cfg, err := m.store.GetWatch(ctx, id)
	if err != nil {
		return err
	}
	cfg.Status = "paused"
	cfg.UpdatedAt = time.Now()
	return m.store.UpdateWatch(ctx, cfg)
}

// ResumeWatch restarts a paused watch.
func (m *Manager) ResumeWatch(ctx context.Context, id string) error {
	if m.store == nil {
		return nil
	}
	cfg, err := m.store.GetWatch(ctx, id)
	if err != nil {
		return err
	}
	cfg.Status = "active"
	cfg.UpdatedAt = time.Now()
	if err := m.store.UpdateWatch(ctx, cfg); err != nil {
		return err
	}
	return m.startWatch(ctx, cfg)
}

func (m *Manager) cancelWatch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancels[id]; ok {
		cancel()
		delete(m.cancels, id)
	}
}

func (m *Manager) startWatch(ctx context.Context, cfg *WatchConfig) error {
	watchCtx, cancel := context.WithCancel(ctx) // #nosec G118 -- cancel is stored in m.cancels and invoked by stopWatch
	m.mu.Lock()
	m.cancels[cfg.ID] = cancel
	m.mu.Unlock()
	go m.watchLoop(watchCtx, cfg)
	return nil
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
	defer fsw.Close()

	// Walk directory tree and add each subdirectory to the watcher.
	// A per-entry error (unreadable dir, race with a concurrent delete) skips
	// that entry only: returning it would abort the whole walk and leave the
	// remaining subtree unwatched, which is strictly worse than missing one dir.
	filepath.WalkDir(cfg.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entry; aborting the walk would leave the subtree unwatched
		}
		if d.IsDir() {
			fsw.Add(path)
		}
		return nil
	})

	// Debounce timers keyed by absolute path.
	debounce := make(map[string]*time.Timer)
	debounceDelay := time.Duration(cfg.DebounceMS) * time.Millisecond
	if debounceDelay <= 0 {
		debounceDelay = 500 * time.Millisecond
	}

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
					filepath.WalkDir(absPath, func(p string, d fs.DirEntry, werr error) error {
						if werr != nil {
							// Skip this entry; aborting would drop the rest of
							// the newly created subtree from the watch set.
							return nil //nolint:nilerr // skip unreadable entry, keep walking the new subtree
						}
						if d.IsDir() {
							fsw.Add(p)
							return nil
						}
						relP := strings.TrimPrefix(p, cfg.Path+string(os.PathSeparator))
						relP = filepath.ToSlash(relP)
						if !MatchAny(cfg.IncludePatterns, relP) {
							return nil
						}
						if len(cfg.ExcludePatterns) > 0 && MatchAny(cfg.ExcludePatterns, relP) {
							return nil
						}
						pp := p
						if t, ok := debounce[pp]; ok {
							t.Stop()
						}
						debounce[pp] = time.AfterFunc(debounceDelay, func() {
							m.processFile(ctx, cfg, pp)
						})
						return nil
					})
					continue
				}

				relPath := strings.TrimPrefix(absPath, cfg.Path+string(os.PathSeparator))
				relPath = filepath.ToSlash(relPath)

				if !MatchAny(cfg.IncludePatterns, relPath) {
					continue
				}
				if len(cfg.ExcludePatterns) > 0 && MatchAny(cfg.ExcludePatterns, relPath) {
					continue
				}

				// Debounce: cancel existing timer, start new one.
				if t, ok := debounce[absPath]; ok {
					t.Stop()
				}
				ap := absPath
				debounce[ap] = time.AfterFunc(debounceDelay, func() {
					m.processFile(ctx, cfg, ap)
				})

			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				m.deleteFile(ctx, cfg, absPath)
			}

		case _, ok := <-fsw.Errors:
			if !ok {
				return
			}
			// Log error and continue.
		}
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
			filepath.WalkDir(cfg.Path, func(path string, d fs.DirEntry, err error) error {
				// Poll tick: skip entries we cannot stat this round rather
				// than aborting the sweep — the next tick retries them.
				if err != nil || d.IsDir() {
					return nil //nolint:nilerr // skip unreadable entry; next poll tick retries
				}
				relPath := strings.TrimPrefix(path, cfg.Path+string(os.PathSeparator))
				relPath = filepath.ToSlash(relPath)
				if !MatchAny(cfg.IncludePatterns, relPath) {
					return nil
				}
				if len(cfg.ExcludePatterns) > 0 && MatchAny(cfg.ExcludePatterns, relPath) {
					return nil
				}
				current[path] = struct{}{}
				m.processFile(ctx, cfg, path)
				return nil
			})
			// Detect deletions.
			for path := range seen {
				if _, ok := current[path]; !ok {
					m.deleteFile(ctx, cfg, path)
				}
			}
			seen = current
		}
	}
}

func (m *Manager) processFile(ctx context.Context, cfg *WatchConfig, absPath string) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return
	}

	h := sha256.Sum256(data)
	newHash := hex.EncodeToString(h[:])

	// Check existing record.
	if m.store != nil {
		existing, err := m.store.GetFileRecord(ctx, cfg.ID, absPath)
		if err == nil && existing.ContentHash == newHash {
			return // no change
		}
	}

	// Build AnalyzeRequest.
	relPath := strings.TrimPrefix(absPath, cfg.Path+string(os.PathSeparator))
	source := "watch:" + cfg.Mode + ":" + filepath.ToSlash(relPath)

	var req service.AnalyzeRequest
	if cfg.Mode == "obsidian" || cfg.Mode == "logseq" {
		req, err = ParseFile(absPath, cfg.Mode, cfg.Path)
		if err != nil {
			req = service.AnalyzeRequest{
				Content: string(data),
				Type:    detectType(absPath),
				Source:  source,
			}
		}
	} else {
		req = service.AnalyzeRequest{
			Content:  string(data),
			Type:     detectType(absPath),
			Source:   source,
			Pipeline: cfg.PipelineOverride,
		}
	}
	if req.Pipeline == "" {
		req.Pipeline = cfg.PipelineOverride
	}

	var objectID string
	if m.ingester != nil {
		jobID, err := m.ingester.Analyze(ctx, req)
		if err == nil {
			objectID = jobID // store job ID as placeholder until result available
		}
		if err != nil && m.store != nil {
			// Store the error in the watch config.
			if wc, werr := m.store.GetWatch(ctx, cfg.ID); werr == nil {
				wc.LastError = err.Error()
				wc.UpdatedAt = time.Now()
				m.store.UpdateWatch(ctx, wc)
			}
		}
	}

	if m.store != nil {
		rec := &WatchFileRecord{
			WatchID:     cfg.ID,
			FilePath:    absPath,
			ObjectID:    objectID,
			ContentHash: newHash,
			LastSeen:    time.Now(),
		}
		m.store.UpsertFileRecord(ctx, rec)
	}
}

func (m *Manager) deleteFile(ctx context.Context, cfg *WatchConfig, absPath string) {
	if m.store == nil {
		return
	}
	rec, err := m.store.GetFileRecord(ctx, cfg.ID, absPath)
	if err != nil {
		return // not found
	}
	if rec.ObjectID != "" && m.ingester != nil {
		m.ingester.DeleteObject(ctx, rec.ObjectID)
	}
	m.store.DeleteFileRecord(ctx, cfg.ID, absPath)
}

// detectType infers content type from file extension.
func detectType(absPath string) string {
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".md", ".markdown":
		return "text"
	case ".pdf":
		return "pdf"
	case ".html", ".htm":
		return "url"
	default:
		return "text"
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
		return "obsidian"
	}
	if fi, err := os.Stat(filepath.Join(path, "logseq")); err == nil && fi.IsDir() {
		return "logseq"
	}
	return "generic"
}
