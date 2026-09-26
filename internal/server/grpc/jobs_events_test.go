package grpc

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
			ev, err := events.NewEvent("worker.pool", tc.topic, tc.payload)
			if err != nil {
				t.Fatal(err)
			}
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
