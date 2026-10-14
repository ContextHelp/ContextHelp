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
	TopicJobEnqueued    bus.Topic = "ctxt.runtime.job.enqueued"

	// TopicObjectRawStored fires when the analyze raw path stored an
	// object without running a pipeline. Carries the stored object.
	TopicObjectRawStored bus.Topic = "ctxt.runtime.object.raw_stored"
	// TopicInboxCaptured fires when an item lands in the inbox. Carries
	// the stored object.
	TopicInboxCaptured bus.Topic = "ctxt.runtime.inbox.captured"
	// TopicInboxTriaged fires when an inbox item was sent to a pipeline.
	// Carries the object.
	TopicInboxTriaged bus.Topic = "ctxt.runtime.inbox.triaged"

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

	// Embedding migration (ADR-071 "Migration job"): the embeddings_migrate
	// run that fills one model's missing rows. Every event carries
	// EmbeddingsMigrationPayload.
	//
	// TopicDpkmsEmbeddingsMigrationStarted fires when the run has
	// counted the objects missing rows for the target model.
	TopicDpkmsEmbeddingsMigrationStarted bus.Topic = "dpkms.embeddings.migration.started"
	// TopicDpkmsEmbeddingsMigrationProgressed fires about every
	// total/100 objects.
	TopicDpkmsEmbeddingsMigrationProgressed bus.Topic = "dpkms.embeddings.migration.progressed"
	// TopicDpkmsEmbeddingsMigrationCompleted fires when the run has
	// passed over every missing object. Failed > 0 means some objects are
	// still missing rows; a re-run retries them.
	TopicDpkmsEmbeddingsMigrationCompleted bus.Topic = "dpkms.embeddings.migration.completed"
	// TopicDpkmsEmbeddingsMigrationFailed fires when the run stops
	// early: cancelled, interrupted by shutdown, or a storage error.
	TopicDpkmsEmbeddingsMigrationFailed bus.Topic = "dpkms.embeddings.migration.failed"

	// Embedding-model lifecycle (ADR-071 "Default-flip control"), emitted
	// by `ctxt embeddings` after the change commits. Every event carries
	// EmbeddingModelLifecyclePayload. The query path reads the default
	// per query, so no consumer needs these to stay correct; they record
	// operator actions.
	//
	// TopicCtxtEmbeddingsModelPromoted fires when set-default makes
	// a model the default.
	TopicCtxtEmbeddingsModelPromoted bus.Topic = "ctxt.embeddings.model.promoted"
	// TopicCtxtEmbeddingsModelDeprecated fires when a model's
	// retirement is scheduled.
	TopicCtxtEmbeddingsModelDeprecated bus.Topic = "ctxt.embeddings.model.deprecated"
	// TopicCtxtEmbeddingsModelPurged fires when a deprecated model's
	// rows, index and registry entry are deleted.
	TopicCtxtEmbeddingsModelPurged bus.Topic = "ctxt.embeddings.model.purged"
)

