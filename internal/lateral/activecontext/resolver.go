package activecontext

import (
	"context"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// SessionSource yields the active work session's derived topic vector. Returns
// nil when no session is live.
type SessionSource interface {
	DerivedTopic(ctx context.Context) (map[string]float64, error)
}

// WindowSource yields the rolling capture-window aggregate (default 30d).
// Always returns a non-nil map once any captures exist.
type WindowSource interface {
	Aggregate(ctx context.Context) (map[string]float64, error)
}

// InterestSource yields tags from the user's currently-active interest
// registry entries. Returns nil when no interests are defined or active.
type InterestSource interface {
	ActiveTags(ctx context.Context) (map[string]float64, error)
}

// Resolver collects the three active-context signals (session topic, capture
// window aggregate, interest registry) and computes a stable fingerprint
// usable as a triage cache key.
type Resolver struct {
	session  SessionSource
	window   WindowSource
	interest InterestSource
}

// NewResolver wires a Resolver to its three signal sources.
func NewResolver(s SessionSource, w WindowSource, i InterestSource) *Resolver {
	return &Resolver{session: s, window: w, interest: i}
}

// Resolve fetches all three signals and returns the assembled ActiveContext.
// Missing signals stay nil — the scorer's redistribution rule (T06) handles
// them. Any source error short-circuits and returns the zero-value context.
func (r *Resolver) Resolve(ctx context.Context) (lateral.ActiveContext, error) {
	topic, err := r.session.DerivedTopic(ctx)
	if err != nil {
		return lateral.ActiveContext{}, err
	}
	agg, err := r.window.Aggregate(ctx)
	if err != nil {
		return lateral.ActiveContext{}, err
	}
	regs, err := r.interest.ActiveTags(ctx)
	if err != nil {
		return lateral.ActiveContext{}, err
	}
	ac := lateral.ActiveContext{
		SessionTopic:     topic,
		CaptureWindow:    agg,
		InterestRegistry: regs,
	}
	ac.Fingerprint = Fingerprint(ac)
	return ac, nil
}
