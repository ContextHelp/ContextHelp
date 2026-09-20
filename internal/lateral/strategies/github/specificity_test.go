package github

import (
	"context"
	"math/rand"
	"strconv"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// TestPropertyChildShadowsParent_GistOnGistURLs is a property-style test
// that confirms the dispatch invariant for github children:
//
//	For every URL where GistStrategy.Applies = true, the registry's
//	Dispatch must return GistStrategy (not GitHubStrategy), regardless
//	of the order of registration.
//
// This is the platform-family contract: highest specificity wins.
//
// We exercise the property over a generated set of gist URLs (random
// owners, random gist IDs) and over both registration orders (parent
// first, child first).
func TestPropertyChildShadowsParent_GistOnGistURLs(t *testing.T) {
	rng := rand.New(rand.NewSource(20260509))
	urls := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		owner := randAlnum(rng, 4+rng.Intn(10))
		gistID := randAlnum(rng, 8+rng.Intn(24))
		urls = append(urls, "https://gist.github.com/"+owner+"/"+gistID)
	}
	// Owner-only gist URLs
	for i := 0; i < 16; i++ {
		urls = append(urls, "https://gist.github.com/"+randAlnum(rng, 6))
	}

	parent := NewGitHubStrategy(Dependencies{})
	child := NewGistStrategy(Dependencies{})

	for _, order := range []string{"parent_first", "child_first"} {
		order := order
		t.Run(order, func(t *testing.T) {
			reg := lateral.NewRegistry()
			if order == "parent_first" {
				reg.Register(parent)
				reg.Register(child)
			} else {
				reg.Register(child)
				reg.Register(parent)
			}
			for _, u := range urls {
				dispatched := reg.Dispatch(context.Background(),
					lateral.CapturedEvent{SourceURL: u})
				if len(dispatched) != 1 {
					t.Errorf("URL %q: dispatched %d strategies, want 1", u, len(dispatched))
					continue
				}
				if dispatched[0].ID() != StrategyIDGist {
					t.Errorf("URL %q: dispatched %q, want %q",
						u, dispatched[0].ID(), StrategyIDGist)
				}
			}
		})
	}
}

// TestPropertyChildShadowsParent_AdvisoryOnAdvisoryURLs is the same
// property for SecurityAdvisoryStrategy on advisory URLs. Exercises
// both per-repo and global surfaces.
func TestPropertyChildShadowsParent_AdvisoryOnAdvisoryURLs(t *testing.T) {
	rng := rand.New(rand.NewSource(20260510))
	urls := make([]string, 0, 64)
	for i := 0; i < 32; i++ {
		// Per-repo advisories
		owner := randAlnum(rng, 4+rng.Intn(10))
		repo := randAlnum(rng, 4+rng.Intn(10))
		ghsa := "GHSA-" + randAlnum(rng, 4) + "-" + randAlnum(rng, 4) + "-" + randAlnum(rng, 4)
		urls = append(urls, "https://github.com/"+owner+"/"+repo+"/security/advisories/"+ghsa)
	}
	for i := 0; i < 32; i++ {
		// Global advisories
		urls = append(urls, "https://github.com/advisories/GHSA-"+randAlnum(rng, 4)+"-"+randAlnum(rng, 4)+"-"+strconv.Itoa(i))
	}

	parent := NewGitHubStrategy(Dependencies{})
	child := NewSecurityAdvisoryStrategy(Dependencies{})

	for _, order := range []string{"parent_first", "child_first"} {
		order := order
		t.Run(order, func(t *testing.T) {
			reg := lateral.NewRegistry()
			if order == "parent_first" {
				reg.Register(parent)
				reg.Register(child)
			} else {
				reg.Register(child)
				reg.Register(parent)
			}
			for _, u := range urls {
				dispatched := reg.Dispatch(context.Background(),
					lateral.CapturedEvent{SourceURL: u})
				if len(dispatched) != 1 {
					t.Errorf("URL %q: dispatched %d strategies, want 1", u, len(dispatched))
					continue
				}
				if dispatched[0].ID() != StrategyIDSecurityAdvisory {
					t.Errorf("URL %q: dispatched %q, want %q",
						u, dispatched[0].ID(), StrategyIDSecurityAdvisory)
				}
			}
		})
	}
}

// TestPropertyParentClaimsNonChildURLs confirms the symmetric case:
// for plain github.com URLs that no child claims, the parent picks them
// up.
func TestPropertyParentClaimsNonChildURLs(t *testing.T) {
	rng := rand.New(rand.NewSource(20260511))
	parent := NewGitHubStrategy(Dependencies{})
	gist := NewGistStrategy(Dependencies{})
	advisory := NewSecurityAdvisoryStrategy(Dependencies{})

	urls := []string{}
	// Plain repo URLs.
	for i := 0; i < 32; i++ {
		owner := randAlnum(rng, 4+rng.Intn(10))
		repo := randAlnum(rng, 4+rng.Intn(10))
		urls = append(urls, "https://github.com/"+owner+"/"+repo)
	}
	// Profiles.
	for i := 0; i < 16; i++ {
		urls = append(urls, "https://github.com/"+randAlnum(rng, 6))
	}
	// PR/issue URLs.
	for i := 0; i < 16; i++ {
		owner := randAlnum(rng, 4)
		repo := randAlnum(rng, 4)
		num := 1 + rng.Intn(1000)
		urls = append(urls, "https://github.com/"+owner+"/"+repo+"/pull/"+strconv.Itoa(num))
		urls = append(urls, "https://github.com/"+owner+"/"+repo+"/issues/"+strconv.Itoa(num))
	}

	reg := lateral.NewRegistry()
	reg.Register(parent)
	reg.Register(gist)
	reg.Register(advisory)

	for _, u := range urls {
		dispatched := reg.Dispatch(context.Background(),
			lateral.CapturedEvent{SourceURL: u})
		if len(dispatched) != 1 {
			t.Errorf("URL %q: dispatched %d strategies, want 1", u, len(dispatched))
			continue
		}
		if dispatched[0].ID() != StrategyIDGitHub {
			t.Errorf("URL %q: dispatched %q, want %q",
				u, dispatched[0].ID(), StrategyIDGitHub)
		}
	}
}

// TestSpecificity_ChildScoresStrictlyHigherThanParent is a basic
// invariant the property tests rely on. If the constants drift, the
// dispatcher would silently break.
func TestSpecificity_ChildScoresStrictlyHigherThanParent(t *testing.T) {
	if SpecificityChild <= SpecificityParent {
		t.Errorf("SpecificityChild (%d) must be > SpecificityParent (%d)",
			SpecificityChild, SpecificityParent)
	}
}

// randAlnum returns a random lowercase-alnum string of length n.
func randAlnum(rng *rand.Rand, n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(b)
}
