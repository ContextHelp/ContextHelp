package roster

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Cassette tests assert each platform strategy correctly converts a
// representative captured URL into the expected sub-path candidates,
// using deterministic stub clients in lieu of recorded HTTP fixtures.
// When xrr lands in the project, these stubs are swapped for cassette
// playback without touching strategy code; the canary contract here is
// "given client output X, strategy Y emits candidates Z".

// recorderClient is a multi-method stub satisfying every platform
// client interface. Each method returns the field configured on the
// recorder; tests reset only the fields a given strategy touches.
type recorderClient struct {
	googleResults  []string
	googleAuthor   string
	googleRelated  []string
	googleArticles []string

	arxivAuthors []string

	wikidataQID string

	mediumPub    string
	mediumAuthor string

	substackSlug string

	beehiivSlug string

	ytChannel  string
	ytUploader string
}

func (r *recorderClient) SearchTopResults(_ context.Context, _ string, _ int) ([]string, error) {
	return r.googleResults, nil
}
func (r *recorderClient) ScholarAuthor(_ context.Context, _ string) (string, error) {
	return r.googleAuthor, nil
}
func (r *recorderClient) TrendsRelated(_ context.Context, _ string) ([]string, error) {
	return r.googleRelated, nil
}
func (r *recorderClient) NewsTopicArticles(_ context.Context, _ string, _ int) ([]string, error) {
	return r.googleArticles, nil
}
func (r *recorderClient) ListAuthors(_ context.Context, _ string) ([]string, error) {
	return r.arxivAuthors, nil
}
func (r *recorderClient) ResolveWikidata(_ context.Context, _, _ string) (string, error) {
	return r.wikidataQID, nil
}
func (r *recorderClient) ResolvePublication(_ context.Context, _ string) (string, error) {
	// Multiplexed across medium/substack/beehiiv. The right value is
	// set on the recorder before calling the strategy under test.
	switch {
	case r.substackSlug != "":
		return r.substackSlug, nil
	case r.beehiivSlug != "":
		return r.beehiivSlug, nil
	default:
		return r.mediumPub, nil
	}
}
func (r *recorderClient) ResolveAuthor(_ context.Context, _ string) (string, error) {
	return r.mediumAuthor, nil
}
func (r *recorderClient) ResolveChannel(_ context.Context, _ string) (string, error) {
	return r.ytChannel, nil
}
func (r *recorderClient) VideoUploader(_ context.Context, _ string) (string, error) {
	return r.ytUploader, nil
}

// dispatchOne picks the highest-specificity strategy for url and runs
// its Probe with no active context. Used by every cassette case.
func dispatchOne(t *testing.T, reg *lateral.Registry, url string) []lateral.Candidate {
	t.Helper()
	strats := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: url})
	if len(strats) == 0 {
		t.Fatalf("no strategy dispatched for %s", url)
	}
	cands, err := strats[0].Probe(context.Background(), lateral.CapturedEvent{SourceURL: url}, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("probe error: %v", err)
	}
	return cands
}

func TestCassette_GoogleSearch_TopResultsExpand(t *testing.T) {
	r := &recorderClient{googleResults: []string{"https://a.example/1", "https://b.example/2"}}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{Google: r})

	cands := dispatchOne(t, reg, "https://www.google.com/search?q=test+query")
	if got := countByType(cands, "google_result"); got != 2 {
		t.Fatalf("want 2 result candidates, got %d", got)
	}
}

func TestCassette_Arxiv_AuthorList(t *testing.T) {
	r := &recorderClient{arxivAuthors: []string{"Alan Turing", "Ada Lovelace", "Grace Hopper"}}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{Arxiv: r})

	cands := dispatchOne(t, reg, "https://arxiv.org/abs/2401.12345")
	if got := countByType(cands, "arxiv_author"); got != 3 {
		t.Fatalf("want 3 author candidates, got %d", got)
	}
}

func TestCassette_Wikipedia_WikidataResolved(t *testing.T) {
	r := &recorderClient{wikidataQID: "Q42"}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{Wikipedia: r})

	cands := dispatchOne(t, reg, "https://en.wikipedia.org/wiki/Hitchhiker")
	if countByType(cands, "wiki_wikidata") != 1 {
		t.Fatalf("expected 1 wikidata candidate")
	}
}

func TestCassette_YouTube_HandleResolves(t *testing.T) {
	r := &recorderClient{ytChannel: "UC1234"}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{YouTube: r})

	cands := dispatchOne(t, reg, "https://www.youtube.com/@somehandle")
	// vanity candidate + canonical channel + uploads = 3.
	if len(cands) < 3 {
		t.Fatalf("expected ≥3 candidates after vanity resolution, got %d", len(cands))
	}
}

func TestCassette_GoogleNews_TopArticles(t *testing.T) {
	r := &recorderClient{googleArticles: []string{"https://x.example/a1", "https://y.example/a2"}}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{Google: r})

	cands := dispatchOne(t, reg, "https://news.google.com/topics/CAxBC")
	if countByType(cands, "news_article") != 2 {
		t.Fatalf("want 2 article candidates")
	}
}

func TestCassette_GoogleTrends_RelatedExpand(t *testing.T) {
	r := &recorderClient{googleRelated: []string{"a", "b", "c"}}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{Google: r})

	cands := dispatchOne(t, reg, "https://trends.google.com/trends/explore?q=ai")
	if countByType(cands, "trends_topic") != 4 {
		t.Fatalf("want 1 primary + 3 related = 4 trends candidates")
	}
}

func TestCassette_AllPlatformsParseWithoutClient(t *testing.T) {
	// Smoke-test that every platform produces ≥1 candidate even with
	// nil clients (degraded mode).
	urls := []string{
		"https://x.com/jadb",
		"https://www.linkedin.com/company/anthropic",
		"https://arxiv.org/abs/2401.12345",
		"https://en.wikipedia.org/wiki/Turing_machine",
		"https://www.youtube.com/watch?v=abc",
		"https://medium.com/@author/post-abc",
		"https://author.substack.com/p/title",
		"https://name.beehiiv.com/p/title",
		"https://www.google.com/search?q=foo",
		"https://drive.google.com/file/abc",
	}
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{})
	for _, u := range urls {
		cands := dispatchOne(t, reg, u)
		if len(cands) == 0 {
			t.Errorf("%s: zero candidates in degraded mode", u)
		}
	}
}

func countByType(cs []lateral.Candidate, ct string) int {
	n := 0
	for _, c := range cs {
		if c.CandidateType == ct {
			n++
		}
	}
	return n
}
