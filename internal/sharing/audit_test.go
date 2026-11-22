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
	steward []*storage.AccessEntry
	fail    error
	// stewardFails is how many steward appends fail (errStoreDown)
	// before the store takes them again.
	stewardFails int
}

// errStoreDown is what a failing steward append returns.
var errStoreDown = errors.New("access log: store down")

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

func (m *memAccessLog) AppendSteward(_ context.Context, e *storage.AccessEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if m.stewardFails > 0 {
		m.stewardFails--
		return errStoreDown
	}
	if err := storage.PrepareStewardEntry(e, time.Now()); err != nil {
		return err
	}
	m.steward = append(m.steward, e)
	return nil
}

func (m *memAccessLog) PruneSteward(context.Context, time.Time) (int64, error) { return 0, nil }

func (m *memAccessLog) StewardChain(context.Context) (storage.AccessChainHead, []*storage.AccessEntry, error) {
	return storage.AccessChainHead{}, nil, nil
}

func (m *memAccessLog) count() (batches, entries int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.batches {
		entries += len(b)
	}
	return len(m.batches), entries
}

func entry(principal, route string) *storage.AccessEntry {
	return &storage.AccessEntry{
		PrincipalID: principal, Route: route, PolicyVersions: "p",
		Decision: storage.AccessDecisionAllowed, OperationDigest: storage.DigestPrefix + "00",
	}
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

// One malformed entry is dropped and counted alone: the valid entries
// of its batch, other viewers' included, are still written.
func TestAuditWriterFlushDropsOnlyInvalidEntries(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Batch: 100, Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	bad := entry("prn_b", "GET /events")
	bad.Decision = storage.AccessDecisionDeferred // no UNKNOWN check
	bad.Checks = []storage.AccessCheck{{Check: "backstop", Outcome: storage.AccessOutcomeFail, Reason: "hit_stripped"}}
	noID := entry("prn_c", "GET /objects")
	noID.Checks = []storage.AccessCheck{{Check: "", Outcome: storage.AccessOutcomePass, Reason: "x"}}
	w.Record(entry("prn_a", "GET /objects"))
	w.Record(bad)
	w.Record(noID)
	w.Record(entry("prn_d", "GET /objects/{id}"))

	require.NoError(t, w.Flush(context.Background()))
	_, n := store.count()
	assert.Equal(t, 2, n)
	assert.Equal(t, uint64(2), w.Dropped())
	for _, e := range store.batches[0] {
		assert.NotEqual(t, "prn_b", e.PrincipalID)
		assert.NotEqual(t, "prn_c", e.PrincipalID)
	}
}

// A batch of only invalid entries writes nothing and reports no store
// error.
func TestAuditWriterFlushAllInvalid(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Batch: 100, Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	bad := entry("prn_b", "GET /events")
	bad.OperationDigest = ""
	w.Record(bad)
	require.NoError(t, w.Flush(context.Background()))
	b, _ := store.count()
	assert.Zero(t, b)
	assert.Equal(t, uint64(1), w.Dropped())
}

// A nil writer records nothing and never fails.
func TestAuditWriterNil(t *testing.T) {
	var w *sharing.AuditWriter
	w.Record(entry("prn", "x"))
	assert.NoError(t, w.Flush(context.Background()))
	assert.NoError(t, w.Close(context.Background()))
	assert.Nil(t, sharing.NewAuditWriter(nil, sharing.AuditOptions{}))
}
