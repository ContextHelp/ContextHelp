package events

import (
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/notify"
)

// CyclePayload is the body for routine reaper cycle events
// (started/completed/skipped) and any other Info-class lifecycle
// event that carries timing and counts. Severity = Info.
//
// Embeds bus.Qualifiers for events that carry a Reason (e.g.
// reaper_cycle.skipped with Qualifiers.Reason = "overlap") or other
// axis. Empty Qualifiers JSON-marshal cleanly thanks to omitempty on
// every field.
type CyclePayload struct {
	bus.Qualifiers
	Topic      bus.Topic `json:"topic"`
	JobID      string    `json:"job_id,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Expired    int       `json:"expired,omitempty"`
}

// Severity implements notify.WithSeverity.
func (CyclePayload) Severity() notify.Severity { return notify.SeverityInfo }

// FailurePayload is the body for failure-class events (subpath.failed,
// scan.deferred, scan.skipped, scan.failed, reaper_cycle.failed,
// signal.degraded). Severity = Error. Qualifiers.Reason carries the
// failure cause where applicable.
type FailurePayload struct {
	bus.Qualifiers
	Topic   bus.Topic `json:"topic"`
	Subject string    `json:"subject,omitempty"`
}

// Severity implements notify.WithSeverity.
func (FailurePayload) Severity() notify.Severity { return notify.SeverityError }

// SanityViolationPayload is the body for sanity-class events
// (sanity_check.violated, score.flagged with Property=invalid).
// Severity = Critical because these indicate a structural bug in
// upstream pipelines, not routine operational degradation.
type SanityViolationPayload struct {
	bus.Qualifiers
	Topic   bus.Topic      `json:"topic"`
	Subject string         `json:"subject,omitempty"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// Severity implements notify.WithSeverity.
func (SanityViolationPayload) Severity() notify.Severity { return notify.SeverityCritical }

// CandidateLifecyclePayload is the body for candidate.* state events
// (created/promoted/expired/rejected/resurrected). Severity = Info;
// lifecycle is routine. Failures during a lifecycle transition use
// FailurePayload instead.
type CandidateLifecyclePayload struct {
	bus.Qualifiers
	Topic       bus.Topic `json:"topic"`
	CandidateID string    `json:"candidate_id"`
	FromState   string    `json:"from_state,omitempty"`
	ToState     string    `json:"to_state,omitempty"`
}

// Severity implements notify.WithSeverity.
func (CandidateLifecyclePayload) Severity() notify.Severity { return notify.SeverityInfo }
