package search

import (
	"context"
	"errors"
	"testing"
)

// stubResolver is a test-only AliasResolver.
type stubResolver struct {
	m   map[string][]string
	err error
}

func (s *stubResolver) ResolveAliases(_ context.Context, terms []string) (map[string][]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string][]string, len(terms))
	for _, t := range terms {
		if aliases, ok := s.m[t]; ok {
			out[t] = aliases
		}
	}
	return out, nil
}

// expand runs ExpandQuery and fails the test on an error.
func expand(t *testing.T, query string, r AliasResolver) string {
	t.Helper()
	got, err := ExpandQuery(context.Background(), query, r)
	if err != nil {
		t.Fatalf("ExpandQuery(%q): %v", query, err)
	}
	return got
}

func TestExpandQuery_NoAliases(t *testing.T) {
	r := &stubResolver{m: map[string][]string{}}
	got := expand(t, "docker container", r)
	if got != "docker container" {
		t.Errorf("expected original query, got %q", got)
	}
}

func TestExpandQuery_AddsAliases(t *testing.T) {
	r := &stubResolver{m: map[string][]string{
		"k8s": {"kubernetes"},
	}}
	got := expand(t, "k8s deployment", r)
	if got != "k8s deployment kubernetes" {
		t.Errorf("unexpected expansion: %q", got)
	}
}

func TestExpandQuery_NilResolver(t *testing.T) {
	got := expand(t, "k8s deployment", nil)
	if got != "k8s deployment" {
		t.Errorf("expected original query with nil resolver, got %q", got)
	}
}

func TestExpandQuery_DeduplicatesTerms(t *testing.T) {
	// "kubernetes" is already present in the query; alias should not be added again.
	r := &stubResolver{m: map[string][]string{
		"k8s": {"kubernetes"},
	}}
	got := expand(t, "k8s kubernetes", r)
	if got != "k8s kubernetes" {
		t.Errorf("expected no duplication, got %q", got)
	}
}

// A resolver error is a failed lookup, not "no aliases": it reaches the
// caller instead of searching the unexpanded query as if nothing failed.
func TestExpandQuery_ResolverError_Propagates(t *testing.T) {
	cause := errors.New("store unavailable")
	r := &stubResolver{err: cause}
	got, err := ExpandQuery(context.Background(), "k8s deployment", r)
	if !errors.Is(err, cause) {
		t.Fatalf("ExpandQuery error = %v, want it to wrap %v", err, cause)
	}
	if got != "" {
		t.Errorf("ExpandQuery returned %q alongside an error, want \"\"", got)
	}
}

func TestExpandQuery_EmptyQuery(t *testing.T) {
	r := &stubResolver{m: map[string][]string{}}
	got := expand(t, "", r)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExpandQuery_MultipleAliasesForOneTerm(t *testing.T) {
	r := &stubResolver{m: map[string][]string{
		"ml": {"machine-learning", "artificial-intelligence"},
	}}
	got := expand(t, "ml model", r)
	want := "ml model machine-learning artificial-intelligence"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExpandQuery_CaseInsensitiveDeduplicate(t *testing.T) {
	// Alias "Kubernetes" (different case) should still be deduped against "kubernetes" in query.
	r := &stubResolver{m: map[string][]string{
		"k8s": {"Kubernetes"},
	}}
	got := expand(t, "k8s kubernetes", r)
	if got != "k8s kubernetes" {
		t.Errorf("expected no duplication on case difference, got %q", got)
	}
}
