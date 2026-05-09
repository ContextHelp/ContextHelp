package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hop.top/kit/go/runtime/job"
	"hop.top/kit/go/runtime/job/mock"
)

func TestHandlerMap_RoutesByCause(t *testing.T) {
	eng := mock.New()
	var rateLimitCalls int32
	handlers := HandlerMap(Handlers{
		RateLimit: func(_ context.Context, _ job.Job) error {
			atomic.AddInt32(&rateLimitCalls, 1)
			return nil
		},
	})

	if _, err := EnqueueDeferred(context.Background(), eng, Deferred{
		CandidateID: "lc-1",
		Cause:       CauseRateLimit,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	p := job.Poller{
		Service:  eng,
		Queue:    QueueDeferred,
		WorkerID: "test-worker",
		Handlers: handlers,
		Interval: 10 * time.Millisecond,
	}
	_ = p.Run(ctx) // returns ctx.Err() at deadline

	if atomic.LoadInt32(&rateLimitCalls) == 0 {
		t.Fatal("rate-limit handler never invoked")
	}
}
