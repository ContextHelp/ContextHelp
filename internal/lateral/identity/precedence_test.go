package identity

import (
	"context"
	"testing"
)

// TestResolver_IdentityKeyWinsOverURL pins the precedence rule: when a
// candidate carries a non-empty identity_key in its Preview, the resolver
// trusts the identity_key match over the URL match. The two graph entries
// are deliberately set up so URL would resolve to one canonical and
// identity_key to another; identity_key must win.
func TestResolver_IdentityKeyWinsOverURL(t *testing.T) {
	g := &fakeGraph{
		byURL: map[string]string{"https://example.com/x": "o-from-url"},
		byKey: map[string]string{"github/repo/owner/repo": "o-from-key"},
	}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://example.com/x",
		Preview: preview("github/repo/owner/repo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-from-key" {
		t.Fatalf("identity_key must win over URL; got %+v", res)
	}
}

// TestResolver_DifferentIdentityKeysSameURL covers the URL-collision case:
// two captures land on the same URL but the strategy pinned distinct
// identity_keys. Each candidate must resolve to its own canonical (or to
// probationary if the key is unknown) — never merge under the URL match.
func TestResolver_DifferentIdentityKeysSameURL(t *testing.T) {
	g := &fakeGraph{
		byURL: map[string]string{"https://shared.example/x": "o-shared-url"},
		byKey: map[string]string{
			"github/repo/alice/proj": "o-alice",
			"github/repo/bob/proj":   "o-bob",
		},
	}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://shared.example/x",
		Preview: preview("github/repo/alice/proj"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CanonicalID != "o-alice" {
		t.Fatalf("expected o-alice via identity_key, got %+v", res)
	}
	res2, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://shared.example/x",
		Preview: preview("github/repo/bob/proj"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.CanonicalID != "o-bob" {
		t.Fatalf("expected o-bob via identity_key, got %+v", res2)
	}
	if res.CanonicalID == res2.CanonicalID {
		t.Fatalf("URL-equal candidates with different identity_keys must not merge")
	}
}

// TestResolver_SameIdentityKeyDifferentURLs covers the dedup case: same
// logical entity captured via two URL skews (e.g. mobile host vs custom
// domain) carrying the same identity_key. Both must resolve to one
// canonical. URL match path is irrelevant here.
func TestResolver_SameIdentityKeyDifferentURLs(t *testing.T) {
	g := &fakeGraph{byKey: map[string]string{"medium/article/user/slug": "o-article"}}
	r := NewResolver(g)
	resA, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://m.medium.com/@user/slug",
		Preview: preview("medium/article/user/slug"),
	})
	if err != nil {
		t.Fatal(err)
	}
	resB, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://customblog.example.com/slug",
		Preview: preview("medium/article/user/slug"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resA.CanonicalID != "o-article" || resB.CanonicalID != "o-article" {
		t.Fatalf("expected both URLs to resolve to o-article, got %+v / %+v", resA, resB)
	}
}
