package events

import "hop.top/kit/bus"

// Outbound topic constants for ctxt domain events.
const (
	TopicObjectIngested bus.Topic = "ctxt.object.ingested"
	TopicObjectUpdated  bus.Topic = "ctxt.object.updated"
	TopicObjectDeleted  bus.Topic = "ctxt.object.deleted"
	TopicJobCompleted   bus.Topic = "ctxt.job.completed"
	TopicJobFailed      bus.Topic = "ctxt.job.failed"
)

// Inbound subscription topics.
const (
	TopicApsProfileAll = "aps.profile.*"
)

// ObjectIngestedPayload is published after an object is ingested.
type ObjectIngestedPayload struct {
	ObjectID   string   `json:"object_id"`
	Type       string   `json:"type"`
	Pipeline   string   `json:"pipeline"`
	Tags       []string `json:"tags"`
	ProfileID  string   `json:"profile_id"`
	DurationMs int64    `json:"duration_ms"`
}

// ObjectUpdatedPayload is published after an object is updated.
type ObjectUpdatedPayload struct {
	ObjectID string   `json:"object_id"`
	Type     string   `json:"type"`
	Fields   []string `json:"fields"`
}

// ObjectDeletedPayload is published after an object is deleted.
type ObjectDeletedPayload struct {
	ObjectID string `json:"object_id"`
}

// JobCompletedPayload is published after a pipeline job completes.
type JobCompletedPayload struct {
	JobID       string `json:"job_id"`
	ObjectCount int    `json:"object_count"`
	DurationMs  int64  `json:"duration_ms"`
}

// JobFailedPayload is published after a pipeline job fails.
type JobFailedPayload struct {
	JobID    string `json:"job_id"`
	Error    string `json:"error"`
	ObjectID string `json:"object_id"`
}
