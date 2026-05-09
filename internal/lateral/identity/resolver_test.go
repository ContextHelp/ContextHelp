package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// TODO: replace with kit's domain.MockRepository (kit/runtime/domain) once the
// real adapter lands; the fake will desynchronise from the canonical interface
// otherwise.
type fakeGraph struct {
	byURL    map[string]string
	byKey    map[string]string
	failNext bool
}

func (g *fakeGraph) FindByURL(_ context.Context, url string) (string, error) {
	if g.failNext {
		g.failNext = false
		return "", errors.New("transient")
	}
	if id, ok := g.byURL[url]; ok {
		return id, nil
	}
	return "", ErrNotFound
}

func (g *fakeGraph) FindByIdentityKey(_ context.Context, key string) (string, error) {
	if id, ok := g.byKey[key]; ok {
		return id, nil
	}
	return "", ErrNotFound
}

// preview is shorthand for the strategy-side Preview map carrying an
// identity_key, mirroring identitykey.Set semantics.
func preview(key string) map[string]any {
	return map[string]any{identitykey.KeyField: key}
}

func TestResolver_ExactURLMatchEdgeOnly(t *testing.T) {
	r := NewResolver(&fakeGraph{byURL: map[string]string{"https://x/y": "o-canonical"}})
	res, err := r.Resolve(context.Background(), Candidate{URL: "https://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-canonical" {
		t.Fatalf("expected edge-only with canonical o-canonical, got %+v", res)
	}
}

func TestResolver_NoMatchProbationary(t *testing.T) {
	r := NewResolver(&fakeGraph{})
	res, err := r.Resolve(context.Background(), Candidate{URL: "https://new"})
	if err != nil {
		t.Fatal(err)
	}
	if res.EdgeOnly {
		t.Fatal("expected probationary fallback")
	}
}

func TestResolver_IdentityKeyFromPreview(t *testing.T) {
	g := &fakeGraph{byKey: map[string]string{"github/owner/foo": "o-foo"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://github.com/foo",
		Preview: preview("github/owner/foo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-foo" {
		t.Fatalf("expected edge-only via identity key, got %+v", res)
	}
}

// TestResolver_IdentityKeyFromTypedField pins T-0307: the typed
// Candidate.IdentityKey reaches FindByIdentityKey just like the legacy
// Preview-map form.
func TestResolver_IdentityKeyFromTypedField(t *testing.T) {
	g := &fakeGraph{byKey: map[string]string{"github/owner/foo": "o-foo"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:         "https://github.com/foo",
		IdentityKey: "github/owner/foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-foo" {
		t.Fatalf("expected edge-only via typed key, got %+v", res)
	}
}

// TestResolver_TypedFieldWinsOverPreview pins the precedence rule:
// when both forms are set with different non-empty values, the typed
// field wins. This makes the migration from Preview-map to typed-field
// safe — a stale Preview entry can't override a freshly-set typed key.
func TestResolver_TypedFieldWinsOverPreview(t *testing.T) {
	g := &fakeGraph{byKey: map[string]string{
		"github/owner/typed":   "o-typed",
		"github/owner/preview": "o-preview",
	}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:         "https://example",
		IdentityKey: "github/owner/typed",
		Preview:     preview("github/owner/preview"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-typed" {
		t.Fatalf("expected typed field to win; got %+v", res)
	}
}

// TestResolver_TypedFieldMissingFallsBackToPreview pins that an empty
// typed field doesn't short-circuit the Preview-map fallback during
// the T-0309 migration window.
func TestResolver_TypedFieldMissingFallsBackToPreview(t *testing.T) {
	g := &fakeGraph{byKey: map[string]string{"github/owner/foo": "o-foo"}}
	r := NewResolver(g)
	res, err := r.Resolve(context.Background(), Candidate{
		URL:         "https://github.com/foo",
		IdentityKey: "",
		Preview:     preview("github/owner/foo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EdgeOnly || res.CanonicalID != "o-foo" {
		t.Fatalf("expected Preview-map fallback to resolve, got %+v", res)
	}
}
