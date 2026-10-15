package federation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultBatchSize caps the objects one Push carries.
const DefaultBatchSize = 100

// maxObjectFailures is how many syncs in a row one object may fail
// with an *ObjectError before a Syncer skips it.
const maxObjectFailures = 3

// maxSkippedRecorded caps the skipped objects a Syncer remembers; the
// oldest go first.
const maxSkippedRecorded = 100

// SkippedObject records an object a Syncer gave up pushing to its
// target. An edit gives it a new ChangeSeq, and the next sync tries it
// again.
type SkippedObject struct {
	ID        string    `json:"id"`
	ChangeSeq int64     `json:"change_seq"`
	Err       string    `json:"error"`
	At        time.Time `json:"at"`
}

// Syncer pushes one source's changes to one target, sync after sync. It
// remembers which object failed the last syncs, so one that keeps
// failing (its source blob is gone, the receiver refuses it) is skipped
// instead of holding back every change after it. Safe for one Sync at a
// time; Skipped may be called concurrently.
type Syncer struct {
	src       storage.StorageDriver
	p         Pusher
	batchSize int

	// failing is the object the last syncs failed on, and how many
	// syncs in a row did.
	failingID  string
	failingSeq int64
	failures   int
	// skip holds the objects to leave out, by id to the ChangeSeq they
	// were skipped at, until the watermark passes them.
	skip map[string]int64

	mu      sync.Mutex
	skipped []SkippedObject
}

// NewSyncer returns a Syncer pushing src's changes to p, at most
// batchSize objects per Push (DefaultBatchSize when batchSize <= 0).
func NewSyncer(src storage.StorageDriver, p Pusher, batchSize int) *Syncer {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &Syncer{src: src, p: p, batchSize: batchSize, skip: map[string]int64{}}
}

// Skipped returns the objects this Syncer skipped, oldest first.
func (s *Syncer) Skipped() []SkippedObject {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SkippedObject(nil), s.skipped...)
}

// Sync is a one-off Syncer's Sync: it never skips an object.
func Sync(ctx context.Context, src storage.StorageDriver, p Pusher, batchSize int) (int, error) {
	return NewSyncer(src, p, batchSize).Sync(ctx)
}

// Sync pushes every object of the source whose ChangeSeq is after the
// target's watermark, in ChangeSeq order, a batch per Push, each with
// the edges from it and the entities those edges mention. It stops at
// the first failed Push; batches before it stay acknowledged. An object
// that failed maxObjectFailures syncs in a row with an *ObjectError is
// skipped: logged, recorded in Skipped, and left out of its batch, and
// the watermark moves past it. Returns the number of objects pushed.
//
// The store stamps ChangeSeq on every write and commits the values in
// increasing order, so everything up to the last object listed has
// landed: no change can later appear behind the watermark, whatever
// updated_at it carries (an import, a row a receiver forwards on, a
// restore).
func (s *Syncer) Sync(ctx context.Context) (int, error) {
	pos, err := s.p.Watermark(ctx)
	if err != nil {
		return 0, fmt.Errorf("read watermark: %w", err)
	}
	s.prune(pos)
	pushed := 0
	for {
		page, _, err := s.src.Objects().List(ctx, storage.ObjectFilter{
			Status:       "all",
			ChangedAfter: &pos,
			Limit:        s.batchSize,
		})
		if err != nil {
			return pushed, fmt.Errorf("list changed source objects: %w", err)
		}
		if len(page) == 0 {
			return pushed, nil
		}
		n, err := s.pushPage(ctx, page)
		pushed += n
		if err != nil {
			return pushed, err
		}
		pos = page[len(page)-1].ChangeSeq
		s.prune(pos)
		if len(page) < s.batchSize {
			return pushed, nil
		}
	}
}

// pushPage pushes page less the objects being skipped, skipping one
// more each time an object reaches maxObjectFailures, and moves the
// watermark to the end of page once the rest is acknowledged. Returns
// the number of objects pushed.
func (s *Syncer) pushPage(ctx context.Context, page []*storage.KnowledgeObject) (int, error) {
	for {
		send := make([]*storage.KnowledgeObject, 0, len(page))
		for _, o := range page {
			if seq, ok := s.skip[o.ID]; !ok || seq != o.ChangeSeq {
				send = append(send, o)
			}
		}
		var err error
		if len(send) > 0 {
			err = pushObjects(ctx, s.src, s.p, send)
		}
		if err == nil {
			s.clearFailing(page)
			last := page[len(page)-1].ChangeSeq
			if len(send) == 0 || send[len(send)-1].ChangeSeq != last {
				if err := s.p.Advance(ctx, last); err != nil {
					return len(send), fmt.Errorf("advance watermark past skipped objects: %w", err)
				}
			}
			return len(send), nil
		}
		var oe *ObjectError
		if !errors.As(err, &oe) {
			return 0, err
		}
		obj := findObject(send, oe.ID)
		if obj == nil {
			return 0, err
		}
		if oe.ID == s.failingID && obj.ChangeSeq == s.failingSeq {
			s.failures++
		} else {
			s.failingID, s.failingSeq, s.failures = obj.ID, obj.ChangeSeq, 1
		}
		if s.failures < maxObjectFailures {
			return 0, err
		}
		s.skipObject(obj, oe.Err)
	}
}

// skipObject leaves obj out of every push until the watermark passes
// it, and records it.
func (s *Syncer) skipObject(obj *storage.KnowledgeObject, cause error) {
	slog.Warn("federation: skipping object after repeated push failures",
		"name", s.p.Name(), "id", obj.ID, "change_seq", obj.ChangeSeq,
		"failures", s.failures, "err", cause)
	s.skip[obj.ID] = obj.ChangeSeq
	s.failingID, s.failingSeq, s.failures = "", 0, 0
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipped = append(s.skipped, SkippedObject{
		ID: obj.ID, ChangeSeq: obj.ChangeSeq, Err: cause.Error(), At: time.Now().UTC(),
	})
	if len(s.skipped) > maxSkippedRecorded {
		s.skipped = s.skipped[len(s.skipped)-maxSkippedRecorded:]
	}
}

// clearFailing forgets the failing object once a push of page, which
// holds it, succeeded.
func (s *Syncer) clearFailing(page []*storage.KnowledgeObject) {
	if o := findObject(page, s.failingID); o != nil && o.ChangeSeq == s.failingSeq {
		s.failingID, s.failingSeq, s.failures = "", 0, 0
	}
}

// prune drops the skipped objects the watermark has passed.
func (s *Syncer) prune(pos int64) {
	for id, seq := range s.skip {
		if seq <= pos {
			delete(s.skip, id)
		}
	}
}

func findObject(objs []*storage.KnowledgeObject, id string) *storage.KnowledgeObject {
	for _, o := range objs {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// pushObjects hands objects, with their edges and the entities those
// edges mention, to p. Entities accompany mention edges (ADR-049) so
// the receiver can resolve to_id slugs without a dangling reference.
func pushObjects(ctx context.Context, src storage.StorageDriver, p Pusher, page []*storage.KnowledgeObject) error {
	objects := make([]storage.KnowledgeObject, 0, len(page))
	edges := make([]storage.Edge, 0)
	for _, o := range page {
		objects = append(objects, *o)
		from, err := src.Edges().ListFrom(ctx, "object", o.ID)
		if err != nil {
			return fmt.Errorf("list source edges from %q: %w", o.ID, err)
		}
		for _, e := range from {
			edges = append(edges, *e)
		}
	}
	return p.Push(ctx, objects, edges, collectEntitiesForEdges(ctx, src, edges))
}
