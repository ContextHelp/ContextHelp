package beehiiv

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	slug string
	err  error
}

func (s stubClient) ResolvePublication(_ context.Context, _ string) (string, error) {
	return s.slug, s.err
}

func TestParent_Applies(t *testing.T) {
	p := NewParent(nil)
	if !p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://name.beehiiv.com/p/post"}).Matches {
		t.Fatal("subdomain should match")
	}
	if p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://example.com/post"}).Matches {
		t.Fatal("non-beehiiv should not match")
	}
}

func TestPublication_ShadowsParent(t *testing.T) {
	p := NewParent(nil)
	pub := NewPublication(nil)
	ev := lateral.CapturedEvent{SourceURL: "https://name.beehiiv.com/"}
	if pub.Applies(context.Background(), ev).Specificity <= p.Applies(context.Background(), ev).Specificity {
		t.Fatal("publication must outscore parent on root")
	}
}

func TestPost_Probe(t *testing.T) {
	post := NewPost(nil)
	cands, _ := post.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://name.beehiiv.com/p/post-slug"}, lateral.ActiveContext{})
	if len(cands) != 3 {
		t.Fatalf("post probe should produce 3 candidates, got %d", len(cands))
	}
}

func TestPublication_AboutMatches(t *testing.T) {
	pub := NewPublication(nil)
	if !pub.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://name.beehiiv.com/about"}).Matches {
		t.Fatal("/about should match publication")
	}
}

// TestHintsRoundTrip_BeehiivCustomDomain pins T-0310 for beehiiv:
// MetaPlatform=beehiiv on a non-beehiiv host unlocks the parent
// strategy's custom-domain branch.
func TestHintsRoundTrip_BeehiivCustomDomain(t *testing.T) {
	parent := NewParent(stubClient{slug: "name"})
	customURL := "https://news.example.com/p/post-slug"

	// Negative: zero Hints — no signal, declines.
	if parent.Applies(context.Background(), lateral.CapturedEvent{SourceURL: customURL}).Matches {
		t.Fatal("beehiiv custom-domain without Hints must not match")
	}

	// Positive: MetaPlatform=beehiiv flips the verdict.
	ev := lateral.CapturedEvent{
		SourceURL: customURL,
		Hints:     lateral.Hints{MetaPlatform: "beehiiv"},
	}
	if !parent.Applies(context.Background(), ev).Matches {
		t.Fatal("beehiiv custom-domain with MetaPlatform=beehiiv must match")
	}

	// Probe yields candidates with non-empty IdentityKey.
	cands, _ := parent.Probe(context.Background(), ev, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("probe with Hints produced zero candidates")
	}
	for _, c := range cands {
		if c.IdentityKey == "" {
			t.Errorf("candidate %q missing IdentityKey", c.CandidateType)
		}
	}
}

// TestHintsRoundTrip_BeehiivCanonicalHost pins the rel=canonical
// hint route — pointing at *.beehiiv.com unlocks platform attribution.
func TestHintsRoundTrip_BeehiivCanonicalHost(t *testing.T) {
	parent := NewParent(stubClient{slug: "name"})
	ev := lateral.CapturedEvent{
		SourceURL: "https://news.example.com/p/post",
		Hints:     lateral.Hints{CanonicalHost: "name.beehiiv.com"},
	}
	if !parent.Applies(context.Background(), ev).Matches {
		t.Fatal("CanonicalHost=*.beehiiv.com must unlock custom-domain match")
	}
}
