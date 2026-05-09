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
				if facet, _ := c.Preview["facet"].(string); facet == "archive" {
					archive, _ = c.Preview["identity_key"].(string)
				} else {
					landing, _ = c.Preview["identity_key"].(string)
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
