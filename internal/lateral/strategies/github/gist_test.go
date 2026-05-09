package github

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestGistStrategy_IDFamilySpecificity(t *testing.T) {
	s := NewGistStrategy(Dependencies{})
	if s.ID() != StrategyIDGist {
		t.Errorf("ID = %q, want %q", s.ID(), StrategyIDGist)
	}
	if s.Family() != lateral.FamilyPlatform {
		t.Errorf("Family = %v, want FamilyPlatform", s.Family())
	}
	got := s.Applies(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://gist.github.com/jadb/abc123"})
	if got.Specificity != SpecificityChild {
		t.Errorf("Specificity = %d, want %d (child)", got.Specificity, SpecificityChild)
	}
}

func TestGistStrategy_AppliesMatrix(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"gist owner+id", "https://gist.github.com/jadb/abc123", true},
		{"gist owner only", "https://gist.github.com/jadb", true},
		{"gist root no path", "https://gist.github.com/", true},
		{"uppercase host", "https://Gist.GitHub.com/jadb/abc", true},

		{"github proper - declines", "https://github.com/jadb", false},
		{"raw subdomain", "https://raw.githubusercontent.com/owner/repo", false},
		{"random host", "https://example.com/jadb", false},
		{"empty", "", false},
	}
	s := NewGistStrategy(Dependencies{})
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
			if got.Matches != tc.want {
				t.Errorf("Matches = %v, want %v", got.Matches, tc.want)
			}
		})
	}
}

func TestGistStrategy_Probe_Skeleton(t *testing.T) {
	s := NewGistStrategy(Dependencies{})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://gist.github.com/jadb/abc123"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].CandidateType != TypeOwnerProfile {
		t.Errorf("skeleton mode: got %v, want [owner_profile]", candidateTypes(got))
	}
	if got[0].URL != "https://github.com/jadb" {
		t.Errorf("owner_profile URL = %q, want https://github.com/jadb", got[0].URL)
	}
}

func TestGistStrategy_Probe_FullClient_ExcludesCapturedGist(t *testing.T) {
	api := &stubAPIClient{
		listOwnerGistsFn: func(_ context.Context, login string) ([]GistSummary, error) {
			if login != "jadb" {
				t.Errorf("ListOwnerGists(%q) unexpected", login)
			}
			return []GistSummary{
				{ID: "abc123", Owner: "jadb"}, // captured — must be excluded
				{ID: "def456", Owner: "jadb"},
				{ID: "ghi789", Owner: "jadb"},
			}, nil
		},
	}
	s := NewGistStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://gist.github.com/jadb/abc123"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gistCount := 0
	for _, c := range got {
		if c.CandidateType == TypeOwnerGist {
			gistCount++
			if c.Preview["id"] == "abc123" {
				t.Error("captured gist abc123 must be excluded")
			}
		}
	}
	if gistCount != 2 {
		t.Errorf("got %d owner_gist candidates, want 2", gistCount)
	}
}

func TestGistStrategy_Probe_OwnerOnly_ListsAll(t *testing.T) {
	api := &stubAPIClient{
		listOwnerGistsFn: func(_ context.Context, _ string) ([]GistSummary, error) {
			return []GistSummary{{ID: "abc", Owner: "jadb"}, {ID: "def", Owner: "jadb"}}, nil
		},
	}
	s := NewGistStrategy(Dependencies{APIClient: api})
	got, _ := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://gist.github.com/jadb"},
		lateral.ActiveContext{})
	gistCount := 0
	for _, c := range got {
		if c.CandidateType == TypeOwnerGist {
			gistCount++
		}
	}
	if gistCount != 2 {
		t.Errorf("got %d owner_gist candidates, want 2 (no captured gist to exclude)", gistCount)
	}
}

func TestParseGistPath(t *testing.T) {
	cases := []struct {
		url        string
		wantOwner  string
		wantID     string
		wantOK     bool
	}{
		{"https://gist.github.com/jadb/abc123", "jadb", "abc123", true},
		{"https://gist.github.com/jadb", "jadb", "", true},
		{"https://gist.github.com/", "", "", false},
		{"https://github.com/jadb/abc", "", "", false}, // not gist host
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			owner, id, ok := parseGistPath(tc.url)
			if owner != tc.wantOwner || id != tc.wantID || ok != tc.wantOK {
				t.Errorf("parseGistPath(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tc.url, owner, id, ok, tc.wantOwner, tc.wantID, tc.wantOK)
			}
		})
	}
}
