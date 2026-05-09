package jobs

import (
	"context"
	"sync/atomic"
	"testing"

	"hop.top/kit/go/runtime/job/mock"
)

type capturePub struct {
	topics []string
}

func (c *capturePub) Publish(_ context.Context, topic, _ string, _ any) error {
	c.topics = append(c.topics, topic)
	return nil
}

func TestColdCycleHandler_HeartbeatsAndEmitsCompleted(t *testing.T) {
	eng := mock.New()
	pub := &capturePub{}

	id, err := EnqueueDeferred(context.Background(), eng, Deferred{
		CandidateID: "lc-1",
		Cause:       CauseColdCycleExpiry,
	})
	if err != nil {
		t.Fatal(err)
	}
	j, err := eng.Claim(context.Background(), QueueDeferred, "test-worker")
	if err != nil || j == nil {
		t.Fatalf("claim: j=%v err=%v", j, err)
	}

	var heartbeatCalls int32
	scan := func(_ context.Context) error {
		atomic.AddInt32(&heartbeatCalls, 1)
		return eng.Heartbeat(context.Background(), id)
	}
	h := ColdCycleHandler(eng, pub, scan)
	if err := h(context.Background(), *j); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if atomic.LoadInt32(&heartbeatCalls) == 0 {
		t.Fatal("expected heartbeat invocation")
	}
	found := false
	for _, top := range pub.topics {
		if top == "ctxt.lateral.reaper.cycle.completed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cycle.completed event, got %v", pub.topics)
	}
}
