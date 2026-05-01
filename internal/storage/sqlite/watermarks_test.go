package sqlite

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatermarkStore_MissingReturnsEpoch verifies that GetWatermark returns
// the Unix epoch (never synced) for a federation name with no row.
func TestWatermarkStore_MissingReturnsEpoch(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	got, err := d.Watermarks().GetWatermark(ctx, "no-such-fed")
	require.NoError(t, err)

	want := time.Unix(0, 0).UTC()
	assert.Equal(t, want, got, "missing watermark must return epoch")
}

// TestWatermarkStore_UpsertCreates verifies that SetWatermark inserts a new row
// when none exists, and the value round-trips through GetWatermark.
func TestWatermarkStore_UpsertCreates(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// RFC3339Nano round-trip — strip sub-nanosecond and timezone variance.
	ts := time.Date(2026, 5, 1, 12, 0, 0, 123456789, time.UTC)
	require.NoError(t, d.Watermarks().SetWatermark(ctx, "fed-a", ts))

	got, err := d.Watermarks().GetWatermark(ctx, "fed-a")
	require.NoError(t, err)
	assert.True(t, got.Equal(ts), "round-trip: got %v, want %v", got, ts)
}

// TestWatermarkStore_UpsertUpdates verifies that calling SetWatermark twice
// updates the existing row (no duplicate-key error).
func TestWatermarkStore_UpsertUpdates(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, d.Watermarks().SetWatermark(ctx, "fed-b", ts1))
	require.NoError(t, d.Watermarks().SetWatermark(ctx, "fed-b", ts2))

	got, err := d.Watermarks().GetWatermark(ctx, "fed-b")
	require.NoError(t, err)
	assert.True(t, got.Equal(ts2), "advance: got %v, want %v", got, ts2)
}

// TestWatermarkStore_PerFederationIsolation verifies that watermarks for
// different federation names do not interfere.
func TestWatermarkStore_PerFederationIsolation(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	tsA := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tsB := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, d.Watermarks().SetWatermark(ctx, "fed-a", tsA))
	require.NoError(t, d.Watermarks().SetWatermark(ctx, "fed-b", tsB))

	gotA, err := d.Watermarks().GetWatermark(ctx, "fed-a")
	require.NoError(t, err)
	assert.True(t, gotA.Equal(tsA))

	gotB, err := d.Watermarks().GetWatermark(ctx, "fed-b")
	require.NoError(t, err)
	assert.True(t, gotB.Equal(tsB))
}

// TestWatermarkStore_ConcurrentSet verifies serialised upserts don't error.
// SQLite handles concurrent writes through a busy_timeout pragma and per-call
// retry; this test exercises the upsert path under contention.
func TestWatermarkStore_ConcurrentSet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	const N = 10
	wg.Add(N)
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			ts := time.Unix(int64(i+1), 0).UTC()
			if err := d.Watermarks().SetWatermark(ctx, "fed-concurrent", ts); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent SetWatermark: %v", err)
	}

	// One of the values must be the final state; ensure something was stored.
	got, err := d.Watermarks().GetWatermark(ctx, "fed-concurrent")
	require.NoError(t, err)
	assert.False(t, got.Equal(time.Unix(0, 0).UTC()),
		"after concurrent writes, watermark must be > epoch")
}
