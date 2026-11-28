package events

import "hop.top/kit/go/runtime/bus"

// Automatic re-derive: the background job that re-runs the pipeline of
// objects whose derivation signature is no longer current. Every event
// carries RederivePayload.
const (
	// TopicDpkmsUpgradeRederiveScheduled fires when daemon startup
	// enqueues the re-derive job.
	TopicDpkmsUpgradeRederiveScheduled bus.Topic = "dpkms.upgrade.rederive.scheduled"
	// TopicDpkmsUpgradeRederiveCompleted fires when a run has handled
	// every object it could: the payload holds its summary.
	TopicDpkmsUpgradeRederiveCompleted bus.Topic = "dpkms.upgrade.rederive.completed"
	// TopicDpkmsUpgradeRederiveInterrupted fires when a run stops early
	// (shutdown, a provider outage): the next start resumes it.
	TopicDpkmsUpgradeRederiveInterrupted bus.Topic = "dpkms.upgrade.rederive.interrupted"
)

// RederiveDeferred is a pipeline a run left alone, and why.
type RederiveDeferred struct {
	Pipeline string `json:"pipeline"`
	Objects  int    `json:"objects"`
	Reason   string `json:"reason"`
}

// RederiveStopped is a group a run stopped (five tries in a row), and why.
type RederiveStopped struct {
	Pipeline string `json:"pipeline"`
	Kind     string `json:"kind"`
	Cause    string `json:"cause"`
	Groups   int    `json:"groups,omitempty"`
}

// RederivePayload describes a re-derive run.
type RederivePayload struct {
	JobID  string `json:"job_id,omitempty"`
	Target string `json:"target,omitempty"`
	// Total is the objects the run set out to handle, the deferred ones
	// not counted.
	Total     int `json:"total"`
	Processed int `json:"processed"`
	// Stamped counts objects set to the current signature without
	// running anything (the pipeline only lost steps).
	Stamped int `json:"stamped,omitempty"`
	Failed  int `json:"failed"`
	// Transient counts the tries that failed in a way that says nothing
	// certain about the object: left stale, no record.
	Transient int `json:"transient,omitempty"`
	// Gone counts objects deleted since they were listed.
	Gone int `json:"gone,omitempty"`
	// Skipped counts the objects left as stored, by reason.
	Skipped    map[string]int     `json:"skipped,omitempty"`
	Deferred   []RederiveDeferred `json:"deferred,omitempty"`
	Stopped    []RederiveStopped  `json:"stopped,omitempty"`
	DurationMs int64              `json:"duration_ms,omitempty"`
	Reason     string             `json:"reason,omitempty"`
}
