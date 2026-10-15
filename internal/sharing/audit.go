package sharing

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Read audit defaults (ADR-080 §9, decision D23).
const (
	// DefaultAuditBatch is how many entries the writer flushes at once.
	DefaultAuditBatch = 64
	// DefaultAuditInterval is how long an entry waits in the buffer at
	// most before a flush.
	DefaultAuditInterval = 2 * time.Second
	// DefaultAuditCapacity bounds the buffer; past it entries are
	// dropped and counted rather than making a request wait.
	DefaultAuditCapacity = 10000
	// auditFlushTimeout bounds one flush's write.
	auditFlushTimeout = 5 * time.Second
)

// AuditOptions tunes an AuditWriter; zero values take the defaults.
type AuditOptions struct {
	Batch    int
	Interval time.Duration
	Capacity int
}

// AuditWriter appends read audit entries (storage.AccessEntry) off the
// request path: Record puts an entry in a buffer and returns; one
// goroutine flushes the buffer in batches, when it reaches Batch
// entries or every Interval. A request never waits on the store's
// writer. Entries still in the buffer when the process dies are lost,
// which the sharing guide says; Close flushes what it can first.
type AuditWriter struct {
	store storage.AccessLogStore
	opts  AuditOptions

	mu     sync.Mutex
	buf    []*storage.AccessEntry
	closed bool

	dropped atomic.Uint64
	kick    chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

// NewAuditWriter starts a writer over store. A nil store yields a nil
// writer, which records nothing.
func NewAuditWriter(store storage.AccessLogStore, opts AuditOptions) *AuditWriter {
	if store == nil {
		return nil
	}
	if opts.Batch <= 0 {
		opts.Batch = DefaultAuditBatch
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultAuditInterval
	}
	if opts.Capacity <= 0 {
		opts.Capacity = DefaultAuditCapacity
	}
	w := &AuditWriter{
		store: store, opts: opts,
		kick: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Record buffers e. It never blocks: a full buffer or a closed writer
// drops the entry and counts it. nil-safe.
func (w *AuditWriter) Record(e *storage.AccessEntry) {
	if w == nil || e == nil {
		return
	}
	w.mu.Lock()
	if w.closed || len(w.buf) >= w.opts.Capacity {
		w.mu.Unlock()
		w.dropped.Add(1)
		return
	}
	w.buf = append(w.buf, e)
	full := len(w.buf) >= w.opts.Batch
	w.mu.Unlock()
	if full {
		select {
		case w.kick <- struct{}{}:
		default:
		}
	}
}

// Flush writes every buffered entry now. nil-safe.
func (w *AuditWriter) Flush(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	batch := w.buf
	w.buf = nil
	w.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if err := w.store.Append(ctx, batch); err != nil {
		w.dropped.Add(uint64(len(batch)))
		slog.Error("read audit: flush failed, entries lost", "entries", len(batch), "err", err)
		return err
	}
	return nil
}

// Close stops the writer and flushes what is buffered within ctx.
// Entries recorded after Close are dropped. nil-safe; idempotent.
func (w *AuditWriter) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	close(w.stop)
	<-w.done
	return w.Flush(ctx)
}

// Dropped reports how many entries were lost: buffer full, writer
// closed, or a flush that failed.
func (w *AuditWriter) Dropped() uint64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}

// Buffered reports how many entries wait in the buffer (tests).
func (w *AuditWriter) Buffered() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.buf)
}

// run is the writer goroutine: a flush on every kick and every tick.
func (w *AuditWriter) run() {
	defer close(w.done)
	t := time.NewTicker(w.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-w.kick:
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), auditFlushTimeout)
		_ = w.Flush(ctx)
		cancel()
	}
}

// AccessEntryFor builds the read audit entry of one request by a
// restricted viewer (ADR-080 §9): the viewer's principal and
// credential, the route, the policy versions, and the objects and
// entities the request released, from its ledger. A preview by the
// owner (PreviewTarget) is logged as the owner's request, with the
// previewed viewer's policy versions after a preview= marker.
func AccessEntryFor(ctx context.Context, v Viewer, route string, l *Ledger, now time.Time) *storage.AccessEntry {
	e := &storage.AccessEntry{
		At:             now.UTC(),
		PrincipalID:    v.PrincipalID(),
		IdentityID:     v.IdentityID(),
		Route:          route,
		PolicyVersions: v.PolicyKey(),
		ObjectIDs:      l.Objects(),
	}
	e.ObjectCount = len(e.ObjectIDs)
	e.EntityCount = len(l.Entities())
	if p, ok := PreviewFrom(ctx); ok {
		e.PrincipalID, e.IdentityID = p.Principal, p.Identity
		e.PolicyVersions = "preview=" + p.Target + ";" + e.PolicyVersions
	}
	if e.PrincipalID == "" {
		e.PrincipalID = storage.OwnerPrincipal
	}
	return e
}

// Preview marks a request the owner runs through another viewer
// (ADR-080 §2, owner bypass): who the owner is and what it previews.
type Preview struct {
	// Target is the principal id or policy name previewed.
	Target string
	// Principal and Identity are the owner's, as authenticated.
	Principal string
	Identity  string
}

type previewKey struct{}

// WithPreview returns a context marked as a preview of p.
func WithPreview(ctx context.Context, p Preview) context.Context {
	return context.WithValue(ctx, previewKey{}, p)
}

// PreviewFrom returns the preview ctx is marked as, if any.
func PreviewFrom(ctx context.Context) (Preview, bool) {
	p, ok := ctx.Value(previewKey{}).(Preview)
	return p, ok
}
