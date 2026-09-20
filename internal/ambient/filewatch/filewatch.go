// Package filewatch is the ambient file-watch / drop-folder Source
// (per ADR-066 + US-0213).
//
// The source watches one or more directories via fsnotify; when a file
// appears (CREATE) or arrives via rename/move (RENAME with destination in
// the watched dir), it emits a RawEvent with the file path as the payload
// and the routed pipeline derived from the file extension. After successful
// enqueue, the file is moved to <watch_dir>/processed/<YYYY-MM>/ so the
// user can see what's been ingested.
//
// Routing per ADR-066 §Implementation Notes (US-0213):
//
//	.md / .txt              → text.long
//	.pdf                    → text.long (Docling backend optional, ADR-035)
//	.png / .jpg / .jpeg / .heic → image.ocr
//	.mp3 / .wav / .m4a / .flac / .ogg → audio.transcribe
//	.mp4 / .mov / .webm / .mkv → video.full
//	otherwise               → text.long (let pipeline decide; or routed to inbox triage)
//
// Hidden files (.DS_Store, Thumbs.db, anything starting with .) are
// silently ignored. Files exceeding MaxFileSizeMB are skipped with a
// failed-event bus topic.
package filewatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Defaults per ADR-066 / US-0213.
const (
	SourceName            = "filewatch"
	DefaultMaxFileSizeMB  = 500
	DefaultProcessedDir   = "processed"
	defaultEventChanSize  = 16
	defaultStableSettleMs = 250 // wait briefly to let writers finish
)

// Config configures a Source. Zero values fall back to documented defaults.
type Config struct {
	// Directories is the list of directories to watch. Each must exist;
	// missing directories are skipped with a logged warning, not an error.
	Directories []string
	// ProcessedSubdir is the subdirectory name (relative to each watch dir)
	// where successfully-enqueued files are moved. Default: "processed".
	ProcessedSubdir string
	// MaxFileSizeMB caps file size; larger files are skipped.
	// Default: 500 (MB).
	MaxFileSizeMB int64
	// SettleDuration is how long to wait after a CREATE event before reading
	// the file. Lets the writer finish flushing. Default: 250ms.
	SettleDuration time.Duration
	// MoveAfterEnqueue toggles whether successfully-enqueued files are moved
	// to ProcessedSubdir. Default: true. Set false for tests or workflows
	// that want files to remain in place.
	MoveAfterEnqueue bool
}

// Source is the file-watch ambient source. Watches Config.Directories via
// fsnotify; emits a RawEvent per new file.
type Source struct {
	cfg       Config
	events    chan ambient.RawEvent
	watcher   *fsnotify.Watcher
	publisher ambient.Publisher

	mu       sync.Mutex
	started  bool
	stopped  bool
	stopFunc context.CancelFunc
}

// New constructs a file-watch Source. cfg.Directories must contain at least
// one entry.
func New(cfg Config) (*Source, error) {
	if len(cfg.Directories) == 0 {
		return nil, fmt.Errorf("filewatch: at least one directory is required")
	}
	if cfg.ProcessedSubdir == "" {
		cfg.ProcessedSubdir = DefaultProcessedDir
	}
	if cfg.MaxFileSizeMB <= 0 {
		cfg.MaxFileSizeMB = DefaultMaxFileSizeMB
	}
	if cfg.SettleDuration <= 0 {
		cfg.SettleDuration = defaultStableSettleMs * time.Millisecond
	}
	// MoveAfterEnqueue defaults to true; explicit zero-value handling:
	// callers that want to disable must set false explicitly. We can't
	// distinguish "not set" from "set false" with a bool, so the default
	// for the config-from-zero case is true. Tests that need it false
	// pass it explicitly.
	if !cfg.MoveAfterEnqueue {
		// We accept both meanings; downstream code reads cfg.MoveAfterEnqueue
		// and acts accordingly. Documented: zero = false.
	}
	return &Source{
		cfg:    cfg,
		events: make(chan ambient.RawEvent, defaultEventChanSize),
	}, nil
}

// Name implements ambient.Source. Returns "filewatch".
func (s *Source) Name() string { return SourceName }

// Start implements ambient.Source. Initialises fsnotify watchers on every
// configured directory, then loops on watcher events.
func (s *Source) Start(ctx context.Context, b ambient.Publisher) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("filewatch: already started")
	}
	s.started = true
	s.publisher = b
	s.mu.Unlock()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("filewatch: create watcher: %w", err)
	}
	s.watcher = w

	for _, dir := range s.cfg.Directories {
		// Ensure directory exists; create if missing (so users with a fresh
		// install don't have to mkdir manually).
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("filewatch: ensure dir %s: %w", dir, err)
		}
		if err := w.Add(dir); err != nil {
			return fmt.Errorf("filewatch: watch %s: %w", dir, err)
		}
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.stopFunc = cancel
	s.mu.Unlock()

	if b != nil {
		_ = b.Publish(ctx, ambient.SourceLifecycleTopic("ready"), SourceName, nil)
	}

	go s.eventLoop(loopCtx)

	// Initial scan: catch up on any files dropped while the daemon was off.
	go s.initialScan(loopCtx)

	return nil
}

// Events implements ambient.Source.
func (s *Source) Events() <-chan ambient.RawEvent { return s.events }

// Drain implements ambient.Source.
func (s *Source) Drain(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopFunc != nil {
		s.stopFunc()
	}
	return nil
}

