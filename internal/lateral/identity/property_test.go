package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// urlOnlyResolve is the pre-identitykey resolve path: URL match → canonical
// or probationary. Used as the property-test reference.
func urlOnlyResolve(ctx context.Context, g Graph, c Candidate) (Result, bool) {
	id, err := g.FindByURL(ctx, c.URL)
	if err == nil {
		return Result{EdgeOnly: true, CanonicalID: id}, true
	}
	if !errors.Is(err, ErrNotFound) {
		return Result{}, false
	}
	return Result{EdgeOnly: false}, true
}

// TestResolver_EmptyKeyMatchesURLOnlyBehaviour is the property-style guard
// the brief asks for: for any candidate set whose identity_keys are all
// empty (or absent), the full Resolver must produce the same Result as a
// URL-only resolver. Pins backward compatibility against any future
// identity_key-path leakage when the key is unset.
func TestResolver_EmptyKeyMatchesURLOnlyBehaviour(t *testing.T) {
	graph := &fakeGraph{
		byURL: map[string]string{
			"https://a/1":      "o-a1",
			"https://b/2":      "o-b2",
			"https://shared/3": "o-shared",
		},
		byKey: map[string]string{
			// Populated to prove these never get hit when identity_key is empty.
			"any/key/value": "o-poison",
		},
	}
	r := NewResolver(graph)
	ctx := context.Background()

	// Every variation of "no identity_key" the wild can produce.
	previewVariants := []map[string]any{
		nil,
		{},
		{identitykey.KeyField: ""},
		{"title": "Some Title"},
		{identitykey.KeyField: 0},
		{identitykey.KeyField: nil},
	}

	urls := []string{
		"https://a/1",            // matches
		"https://b/2",            // matches
		"https://shared/3",       // matches
		"https://unknown/x",      // miss → probationary
		"https://another-miss/y", // miss → probationary
	}

	for _, pv := range previewVariants {
		for _, u := range urls {
			c := Candidate{URL: u, Preview: pv}
			gotRes, err := r.Resolve(ctx, c)
			if err != nil {
				t.Fatalf("Resolve(%v) returned err: %v", c, err)
			}
			wantRes, ok := urlOnlyResolve(ctx, graph, c)
			if !ok {
				t.Fatalf("urlOnlyResolve unexpectedly errored for %v", c)
			}
			if gotRes != wantRes {
				t.Fatalf("preview=%v url=%q\n got:  %+v\n want: %+v", pv, u, gotRes, wantRes)
			}
		}
	}
}

// TestResolver_AllEmptySetNoCrossMerging is a stronger property check: a
// batch of candidates with empty keys must not merge across distinct URLs.
// We resolve each candidate and assert that all probationary candidates
// stay probationary (no false canonical pickup) and all URL-matched
// candidates resolve to their own URL's canonical.
func TestResolver_AllEmptySetNoCrossMerging(t *testing.T) {
	graph := &fakeGraph{
		byURL: map[string]string{
			"https://known/1": "o-1",
			"https://known/2": "o-2",
		},
	}
	r := NewResolver(graph)
	ctx := context.Background()

	cases := []struct {
		url  string
		want Result
	}{
		{"https://known/1", Result{EdgeOnly: true, CanonicalID: "o-1"}},
		{"https://known/2", Result{EdgeOnly: true, CanonicalID: "o-2"}},
		{"https://miss/a", Result{EdgeOnly: false}},
		{"https://miss/b", Result{EdgeOnly: false}},
	}
	for _, tc := range cases {
		got, err := r.Resolve(ctx, Candidate{URL: tc.url})
		if err != nil {
			t.Fatalf("resolve %s: %v", tc.url, err)
		}
		if got != tc.want {
			t.Fatalf("url=%s got=%+v want=%+v", tc.url, got, tc.want)
		}
	}
}
