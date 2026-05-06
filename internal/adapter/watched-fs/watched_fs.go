// Package watchedfs is the watched-fs sensor adapter — an
// fsnotify-based delta watcher.
//
// Per ADR-065 §Amendment 2026-05-06 and the policy/ambient.yaml
// catalog in docs/ctxt/ambient.md: watched-fs differs from
// files.local-fs in that it returns deltas (files modified or created
// since the previous Fetch), not the full tree state. Lifecycle is
// non-trivial: Start() registers fsnotify watchers on each configured
// path, Stop() closes the watcher and the event-pump goroutine.
//
// Declared capabilities: fetch + emit-events.
package watchedfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Config carries the watched-fs sensor settings. Paths is the list of
// directories or files to watch; operators populate it via
// policy/ambient.yaml.
type Config struct {
	Paths []string
}

// New constructs a typed watched-fs sensor Adapter. Watchers are NOT
// registered until Start runs.
func New(cfg Config) *Adapter {
	return &Adapter{cfg: cfg}
}

// Adapter is the typed watched-fs sensor. It maintains an in-memory
// map of pending changes keyed by path; each Fetch drains the map.
// Persistence of the watermark across daemon restarts is a Phase 2
// follow-up — for now a daemon restart loses pending events but
// fsnotify continues delivering new ones.
type Adapter struct {
	cfg Config

	mu      sync.Mutex
	pending map[string]changeEvent
	watcher *fsnotify.Watcher
	cancel  context.CancelFunc
	done    chan struct{}
	ready   bool
}

type changeEvent struct {
	op fsnotify.Op
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "watched-fs" — the slot identity defined in
// internal/adapter/watched-fs/slot.go.
func (a *Adapter) Protocol() string { return Protocol }

// Backend returns "watched-fs" — the canonical backend name surfaced
// in operator UIs (`policy/ambient.yaml`'s `name: watched-fs`).
func (a *Adapter) Backend() string { return "watched-fs" }

// Capabilities returns the watched-fs sensor's declared capabilities:
// fetch (drains pending events) and emit-events (lifecycle topics).
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start registers fsnotify watchers on every configured Path and
// spawns an event-pump goroutine that drains the watcher's Events
// channel into the pending map. Returning an error leaves the adapter
// in a stopped state with no resources held.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ready {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watched-fs: NewWatcher: %w", err)
	}
	for _, p := range a.cfg.Paths {
		if err := w.Add(p); err != nil {
			_ = w.Close()
			return fmt.Errorf("watched-fs: watch %s: %w", p, err)
		}
	}
	a.watcher = w
	a.pending = make(map[string]changeEvent)
	pumpCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.done = make(chan struct{})
	go a.pump(pumpCtx, w, a.done)
	a.ready = true
	return nil
}

// pump drains the fsnotify Events channel and records create/write/
// rename events as pending changes. The watcher and done channel are
// passed in so Stop can safely nil the receiver fields without racing
// with this goroutine. Errors from the watcher are swallowed — Phase 2
// logs nothing; future revs may surface them on the bus.
func (a *Adapter) pump(ctx context.Context, w *fsnotify.Watcher, done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			a.mu.Lock()
			a.pending[ev.Name] = changeEvent{op: ev.Op}
			a.mu.Unlock()
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// Ready reports whether Start has registered the fsnotify watcher.
func (a *Adapter) Ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ready
}

// Drain is a no-op — pending events stay in the map until the next
// Fetch drains them. The runner gives Drain a chance to flush
// in-flight work; for watched-fs there is none.
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop cancels the event pump and closes the fsnotify watcher.
// Idempotent: calling Stop on an already-stopped adapter is a no-op.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	if !a.ready {
		a.mu.Unlock()
		return nil
	}
	cancel := a.cancel
	w := a.watcher
	done := a.done
	a.cancel = nil
	a.watcher = nil
	a.done = nil
	a.ready = false
	a.mu.Unlock()

	cancel()
	if w != nil {
		_ = w.Close()
	}
	if done != nil {
		<-done
	}
	return nil
}

// Fetch drains the pending-changes map and emits one ingest.Object
// per changed path. Callers receive deltas since the previous Fetch
// (or since Start, on the first call). File bodies are NOT loaded;
// downstream pipelines load bodies on demand.
//
// The Object.Metadata map carries:
//
//   - "path"    — absolute file path
//   - "op"      — fsnotify operation string (e.g. "WRITE", "CREATE")
//   - "size"    — file size in bytes (int64) when the file still exists
//   - "modtime" — modification time (time.Time) when the file still exists
func (a *Adapter) Fetch(_ context.Context) ([]ingest.Object, error) {
	a.mu.Lock()
	pending := a.pending
	a.pending = make(map[string]changeEvent)
	a.mu.Unlock()

	if len(pending) == 0 {
		return nil, nil
	}
	out := make([]ingest.Object, 0, len(pending))
	for path, ev := range pending {
		md := map[string]any{
			"path": path,
			"op":   ev.op.String(),
		}
		if info, err := os.Stat(path); err == nil {
			md["size"] = info.Size()
			md["modtime"] = info.ModTime()
		}
		out = append(out, ingest.Object{
			ID:       path,
			Type:     "file",
			Metadata: md,
		})
	}
	return out, nil
}

// Submit returns ErrCapabilityNotDeclared — watched-fs is a sensor
// (fetch-only).
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — watched-fs does not expose
// a wire protocol.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