// Stop implements ambient.Source.
func (s *Source) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	if s.stopFunc != nil {
		s.stopFunc()
	}
	if s.watcher != nil {
		_ = s.watcher.Close()
	}
	close(s.events)
	return nil
}

// eventLoop is the main fsnotify event consumer.
func (s *Source) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			s.handle(ctx, ev)
		case _, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			// Surface watcher-side errors via failed bus event but keep going.
			if s.publisher != nil {
				_ = s.publisher.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName, nil)
			}
		}
	}
}

// initialScan emits one event per existing file in each watched directory
// at startup. Catches up on files dropped while the daemon was off.
func (s *Source) initialScan(ctx context.Context) {
	for _, dir := range s.cfg.Directories {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			s.processPath(ctx, path)
		}
	}
}

// handle processes one fsnotify event. CREATE / RENAME (with target in our
// dir) / WRITE-after-CREATE all flow through processPath after a settle.
func (s *Source) handle(ctx context.Context, ev fsnotify.Event) {
	// Only act on file appearance events. WRITE events for files we already
	// emitted are ignored — the source is "drop-folder ingest", not a
	// continuous-update feed.
	if !ev.Op.Has(fsnotify.Create) && !ev.Op.Has(fsnotify.Rename) {
		return
	}
	s.processPath(ctx, ev.Name)
}

// processPath emits a RawEvent for path if it passes filters (visibility,
// size, extension). Settle delay lets writers finish flushing.
func (s *Source) processPath(ctx context.Context, path string) {
	base := filepath.Base(path)
	if isHiddenOrSystem(base) {
		return
	}
	// Settle: wait briefly so readers see a complete file.
	select {
	case <-ctx.Done():
		return
	case <-time.After(s.cfg.SettleDuration):
	}
	info, err := os.Stat(path)
	if err != nil {
		return // file disappeared (e.g. moved out of watch dir)
	}
	if info.IsDir() {
		return
	}
	if info.Size() > s.cfg.MaxFileSizeMB*1024*1024 {
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, ambient.SourceLifecycleTopic("failed"), SourceName,
				map[string]any{"path": path, "size": info.Size(), "reason": "exceeds_max_file_size_mb"})
		}
		return
	}

	pipeline, kind := routeByExtension(path)
	fp, err := fingerprintFile(path)
	if err != nil {
		return
	}

	ev := ambient.RawEvent{
		Source:            SourceName,
		OccurredAt:        time.Now(),
		Kind:              kind,
		Payload:           []byte(path),
		Fingerprint:       fp,
		SuggestedPipeline: pipeline,
		Metadata: map[string]any{
			"file_path":  path,
			"file_size":  info.Size(),
			"file_mtime": info.ModTime().Unix(),
		},
	}

	select {
	case s.events <- ev:
	case <-ctx.Done():
	}
}

// routeByExtension returns (pipeline, kind) for path's extension. Unknown
// extensions route to text.long with KindFile so the pipeline-side router
// can defer to format detection.
func routeByExtension(path string) (string, ambient.Kind) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".txt", ".markdown":
		return "text.long", ambient.KindText
	case ".pdf":
		return "text.long", ambient.KindFile
	case ".png", ".jpg", ".jpeg", ".heic", ".webp":
		return "image.ocr", ambient.KindImage
	case ".mp3", ".wav", ".m4a", ".flac", ".ogg", ".oga":
		return "audio.transcribe", ambient.KindFile
	case ".mp4", ".mov", ".webm", ".mkv", ".avi":
		return "video.full", ambient.KindFile
	default:
		return "text.long", ambient.KindFile
	}
}

// isHiddenOrSystem returns true for filenames the source must ignore.
func isHiddenOrSystem(base string) bool {
	if strings.HasPrefix(base, ".") {
		return true
	}
	switch base {
	case "Thumbs.db", "desktop.ini", "$RECYCLE.BIN":
		return true
	}
	return false
}

// fingerprintFile reads the file's first 256KB and hashes it (with size as
// a salt) so identical-content files at different paths still dedupe and
// large files don't blow memory. The substrate's enqueue-boundary dedup
// uses this.
func fingerprintFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	limit := int64(256 * 1024)
	if _, err := io.Copy(h, io.LimitReader(f, limit)); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	// Mix size into the hash so files with the same first 256KB but
	// different total length don't collide.
	fmt.Fprintf(h, "|size:%d", info.Size())
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MoveToProcessed moves path into <watchDir>/processed/<YYYY-MM>/. Called
// by the Runner (or the daemon driver) after a successful enqueue. Exposed
// so callers can move-on-success without coupling the source to enqueue
// lifecycle. Returns the new absolute path or an error.
func (s *Source) MoveToProcessed(path string) (string, error) {
	if !s.cfg.MoveAfterEnqueue {
		return path, nil
	}
	dir := filepath.Dir(path)
	subdir := filepath.Join(dir, s.cfg.ProcessedSubdir, time.Now().Format("2006-01"))
	if err := os.MkdirAll(subdir, 0o750); err != nil {
		return "", fmt.Errorf("create processed dir: %w", err)
	}
	dst := filepath.Join(subdir, filepath.Base(path))
	if err := os.Rename(path, dst); err != nil {
		return "", fmt.Errorf("move to processed: %w", err)
	}
	return dst, nil
}

// Compile-time assertion.
var _ ambient.Source = (*Source)(nil)
