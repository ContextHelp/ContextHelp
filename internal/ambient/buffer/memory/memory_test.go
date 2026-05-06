package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

func TestBuffer_AppendThenPopReturnsSameEvent(t *testing.T) {
	t.Parallel()
	b := New()
	ev := ambient.RawEvent{Source: "test", Fingerprint: "fp-1", Kind: ambient.KindText}
	if err := b.Append(context.Background(), ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := b.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.Fingerprint != "fp-1" {
		t.Errorf("Pop returned fingerprint %q, want fp-1", got.Fingerprint)
	}
}

func TestBuffer_PopEmptyReturnsErrBufferEmpty(t *testing.T) {
	t.Parallel()
	b := New()
	_, err := b.Pop(context.Background())
	if !errors.Is(err, ambient.ErrBufferEmpty) {
		t.Errorf("Pop on empty buffer: want ErrBufferEmpty, got %v", err)
	}
}

func TestBuffer_FIFOOrder(t *testing.T) {
	t.Parallel()
	b := New()
	for _, fp := range []string{"a", "b", "c"} {
		_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: fp})
	}
	for _, want := range []string{"a", "b", "c"} {
		got, _ := b.Pop(context.Background())
		if got.Fingerprint != want {
			t.Errorf("Pop returned %q, want %q (FIFO)", got.Fingerprint, want)
		}
	}
}

func TestBuffer_OverflowEvictsOldest(t *testing.T) {
	t.Parallel()
	b := New(WithCapacity(3))
	for _, fp := range []string{"a", "b", "c", "d"} {
		_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: fp})
	}
	// "a" should have been evicted; remaining: b, c, d
	if got := b.Len(); got != 3 {
		t.Errorf("Len after 4 appends (cap=3) = %d, want 3", got)
	}
	if stats := b.Stats(); stats.EvictedTotal != 1 {
		t.Errorf("EvictedTotal = %d, want 1", stats.EvictedTotal)
	}
	got, _ := b.Pop(context.Background())
	if got.Fingerprint != "b" {
		t.Errorf("Pop after eviction returned %q, want b (a evicted)", got.Fingerprint)
	}
}

func TestBuffer_LenTracksCount(t *testing.T) {
	t.Parallel()
	b := New(WithCapacity(10))
	if b.Len() != 0 {
		t.Errorf("empty Len = %d, want 0", b.Len())
	}
	for i := range 5 {
		_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: string(rune('a' + i))})
	}
	if b.Len() != 5 {
		t.Errorf("Len after 5 appends = %d, want 5", b.Len())
	}
	_, _ = b.Pop(context.Background())
	if b.Len() != 4 {
		t.Errorf("Len after Pop = %d, want 4", b.Len())
	}
}

func TestBuffer_RangeYieldsOldestFirstWithoutPopping(t *testing.T) {
	t.Parallel()
	b := New()
	for _, fp := range []string{"a", "b", "c"} {
		_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: fp})
	}
	var seen []string
	_ = b.Range(context.Background(), func(ev ambient.RawEvent) bool {
		seen = append(seen, ev.Fingerprint)
		return true
	})
	if got := b.Len(); got != 3 {
		t.Errorf("Range modified Len: %d, want 3", got)
	}
	if len(seen) != 3 || seen[0] != "a" || seen[2] != "c" {
		t.Errorf("Range yielded %v, want [a b c]", seen)
	}
}

func TestBuffer_RangeStopsOnFalse(t *testing.T) {
	t.Parallel()
	b := New()
	for _, fp := range []string{"a", "b", "c"} {
		_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: fp})
	}
	var seen []string
	_ = b.Range(context.Background(), func(ev ambient.RawEvent) bool {
		seen = append(seen, ev.Fingerprint)
		return ev.Fingerprint != "b" // stop after b
	})
	if len(seen) != 2 {
		t.Errorf("Range yielded %d events, want 2 (early-stop)", len(seen))
	}
}

func TestBuffer_StatsReportsExpectedFields(t *testing.T) {
	t.Parallel()
	b := New(WithCapacity(5))
	now := time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)
	_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: "a", OccurredAt: now})
	_ = b.Append(context.Background(), ambient.RawEvent{Fingerprint: "b", OccurredAt: now.Add(time.Second)})
	_, _ = b.Pop(context.Background())

	stats := b.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
	if stats.Count != 1 {
		t.Errorf("Count = %d, want 1", stats.Count)
	}
	if stats.Capacity != 5 {
		t.Errorf("Capacity = %d, want 5", stats.Capacity)
	}
	if !stats.OldestEventAt.Equal(now.Add(time.Second)) {
		t.Errorf("OldestEventAt = %v, want %v (b's OccurredAt)", stats.OldestEventAt, now.Add(time.Second))
	}
	if stats.AppendedTotal != 2 {
		t.Errorf("AppendedTotal = %d, want 2", stats.AppendedTotal)
	}
	if stats.PoppedTotal != 1 {
		t.Errorf("PoppedTotal = %d, want 1", stats.PoppedTotal)
	}
	if stats.EvictedTotal != 0 {
		t.Errorf("EvictedTotal = %d, want 0", stats.EvictedTotal)
	}
}

func TestBuffer_DefaultCapacityWhenZero(t *testing.T) {
	t.Parallel()
	b := New(WithCapacity(0))
	if b.capacity != DefaultCapacity {
		t.Errorf("capacity = %d, want DefaultCapacity %d", b.capacity, DefaultCapacity)
	}
}

func TestBuffer_ConcurrentAppendsAndPops(t *testing.T) {
	t.Parallel()
	b := New(WithCapacity(1024))

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := range 50 {
				_ = b.Append(context.Background(), ambient.RawEvent{
					Fingerprint: string(rune('a'+seed)) + string(rune('0'+(j%10))),
				})
			}
		}(i)
	}
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, _ = b.Pop(context.Background())
			}
		}()
	}
	wg.Wait()
	// No assertion on exact size — just race-safety.
}

// Compile-time check: Buffer satisfies the ambient.Buffer interface.
var _ ambient.Buffer = (*Buffer)(nil)
