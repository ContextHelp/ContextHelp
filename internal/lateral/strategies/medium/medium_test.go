package medium

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	pub    string
	author string
	err    error
}

func (s stubClient) ResolvePublication(_ context.Context, _ string) (string, error) {
	return s.pub, s.err
}

func (s stubClient) ResolveAuthor(_ context.Context, _ string) (string, error) {
	return s.author, s.err
}

func TestParent_AppliesAll(t *testing.T) {
	p := NewParent(nil)
	cases := []string{
		"https://medium.com/@author/post-abc123",
		"https://blog.medium.com/announcement",
		"https://medium.com/p/abc123",
	}
	for _, u := range cases {
		if !p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: u}).Matches {
			t.Fatalf("%s should match", u)
		}
	}
}

func TestPublication_ShadowsParent(t *testing.T) {
	p := NewParent(nil)
	pub := NewPublication(nil)
	ev := lateral.CapturedEvent{SourceURL: "https://uxdesign.medium.com"}
	if pub.Applies(context.Background(), ev).Specificity <= p.Applies(context.Background(), ev).Specificity {
		t.Fatal("publication must outscore parent on subdomain root")
	}
}

func TestPublication_PathBased(t *testing.T) {
	pub := NewPublication(nil)
	cands, _ := pub.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://medium.com/uxdesign"}, lateral.ActiveContext{})
	if len(cands) < 3 {
		t.Fatalf("publication probe should produce ≥3 candidates, got %d", len(cands))
	}
}

// TestPublication_ArchiveSharesIdentityKey guards the dedup contract:
// landing + archive candidates for the same publication must share the
// publication identity key so the resolver collapses them onto the same
// entity. Facets are differentiated via Preview, not via the key.
func TestPublication_ArchiveSharesIdentityKey(t *testing.T) {
	pub := NewPublication(nil)
	cases := []string{
		"https://medium.com/uxdesign",
		"https://uxdesign.medium.com",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			cands, _ := pub.Probe(context.Background(), lateral.CapturedEvent{SourceURL: u}, lateral.ActiveContext{})
			var landing, archive string
			for _, c := range cands {
				if c.CandidateType != CandidateTypePublication {
					continue
				}
				// Post-T-0309 the strategy emits the typed
				// IdentityKey field; the legacy Preview entry is
				// no longer set.
				if facet, _ := c.Preview["facet"].(string); facet == "archive" {
					archive = c.IdentityKey
				} else {
					landing = c.IdentityKey
				}
			}
			if landing == "" || archive == "" {
				t.Fatalf("missing landing or archive candidate (landing=%q archive=%q)", landing, archive)
			}
			if landing != archive {
				t.Fatalf("archive identity_key %q must match landing %q for resolver dedup", archive, landing)
			}
		})
	}
}

func TestProfile_Applies(t *testing.T) {
	pr := NewProfile(nil)
	if !pr.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://medium.com/@jadb"}).Matches {
		t.Fatal("@user should match profile")
	}
	if pr.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://medium.com/@/empty"}).Matches {
		t.Fatal("empty handle should not match")
	}
}

func TestProfile_Probe(t *testing.T) {
	pr := NewProfile(nil)
	cands, _ := pr.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://medium.com/@jadb"}, lateral.ActiveContext{})
	if len(cands) != 3 {
		t.Fatalf("want 3 profile probes, got %d", len(cands))
	}
}

// TestHintsRoundTrip_MediumCustomDomain pins T-0310 for medium:
// MetaPlatform=medium on a non-medium host unlocks the parent
// strategy's custom-domain branch.
func TestHintsRoundTrip_MediumCustomDomain(t *testing.T) {
	parent := NewParent(stubClient{pub: "uxdesign"})
	customURL := "https://blog.example.com/some-post"

	// Negative: no Hints — declines.
	if parent.Applies(context.Background(), lateral.CapturedEvent{SourceURL: customURL}).Matches {
		t.Fatal("medium custom-domain without Hints must not match")
	}

	// Positive: MetaPlatform unlocks match.
	ev := lateral.CapturedEvent{
		SourceURL: customURL,
		Hints:     lateral.Hints{MetaPlatform: "medium"},
	}
	if !parent.Applies(context.Background(), ev).Matches {
		t.Fatal("medium custom-domain with MetaPlatform=medium must match")
	}
}

// TestHintsRoundTrip_MediumGeneratorHint exercises the Generator
// signal pathway.
func TestHintsRoundTrip_MediumGeneratorHint(t *testing.T) {
	parent := NewParent(stubClient{})
	ev := lateral.CapturedEvent{
		SourceURL: "https://blog.example.com/post",
		Hints:     lateral.Hints{Generator: "Medium"},
	}
	if !parent.Applies(context.Background(), ev).Matches {
		t.Fatal("Generator=Medium hint must unlock custom-domain match")
	}
}
