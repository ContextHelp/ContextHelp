package google

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	results  []string
	author   string
	related  []string
	articles []string
	err      error
}

func (s stubClient) SearchTopResults(_ context.Context, _ string, _ int) ([]string, error) {
	return s.results, s.err
}
func (s stubClient) ScholarAuthor(_ context.Context, _ string) (string, error) {
	return s.author, s.err
}
func (s stubClient) TrendsRelated(_ context.Context, _ string) ([]string, error) {
	return s.related, s.err
}
func (s stubClient) NewsTopicArticles(_ context.Context, _ string, _ int) ([]string, error) {
	return s.articles, s.err
}

func TestParent_AppliesAndSpecificity(t *testing.T) {
	p := NewParent(nil)
	cases := []struct {
		url  string
		spec int
	}{
		{"https://google.com", 1},
		{"https://www.google.com", 2},
		{"https://drive.google.com/file/abc", 2},
		{"https://forms.gle/abc", 2},
		{"https://example.com", 0},
	}
	for _, tc := range cases {
		res := p.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
		if tc.spec == 0 && res.Matches {
			t.Errorf("%s should not match", tc.url)
			continue
		}
		if tc.spec > 0 && (!res.Matches || res.Specificity != tc.spec) {
			t.Errorf("%s: spec=%d want=%d", tc.url, res.Specificity, tc.spec)
		}
	}
}

func TestParent_Probe(t *testing.T) {
	p := NewParent(nil)
	cands, _ := p.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://drive.google.com/file/abc"}, lateral.ActiveContext{})
	if len(cands) != 1 || cands[0].CandidateType != CandidateTypeGeneric {
		t.Fatalf("want one generic candidate, got %v", cands)
	}
}

func TestSearch_AppliesShadowsParent(t *testing.T) {
	p := NewParent(nil)
	s := NewSearch(nil)
	ev := lateral.CapturedEvent{SourceURL: "https://www.google.com/search?q=foo"}
	if s.Applies(context.Background(), ev).Specificity <= p.Applies(context.Background(), ev).Specificity {
		t.Fatal("search must outscore parent on /search")
	}
}

func TestSearch_Probe(t *testing.T) {
	s := NewSearch(stubClient{results: []string{"https://a.example.com", "https://b.example.com"}})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.google.com/search?q=foo+bar"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeQuery) {
		t.Fatal("missing query candidate")
	}
	count := 0
	for _, c := range cands {
		if c.CandidateType == CandidateTypeResult {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 result candidates, got %d", count)
	}
}

func TestScholar_Author(t *testing.T) {
	s := NewScholar(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://scholar.google.com/citations?user=ABC123"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeAuthor) {
		t.Fatal("citations URL should produce author candidate")
	}
}

func TestScholar_Cluster(t *testing.T) {
	s := NewScholar(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://scholar.google.com/scholar?cluster=99999"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypePaper) {
		t.Fatal("cluster URL should produce paper candidate")
	}
}

func TestTrends_RelatedExpansion(t *testing.T) {
	s := NewTrends(stubClient{related: []string{"a", "b"}})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://trends.google.com/trends/explore?q=ai"}, lateral.ActiveContext{})
	if len(cands) < 3 {
		t.Fatalf("expected primary + 2 related, got %d", len(cands))
	}
}

func TestNews_Topic(t *testing.T) {
	s := NewNews(stubClient{articles: []string{"https://x.example/a", "https://y.example/b"}})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://news.google.com/topics/ABC"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeTopic) {
		t.Fatal("missing topic candidate")
	}
	count := 0
	for _, c := range cands {
		if c.CandidateType == CandidateTypeArticle {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want 2 article candidates, got %d", count)
	}
}

func TestNews_Article(t *testing.T) {
	s := NewNews(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://news.google.com/articles/CAIid"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeArticle) {
		t.Fatal("article URL should produce article candidate")
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
