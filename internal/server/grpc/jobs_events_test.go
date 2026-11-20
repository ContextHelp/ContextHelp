package grpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// WatchJob must recognise the events the worker pool actually publishes:
// its topics and its payload shape.
func TestForwardIfMatchReadsWorkerJobEvents(t *testing.T) {
	cases := []struct {
		topic   string
		payload any
		status  storage.JobStatus
		errText string
	}{
		{string(events.TopicJobCompleted), events.JobCompletedPayload{JobID: "job-1", ObjectCount: 1}, storage.JobCompleted, ""},
		{string(events.TopicJobFailed), events.JobFailedPayload{JobID: "job-1", Error: "boom"}, storage.JobFailed, "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.topic, func(t *testing.T) {
			ev := bus.NewEvent(bus.Topic(tc.topic), "worker.pool", tc.payload)
			ch := make(chan *pb.JobStatusUpdate, 1)
			if err := forwardIfMatch("job-1", ev, ch); err != nil {
				t.Fatal(err)
			}
			select {
			case upd := <-ch:
				if upd.Status != string(tc.status) {
					t.Errorf("status = %q, want %q", upd.Status, tc.status)
				}
				if upd.Error != tc.errText {
					t.Errorf("error = %q, want %q", upd.Error, tc.errText)
				}
			default:
				t.Fatal("worker event for the watched job was not forwarded")
			}
		})
	}
}

// countingBus counts the subscriptions a bus holds now.
type countingBus struct {
	bus.Bus
	active atomic.Int64
}

func (c *countingBus) Subscribe(p string, h bus.Handler) bus.Unsubscribe {
	c.active.Add(1)
	un := c.Bus.Subscribe(p, h)
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			c.active.Add(-1)
		}
		un()
	}
}

// watchStream is a WatchJob server stream cut by its context.
type watchStream struct {
	pb.JobService_WatchJobServer
	ctx context.Context
}

func (w watchStream) Context() context.Context       { return w.ctx }
func (w watchStream) Send(*pb.JobStatusUpdate) error { return nil }

// WatchJob leaves nothing on the bus when its stream ends: N watches
// that disconnect return the subscriber count to where it began.
func TestWatchJobUnsubscribesWhenTheStreamEnds(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	cb := &countingBus{Bus: svc.Bus}
	svc.Bus = cb
	owner := sharing.WithViewer(context.Background(), sharing.Unrestricted)
	jobID, err := svc.Analyze(owner, service.AnalyzeRequest{Content: "watched body", Type: "text"})
	if err != nil {
		t.Fatal(err)
	}
	h := newJobHandler(svc)
	base := cb.active.Load()

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(owner)
		done := make(chan error, 1)
		go func() { done <- h.WatchJob(&pb.WatchJobRequest{Id: jobID}, watchStream{ctx: ctx}) }()
		deadline := time.Now().Add(3 * time.Second)
		for cb.active.Load() <= base && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("WatchJob: %v", err)
		}
	}
	if got := cb.active.Load(); got != base {
		t.Fatalf("subscribers after 5 watches: %d, want %d", got, base)
	}
}
