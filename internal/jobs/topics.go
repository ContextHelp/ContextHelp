package jobs

import (
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// Event source identifier (CloudEvents source field) used by the worker pool
// when publishing job-lifecycle events. Convention §9: ad-hoc string literals
// at emit sites are replaced with the constants below so renames remain
// localised and topic taxonomy is grep-able.
const (
	// SourceWorkerPool is the CloudEvents source for worker-pool emissions.
	SourceWorkerPool = "worker.pool"
	// SourceWorkerPoolFanOut is the CloudEvents source for fan-out enqueue
	// emissions issued by the pool while distributing per-item jobs.
	SourceWorkerPoolFanOut = "worker.pool.fanout"
)

// Outbound event topics emitted by the job worker pool.
//
// All topics follow kit's 4-segment past-tense convention enforced by
// bus.ValidateTopic: source.category.object.action. They alias the
// canonical constants in internal/events so the jobs package can publish
// without importing internal/events at every call site.
const (
	TopicJobCompleted   bus.Topic = events.TopicJobCompleted
	TopicJobFailed      bus.Topic = events.TopicJobFailed
	TopicJobEnqueued    bus.Topic = events.TopicJobEnqueued
	TopicObjectIngested bus.Topic = events.TopicObjectIngested
)
