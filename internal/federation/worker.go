// Package federation. worker.go implements WorkerSet — one async push
// goroutine per configured federation target. Wakes on `interval`, lists
// source objects + their edges, hands the batch to the target's Pusher,
// logs errors and continues. Honors context cancel for graceful shutdown
// (US-0319 AC #9). Cycle detection runs at construction time per US-0318.
package federation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FederationTarget is the runtime view of one federation entry from
// config. Worker code stays decoupled from config struct evolution.
type FederationTarget struct {
	Name     string
	URL      string
	SyncMode string
	Interval time.Duration
}

// Worker is one async federation pusher goroutine. Owns its ticker; exits
// on ctx.Done.
type Worker struct {
	target FederationTarget
	src    storage.StorageDriver
	pusher Pusher

	done chan struct{} // closed when run() returns
}

// WorkerSet manages the lifecycle of all federation workers for a server
// instance. Start() spawns goroutines; Stop() waits for them to drain
// (capped at stopTimeout).
type WorkerSet struct {
	src     storage.StorageDriver
	workers []*Worker

	mu      sync.Mutex
	cancel  context.CancelFunc
	started bool
	stopped bool
}

// stopTimeout bounds how long Stop will wait for workers to drain.
// US-0319 AC #9: graceful shutdown within 5s.
const stopTimeout = 5 * time.Second

// New constructs a WorkerSet from config + an opened source driver. Performs
// startup validation (cycle detection per US-0318) before any goroutine is
// spawned. Targets in `inline` mode are skipped (Phase 2 only spawns async
// goroutines per AC US-0319 + US-0318).
//
// Returns a non-nil WorkerSet even when zero workers are configured so
// callers can call Start/Stop unconditionally.
func New(cfg config.Config, src storage.StorageDriver) (*WorkerSet, error) {
	if err := detectCycles(cfg); err != nil {
		return nil, err
	}

	ws := &WorkerSet{src: src}
	for _, entry := range cfg.Federations {
		if entry.SyncMode != "async" {
			// inline targets handled at capture time (US-0320, Phase 2);
			// no async goroutine spawned.
			continue
		}
		tgt := FederationTarget{
			Name:     entry.Name,
			URL:      entry.URL,
			SyncMode: entry.SyncMode,
			Interval: entry.Interval,
		}
		ws.workers = append(ws.workers, &Worker{
			target: tgt,
			src:    src,
			pusher: pusherFor(entry),
		})
	}
	return ws, nil
}

// Len returns the number of active workers.
func (ws *WorkerSet) Len() int { return len(ws.workers) }

// Start spawns one goroutine per worker. Idempotent; subsequent calls are
// no-ops. Pass the server's root context — workers exit when it is canceled.
func (ws *WorkerSet) Start(ctx context.Context) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.started {
		return
	}
	ws.started = true

	runCtx, cancel := context.WithCancel(ctx)
	ws.cancel = cancel

	for _, w := range ws.workers {
		w.done = make(chan struct{})
		go w.run(runCtx)
	}
}

// Stop signals all workers to exit and waits up to stopTimeout for them to
// drain. Returns an error if any worker fails to exit in time.
// Idempotent and safe to call without a prior Start.
func (ws *WorkerSet) Stop(_ context.Context) error {
	ws.mu.Lock()
	if !ws.started || ws.stopped {
		ws.stopped = true
		ws.mu.Unlock()
		return nil
	}
	ws.stopped = true
	cancel := ws.cancel
	workers := ws.workers
	ws.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	deadline := time.NewTimer(stopTimeout)
	defer deadline.Stop()

	for _, w := range workers {
		select {
		case <-w.done:
			// drained
		case <-deadline.C:
			return fmt.Errorf("federation worker %q did not exit within %s",
				w.target.Name, stopTimeout)
		}
	}
	return nil
}

// run is the worker's main loop: tick → push → log+continue on err →
// exit on ctx.Done.
func (w *Worker) run(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(w.target.Interval)
	defer ticker.Stop()

	slog.Info("federation: worker started",
		"name", w.target.Name,
		"url", w.target.URL,
		"interval", w.target.Interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("federation: worker stopping",
				"name", w.target.Name,
				"reason", ctx.Err())
			return
		case <-ticker.C:
			if err := w.tick(ctx); err != nil {
				// AC #8: errors are logged, worker continues on next tick.
				slog.Warn("federation: push failed",
					"name", w.target.Name,
					"url", w.target.URL,
					"err", err)
			}
		}
	}
}

// tick performs one push cycle: list source objects + their edges, hand
// off to the Pusher. Watermark filtering is delegated to LocalPusher
// (which dedups by content_hash and advances the watermark on success).
func (w *Worker) tick(ctx context.Context) error {
	objs, _, err := w.src.Objects().List(ctx, storage.ObjectFilter{Status: "all"})
	if err != nil {
		return fmt.Errorf("list source objects: %w", err)
	}
	if len(objs) == 0 {
		return nil
	}

	materialized := make([]storage.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		materialized = append(materialized, *o)
	}

	edges := make([]storage.Edge, 0)
	for _, o := range objs {
		from, err := w.src.Edges().ListFrom(ctx, "object", o.ID)
		if err != nil {
			return fmt.Errorf("list source edges from %q: %w", o.ID, err)
		}
		for _, e := range from {
			edges = append(edges, *e)
		}
	}

	return w.pusher.Push(ctx, materialized, edges)
}

// pusherFor returns the appropriate Pusher implementation for a federation
// entry. Local SQLite paths use LocalPusher; HTTP(S) URLs use RemotePusher.
func pusherFor(entry config.FederationEntry) Pusher {
	if isHTTP(entry.URL) {
		return NewRemotePusher(entry.Name, entry.URL, entry.Token)
	}
	return NewLocalPusher(entry.Name, entry.URL)
}

func isHTTP(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// detectCycles runs DFS over the federation DAG rooted at the local
// instance and returns a descriptive error on the first cycle found. Per
// US-0318 Phase 1: a cycle exists when any target.URL resolves back to
// the current instance's storage path.
//
// Coverage:
//   - self-loop: target.URL == cfg.Storage.Path
//   - A → B → A: any subsequent hop's URL points to storage path
//     (Phase 1 only walks one level; Phase 2 will query remote topology APIs).
func detectCycles(cfg config.Config) error {
	selfPath, err := canonicalPath(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("federation cycle check: resolve self path: %w", err)
	}

	seen := map[string]bool{}
	for _, entry := range cfg.Federations {
		if entry.Name == "" {
			return errors.New("federation cycle check: target with empty name")
		}
		if seen[entry.Name] {
			return fmt.Errorf("federation cycle check: duplicate target name %q",
				entry.Name)
		}
		seen[entry.Name] = true

		if isHTTP(entry.URL) {
			// Phase 1: cannot resolve remote topology; trust operator. A
			// follow-up will query /api/v1/federation/topology per US-0322.
			continue
		}
		targetPath, err := canonicalPath(entry.URL)
		if err != nil {
			return fmt.Errorf("federation cycle check: resolve %q: %w",
				entry.Name, err)
		}
		if selfPath != "" && targetPath == selfPath {
			return fmt.Errorf(
				"federation cycle detected: target %q (%s) points back to self (%s)",
				entry.Name, entry.URL, cfg.Storage.Path)
		}
	}
	return nil
}

// canonicalPath returns an absolute, cleaned filesystem path for a
// federation target URL. file:// URIs are unwrapped; relative paths are
// resolved against the current working dir. Returns "" with nil error when
// the input is empty so callers can no-op gracefully.
func canonicalPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil {
			return "", err
		}
		p = u.Path
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
