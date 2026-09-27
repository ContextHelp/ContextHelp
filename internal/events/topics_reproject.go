package events

import "hop.top/kit/go/runtime/bus"

// Re-projection (ADR-070 reindex_auto for a projection change): the
// background job that re-derives every stale object's FTS body after the
// FTS signature stops matching. Every event carries ReprojectionPayload.
const (
	// TopicDpkmsUpgradeReprojectionScheduled fires when daemon startup
	// enqueues the re-projection job after a signature mismatch.
	TopicDpkmsUpgradeReprojectionScheduled bus.Topic = "dpkms.upgrade.reprojection.scheduled"
	// TopicDpkmsUpgradeReprojectionStarted fires when the job has counted
	// the stale objects.
	TopicDpkmsUpgradeReprojectionStarted bus.Topic = "dpkms.upgrade.reprojection.started"
	// TopicDpkmsUpgradeReprojectionProgressed fires about every total/100
	// objects.
	TopicDpkmsUpgradeReprojectionProgressed bus.Topic = "dpkms.upgrade.reprojection.progressed"
	// TopicDpkmsUpgradeReprojectionCompleted fires when every object is
	// re-projected and the FTS signature is stamped.
	TopicDpkmsUpgradeReprojectionCompleted bus.Topic = "dpkms.upgrade.reprojection.completed"
	// TopicDpkmsUpgradeReprojectionFailed fires when the run stops early
	// (cancelled, shutdown, storage error) or leaves objects stale; the
	// signature stays unstamped and the next start schedules it again.
	TopicDpkmsUpgradeReprojectionFailed bus.Topic = "dpkms.upgrade.reprojection.failed"
)

// ReprojectionPayload describes a re-projection run. Done counts the
// objects handled so far: Reprojected + Skipped + Failed (skipped objects
// were deleted, or re-projected by a concurrent write, after listing).
type ReprojectionPayload struct {
	// ProjectionVersion is the version objects are re-projected to.
	ProjectionVersion string `json:"projection_version"`
	JobID             string `json:"job_id,omitempty"`
	Done              int    `json:"done"`
	Total             int    `json:"total"`
	Reprojected       int    `json:"reprojected"`
	Skipped           int    `json:"skipped"`
	Failed            int    `json:"failed"`
	// FailedObjects lists the first failed object IDs (capped).
	FailedObjects []string `json:"failed_objects,omitempty"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}
