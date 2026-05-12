package github

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestGitHubStrategy_IDFamily(t *testing.T) {
	s := NewGitHubStrategy(Dependencies{})
	if s.ID() != StrategyIDGitHub {
		t.Errorf("ID = %q, want %q", s.ID(), StrategyIDGitHub)
	}
	if s.Family() != lateral.FamilyPlatform {
		t.Errorf("Family = %v, want FamilyPlatform", s.Family())
	}
}

func TestGitHubStrategy_Applies(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
		spec int
	}{
		{"repo", "https://github.com/samber/lo", true, SpecificityParent},
		{"profile", "https://github.com/jadb", true, SpecificityParent},
		{"www subdomain", "https://www.github.com/jadb", true, SpecificityParent},
		{"trailing slash", "https://github.com/jadb/", true, SpecificityParent},
		{"uppercase host", "https://GitHub.com/jadb", true, SpecificityParent},
		{"PR", "https://github.com/owner/repo/pull/42", true, SpecificityParent},
		{"issue", "https://github.com/owner/repo/issues/7", true, SpecificityParent},
		{"sponsor", "https://github.com/sponsors/jadb", true, SpecificityParent},

		{"gist subdomain - parent declines", "https://gist.github.com/jadb/abc", false, 0},
		{"non-github", "https://gitlab.com/jadb", false, 0},
		{"malformed", "://nope", false, 0},
		{"empty", "", false, 0},
		{"raw subdomain", "https://raw.githubusercontent.com/owner/repo/main/README.md", false, 0},

		// Advisory paths must decline so SecurityAdvisoryStrategy claims
		// uncontested at higher specificity, and JIT can fall back when
		// the child is not registered.
		{"advisory global list - parent declines", "https://github.com/advisories", false, 0},
		{"advisory global ghsa - parent declines", "https://github.com/advisories/GHSA-aaa", false, 0},
		{"advisory per-repo - parent declines", "https://github.com/owner/repo/security/advisories", false, 0},
		{"advisory per-repo ghsa - parent declines", "https://github.com/owner/repo/security/advisories/GHSA-bbb", false, 0},
	}
	s := NewGitHubStrategy(Dependencies{})
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url})
			if got.Matches != tc.want {
				t.Errorf("Matches = %v, want %v", got.Matches, tc.want)
			}
			if tc.want && got.Specificity != tc.spec {
				t.Errorf("Specificity = %d, want %d", got.Specificity, tc.spec)
			}
		})
	}
}

func TestClassifyPath(t *testing.T) {
	cases := []struct {
		url  string
		want pageType
	}{
		{"https://github.com/jadb", pageTypeProfile},
		{"https://github.com/samber/lo", pageTypeRepo},
		{"https://github.com/owner/repo/pull/42", pageTypePullRequest},
		{"https://github.com/owner/repo/pulls", pageTypePullRequest},
		{"https://github.com/owner/repo/issues/7", pageTypeIssue},
		{"https://github.com/owner/repo/issues", pageTypeIssue},
		{"https://github.com/sponsors/jadb", pageTypeSponsor},
		{"https://github.com/", pageTypeUnknown},
		{"https://github.com/marketplace", pageTypeUnknown},
		{"https://github.com/settings/profile", pageTypeUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			got := classifyPath(tc.url)
			if got != tc.want {
				t.Errorf("classifyPath(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestDispatch_AdvisoryFallsThroughWhenChildDisabled verifies that
// dispatching an advisory URL when SecurityAdvisoryStrategy is not in
// the registry results in zero strategies (JIT fallback territory) —
// not the parent claiming the URL and producing no candidates.
func TestDispatch_AdvisoryFallsThroughWhenChildDisabled(t *testing.T) {
	reg := lateral.NewRegistry()
	reg.Register(NewGitHubStrategy(Dependencies{}))
	// SecurityAdvisoryStrategy intentionally NOT registered.

	cases := []string{
		"https://github.com/advisories",
		"https://github.com/advisories/GHSA-aaa",
		"https://github.com/owner/repo/security/advisories",
		"https://github.com/owner/repo/security/advisories/GHSA-bbb",
	}
	for _, url := range cases {
		url := url
		t.Run(url, func(t *testing.T) {
			ds := reg.Dispatch(context.Background(), lateral.CapturedEvent{SourceURL: url})
			if len(ds) != 0 {
				ids := make([]string, len(ds))
				for i, d := range ds {
					ids[i] = d.ID()
				}
				t.Errorf("dispatch(%q) = %v, want [] (JIT fallback)", url, ids)
			}
		})
	}
}

func TestProbe_UnknownPageType_NoCandidates(t *testing.T) {
	s := NewGitHubStrategy(Dependencies{})
	cs, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/marketplace"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs) != 0 {
		t.Errorf("got %d candidates, want 0", len(cs))
	}
}
