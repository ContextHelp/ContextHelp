package jobs_test

import (
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
)

// TestJobsTopicsAreKitConformant ensures the jobs-package topic constants
// satisfy bus.ValidateTopic so that emissions from the worker pool do not
// get rejected when kit flips the validator from warn to strict.
func TestJobsTopicsAreKitConformant(t *testing.T) {
	cases := map[string]bus.Topic{
		"TopicJobCompleted":   jobs.TopicJobCompleted,
		"TopicJobFailed":      jobs.TopicJobFailed,
		"TopicJobEnqueued":    jobs.TopicJobEnqueued,
		"TopicObjectIngested": jobs.TopicObjectIngested,
	}
	for name, topic := range cases {
		t.Run(name, func(t *testing.T) {
			if err := bus.ValidateTopic(topic); err != nil {
				t.Errorf("%s = %q: %v", name, topic, err)
			}
		})
	}
}

// TestJobsTopicsMatchEventsPackage ensures the jobs-local aliases stay in
// sync with the canonical declarations in internal/events. If someone edits
// one without the other, this test catches the drift.
func TestJobsTopicsMatchEventsPackage(t *testing.T) {
	cases := []struct {
		name        string
		jobs, event bus.Topic
	}{
		{"JobCompleted", jobs.TopicJobCompleted, events.TopicJobCompleted},
		{"JobFailed", jobs.TopicJobFailed, events.TopicJobFailed},
		{"JobEnqueued", jobs.TopicJobEnqueued, events.TopicJobEnqueued},
		{"ObjectIngested", jobs.TopicObjectIngested, events.TopicObjectIngested},
	}
	for _, c := range cases {
		if c.jobs != c.event {
			t.Errorf("%s: jobs=%q events=%q drift", c.name, c.jobs, c.event)
		}
	}
}
