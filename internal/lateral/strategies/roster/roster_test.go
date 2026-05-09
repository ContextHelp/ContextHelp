package roster

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestRegister_DefaultGatesEnableEverything(t *testing.T) {
	reg := lateral.NewRegistry()
	Register(reg, Gates{}, Deps{})

	cases := []struct {
		url        string
		wantStrat  string
	}{
		{"https://x.com/jadb/status/1", "XStrategy"},
		{"https://www.linkedin.com/in/foo", "LinkedInStrategy"},
		{"https://arxiv.org/abs/2401.12345", "ArxivStrategy"},
		{"https://en.wikipedia.org/wiki/Turing_machine", "WikipediaStrategy"},
		{"https://www.youtube.com/watch?v=abc", "YouTubeStrategy"},
		{"https://www.google.com/search?q=foo", "GoogleSearchStrategy"},
		{"https://scholar.google.com/citations?user=A", "GoogleScholarStrategy"},
		{"https://trends.google.com/trends/explore?q=ai", "GoogleTrendsStrategy"},
		{"https://news.google.com/articles/x", "GoogleNewsStrategy"},
		{"https://drive.google.com/file/abc", "GoogleStrategy"},
		{"https://author.substack.com/p/post", "SubstackPostStrategy"},
		{"https://author.substack.com/", "SubstackPublicationStrategy"},
		{"https://substack.com/notes", "SubstackNotesStrategy"},
		{"https://name.beehiiv.com/p/post", "BeehiivPostStrategy"},
		{"https://name.beehiiv.com/", "BeehiivPublicationStrategy"},
		{"https://medium.com/@jadb", "MediumProfileStrategy"},
		{"https://medium.com/uxdesign", "MediumPublicationStrategy"},
	}
	for _, tc := range cases {
		strats := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
		if len(strats) == 0 {
			t.Errorf("%s: no strategy claimed", tc.url)
			continue
		}
		// For platform family the dispatcher returns the highest-spec match
		// at index 0 (no shape strategies registered here).
		if got := strats[0].ID(); got != tc.wantStrat {
			t.Errorf("%s: dispatched %s, want %s", tc.url, got, tc.wantStrat)
		}
	}
}

func TestRegister_DisabledStrategyDoesNotMatch(t *testing.T) {
	reg := lateral.NewRegistry()
	off := false
	g := AllEnabled()
	g.YouTubeStrategy = &off
	Register(reg, g, Deps{})

	strats := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/watch?v=abc"})
	for _, s := range strats {
		if s.ID() == "YouTubeStrategy" {
			t.Fatal("disabled YouTubeStrategy should not dispatch")
		}
	}
}

func TestAllEnabled_AllPointersTrue(t *testing.T) {
	g := AllEnabled()
	if !enabled(g.GoogleStrategy) || !enabled(g.YouTubeStrategy) {
		t.Fatal("AllEnabled must produce true flags")
	}
}
