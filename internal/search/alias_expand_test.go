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

func TestExpandQuery_NoAliases(t *testing.T) {
	r := &stubResolver{m: map[string][]string{}}
	got := ExpandQuery(context.Background(), "docker container", r)
	if got != "docker container" {
		t.Errorf("expected original query, got %q", got)
	}
}

func TestExpandQuery_AddsAliases(t *testing.T) {
	r := &stubResolver{m: map[string][]string{
		"k8s": {"kubernetes"},
	}}
	got := ExpandQuery(context.Background(), "k8s deployment", r)
	if got != "k8s deployment kubernetes" {
		t.Errorf("unexpected expansion: %q", got)
	}
}

func TestExpandQuery_NilResolver(t *testing.T) {
	got := ExpandQuery(context.Background(), "k8s deployment", nil)
	if got != "k8s deployment" {
		t.Errorf("expected original query with nil resolver, got %q", got)
	}
}

func TestExpandQuery_DeduplicatesTerms(t *testing.T) {
	// "kubernetes" is already present in the query; alias should not be added again.
	r := &stubResolver{m: map[string][]string{
		"k8s": {"kubernetes"},
	}}
	got := ExpandQuery(context.Background(), "k8s kubernetes", r)
	if got != "k8s kubernetes" {
		t.Errorf("expected no duplication, got %q", got)
	}
}

func TestExpandQuery_ResolverError_ReturnsOriginal(t *testing.T) {
	r := &stubResolver{err: errors.New("store unavailable")}
	got := ExpandQuery(context.Background(), "k8s deployment", r)
	if got != "k8s deployment" {
		t.Errorf("expected original on error, got %q", got)
	}
}

func TestExpandQuery_EmptyQuery(t *testing.T) {
	r := &stubResolver{m: map[string][]string{}}
	got := ExpandQuery(context.Background(), "", r)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExpandQuery_MultipleAliasesForOneTerm(t *testing.T) {
	r := &stubResolver{m: map[string][]string{
		"ml": {"machine-learning", "artificial-intelligence"},
	}}
	got := ExpandQuery(context.Background(), "ml model", r)
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
	got := ExpandQuery(context.Background(), "k8s kubernetes", r)
	if got != "k8s kubernetes" {
		t.Errorf("expected no duplication on case difference, got %q", got)
	}
}
