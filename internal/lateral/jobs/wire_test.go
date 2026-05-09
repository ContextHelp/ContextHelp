package jobs

import (
	"context"
	"testing"
	"time"

	"hop.top/kit/go/runtime/job"
	"hop.top/kit/go/runtime/job/mock"
)

func TestEnqueueDeferred_RoundTrip(t *testing.T) {
	eng := mock.New()
	id, err := EnqueueDeferred(context.Background(), eng, Deferred{
		CandidateID: "lc-1",
		Cause:       CauseRateLimit,
		ScheduledAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Queue != QueueDeferred {
		t.Fatalf("queue=%q want %q", got.Queue, QueueDeferred)
	}
	if got.Type != string(CauseRateLimit) {
		t.Fatalf("type=%q want %q", got.Type, CauseRateLimit)
	}
}

func TestEnqueueDeferred_ListByCause(t *testing.T) {
	eng := mock.New()
	for _, c := range []Cause{CauseRateLimit, CauseColdCycleExpiry, CauseRefreshDue} {
		_, err := EnqueueDeferred(context.Background(), eng, Deferred{
			CandidateID: "lc-x",
			Cause:       c,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := eng.List(context.Background(), job.JobQuery{
		Queue: QueueDeferred,
		Type:  string(CauseRateLimit),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 rate-limit job, got %d", len(got))
	}
}
