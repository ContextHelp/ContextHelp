// Package x implements the lateral platform strategy for X (formerly
// Twitter). It claims captures hosted on x.com or twitter.com and emits
// sub-path probe candidates that the daemon's fetcher can later resolve
// to concrete entities (posts, threads, profiles).
//
// # Sub-path probes
//
// For a captured tweet `https://x.com/{user}/status/{id}`, the strategy
// proposes:
//
//   - The author profile          /<user>
//   - The author's recent media   /<user>/media
//   - The thread root if quoted   <quoted_url> (passed via Preview hints)
//
// For a profile `https://x.com/{user}`:
//
//   - The user's lists            /<user>/lists
//   - The user's likes            /<user>/likes
//
// For a generic path the strategy falls back to the apex profile (best
// effort dedup target).
//
// The strategy does NOT call x.com directly. Identity keys are
// username-keyed; a future patch may re-introduce a daemon-side client
// interface for username→user_id resolution.
package x

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ID is the strategy registry key. Stable across releases.
const ID = "XStrategy"

// CandidateTypeProfile, CandidateTypePost, CandidateTypeThread,
// CandidateTypeMedia, CandidateTypeList classify sub-path probes so the
// scoring layer can apply per-type thresholds and cap_k.
const (
	CandidateTypeProfile = "x_profile"
	CandidateTypePost    = "x_post"
	CandidateTypeThread  = "x_thread"
	CandidateTypeMedia   = "x_media"
	CandidateTypeList    = "x_list"
	CandidateTypeLikes   = "x_likes"
)

// Strategy implements lateral.LateralStrategy for X. It is purely
// URL-structural; identity keys are username-keyed.
type Strategy struct{}

// New constructs a Strategy.
func New() *Strategy { return &Strategy{} }

// ID returns the registry identifier.
func (*Strategy) ID() string { return ID }

// Family — X is a platform-keyed parent.
func (*Strategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }

// Preconditions — none beyond what readParent already enforces.
func (*Strategy) Preconditions() []string { return nil }

// Applies returns Matches=true on x.com and twitter.com (apex or any
// subdomain). Specificity is +1 for domain match, +1 for subdomain
// match (so e.g. mobile.twitter.com scores 2, parity with no-subdomain
// captures going through plain twitter.com).
func (*Strategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "" {
		return lateral.AppliesResult{}
	}
	switch {
	case host == "x.com" || strings.HasSuffix(host, ".x.com"):
		spec := 1
		if host != "x.com" {
			spec++
		}
		return lateral.AppliesResult{Matches: true, Specificity: spec}
	case host == "twitter.com" || strings.HasSuffix(host, ".twitter.com"):
		spec := 1
		if host != "twitter.com" {
			spec++
		}
		return lateral.AppliesResult{Matches: true, Specificity: spec}
	}
	return lateral.AppliesResult{}
}

// Probe parses the captured URL and proposes sub-path candidates.
func (s *Strategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	// Normalise to x.com so dedup keys collapse twitter→x.
	apex := "https://x.com"

	parts := pathSegments(u.Path)
	if len(parts) == 0 {
		return nil, nil
	}
	user := parts[0]
	if user == "" || isReservedUser(user) {
		return nil, nil
	}

	var out []lateral.Candidate

	// Always emit the profile candidate.
	out = append(out, lateral.Candidate{
		URL:           apex + "/" + user,
		CandidateType: CandidateTypeProfile,
		Strategy:      ID,
		IdentityKey:   identitykey.Build("x", identitykey.EntityProfile, user),
		Preview:       map[string]any{"username": user},
	})

	switch {
	case len(parts) >= 3 && parts[1] == "status":
		// Tweet. Add author media + thread anchor.
		tweetID := parts[2]
		out = append(out, lateral.Candidate{
			URL:           apex + "/" + user + "/media",
			CandidateType: CandidateTypeMedia,
			Strategy:      ID,
			IdentityKey:   identitykey.Build("x", identitykey.EntityProfile, user, "media"),
			Preview:       map[string]any{"username": user},
		})
		out = append(out, lateral.Candidate{
			URL:           apex + "/" + user + "/status/" + tweetID,
			CandidateType: CandidateTypeThread,
			Strategy:      ID,
			IdentityKey:   identitykey.Build("x", identitykey.EntityThread, user+"_"+tweetID),
			Preview:       map[string]any{"username": user, "tweet_id": tweetID},
		})
	case len(parts) == 1:
		// Bare profile capture. Surface the user's curated lists + likes.
		out = append(out, lateral.Candidate{
			URL:           apex + "/" + user + "/lists",
			CandidateType: CandidateTypeList,
			Strategy:      ID,
			IdentityKey:   identitykey.Build("x", identitykey.EntityProfile, user, "lists"),
			Preview:       map[string]any{"username": user},
		})
		out = append(out, lateral.Candidate{
			URL:           apex + "/" + user + "/likes",
			CandidateType: CandidateTypeLikes,
			Strategy:      ID,
			IdentityKey:   identitykey.Build("x", identitykey.EntityProfile, user, "likes"),
			Preview:       map[string]any{"username": user},
		})
	}

	return out, nil
}

// hostOf returns the lowercased host of rawURL or "" on parse error.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// pathSegments splits URL path into non-empty segments. Case is
// preserved (callers expect username casing — X is case-insensitive on
// lookup but canonical-cased URLs improve cache hits).
func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// isReservedUser returns true for path segments that aren't usernames
// (X reserves these for site-level surfaces).
func isReservedUser(s string) bool {
	switch strings.ToLower(s) {
	case "search", "explore", "i", "home", "notifications", "messages",
		"compose", "settings", "login", "signup", "tos", "privacy",
		"about", "intent", "share", "hashtag":
		return true
	}
	return false
}
