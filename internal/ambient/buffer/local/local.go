// Package local is the local-filesystem ambient.Buffer backend.
//
// Per the storage-lives-in-kit principle (saved feedback), this package
// is a THIN SHIM over kit/go/storage/blob/local. The Buffer interface
// adds RawEvent JSON encoding + chronological-ordering semantics on top
// of the blob.Store key/value primitive; the underlying disk layout,
// path resolution, atomic-write semantics, and basic CRUD all live in
// kit and are reused by other packages and downstream tools.
//
// XDG-compliant default root: $XDG_STATE_HOME/ctxt/ambient/ falling back
// to ~/.local/state/ctxt/ambient/. Override via $CTXT_AMBIENT_BUFFER_DIR
// or Config.RootDir.
//
// Layout under <RootDir>/:
//
//	events/<source>/<yyyy>/<mm>/<dd>/<event-id>.json
//
// Lex-sorted blob.Store.List output is roughly chronological because of
// the date tree, so Pop returns oldest-first.
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kitblob "hop.top/kit/go/storage/blob"
	kitlocal "hop.top/kit/go/storage/blob/local"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Defaults per ADR-066 §Decision item 5.
const (
	DefaultRetentionHours = 24 * 7 // 7 days
	DefaultMaxGB          = 2
)

// Config configures a local-filesystem buffer.
type Config struct {
	// RootDir is the absolute path under which events are stored. When
	// empty, ResolveDefaultDir() is consulted (XDG Base Directory).
	RootDir string
	// RetentionHours is the TTL for events. Default: 168 (7 days).
	RetentionHours int
	// MaxGB caps total bytes on disk. Default: 2.
	MaxGB int
	// Now overrides the wall clock; tests use a fake.
	Now func() time.Time
}

// Buffer is the local-filesystem ambient.Buffer.
type Buffer struct {
	cfg     Config
	rootDir string
	store   kitblob.Store

	mu            sync.Mutex
	appendedTotal atomic.Uint64
	poppedTotal   atomic.Uint64
	evictedTotal  atomic.Uint64
}

// New constructs a local-filesystem buffer.
func New(cfg Config) (*Buffer, error) {
	if cfg.RootDir == "" {
		dir, err := ResolveDefaultDir()
		if err != nil {
			return nil, fmt.Errorf("ambient/buffer/local: resolve default dir: %w", err)
		}
		cfg.RootDir = dir
	}
	if cfg.RetentionHours <= 0 {
		cfg.RetentionHours = DefaultRetentionHours
	}
	if cfg.MaxGB <= 0 {
		cfg.MaxGB = DefaultMaxGB
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	store, err := kitlocal.New(cfg.RootDir)
	if err != nil {
		return nil, fmt.Errorf("ambient/buffer/local: kit/blob/local: %w", err)
	}
	return &Buffer{cfg: cfg, rootDir: cfg.RootDir, store: store}, nil
}

// ResolveDefaultDir computes the XDG-compliant default buffer path.
//
// Resolution order:
//   1. $CTXT_AMBIENT_BUFFER_DIR (ctxt-specific override)
//   2. $XDG_STATE_HOME/ctxt/ambient/
//   3. $HOME/.local/state/ctxt/ambient/
func ResolveDefaultDir() (string, error) {
	if v := os.Getenv("CTXT_AMBIENT_BUFFER_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "ctxt", "ambient"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ctxt", "ambient"), nil
}

func keyForEvent(ev ambient.RawEvent, fallbackNow time.Time) string {
	when := ev.OccurredAt
	if when.IsZero() {
		when = fallbackNow
	}
	id := ev.Fingerprint
	if id == "" {
		id = fmt.Sprintf("%d", when.UnixNano())
	}
	return fmt.Sprintf("events/%s/%04d/%02d/%02d/%s.json",
		ev.Source, when.Year(), int(when.Month()), when.Day(), id)
}

// Append implements ambient.Buffer.
func (b *Buffer) Append(ctx context.Context, ev ambient.RawEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := keyForEvent(ev, b.cfg.Now())
	if err := b.store.Put(ctx, key, bytes.NewReader(body), "application/json"); err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	b.appendedTotal.Add(1)
	return nil
}

// Pop implements ambient.Buffer.
func (b *Buffer) Pop(ctx context.Context) (ambient.RawEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	objs, err := b.store.List(ctx, "events/")
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("list events: %w", err)
	}
	if len(objs) == 0 {
		return ambient.RawEvent{}, ambient.ErrBufferEmpty
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	oldest := objs[0]

	rc, err := b.store.Get(ctx, oldest.Key)
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("get %s: %w", oldest.Key, err)
	}
	body, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("read %s: %w", oldest.Key, err)
	}
	var ev ambient.RawEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return ambient.RawEvent{}, fmt.Errorf("unmarshal %s: %w", oldest.Key, err)
	}
	if err := b.store.Delete(ctx, oldest.Key); err != nil {
		return ambient.RawEvent{}, fmt.Errorf("delete %s: %w", oldest.Key, err)
	}
	b.poppedTotal.Add(1)
	return ev, nil
}

