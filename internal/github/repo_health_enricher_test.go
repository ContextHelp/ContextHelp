package github

import (
	"testing"
	"time"
)

// fixedNow is a stable reference point for all tests.
var fixedNow = time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

func enricherWithFixedClock() *RepoHealthEnricher {
	return &RepoHealthEnricher{now: func() time.Time { return fixedNow }}
}

func TestRepoHealthEnricher_Enrich(t *testing.T) {
	t.Parallel()

	type want struct {
		label           string
		isStale         bool
		depCount        int
		releaseFreqZero bool // true when we expect exactly 0
	}

	tests := []struct {
		name string
		snap RepoSnapshot
		want want
	}{
		{
			name: "archived repo",
			snap: RepoSnapshot{
				IsArchived: true,
				CreatedAt:  fixedNow.Add(-2 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-400 * 24 * time.Hour), // also stale
				Stars:      500,
			},
			want: want{label: "archived", isStale: true},
		},
		{
			name: "stale by push date",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-3 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-400 * 24 * time.Hour), // > 12 months
				Stars:      10,
			},
			want: want{label: "stale", isStale: true},
		},
		{
			name: "new repo (< 6 months old)",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-60 * 24 * time.Hour), // 2 months old
				PushedAt:   fixedNow.Add(-5 * 24 * time.Hour),  // recent push
				Stars:      5,
			},
			want: want{label: "new", isStale: false},
		},
		{
			name: "healthy active repo",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-2 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-10 * 24 * time.Hour), // recent
				Stars:      2000,
				Forks:      300,
				OpenIssues: 15,
			},
			want: want{label: "healthy", isStale: false},
		},
		{
			name: "healthy default (low activity but not stale/new)",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-18 * 30 * 24 * time.Hour), // 18 months
				PushedAt:   fixedNow.Add(-30 * 24 * time.Hour),      // 1 month ago
				Stars:      1,
				Forks:      0,
				OpenIssues: 0,
			},
			want: want{label: "healthy", isStale: false},
		},
		{
			name: "dependency count propagated",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-2 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-10 * 24 * time.Hour),
				Dependencies: []Dependency{
					{Name: "foo", Ecosystem: "npm"},
					{Name: "bar", Ecosystem: "npm"},
					{Name: "baz", Ecosystem: "go"},
				},
			},
			want: want{label: "healthy", isStale: false, depCount: 3},
		},
		{
			name: "release frequency with single release returns zero",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-2 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-10 * 24 * time.Hour),
				Releases: []Release{
					{Tag: "v1.0.0", PublishedAt: fixedNow.Add(-30 * 24 * time.Hour)},
				},
			},
			want: want{label: "healthy", isStale: false, releaseFreqZero: true},
		},
		{
			name: "release frequency computed from two releases",
			snap: RepoSnapshot{
				IsArchived: false,
				CreatedAt:  fixedNow.Add(-2 * 365 * 24 * time.Hour),
				PushedAt:   fixedNow.Add(-5 * 24 * time.Hour),
				Releases: []Release{
					{Tag: "v2.0.0", PublishedAt: fixedNow.Add(-10 * 24 * time.Hour)},
					{Tag: "v1.0.0", PublishedAt: fixedNow.Add(-40 * 24 * time.Hour)},
				},
			},
			// gap = 30 days → ReleaseFrequency should be ~30
			want: want{label: "healthy", isStale: false},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := enricherWithFixedClock()
			h := e.Enrich(&tc.snap)

			if h.HealthLabel != tc.want.label {
				t.Errorf("HealthLabel = %q; want %q", h.HealthLabel, tc.want.label)
			}
			if h.IsStale != tc.want.isStale {
				t.Errorf("IsStale = %v; want %v", h.IsStale, tc.want.isStale)
			}
			if tc.want.depCount != 0 && h.DependencyCount != tc.want.depCount {
				t.Errorf("DependencyCount = %d; want %d", h.DependencyCount, tc.want.depCount)
			}
			if tc.want.releaseFreqZero && h.ReleaseFrequency != 0 {
				t.Errorf("ReleaseFrequency = %v; want 0", h.ReleaseFrequency)
			}
			if h.ActivityScore < 0 || h.ActivityScore > 1 {
				t.Errorf("ActivityScore = %v; want in [0,1]", h.ActivityScore)
			}
		})
	}
}

// TestReleaseFrequency_TwoReleases verifies the 30-day interval calculation directly.
func TestReleaseFrequency_TwoReleases(t *testing.T) {
	t.Parallel()
	releases := []Release{
		{Tag: "v2.0.0", PublishedAt: fixedNow.Add(-10 * 24 * time.Hour)},
		{Tag: "v1.0.0", PublishedAt: fixedNow.Add(-40 * 24 * time.Hour)},
	}
	freq := computeReleaseFrequency(releases)
	if freq != 30 {
		t.Errorf("expected 30 days, got %v", freq)
	}
}

// TestReleaseFrequency_NoReleases ensures 0 is returned when slice is empty.
func TestReleaseFrequency_NoReleases(t *testing.T) {
	t.Parallel()
	if got := computeReleaseFrequency(nil); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

// TestNewRepoHealthEnricher_UsesRealClock verifies the public constructor
// produces a non-nil enricher with a working clock.
func TestNewRepoHealthEnricher_UsesRealClock(t *testing.T) {
	t.Parallel()
	e := NewRepoHealthEnricher()
	if e == nil {
		t.Fatal("expected non-nil enricher")
	}
	snap := &RepoSnapshot{
		CreatedAt: time.Now().Add(-365 * 24 * time.Hour),
		PushedAt:  time.Now().Add(-10 * 24 * time.Hour),
	}
	h := e.Enrich(snap)
	if h.HealthLabel == "" {
		t.Error("expected non-empty HealthLabel")
	}
}