// Ambient capture substrate topics (ADR-066), published on the ctxd daemon
// bus by the ambient Runner and its Sources. The event source field carries
// the ambient source name (clipboard, filewatch, meeting, ...).
const (
	// TopicAmbientSourceStarted fires when a Source's Start returned.
	TopicAmbientSourceStarted bus.Topic = "ctxt.ambient.source.started"
	// TopicAmbientSourceReadied fires when a Source has declared readiness.
	TopicAmbientSourceReadied bus.Topic = "ctxt.ambient.source.readied"
	// TopicAmbientSourceStopped fires when a Source's Stop returned.
	TopicAmbientSourceStopped bus.Topic = "ctxt.ambient.source.stopped"
	// TopicAmbientSourceFailed fires on a Source start or runtime error.
	TopicAmbientSourceFailed bus.Topic = "ctxt.ambient.source.failed"

	// TopicAmbientEventCaptured fires for every RawEvent reaching dispatch.
	TopicAmbientEventCaptured bus.Topic = "ctxt.ambient.event.captured"
	// TopicAmbientEventDeduped fires when a fingerprint matched a recent
	// event and the event was dropped before enqueue.
	TopicAmbientEventDeduped bus.Topic = "ctxt.ambient.event.deduped"
	// TopicAmbientEventFiltered fires when a Source dropped an event
	// (deny list, excluded app, ...).
	TopicAmbientEventFiltered bus.Topic = "ctxt.ambient.event.filtered"

	// TopicAmbientSessionEventJoined fires when an event was tagged with
	// the active session.
	TopicAmbientSessionEventJoined bus.Topic = "ctxt.ambient.session.event_joined"

	// TopicAmbientEnqueueAttempted, TopicAmbientEnqueueSucceeded and
	// TopicAmbientEnqueueFailed bracket the POST to dpkms.
	TopicAmbientEnqueueAttempted bus.Topic = "ctxt.ambient.enqueue.attempted"
	TopicAmbientEnqueueSucceeded bus.Topic = "ctxt.ambient.enqueue.succeeded"
	TopicAmbientEnqueueFailed    bus.Topic = "ctxt.ambient.enqueue.failed"

	// Meeting source (ADR-066 meeting capture).
	TopicAmbientMeetingRequested          bus.Topic = "ctxt.ambient.meeting.requested"
	TopicAmbientMeetingStarted            bus.Topic = "ctxt.ambient.meeting.started"
	TopicAmbientMeetingIndicatorDisplayed bus.Topic = "ctxt.ambient.meeting.indicator_displayed"
	TopicAmbientMeetingPaused             bus.Topic = "ctxt.ambient.meeting.paused"
	TopicAmbientMeetingResumed            bus.Topic = "ctxt.ambient.meeting.resumed"
	TopicAmbientMeetingStopped            bus.Topic = "ctxt.ambient.meeting.stopped"
	TopicAmbientMeetingEnqueued           bus.Topic = "ctxt.ambient.meeting.enqueued"
	TopicAmbientMeetingFailed             bus.Topic = "ctxt.ambient.meeting.failed"
	TopicAmbientMeetingAutoDetected       bus.Topic = "ctxt.ambient.meeting.auto_detected"
	TopicAmbientMeetingPromptDismissed    bus.Topic = "ctxt.ambient.meeting.prompt_dismissed"
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

// EmbeddingsMigrationPayload describes an embeddings_migrate run. Done
// counts the objects handled so far: Embedded + Failed + Skipped (skipped
// objects have no embeddable text or were deleted mid-run).
type EmbeddingsMigrationPayload struct {
	ModelID  string `json:"model_id"`
	JobID    string `json:"job_id,omitempty"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Embedded int    `json:"embedded"`
	Failed   int    `json:"failed"`
	Skipped  int    `json:"skipped"`
	// RateLimit is the provider-call cap in calls per second; 0 is none.
	RateLimit float64 `json:"rate_limit,omitempty"`
	// FailedObjects lists the first failed object IDs (capped).
	FailedObjects []string `json:"failed_objects,omitempty"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// EmbeddingModelLifecyclePayload describes a set-default, deprecate or
// purge. Timestamps are RFC 3339 UTC; fields that do not apply to the
// event are omitted.
type EmbeddingModelLifecyclePayload struct {
	ModelID string `json:"model_id"`
	// PreviousDefault is the default before a promotion; empty when there
	// was none.
	PreviousDefault string `json:"previous_default,omitempty"`
	// Coverage and MinCoverage are the promoted model's measured corpus
	// coverage and the threshold it met (0..1).
	Coverage    *float64 `json:"coverage,omitempty"`
	MinCoverage *float64 `json:"min_coverage,omitempty"`
	// DeprecatedAt is when the deprecation takes (or took) effect.
	DeprecatedAt string `json:"deprecated_at,omitempty"`
	// PurgeEligibleAt is DeprecatedAt plus the grace period.
	PurgeEligibleAt string `json:"purge_eligible_at,omitempty"`
	// Rows is the number of embedding rows a purge deleted.
	Rows *int64 `json:"rows,omitempty"`
}
