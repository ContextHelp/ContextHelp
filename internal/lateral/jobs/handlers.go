package jobs

import (
	"context"

	"hop.top/kit/go/runtime/job"
)

// Handlers bundles one function per Cause. Nil entries are skipped — the
// resulting HandlerMap simply omits them, so unset causes fail the poller's
// "no handler for type" check (visible failure, not a silent drop). Wire
// only the causes a deployment supports.
type Handlers struct {
	ColdCycleExpiry func(ctx context.Context, j job.Job) error
	RefreshDue      func(ctx context.Context, j job.Job) error
	RateLimit       func(ctx context.Context, j job.Job) error
	ResolverRetry   func(ctx context.Context, j job.Job) error
	ScoringRetry    func(ctx context.Context, j job.Job) error
	JITUnavailable  func(ctx context.Context, j job.Job) error
}

// HandlerMap returns a job.HandlerMap with one entry per non-nil field of h.
// Entry keys are the string form of the Cause constants.
func HandlerMap(h Handlers) job.HandlerMap {
	m := job.HandlerMap{}
	if h.ColdCycleExpiry != nil {
		m[string(CauseColdCycleExpiry)] = h.ColdCycleExpiry
	}
	if h.RefreshDue != nil {
		m[string(CauseRefreshDue)] = h.RefreshDue
	}
	if h.RateLimit != nil {
		m[string(CauseRateLimit)] = h.RateLimit
	}
	if h.ResolverRetry != nil {
		m[string(CauseResolverRetry)] = h.ResolverRetry
	}
	if h.ScoringRetry != nil {
		m[string(CauseScoringRetry)] = h.ScoringRetry
	}
	if h.JITUnavailable != nil {
		m[string(CauseJITUnavailable)] = h.JITUnavailable
	}
	return m
}
