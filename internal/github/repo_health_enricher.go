// Package github provides typed snapshot structs for the GitHub pipeline adapter.
package github

import (
	"math"
	"time"
)

// RepoHealth holds computed health signals derived from a RepoSnapshot.
type RepoHealth struct {
	// ActivityScore is a normalised [0,1] composite: star velocity + recency + issue ratio.
	ActivityScore float64 `json:"activity_score"`

	// IsStale is true when PushedAt > 12 months ago or IsArchived.
	IsStale bool `json:"is_stale"`

	// ReleaseFrequency is the average number of days between releases.
	// 0 when fewer than 2 releases are available.
	ReleaseFrequency float64 `json:"release_frequency"`

	// DependencyCount is len(snap.Dependencies).
	DependencyCount int `json:"dependency_count"`

	// HealthLabel is one of "healthy" | "stale" | "archived" | "new".
	HealthLabel string `json:"health_label"`
}

// RepoHealthEnricher computes health signals from a RepoSnapshot.
// It is stateless and safe for concurrent use.
type RepoHealthEnricher struct {
	// now is injectable for testing; defaults to time.Now().
	now func() time.Time
}

// NewRepoHealthEnricher returns a RepoHealthEnricher using the real clock.
func NewRepoHealthEnricher() *RepoHealthEnricher {
	return &RepoHealthEnricher{now: time.Now}
}

// Enrich derives a RepoHealth from snap. snap must not be nil.
func (e *RepoHealthEnricher) Enrich(snap *RepoSnapshot) RepoHealth {
	now := e.now()

	isStale := snap.IsArchived || now.Sub(snap.PushedAt) > 12*30*24*time.Hour

	activityScore := computeActivityScore(snap, now)
	releaseFreq := computeReleaseFrequency(snap.Releases)
	label := computeHealthLabel(snap, isStale, activityScore, now)

	return RepoHealth{
		ActivityScore:    activityScore,
		IsStale:          isStale,
		ReleaseFrequency: releaseFreq,
		DependencyCount:  len(snap.Dependencies),
		HealthLabel:      label,
	}
}

// computeActivityScore returns a normalised [0,1] score built from three signals:
//   - star velocity (stars / repo age in years), capped at 1000 stars/yr → [0,1]
//   - recency: days since last push, capped at 365 → linear decay [0,1]
//   - open-issues ratio: capped at 100 issues relative to forks+1 → [0,1]
//
// Weights: velocity 40 %, recency 40 %, issue ratio 20 %.
func computeActivityScore(snap *RepoSnapshot, now time.Time) float64 {
	const (
		wVelocity   = 0.40
		wRecency    = 0.40
		wIssueRatio = 0.20

		maxStarsPerYear  = 1000.0
		maxDaysSincePush = 365.0
		maxIssueForks    = 100.0
	)

	// Star velocity signal.
	ageYears := now.Sub(snap.CreatedAt).Hours() / 8760
	if ageYears < 0.01 {
		ageYears = 0.01 // avoid div-by-zero for brand-new repos
	}
	velocity := math.Min(float64(snap.Stars)/ageYears/maxStarsPerYear, 1.0)

	// Recency signal: 1 = pushed today, 0 = pushed ≥ 365 days ago.
	daysSincePush := now.Sub(snap.PushedAt).Hours() / 24
	recency := math.Max(0, 1.0-daysSincePush/maxDaysSincePush)

	// Open-issues-to-forks ratio signal.
	base := float64(snap.Forks) + 1
	issueRatio := math.Min(float64(snap.OpenIssues)/base/maxIssueForks, 1.0)

	return wVelocity*velocity + wRecency*recency + wIssueRatio*issueRatio
}

// computeReleaseFrequency returns the mean interval (days) between consecutive
// releases. Returns 0 when fewer than 2 releases exist.
func computeReleaseFrequency(releases []Release) float64 {
	if len(releases) < 2 {
		return 0
	}
	// Releases are stored newest-first; iterate from oldest pair to newest.
	var totalDays float64
	for i := 0; i < len(releases)-1; i++ {
		newer := releases[i].PublishedAt
		older := releases[i+1].PublishedAt
		totalDays += newer.Sub(older).Hours() / 24
	}
	return totalDays / float64(len(releases)-1)
}

// computeHealthLabel applies the label precedence rules:
//  1. "archived" if IsArchived
//  2. "stale"    if IsStale
//  3. "new"      if repo is < 6 months old
//  4. "healthy"  if ActivityScore > 0.5
//  5. default "healthy"
func computeHealthLabel(snap *RepoSnapshot, isStale bool, score float64, now time.Time) string {
	if snap.IsArchived {
		return "archived"
	}
	if isStale {
		return "stale"
	}
	if now.Sub(snap.CreatedAt) < 6*30*24*time.Hour {
		return "new"
	}
	return "healthy"
}
