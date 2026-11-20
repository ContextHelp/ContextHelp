package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestPrunerDeletesRowsPastRetention(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	ob := drv.(interface{ Outbox() storage.OutboxStore }).Outbox()
	ctx := context.Background()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	add := func(age time.Duration) int64 {
		ev := &storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "t", Payload: 1, OccurredAt: now.Add(-age)}
		require.NoError(t, ob.Append(ctx, ev))
		return ev.Seq
	}
	old := add(8 * 24 * time.Hour)
	edge := add(6*24*time.Hour + 23*time.Hour)
	fresh := add(time.Hour)

	p := &events.Pruner{Store: ob, Retention: 7 * 24 * time.Hour, Now: func() time.Time { return now }}
	n, err := p.PruneOnce(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	rows, err := ob.Since(ctx, old, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, edge, rows[0].Seq)
	assert.Equal(t, fresh, rows[1].Seq)
	_, err = ob.Since(ctx, old-1, 0)
	assert.ErrorIs(t, err, storage.ErrOutboxExpired)
}

func TestPrunerRunPrunesAtStartAndStopsWithContext(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	ob := drv.(interface{ Outbox() storage.OutboxStore }).Outbox()
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < 2; i++ {
		require.NoError(t, ob.Append(ctx, &storage.OutboxEvent{Topic: "ctxt.runtime.object.updated", Source: "t", Payload: i, OccurredAt: time.Now().Add(-30 * 24 * time.Hour)}))
	}
	done := make(chan error)
	go func() { done <- (&events.Pruner{Store: ob, Retention: time.Hour, Every: time.Hour}).Run(ctx) }()
	require.Eventually(t, func() bool {
		old, _ := ob.Oldest(ctx)
		return old == 2
	}, 3*time.Second, 10*time.Millisecond, "startup prune")
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}
