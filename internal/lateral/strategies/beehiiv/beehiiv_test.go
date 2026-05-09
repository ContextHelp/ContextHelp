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
