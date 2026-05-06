package dedup

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock returns time.Time values controlled by tests. Wraps a sync/atomic
// int64 so concurrent IsDuplicate calls observe a consistent advancing time
// without races.
type fakeClock struct{ ns atomic.Int64 }

func newFakeClock(t time.Time) *fakeClock {
	c := &fakeClock{}
	c.ns.Store(t.UnixNano())
	return c
}

func (c *fakeClock) Now() time.Time {
	return time.Unix(0, c.ns.Load())
}

func (c *fakeClock) Advance(d time.Duration) {
	c.ns.Add(int64(d))
}

func TestCache_FirstInsertReturnsFalse(t *testing.T) {
	t.Parallel()
	c := NewCache()
	if c.IsDuplicate("fp-1") {
		t.Error("first IsDuplicate(fp-1) returned true; want false")
	}
}

func TestCache_SecondInsertWithinWindowReturnsTrue(t *testing.T) {
	t.Parallel()
	c := NewCache()
	_ = c.IsDuplicate("fp-1")
	if !c.IsDuplicate("fp-1") {
		t.Error("second IsDuplicate(fp-1) within window returned false; want true")
	}
}

func TestCache_DistinctFingerprintsAreNotDuplicates(t *testing.T) {
	t.Parallel()
	c := NewCache()
	if c.IsDuplicate("fp-1") {
		t.Error("first fp-1: want not duplicate")
	}
	if c.IsDuplicate("fp-2") {
		t.Error("first fp-2: want not duplicate")
	}
}

func TestCache_EmptyFingerprintBypassesDedup(t *testing.T) {
	t.Parallel()
	c := NewCache()
	// Empty fingerprint is the source-side opt-out; substrate honors it.
	if c.IsDuplicate("") {
		t.Error("empty fingerprint: want not duplicate (bypass)")
	}
	if c.IsDuplicate("") {
		t.Error("empty fingerprint repeated: want still not duplicate (bypass)")
	}
	if c.Len() != 0 {
		t.Errorf("empty fingerprints should not occupy cache slots; Len = %d", c.Len())
	}
}

func TestCache_ExpirationAllowsReinsert(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC))
	c := NewCache(WithWindow(60*time.Second), withClock(clk.Now))

	if c.IsDuplicate("fp-1") {
		t.Fatal("first insert: want not duplicate")
	}
	clk.Advance(30 * time.Second)
	if !c.IsDuplicate("fp-1") {
		t.Error("after 30s (within window): want duplicate")
	}
	// IsDuplicate refreshes the entry's insertedAt; from this point we
	// need another 60s of inactivity for expiration to kick in.
	clk.Advance(61 * time.Second)
	if c.IsDuplicate("fp-1") {
		t.Error("after 91s (with last-touched at 30s, window 60s, so 61s past last-touch): want not duplicate")
	}
}

func TestCache_LRUEvictsOldestWhenAtCapacity(t *testing.T) {
	t.Parallel()
	c := NewCache(WithCapacity(3))

	_ = c.IsDuplicate("a")
	_ = c.IsDuplicate("b")
	_ = c.IsDuplicate("c")
	if c.Len() != 3 {
		t.Fatalf("Len after 3 inserts = %d, want 3", c.Len())
	}
	// Inserting fourth evicts oldest ("a").
	_ = c.IsDuplicate("d")
	if c.Len() != 3 {
		t.Errorf("Len after 4th insert (cap=3) = %d, want 3", c.Len())
	}
	// "a" should now be absent → re-inserting reports not-duplicate.
	if c.IsDuplicate("a") {
		t.Error("after eviction, IsDuplicate(a) returned true; want false (a was evicted)")
	}
}

func TestCache_RecentAccessRefreshesLRUPosition(t *testing.T) {
	t.Parallel()
	c := NewCache(WithCapacity(3))

	_ = c.IsDuplicate("a") // a is oldest
	_ = c.IsDuplicate("b")
	_ = c.IsDuplicate("c")

	// Touch "a" — moves it to front.
	_ = c.IsDuplicate("a")

	// Insert "d" at capacity — should evict the now-oldest ("b"), not "a".
	_ = c.IsDuplicate("d")

	if !c.IsDuplicate("a") {
		t.Error("a should still be present after refresh-then-insert-d (LRU put 'b' as oldest)")
	}
	if c.IsDuplicate("b") {
		t.Error("b should have been evicted (was oldest when d was inserted)")
	}
}

func TestCache_SweepRemovesExpired(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC))
	c := NewCache(WithWindow(60*time.Second), withClock(clk.Now))

	_ = c.IsDuplicate("a")
	clk.Advance(30 * time.Second)
	_ = c.IsDuplicate("b")
	clk.Advance(40 * time.Second)
	// Now "a" is 70s old (expired); "b" is 40s old (within window).
	removed := c.Sweep()
	if removed != 1 {
		t.Errorf("Sweep removed %d, want 1", removed)
	}
	if c.Len() != 1 {
		t.Errorf("after Sweep, Len = %d, want 1", c.Len())
	}
}

func TestCache_DefaultsAreApplied(t *testing.T) {
	t.Parallel()
	c := NewCache(WithWindow(0), WithCapacity(0)) // both invalid; should fall back to defaults
	if c.window != DefaultWindow {
		t.Errorf("window = %s, want %s", c.window, DefaultWindow)
	}
	if c.capacity != DefaultCapacity {
		t.Errorf("capacity = %d, want %d", c.capacity, DefaultCapacity)
	}
}

func TestCache_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	c := NewCache(WithCapacity(1024))

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := range 100 {
				fp := []byte("fp-")
				fp = append(fp, byte('0'+seed))
				fp = append(fp, byte('0'+(j%10)))
				c.IsDuplicate(string(fp))
			}
		}(i)
	}
	wg.Wait()
	// No assertion on exact size — just that no panics or races occurred.
	if c.Len() == 0 {
		t.Error("expected some entries after concurrent inserts; got 0")
	}
}

// recordingDedup vs Cache contract verification: Cache satisfies the
// ambient.Dedup interface (verified by compile-time check on the import side
// in the substrate runner_test.go's stubCutter pattern; here we just make
// sure our Cache type can be substituted).
var _ interface{ IsDuplicate(string) bool } = (*Cache)(nil)