// Range implements ambient.Buffer.
func (b *Buffer) Range(ctx context.Context, fn func(ambient.RawEvent) bool) error {
	objs, err := b.store.List(ctx, "events/")
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	for _, obj := range objs {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := b.store.Get(ctx, obj.Key)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		var ev ambient.RawEvent
		if json.Unmarshal(body, &ev) != nil {
			continue
		}
		if !fn(ev) {
			return nil
		}
	}
	return nil
}

// Len implements ambient.Buffer.
func (b *Buffer) Len() int {
	objs, err := b.store.List(context.Background(), "events/")
	if err != nil {
		return 0
	}
	return len(objs)
}

// Stats implements ambient.Buffer.
func (b *Buffer) Stats() ambient.BufferStats {
	objs, _ := b.store.List(context.Background(), "events/")
	var totalBytes int64
	for _, o := range objs {
		totalBytes += o.Size
	}
	var oldest time.Time
	if len(objs) > 0 {
		sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
		oldest = parseDateFromKey(objs[0].Key)
	}
	return ambient.BufferStats{
		Backend:       "local-fs",
		Count:         len(objs),
		Capacity:      b.cfg.MaxGB * 1024,
		OldestEventAt: oldest,
		AppendedTotal: b.appendedTotal.Load(),
		PoppedTotal:   b.poppedTotal.Load(),
		EvictedTotal:  b.evictedTotal.Load(),
		Extra: map[string]any{
			"root_dir":    b.rootDir,
			"total_bytes": totalBytes,
		},
	}
}

// Sweep enforces TTL + cap retention. Walks the underlying filesystem
// directly (vs. via blob.Store) so we can read ModTime — kit/blob's
// Object struct doesn't carry it. This is the one place we touch the
// filesystem directly; everything else goes through kit/blob.
func (b *Buffer) Sweep(ctx context.Context) (evictedTTL, evictedCap int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	files, err := b.listFilesWithMtimes(ctx)
	if err != nil {
		return 0, 0, err
	}

	cutoff := b.cfg.Now().Add(-time.Duration(b.cfg.RetentionHours) * time.Hour)

	survivors := make([]eventFile, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return evictedTTL, evictedCap, err
		}
		if f.ModTime.Before(cutoff) {
			if rerr := b.store.Delete(ctx, f.Key); rerr == nil {
				evictedTTL++
				b.evictedTotal.Add(1)
			}
			continue
		}
		survivors = append(survivors, f)
	}

	maxBytes := int64(b.cfg.MaxGB) * 1024 * 1024 * 1024
	var totalBytes int64
	for _, f := range survivors {
		totalBytes += f.Size
	}
	if totalBytes > maxBytes {
		sort.Slice(survivors, func(i, j int) bool {
			return survivors[i].ModTime.Before(survivors[j].ModTime)
		})
		for _, f := range survivors {
			if totalBytes <= maxBytes {
				break
			}
			if rerr := b.store.Delete(ctx, f.Key); rerr == nil {
				evictedCap++
				b.evictedTotal.Add(1)
				totalBytes -= f.Size
			}
		}
	}
	return evictedTTL, evictedCap, nil
}

type eventFile struct {
	Key     string // blob.Store key (forward-slash)
	Path    string // absolute fs path
	Size    int64
	ModTime time.Time
}

// listFilesWithMtimes walks the filesystem under <RootDir>/events/.
// Used only by Sweep; routine reads/writes go through kit/blob.
func (b *Buffer) listFilesWithMtimes(ctx context.Context) ([]eventFile, error) {
	root := filepath.Join(b.rootDir, "events")
	var out []eventFile
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		rel, rerr := filepath.Rel(b.rootDir, path)
		if rerr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		out = append(out, eventFile{Key: key, Path: path, Size: info.Size(), ModTime: info.ModTime()})
		return ctx.Err()
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// parseDateFromKey extracts YYYY/MM/DD from a key like
// events/<source>/<yyyy>/<mm>/<dd>/<id>.json. Zero time on parse failure.
func parseDateFromKey(key string) time.Time {
	parts := strings.Split(key, "/")
	if len(parts) < 6 {
		return time.Time{}
	}
	t, err := time.Parse("2006/01/02", parts[2]+"/"+parts[3]+"/"+parts[4])
	if err != nil {
		return time.Time{}
	}
	return t
}

// Compile-time assertion.
var _ ambient.Buffer = (*Buffer)(nil)
