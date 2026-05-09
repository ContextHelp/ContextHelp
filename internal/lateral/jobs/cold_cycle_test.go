package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"hop.top/kit/go/runtime/job"
	"hop.top/kit/go/runtime/job/mock"
)

type capturePub struct {
	topics []string
}

func (c *capturePub) Publish(_ context.Context, topic, _ string, _ any) error {
	c.topics = append(c.topics, topic)
	return nil
}

type errPub struct{}

func (errPub) Publish(_ context.Context, _, _ string, _ any) error {
	return errors.New("boom")
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
	scan := func(_ context.Context) (CycleStats, error) {
		atomic.AddInt32(&heartbeatCalls, 1)
		return CycleStats{Expired: 1, DurationMS: 5}, eng.Heartbeat(context.Background(), id)
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
		if top == TopicReaperCycleCompleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s event, got %v", TopicReaperCycleCompleted, pub.topics)
	}
}

func TestColdCycleHandler_ScanErrorReturnsErrorAndNoEvent(t *testing.T) {
	eng := mock.New()
	pub := &capturePub{}

	wantErr := errors.New("scan failed")
	scan := func(_ context.Context) (CycleStats, error) {
		return CycleStats{}, wantErr
	}
	h := ColdCycleHandler(eng, pub, scan)
	if err := h(context.Background(), job.Job{ID: "j-1"}); err != wantErr {
		t.Fatalf("handler err = %v, want %v", err, wantErr)
	}
	if len(pub.topics) != 0 {
		t.Fatalf("expected no events on scan error, got %v", pub.topics)
	}
}

func TestColdCycleHandler_NilPubDoesNotPanic(t *testing.T) {
	eng := mock.New()

	scan := func(_ context.Context) (CycleStats, error) {
		return CycleStats{Expired: 0, DurationMS: 1}, nil
	}
	h := ColdCycleHandler(eng, nil, scan)
	if err := h(context.Background(), job.Job{ID: "j-2"}); err != nil {
		t.Fatalf("handler with nil pub returned err: %v", err)
	}
}

func TestColdCycleHandler_PublishErrorIgnored(t *testing.T) {
	eng := mock.New()

	scan := func(_ context.Context) (CycleStats, error) {
		return CycleStats{Expired: 2, DurationMS: 3}, nil
	}
	h := ColdCycleHandler(eng, errPub{}, scan)
	if err := h(context.Background(), job.Job{ID: "j-3"}); err != nil {
		t.Fatalf("handler should swallow publish error, got: %v", err)
	}
}
