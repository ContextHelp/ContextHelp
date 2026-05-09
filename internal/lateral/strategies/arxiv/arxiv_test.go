package arxiv

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	authors []string
	err     error
}

func (s stubClient) ListAuthors(_ context.Context, _ string) ([]string, error) {
	return s.authors, s.err
}

func TestApplies(t *testing.T) {
	s := New(nil)
	if !s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/abs/2401.12345"}).Matches {
		t.Fatal("arxiv.org should match")
	}
	if s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: "https://example.com/abs/2401.12345"}).Matches {
		t.Fatal("non-arxiv should not match")
	}
}

func TestProbe_AbsPage(t *testing.T) {
	s := New(nil)
	cands, err := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/abs/2401.12345"}, lateral.ActiveContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) < 3 {
		t.Fatalf("expected ≥3 candidates (paper, pdf, mirror); got %d", len(cands))
	}
	if !hasType(cands, CandidateTypePaper) || !hasType(cands, CandidateTypePDF) || !hasType(cands, CandidateTypeMirror) {
		t.Fatalf("missing core candidate types: %v", typesOf(cands))
	}
}

func TestProbe_VersionedID(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/abs/2401.12345v3"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeVersion) {
		t.Fatalf("versioned input should emit version candidate")
	}
}

func TestProbe_LegacyID(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/abs/cs.LG/0303001"}, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("legacy slash-id should still produce candidates")
	}
}

func TestProbe_AuthorsViaClient(t *testing.T) {
	s := New(stubClient{authors: []string{"Alan Turing", "Ada Lovelace"}})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/abs/2401.12345"}, lateral.ActiveContext{})
	count := 0
	for _, c := range cands {
		if c.CandidateType == CandidateTypeAuthor {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 author candidates, got %d", count)
	}
}

func TestProbe_BogusPathReturnsEmpty(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://arxiv.org/list/cs.LG/recent"}, lateral.ActiveContext{})
	if len(cands) != 0 {
		t.Fatalf("non-paper surface should no-op; got %d", len(cands))
	}
}

func hasType(cs []lateral.Candidate, t string) bool {
	for _, c := range cs {
		if c.CandidateType == t {
			return true
		}
	}
	return false
}

func typesOf(cs []lateral.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CandidateType)
	}
	return out
}
