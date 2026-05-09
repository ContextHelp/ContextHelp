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
