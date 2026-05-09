package github

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeBreaker controls Allow + records counts of Record calls. Used to
// verify breaker integration in guardedAPI.
type fakeBreaker struct {
	allowErr      error
	successCount  int
	failureCount  int
	lastBytes     int64
	lastSuccess   bool
}

func (b *fakeBreaker) Allow() error { return b.allowErr }
func (b *fakeBreaker) Record(success bool, n int64) {
	b.lastBytes = n
	b.lastSuccess = success
	if success {
		b.successCount++
	} else {
		b.failureCount++
	}
}

func TestGuardedAPI_PassthroughWhenAllowOK(t *testing.T) {
	called := 0
	inner := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			called++
			return []RepoSummary{{Owner: "o", Name: "r"}}, nil
		},
	}
	br := &fakeBreaker{}
	g := NewGuardedAPI(inner, br, nil)
	out, err := g.ListRepoSiblings(context.Background(), "o", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("len(out) = %d, want 1", len(out))
	}
	if called != 1 {
		t.Errorf("inner called %d times, want 1", called)
	}
	if br.successCount != 1 || br.failureCount != 0 {
		t.Errorf("breaker counts: success=%d failure=%d, want 1/0", br.successCount, br.failureCount)
	}
}

func TestGuardedAPI_BreakerOpen_ShortCircuits(t *testing.T) {
	called := 0
	inner := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			called++
			return nil, nil
		},
	}
	br := &fakeBreaker{allowErr: errors.New("breaker tripped")}
	g := NewGuardedAPI(inner, br, nil)
	_, err := g.ListRepoSiblings(context.Background(), "o", "")
	if !errors.Is(err, ErrBreakerOpen) {
		t.Errorf("err = %v, want errors.Is ErrBreakerOpen", err)
	}
	if called != 0 {
		t.Error("inner should not be called when breaker is open")
	}
	if br.successCount != 0 || br.failureCount != 0 {
		t.Errorf("breaker should not record outcome when Allow rejected; got %d/%d", br.successCount, br.failureCount)
	}
}

func TestGuardedAPI_FloorThrottle_ShortCircuits(t *testing.T) {
	called := 0
	inner := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			called++
			return nil, nil
		},
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(time.Hour, FloorBounds{MinPct: 0.9, MaxPct: 0.9})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 1, ObservedAt: now})
	g := NewGuardedAPI(inner, &fakeBreaker{}, ft)
	_, err := g.ListRepoSiblings(context.Background(), "o", "")
	if !errors.Is(err, ErrFloorThrottled) {
		t.Errorf("err = %v, want ErrFloorThrottled", err)
	}
	if called != 0 {
		t.Error("inner should not be called when floor throttled")
	}
}

func TestGuardedAPI_RecordsFailureOnInnerError(t *testing.T) {
	innerErr := errors.New("api 500")
	inner := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			return nil, innerErr
		},
	}
	br := &fakeBreaker{}
	g := NewGuardedAPI(inner, br, nil)
	_, err := g.ListRepoSiblings(context.Background(), "o", "")
	if !errors.Is(err, innerErr) {
		t.Errorf("err = %v, want innerErr passthrough", err)
	}
	if br.failureCount != 1 {
		t.Errorf("breaker.failureCount = %d, want 1", br.failureCount)
	}
}

func TestGuardedAPI_NoopBreaker_AlwaysPasses(t *testing.T) {
	inner := &stubAPIClient{
		hasSponsorPageFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	g := NewGuardedAPI(inner, nil, nil) // nil breaker → noop
	ok, err := g.HasSponsorPage(context.Background(), "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("HasSponsorPage = false; want true")
	}
}

func TestGuardedAPI_RateSnapshot_NotGated(t *testing.T) {
	called := false
	inner := &stubAPIClient{}
	// Replace by anonymous wrapper that intercepts RateSnapshot — stubAPIClient
	// returns zero RateSnapshot regardless. Instead set RateSnapshot via a
	// custom inner.
	custom := &rateSnapshotStub{snap: RateSnapshot{Limit: 999}, recorded: &called}
	br := &fakeBreaker{allowErr: errors.New("trip")}
	g := NewGuardedAPI(custom, br, nil)
	got := g.RateSnapshot(context.Background())
	if !called {
		t.Error("RateSnapshot should pass through even when breaker is open")
	}
	if got.Limit != 999 {
		t.Errorf("got.Limit = %d, want 999", got.Limit)
	}
	_ = inner
}

// rateSnapshotStub is a minimal APIClient that records a RateSnapshot
// invocation flag. Used by TestGuardedAPI_RateSnapshot_NotGated to
// verify rate-snapshot pass-through under an open breaker.
type rateSnapshotStub struct {
	snap     RateSnapshot
	recorded *bool
}

func (r *rateSnapshotStub) ListRepoSiblings(_ context.Context, _, _ string) ([]RepoSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListOwnerStarred(_ context.Context, _ string) ([]RepoSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListOwnerPinned(_ context.Context, _ string) ([]RepoSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) HasSponsorPage(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (r *rateSnapshotStub) ListAuthoredPRs(_ context.Context, _ string, _ int) ([]PullRequestSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListPRReviewers(_ context.Context, _, _ string, _ int) ([]UserSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListAuthoredIssues(_ context.Context, _ string, _ int) ([]IssueSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListIssueLabels(_ context.Context, _, _ string, _ int) ([]LabelSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListSponsored(_ context.Context, _ string) ([]UserSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListContributionOrgs(_ context.Context, _ string) ([]OrgSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListSimilarSponsors(_ context.Context, _ string) ([]UserSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListOwnerGists(_ context.Context, _ string) ([]GistSummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) ListGlobalAdvisories(_ context.Context, _, _ string, _ int) ([]AdvisorySummary, error) {
	return nil, nil
}
func (r *rateSnapshotStub) RateSnapshot(_ context.Context) RateSnapshot {
	if r.recorded != nil {
		*r.recorded = true
	}
	return r.snap
}
