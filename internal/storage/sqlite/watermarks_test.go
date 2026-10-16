package sqlite

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_Watermark runs the cross-driver watermark contract
// against a fresh SQLite database.
func TestConformance_Watermark(t *testing.T) {
	storagetest.WatermarkConformance(t, newTestDriver(t))
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
			if err := d.Watermarks().SetWatermark(ctx, "fed-concurrent", int64(i+1)); err != nil {
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
	assert.NotZero(t, got, "after concurrent writes, a watermark must be stored")
}
