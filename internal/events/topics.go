package events

import "hop.top/kit/go/runtime/bus"

// Outbound topic constants for ctxt domain events.
//
// Topics follow kit's 4-segment past-tense contract enforced by
// bus.ValidateTopic: source.category.object.action. The "runtime" category
// matches kit's own convention for events emitted from the runtime layer
// (cf. wsm.runtime.workspace.created).
const (
	TopicObjectIngested bus.Topic = "ctxt.runtime.object.ingested"
	TopicObjectUpdated  bus.Topic = "ctxt.runtime.object.updated"
	TopicObjectDeleted  bus.Topic = "ctxt.runtime.object.deleted"
	TopicJobCompleted   bus.Topic = "ctxt.runtime.job.completed"
	TopicJobFailed      bus.Topic = "ctxt.runtime.job.failed"

	// TopicDpkmsUpgradeSignatureMismatch fires when daemon startup detects an
	// index-signature drift (ADR-070 §3, T-0579). Detection only — the
	// actual reindex worker (T-0581) subscribes downstream.
	TopicDpkmsUpgradeSignatureMismatch bus.Topic = "dpkms.upgrade.signature.mismatched"

	// TopicDpkmsUpgradePlanComputed fires when `ctxt upgrade plan` (or a
	// dry-run worker invocation) produces a plan summary. Carries
	// UpgradePlanComputedPayload (ADR-070 §6, T-0581).
	TopicDpkmsUpgradePlanComputed bus.Topic = "dpkms.upgrade.plan.computed"
	// TopicDpkmsUpgradeReingestStarted fires when the reingest_selective
	// worker begins. Carries UpgradeReingestStartedPayload.
	TopicDpkmsUpgradeReingestStarted bus.Topic = "dpkms.upgrade.reingest.started"
	// TopicDpkmsUpgradeReingestProgress fires periodically (~every total/100
	// objects) during a reingest_selective run. Carries
	// UpgradeReingestProgressPayload.
	TopicDpkmsUpgradeReingestProgress bus.Topic = "dpkms.upgrade.reingest.progressed"
	// TopicDpkmsUpgradeReingestCompleted fires when a reingest_selective run
	// finishes successfully. Carries UpgradeReingestCompletedPayload.
	TopicDpkmsUpgradeReingestCompleted bus.Topic = "dpkms.upgrade.reingest.completed"
	// TopicDpkmsUpgradeReingestFailed fires when a reingest_selective run
	// aborts (cancellation, budget exceeded, or worker error). Carries
	// UpgradeReingestFailedPayload.
	TopicDpkmsUpgradeReingestFailed bus.Topic = "dpkms.upgrade.reingest.failed"
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

// UpgradeSignatureMismatchPayload is published when daemon startup detects
// that an on-disk index signature does not match the build-time-known
// signature (ADR-070 §3, T-0579). Per ADR-070 the bucket-1 reindex_auto
// worker (T-0581) subscribes to act on this; in T-0579 we ship detection
// only — receivers may log/notify but do not rebuild yet.
type UpgradeSignatureMismatchPayload struct {
	SignatureID   string `json:"signature_id"`
	OldHash       string `json:"old_hash"`
	NewHash       string `json:"new_hash"`
	InputsSummary string `json:"inputs_summary"`
}

// UpgradePlanComputedPayload describes the output of `ctxt upgrade plan` or
// a dry-run worker invocation (ADR-070 §6, T-0581). PlanItems is keyed by
// "<from>→<to>" pipeline transitions (e.g. "text.short@v0→text.short@v1").
type UpgradePlanComputedPayload struct {
	Bucket    string         `json:"bucket"`
	PlanItems map[string]int `json:"plan_items"`
	Total     int            `json:"total"`
}

// UpgradeReingestStartedPayload is fired when the reingest_selective worker
// begins iterating its selector.
type UpgradeReingestStartedPayload struct {
	Selector string `json:"selector"`
	Total    int    `json:"total"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

// UpgradeReingestProgressPayload is fired ~every total/100 objects during a
// reingest run.
type UpgradeReingestProgressPayload struct {
	Selector  string  `json:"selector"`
	Done      int     `json:"done"`
	Total     int     `json:"total"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
	BudgetUSD float64 `json:"budget_usd,omitempty"`
}

// UpgradeReingestCompletedPayload is fired when a reingest run finishes
// successfully.
type UpgradeReingestCompletedPayload struct {
	Selector   string  `json:"selector"`
	Done       int     `json:"done"`
	Total      int     `json:"total"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	DurationMs int64   `json:"duration_ms"`
}

// UpgradeReingestFailedPayload is fired when a reingest run aborts.
type UpgradeReingestFailedPayload struct {
	Selector string  `json:"selector"`
	Done     int     `json:"done"`
	Total    int     `json:"total"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
	Reason   string  `json:"reason"`
}
