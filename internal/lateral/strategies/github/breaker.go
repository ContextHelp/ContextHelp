package github

import (
	"context"
	"errors"
)

// Breaker is the package-local circuit-breaker contract used to protect
// the github API fetch budget. The daemon wires kit/core/breaker.Breaker
// (or any other implementation) to this interface; the github strategy
// never imports kit/core/breaker directly so probe tests can inject a
// stub.
//
// Allow returns nil when the breaker is closed (executions allowed) and
// a non-nil error when the breaker is open or half-open and rejects.
// Record feeds success/failure outcomes back so the breaker's state
// machine sees real signal. n is informational (kit/breaker tracks a
// "bytes" counter via Record); callers that don't track byte counts
// can pass 0.
type Breaker interface {
	Allow() error
	Record(success bool, n int64)
}

// ErrBreakerOpen is returned by guarded calls when the breaker rejects.
// Wraps the underlying breaker's error so callers using errors.Is can
// inspect both the github sentinel and the underlying breaker error.
var ErrBreakerOpen = errors.New("github: breaker open")

// noopBreaker permits everything and records nothing. Default when no
// breaker is wired (skeleton mode + tests that don't care).
type noopBreaker struct{}

func (noopBreaker) Allow() error           { return nil }
func (noopBreaker) Record(_ bool, _ int64) {}

// guardedAPI wraps an APIClient with breaker + floor-tracker checks.
// Each method consults the breaker via Allow before delegating; the
// outcome is fed back via Record so the breaker's failure-rate state
// machine adjusts correctly.
//
// guardedAPI is constructed via NewGuardedAPI — daemon wiring code
// uses it; the strategy itself stays APIClient-shaped so tests with
// raw stubs keep working unchanged.
type guardedAPI struct {
	inner   APIClient
	breaker Breaker
	floor   *FloorTracker
}

// NewGuardedAPI wraps inner with a circuit breaker + dynamic-floor
// tracker. Pass nil for either to use the no-op default. The returned
// APIClient short-circuits with ErrBreakerOpen when the breaker rejects
// and with ErrFloorThrottled when the floor tracker says we should
// throttle (Remaining < Floor).
func NewGuardedAPI(inner APIClient, b Breaker, f *FloorTracker) APIClient {
	if inner == nil {
		return nil
	}
	if b == nil {
		b = noopBreaker{}
	}
	return &guardedAPI{inner: inner, breaker: b, floor: f}
}

// ErrFloorThrottled is returned when the dynamic floor tracker says
// remaining budget is below the computed floor and new fetches must
// pause.
var ErrFloorThrottled = errors.New("github: dynamic-floor throttle engaged")

// gate consults breaker + floor tracker before any wrapped call. When
// either gate trips, the wrapped call is skipped entirely and an empty
// result + the gating error is returned to the caller. Callers (the
// probe pipeline) treat gating errors the same as transient API errors:
// emit subpath.failed, continue with other sub-paths.
func (g *guardedAPI) gate() error {
	if g.floor != nil && g.floor.ShouldThrottle() {
		return ErrFloorThrottled
	}
	if err := g.breaker.Allow(); err != nil {
		return errors.Join(ErrBreakerOpen, err)
	}
	return nil
}

// recordOutcome is shared post-call book-keeping: feed success/failure
// to the breaker, record a call to the floor tracker so pressure
// updates, and observe the latest rate-limit snapshot so ShouldThrottle
// has signal on the next gate check.
func (g *guardedAPI) recordOutcome(ctx context.Context, err error) {
	g.breaker.Record(err == nil, 0)
	if g.floor != nil {
		g.floor.Record(g.floor.now())
		// Pull the latest snapshot from the inner client and feed it
		// into the tracker. Without this Observe step, snapshot stays
		// zero and ShouldThrottle returns false — making the floor
		// gate effectively unreachable in production.
		if snap := g.inner.RateSnapshot(ctx); !snap.IsZero() {
			g.floor.Observe(snap)
		}
	}
}

func (g *guardedAPI) ListRepoSiblings(ctx context.Context, owner, exclude string) ([]RepoSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListRepoSiblings(ctx, owner, exclude)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListOwnerStarred(ctx context.Context, login string) ([]RepoSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListOwnerStarred(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListOwnerPinned(ctx context.Context, login string) ([]RepoSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListOwnerPinned(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) HasSponsorPage(ctx context.Context, login string) (bool, error) {
	if err := g.gate(); err != nil {
		return false, err
	}
	out, err := g.inner.HasSponsorPage(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListAuthoredPRs(ctx context.Context, login string, limit int) ([]PullRequestSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListAuthoredPRs(ctx, login, limit)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListPRReviewers(ctx context.Context, owner, repo string, number int) ([]UserSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListPRReviewers(ctx, owner, repo, number)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListAuthoredIssues(ctx context.Context, login string, limit int) ([]IssueSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListAuthoredIssues(ctx, login, limit)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListIssueLabels(ctx context.Context, owner, repo string, number int) ([]LabelSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListIssueLabels(ctx, owner, repo, number)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListSponsored(ctx context.Context, login string) ([]UserSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListSponsored(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListContributionOrgs(ctx context.Context, login string) ([]OrgSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListContributionOrgs(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListSimilarSponsors(ctx context.Context, login string) ([]UserSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListSimilarSponsors(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListOwnerGists(ctx context.Context, login string) ([]GistSummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListOwnerGists(ctx, login)
	g.recordOutcome(ctx, err)
	return out, err
}

func (g *guardedAPI) ListGlobalAdvisories(ctx context.Context, ecosystem, severity string, limit int) ([]AdvisorySummary, error) {
	if err := g.gate(); err != nil {
		return nil, err
	}
	out, err := g.inner.ListGlobalAdvisories(ctx, ecosystem, severity, limit)
	g.recordOutcome(ctx, err)
	return out, err
}

// RateSnapshot passes through unguarded — observing the rate-limit
// snapshot must not be itself rate-limited. Floor + breaker are
// observability sinks for it, not gates on it.
func (g *guardedAPI) RateSnapshot(ctx context.Context) RateSnapshot {
	return g.inner.RateSnapshot(ctx)
}
