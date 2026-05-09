package identity

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// TestResolver_NilPreviewFallsBackToURL guards the most-common pre-identitykey
// case: a candidate carries no Preview at all. Resolver must behave exactly
// as the URL-only resolver did before identity_key existed.
func TestResolver_NilPreviewFallsBackToURL(t *testing.T) {
	g := &fakeGraph{byURL: map[string]string{"https://x/y": "o-canonical"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{URL: "https://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-canonical" {
		t.Fatalf("expected URL match with nil preview, got %+v", res)
	}
}

// TestResolver_EmptyIdentityKeyFallsBackToURL covers the explicit
// degenerate case: identitykey.Build returned "" (per its contract for
// empty inputs) and the strategy still emitted the candidate. The resolver
// must skip the identity_key path and consult URL.
func TestResolver_EmptyIdentityKeyFallsBackToURL(t *testing.T) {
	g := &fakeGraph{byURL: map[string]string{"https://x/y": "o-canonical"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://x/y",
		Preview: map[string]any{identitykey.KeyField: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-canonical" {
		t.Fatalf("expected URL match with empty identity_key, got %+v", res)
	}
}

// TestResolver_MissingIdentityKeyEntryFallsBackToURL covers the case where
// Preview exists with other fields but no identity_key. identitykey.Get
// returns "" for missing entries; resolver must fall through to URL.
func TestResolver_MissingIdentityKeyEntryFallsBackToURL(t *testing.T) {
	g := &fakeGraph{byURL: map[string]string{"https://x/y": "o-canonical"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://x/y",
		Preview: map[string]any{"title": "Some Title", "stars": 42},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-canonical" {
		t.Fatalf("expected URL match with no identity_key entry, got %+v", res)
	}
}

// TestResolver_EmptyIdentityKeyNoURLMatch confirms the bottom of the
// fallback ladder: empty key, URL not in graph → probationary, never
// "merge with everything else just because identity_key is missing".
func TestResolver_EmptyIdentityKeyNoURLMatch(t *testing.T) {
	r := NewResolver(&fakeGraph{})
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://unknown",
		Preview: map[string]any{identitykey.KeyField: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.EdgeOnly {
		t.Fatalf("expected probationary fallback with empty key + URL miss, got %+v", res)
	}
}

// TestResolver_NonStringIdentityKeyFallsBackToURL guards against the
// type-assertion path inside identitykey.Get: if Preview[KeyField] holds
// a non-string value, Get returns "" and the resolver falls through to
// URL.
func TestResolver_NonStringIdentityKeyFallsBackToURL(t *testing.T) {
	g := &fakeGraph{byURL: map[string]string{"https://x/y": "o-canonical"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://x/y",
		Preview: map[string]any{identitykey.KeyField: 42},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-canonical" {
		t.Fatalf("expected URL match with non-string identity_key, got %+v", res)
	}
}
