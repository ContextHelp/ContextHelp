package wikipedia

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	qid string
	err error
}

func (s stubClient) ResolveWikidata(_ context.Context, _, _ string) (string, error) {
	return s.qid, s.err
}

func TestApplies(t *testing.T) {
	s := New(nil)
	cases := []struct {
		url     string
		matches bool
	}{
		{"https://en.wikipedia.org/wiki/Turing_machine", true},
		{"https://fr.wikipedia.org/wiki/Machine_de_Turing", true},
		{"https://wikipedia.org/", true},
		{"https://example.com/wiki/Turing_machine", false},
	}
	for _, tc := range cases {
		if got := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url}); got.Matches != tc.matches {
			t.Errorf("%s: matches=%v want=%v", tc.url, got.Matches, tc.matches)
		}
	}
}

func TestProbe_BaseFacets(t *testing.T) {
	s := New(nil)
	cands, err := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://en.wikipedia.org/wiki/Turing_machine"}, lateral.ActiveContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{CandidateTypeArticle, CandidateTypeTalk, CandidateTypeHistory, CandidateTypeCategories, CandidateTypeInterwiki} {
		if !hasType(cands, want) {
			t.Fatalf("missing %s in %v", want, typesOf(cands))
		}
	}
}

func TestProbe_LanguageEdition(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://fr.wikipedia.org/wiki/Machine_de_Turing"}, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("french article should produce candidates")
	}
	for _, c := range cands {
		if !strings.HasPrefix(c.URL, "https://fr.") && !strings.HasPrefix(c.URL, "https://www.wikidata.org") {
			t.Fatalf("language not preserved: %s", c.URL)
		}
	}
}

func TestProbe_NamespaceSkipped(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://en.wikipedia.org/wiki/Special:Random"}, lateral.ActiveContext{})
	if len(cands) != 0 {
		t.Fatalf("special-namespace should no-op")
	}
}

func TestProbe_WikidataViaClient(t *testing.T) {
	s := New(stubClient{qid: "Q12345"})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://en.wikipedia.org/wiki/Turing_machine"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeWikidata) {
		t.Fatal("client should yield wikidata candidate")
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
