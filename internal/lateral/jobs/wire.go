// Package jobs is a typed wrapper over hop.top/kit/go/runtime/job for
// the lateral substrate's deferred-action queue. It does not implement
// a queue: kit's Service handles claim-TTLs, retries, backoff, and
// state transitions. We only declare queue name, cause taxonomy, and
// the typed payload shape.
package jobs

import (
	"context"
	"time"

	"hop.top/kit/go/runtime/job"
)

// QueueDeferred is the kit job queue carrying lateral deferred actions.
const QueueDeferred = "lateral.deferred"

// Cause enumerates the why behind a deferred action. The string value
// becomes job.Type so handlers register by Cause name.
type Cause string

const (
	CauseColdCycleExpiry Cause = "cold_cycle_expiry"
	CauseRefreshDue      Cause = "refresh_due"
	CauseRateLimit       Cause = "rate_limit"
	CauseResolverRetry   Cause = "resolver_retry"
	CauseScoringRetry    Cause = "scoring_retry"
	CauseJITUnavailable  Cause = "jit_unavailable"
)

// Deferred is the typed payload for a lateral deferred-action job. It
// is passed through to job.EnqueueOpts.Payload by EnqueueDeferred;
// kit's engine handles serialization on its own.
type Deferred struct {
	CandidateID string         `json:"candidate_id"`
	Cause       Cause          `json:"cause"`
	Extra       map[string]any `json:"extra,omitempty"`
	ScheduledAt time.Time      `json:"scheduled_at,omitzero"`
}

// EnqueueDeferred submits a deferred action to the kit job engine. The
// engine handles retries, claim-TTLs, stale-claim release, and state
// transitions; we only declare queue, type, and payload.
func EnqueueDeferred(ctx context.Context, svc job.Service, d Deferred) (string, error) {
	opts := job.EnqueueOpts{
		Queue:    QueueDeferred,
		Type:     string(d.Cause),
		Payload:  d,
		Backoff:  job.DefaultBackoff(),
		ClaimTTL: 5 * time.Minute,
	}
	if !d.ScheduledAt.IsZero() {
		opts.ScheduledAt = &d.ScheduledAt
	}
	return svc.Enqueue(ctx, opts)
}
