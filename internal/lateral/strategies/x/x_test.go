package x

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestApplies(t *testing.T) {
	cases := []struct {
		url        string
		matches    bool
		minSpecGTE int
	}{
		{"https://x.com/jadb", true, 1},
		{"https://twitter.com/jadb", true, 1},
		{"https://mobile.twitter.com/jadb", true, 2},
		{"https://example.com/x", false, 0},
		{"", false, 0},
	}
	s := New()
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			res := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
			if res.Matches != tc.matches {
				t.Fatalf("Matches = %v, want %v", res.Matches, tc.matches)
			}
			if res.Specificity < tc.minSpecGTE {
				t.Fatalf("Specificity = %d, want ≥ %d", res.Specificity, tc.minSpecGTE)
			}
		})
	}
}

func TestProbe_Tweet(t *testing.T) {
	s := New()
	cands, err := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://x.com/jadb/status/1234567890",
	}, lateral.ActiveContext{})
	if err != nil {
		t.Fatal(err)
	}
	gotTypes := candTypes(cands)
	for _, want := range []string{CandidateTypeProfile, CandidateTypeMedia, CandidateTypeThread} {
		if !contains(gotTypes, want) {
			t.Fatalf("missing %s in %v", want, gotTypes)
		}
	}
}

func TestProbe_BareProfile(t *testing.T) {
	s := New()
	cands, err := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://twitter.com/jadb",
	}, lateral.ActiveContext{})
	if err != nil {
		t.Fatal(err)
	}
	gotTypes := candTypes(cands)
	for _, want := range []string{CandidateTypeProfile, CandidateTypeList, CandidateTypeLikes} {
		if !contains(gotTypes, want) {
			t.Fatalf("missing %s in %v", want, gotTypes)
		}
	}
	// All candidate URLs must be normalised to x.com (twitter.com → x.com).
	for _, c := range cands {
		if !startsWith(c.URL, "https://x.com/") {
			t.Fatalf("non-normalised URL: %s", c.URL)
		}
	}
}

func TestProbe_ReservedUser(t *testing.T) {
	s := New()
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://x.com/search?q=foo",
	}, lateral.ActiveContext{})
	if len(cands) != 0 {
		t.Fatalf("reserved path /search should not emit candidates; got %d", len(cands))
	}
}

func TestProbe_HasIdentityKey(t *testing.T) {
	s := New()
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://x.com/jadb/status/1",
	}, lateral.ActiveContext{})
	for _, c := range cands {
		// Post-T-0309 strategies emit the typed field; the legacy
		// Preview-map form is acceptable too during the migration
		// window but x.Strategy now sets the typed field directly.
		key := c.IdentityKey
		if key == "" {
			key, _ = c.Preview["identity_key"].(string)
		}
		if key == "" {
			t.Fatalf("candidate %q missing identity_key", c.CandidateType)
		}
	}
}

func TestStrategyMetadata(t *testing.T) {
	s := New()
	if s.ID() != ID {
		t.Fatalf("ID mismatch")
	}
	if s.Family() != lateral.FamilyPlatform {
		t.Fatalf("Family must be FamilyPlatform")
	}
}

func candTypes(cs []lateral.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CandidateType)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
