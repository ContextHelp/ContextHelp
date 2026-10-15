package sharing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// memAccessLog is an AccessLogStore that keeps every batch it was
// appended, and can fail.
type memAccessLog struct {
	mu      sync.Mutex
	batches [][]*storage.AccessEntry
	fail    error
}

func (m *memAccessLog) Append(_ context.Context, entries []*storage.AccessEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.batches = append(m.batches, entries)
	return nil
}

func (m *memAccessLog) Query(context.Context, storage.AccessFilter) ([]*storage.AccessEntry, error) {
	return nil, nil
}

func (m *memAccessLog) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

func (m *memAccessLog) count() (batches, entries int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.batches {
		entries += len(b)
	}
	return len(m.batches), entries
}

func entry(principal, route string) *storage.AccessEntry {
	return &storage.AccessEntry{PrincipalID: principal, Route: route, PolicyVersions: "p"}
}

// A full batch flushes on its own; a partial one waits for the
// interval; Flush writes it at once; Close drains.
func TestAuditWriterBatchesAndFlushes(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Batch: 3, Interval: time.Hour})
	for i := 0; i < 3; i++ {
		w.Record(entry("prn", "GET /objects"))
	}
	require.Eventually(t, func() bool { b, _ := store.count(); return b == 1 }, 2*time.Second, 5*time.Millisecond)
	_, n := store.count()
	assert.Equal(t, 3, n)

	w.Record(entry("prn", "GET /objects/{id}"))
	assert.Equal(t, 1, w.Buffered())
	require.NoError(t, w.Flush(context.Background()))
	assert.Equal(t, 0, w.Buffered())
	b, n := store.count()
	assert.Equal(t, 2, b)
	assert.Equal(t, 4, n)

	w.Record(entry("prn", "POST /find"))
	require.NoError(t, w.Close(context.Background()))
	_, n = store.count()
	assert.Equal(t, 5, n)
	w.Record(entry("prn", "after close"))
	assert.Equal(t, uint64(1), w.Dropped())
	assert.NoError(t, w.Close(context.Background()), "idempotent")
}

// The interval flushes a partial buffer without a kick.
func TestAuditWriterIntervalFlush(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Batch: 100, Interval: 20 * time.Millisecond})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	w.Record(entry("prn", "GET /objects"))
	require.Eventually(t, func() bool { _, n := store.count(); return n == 1 }, 2*time.Second, 5*time.Millisecond)
}

// A full buffer drops, a failed flush loses its batch; both count.
func TestAuditWriterDropsAndLosses(t *testing.T) {
	store := &memAccessLog{fail: errors.New("disk full")}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Batch: 100, Interval: time.Hour, Capacity: 2})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	for i := 0; i < 3; i++ {
		w.Record(entry("prn", "GET /objects"))
	}
	assert.Equal(t, uint64(1), w.Dropped())
	assert.Error(t, w.Flush(context.Background()))
	assert.Equal(t, uint64(3), w.Dropped())
	assert.Equal(t, 0, w.Buffered())
}

// A nil writer records nothing and never fails.
func TestAuditWriterNil(t *testing.T) {
	var w *sharing.AuditWriter
	w.Record(entry("prn", "x"))
	assert.NoError(t, w.Flush(context.Background()))
	assert.NoError(t, w.Close(context.Background()))
	assert.Nil(t, sharing.NewAuditWriter(nil, sharing.AuditOptions{}))
}

// The entry of a request carries the viewer, the route, the policy
// versions and the released rows; a preview is the owner's request.
func TestAccessEntryFor(t *testing.T) {
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_a", IdentityID: "idn_1", Grants: []sharing.Grant{{ID: "g1", Audience: "colleagues", Policy: tpl}}})
	require.NoError(t, err)
	l := sharing.NewLedger()
	l.AddObject("o2")
	l.AddObject("o1")
	l.AddEntity("person.sarah")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	e := sharing.AccessEntryFor(context.Background(), v, "GET /objects", l, now)
	assert.Equal(t, "prn_a", e.PrincipalID)
	assert.Equal(t, "idn_1", e.IdentityID)
	assert.Equal(t, "GET /objects", e.Route)
	assert.Equal(t, v.PolicyKey(), e.PolicyVersions)
	assert.Equal(t, []string{"o1", "o2"}, e.ObjectIDs)
	assert.Equal(t, 2, e.ObjectCount)
	assert.Equal(t, 1, e.EntityCount)
	assert.Equal(t, now, e.At)

	ctx := sharing.WithPreview(context.Background(), sharing.Preview{Target: "prn_a", Principal: storage.OwnerPrincipal, Identity: "idn_owner"})
	p := sharing.AccessEntryFor(ctx, v, "GET /objects", l, now)
	assert.Equal(t, storage.OwnerPrincipal, p.PrincipalID)
	assert.Equal(t, "idn_owner", p.IdentityID)
	assert.Equal(t, "preview=prn_a;"+v.PolicyKey(), p.PolicyVersions)
}
