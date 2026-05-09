package roster

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// TestIntegration_PlatformRoster wires every P4 platform strategy into
// a single registry, dispatches one representative URL per platform,
// and asserts the dispatcher routes to the expected (most-specific)
// strategy and that the candidate types match the spec.
func TestIntegration_PlatformRoster(t *testing.T) {
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{})

	cases := []struct {
		name      string
		url       string
		strategy  string
		wantTypes []string // each must appear at least once
	}{
		{
			name:      "x_tweet",
			url:       "https://x.com/jadb/status/1234567890",
			strategy:  "XStrategy",
			wantTypes: []string{"x_profile", "x_thread"},
		},
		{
			name:      "linkedin_company",
			url:       "https://www.linkedin.com/company/anthropic",
			strategy:  "LinkedInStrategy",
			wantTypes: []string{"linkedin_org"},
		},
		{
			name:      "arxiv_paper",
			url:       "https://arxiv.org/abs/2401.12345",
			strategy:  "ArxivStrategy",
			wantTypes: []string{"arxiv_paper", "arxiv_pdf", "arxiv_mirror"},
		},
		{
			name:      "wikipedia_article",
			url:       "https://en.wikipedia.org/wiki/Turing_machine",
			strategy:  "WikipediaStrategy",
			wantTypes: []string{"wiki_article", "wiki_talk", "wiki_history", "wiki_categories"},
		},
		{
			name:      "youtube_watch",
			url:       "https://www.youtube.com/watch?v=ABCdef12345",
			strategy:  "YouTubeStrategy",
			wantTypes: []string{"yt_video"},
		},
		{
			name:      "youtu_be_short",
			url:       "https://youtu.be/ABC",
			strategy:  "YouTubeStrategy",
			wantTypes: []string{"yt_video"},
		},
		{
			name:      "google_search",
			url:       "https://www.google.com/search?q=lateral+capture",
			strategy:  "GoogleSearchStrategy",
			wantTypes: []string{"google_query"},
		},
		{
			name:      "google_scholar",
			url:       "https://scholar.google.com/citations?user=ABC",
			strategy:  "GoogleScholarStrategy",
			wantTypes: []string{"scholar_author"},
		},
		{
			name:      "google_trends",
			url:       "https://trends.google.com/trends/explore?q=ai",
			strategy:  "GoogleTrendsStrategy",
			wantTypes: []string{"trends_topic"},
		},
		{
			name:      "google_news_topic",
			url:       "https://news.google.com/topics/CAxBC",
			strategy:  "GoogleNewsStrategy",
			wantTypes: []string{"news_topic"},
		},
		{
			name:      "google_other",
			url:       "https://drive.google.com/file/abc",
			strategy:  "GoogleStrategy",
			wantTypes: []string{"google_generic"},
		},
		{
			name:      "medium_profile",
			url:       "https://medium.com/@jadb",
			strategy:  "MediumProfileStrategy",
			wantTypes: []string{"medium_author"},
		},
		{
			name:      "medium_publication_path",
			url:       "https://medium.com/uxdesign",
			strategy:  "MediumPublicationStrategy",
			wantTypes: []string{"medium_publication"},
		},
		{
			name:      "medium_publication_subdomain",
			url:       "https://uxdesign.medium.com",
			strategy:  "MediumPublicationStrategy",
			wantTypes: []string{"medium_publication"},
		},
		{
			name:      "substack_post",
			url:       "https://author.substack.com/p/some-post",
			strategy:  "SubstackPostStrategy",
			wantTypes: []string{"substack_post", "substack_publication"},
		},
		{
			name:      "substack_publication_root",
			url:       "https://author.substack.com/",
			strategy:  "SubstackPublicationStrategy",
			wantTypes: []string{"substack_publication", "substack_archive"},
		},
		{
			name:      "substack_notes_global",
			url:       "https://substack.com/notes",
			strategy:  "SubstackNotesStrategy",
			wantTypes: []string{"substack_note"},
		},
		{
			name:      "beehiiv_post",
			url:       "https://name.beehiiv.com/p/post-slug",
			strategy:  "BeehiivPostStrategy",
			wantTypes: []string{"beehiiv_post", "beehiiv_publication"},
		},
		{
			name:      "beehiiv_publication",
			url:       "https://name.beehiiv.com/",
			strategy:  "BeehiivPublicationStrategy",
			wantTypes: []string{"beehiiv_publication", "beehiiv_archive"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			strats := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
			if len(strats) == 0 {
				t.Fatalf("no strategy dispatched")
			}
			if got := strats[0].ID(); got != tc.strategy {
				t.Fatalf("dispatched %s, want %s", got, tc.strategy)
			}
			cands, err := strats[0].Probe(context.Background(), lateral.CapturedEvent{SourceURL: tc.url}, lateral.ActiveContext{})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantTypes {
				if countByType(cands, want) == 0 {
					t.Errorf("missing %s in %v", want, candTypes(cands))
				}
			}
			// Every candidate must carry a non-empty identity_key —
			// post-T-0309 strategies write to the typed field, but
			// the legacy Preview-map form is also accepted for
			// one-cycle backward compat.
			for _, c := range cands {
				key := c.IdentityKey
				if key == "" {
					key, _ = c.Preview["identity_key"].(string)
				}
				if key == "" {
					t.Errorf("candidate %s missing identity_key", c.CandidateType)
				}
			}
		})
	}
}

// TestIntegration_ChildrenShadowParents asserts that for every platform
// family with children, dispatching a child-URL produces the child (not
// the parent) at index 0.
func TestIntegration_ChildrenShadowParents(t *testing.T) {
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{})

	pairs := []struct{ url, child string }{
		{"https://www.google.com/search?q=foo", "GoogleSearchStrategy"},
		{"https://scholar.google.com/scholar?q=foo", "GoogleScholarStrategy"},
		{"https://trends.google.com/trends/explore?q=foo", "GoogleTrendsStrategy"},
		{"https://news.google.com/topics/abc", "GoogleNewsStrategy"},
		{"https://medium.com/uxdesign", "MediumPublicationStrategy"},
		{"https://medium.com/@jadb", "MediumProfileStrategy"},
		{"https://author.substack.com/", "SubstackPublicationStrategy"},
		{"https://author.substack.com/p/x", "SubstackPostStrategy"},
		{"https://substack.com/notes", "SubstackNotesStrategy"},
		{"https://name.beehiiv.com/", "BeehiivPublicationStrategy"},
		{"https://name.beehiiv.com/p/x", "BeehiivPostStrategy"},
	}
	for _, p := range pairs {
		strats := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: p.url})
		if len(strats) == 0 || strats[0].ID() != p.child {
			got := "<none>"
			if len(strats) > 0 {
				got = strats[0].ID()
			}
			t.Errorf("%s: child %s did not shadow parent (got %s)", p.url, p.child, got)
		}
	}
}

func candTypes(cs []lateral.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CandidateType)
	}
	return out
}
