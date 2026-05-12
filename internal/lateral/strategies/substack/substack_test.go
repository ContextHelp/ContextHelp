package substack

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
	if !p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://author.substack.com/p/x"}).Matches {
		t.Fatal("subdomain should match")
	}
	if p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://example.com/post"}).Matches {
		t.Fatal("non-substack should not match")
	}
}

func TestPublication_ShadowsParent(t *testing.T) {
	p := NewParent(nil)
	pub := NewPublication(nil)
	ev := lateral.CapturedEvent{SourceURL: "https://author.substack.com/"}
	if pub.Applies(context.Background(), ev).Specificity <= p.Applies(context.Background(), ev).Specificity {
		t.Fatal("publication must outscore parent on root path")
	}
}

func TestPost_Applies(t *testing.T) {
	post := NewPost(nil)
	if !post.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://author.substack.com/p/my-post"}).Matches {
		t.Fatal("/p/<slug> should match")
	}
}

func TestPost_Probe(t *testing.T) {
	post := NewPost(nil)
	cands, _ := post.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://author.substack.com/p/my-post"}, lateral.ActiveContext{})
	if len(cands) != 3 {
		t.Fatalf("post probe should produce 3 candidates, got %d", len(cands))
	}
}

func TestNotes_Variants(t *testing.T) {
	n := NewNotes(nil)
	cases := []string{
		"https://substack.com/notes",
		"https://substack.com/note/123",
		"https://substack.com/profile/123-jadb/note/abc",
	}
	for _, u := range cases {
		if !n.Applies(context.Background(), lateral.CapturedEvent{SourceURL: u}).Matches {
			t.Fatalf("%s should match notes", u)
		}
		cands, _ := n.Probe(context.Background(), lateral.CapturedEvent{SourceURL: u}, lateral.ActiveContext{})
		if len(cands) == 0 {
			t.Fatalf("%s probe yielded no candidates", u)
		}
	}
}

func TestPublication_CustomDomainResolved(t *testing.T) {
	pub := NewPublication(stubClient{slug: "author"})
	ev := lateral.CapturedEvent{SourceURL: "https://newsletter.example.com/"}
	// Without hints, customdomain returns Unknown — so this should NOT match;
	// covering the negative path.
	if pub.Applies(context.Background(), ev).Matches {
		t.Fatal("custom-domain without hints should not match (yet)")
	}
}

// TestHintsRoundTrip_CustomDomainPlatformAttribution pins T-0310:
// Hints set on CapturedEvent reach the strategy's customdomain
// detector and unlock custom-domain matching. Without the hint, the
// same URL declines.
func TestHintsRoundTrip_CustomDomainPlatformAttribution(t *testing.T) {
	pub := NewPublication(stubClient{slug: "author"})
	customURL := "https://newsletter.example.com/"

	// Negative: zero Hints — customdomain has no signal.
	if pub.Applies(context.Background(), lateral.CapturedEvent{SourceURL: customURL}).Matches {
		t.Fatal("custom-domain without Hints must not match")
	}

	// Positive: Hints.MetaPlatform=substack flips the verdict.
	ev := lateral.CapturedEvent{
		SourceURL: customURL,
		Hints:     lateral.Hints{MetaPlatform: "substack"},
	}
	if !pub.Applies(context.Background(), ev).Matches {
		t.Fatal("custom-domain with MetaPlatform=substack must match")
	}

	// Probe must produce candidates with non-empty IdentityKey.
	cands, _ := pub.Probe(context.Background(), ev, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("probe with Hints produced zero candidates")
	}
	for _, c := range cands {
		if c.IdentityKey == "" {
			t.Errorf("candidate %q missing IdentityKey", c.CandidateType)
		}
	}
}

// TestHintsRoundTrip_GeneratorAlternative pins that Generator-style
// hints (meta name=generator) also land at the detector — different
// pipeline, same wiring contract.
func TestHintsRoundTrip_GeneratorAlternative(t *testing.T) {
	pub := NewPublication(stubClient{slug: "author"})
	ev := lateral.CapturedEvent{
		SourceURL: "https://newsletter.example.com/",
		Hints:     lateral.Hints{Generator: "Substack"},
	}
	if !pub.Applies(context.Background(), ev).Matches {
		t.Fatal("Generator=Substack hint must unlock custom-domain match")
	}
}

// TestHintsRoundTrip_CanonicalHostFallback pins the rel=canonical
// hint path: when the page's canonical link points at *.substack.com,
// the detector classifies it as Substack.
func TestHintsRoundTrip_CanonicalHostFallback(t *testing.T) {
	pub := NewPublication(stubClient{slug: "author"})
	ev := lateral.CapturedEvent{
		SourceURL: "https://newsletter.example.com/",
		Hints:     lateral.Hints{CanonicalHost: "author.substack.com"},
	}
	if !pub.Applies(context.Background(), ev).Matches {
		t.Fatal("CanonicalHost=*.substack.com hint must unlock custom-domain match")
	}
}
