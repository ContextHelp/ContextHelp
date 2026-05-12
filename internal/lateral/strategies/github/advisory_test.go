package github

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestSecurityAdvisoryStrategy_IDFamilySpecificity(t *testing.T) {
	s := NewSecurityAdvisoryStrategy(Dependencies{})
	if s.ID() != StrategyIDSecurityAdvisory {
		t.Errorf("ID = %q, want %q", s.ID(), StrategyIDSecurityAdvisory)
	}
	if s.Family() != lateral.FamilyPlatform {
		t.Errorf("Family = %v, want FamilyPlatform", s.Family())
	}
	got := s.Applies(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://github.com/advisories/GHSA-1234",
	})
	if got.Specificity != SpecificityChild {
		t.Errorf("Specificity = %d, want %d", got.Specificity, SpecificityChild)
	}
}

func TestSecurityAdvisoryStrategy_AppliesMatrix(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"global db root", "https://github.com/advisories", true},
		{"global db ghsa", "https://github.com/advisories/GHSA-aaaa-bbbb-cccc", true},
		{"per-repo advisory list", "https://github.com/owner/repo/security/advisories", true},
		{"per-repo advisory ghsa", "https://github.com/owner/repo/security/advisories/GHSA-x", true},
		{"www host", "https://www.github.com/advisories", true},

		{"plain repo", "https://github.com/owner/repo", false},
		{"profile", "https://github.com/jadb", false},
		{"security policy (not advisories)", "https://github.com/owner/repo/security/policy", false},
		{"gist host", "https://gist.github.com/jadb/abc", false},
		{"random host", "https://example.com/advisories", false},
		{"empty", "", false},
	}
	s := NewSecurityAdvisoryStrategy(Dependencies{})
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

func TestSecurityAdvisoryStrategy_ProbeRepo_EmitsOwnerAndRepo(t *testing.T) {
	s := NewSecurityAdvisoryStrategy(Dependencies{})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo/security/advisories/GHSA-x"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasType(got, TypeOwnerProfile) || !hasType(got, TypeAdvisoryRepo) {
		t.Errorf("missing expected types in %v", candidateTypes(got))
	}
	repo := findFirst(got, TypeAdvisoryRepo)
	if got, want := ExtractIdentityKey(*repo), "@github.repo.owner/repo"; got != want {
		t.Errorf("repo identity_key = %q, want %q", got, want)
	}
}

func TestSecurityAdvisoryStrategy_ProbeGlobal_EmitsSimilar(t *testing.T) {
	api := &stubAPIClient{
		listGlobalAdvisoriesFn: func(_ context.Context, eco, sev string, lim int) ([]AdvisorySummary, error) {
			if lim != 10 {
				t.Errorf("limit = %d, want 10", lim)
			}
			return []AdvisorySummary{
				{GHSAID: "GHSA-aaa", Severity: "high", Ecosystem: "go"},
				{GHSAID: "GHSA-x", Severity: "low"}, // captured — must be excluded
				{GHSAID: "GHSA-bbb", Severity: "critical", Ecosystem: "npm"},
			}, nil
		},
	}
	s := NewSecurityAdvisoryStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/advisories/GHSA-x"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := 0
	for _, c := range got {
		if c.CandidateType == TypeSimilarAdv {
			count++
			if c.Preview["ghsa_id"] == "GHSA-x" {
				t.Error("captured advisory must be excluded from similar")
			}
		}
	}
	if count != 2 {
		t.Errorf("similar_advisory count = %d, want 2", count)
	}
}

func TestParseAdvisoryURL(t *testing.T) {
	cases := []struct {
		url       string
		wantSurf  advisorySurface
		wantOwner string
		wantRepo  string
		wantGHSA  string
	}{
		{"https://github.com/advisories/GHSA-1234", advisoryGlobal, "", "", "GHSA-1234"},
		{"https://github.com/advisories", advisoryGlobal, "", "", ""},
		{"https://github.com/o/r/security/advisories/GHSA-x", advisoryRepo, "o", "r", "GHSA-x"},
		{"https://github.com/o/r/security/advisories", advisoryRepo, "o", "r", ""},
		{"https://github.com/o/r", advisoryNone, "", "", ""},
		{"https://gist.github.com/o/r", advisoryNone, "", "", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			s, o, r, g := parseAdvisoryURL(tc.url)
			if s != tc.wantSurf || o != tc.wantOwner || r != tc.wantRepo || g != tc.wantGHSA {
				t.Errorf("parseAdvisoryURL(%q) = (%v,%q,%q,%q), want (%v,%q,%q,%q)",
					tc.url, s, o, r, g, tc.wantSurf, tc.wantOwner, tc.wantRepo, tc.wantGHSA)
			}
		})
	}
}

func TestGitHubStrategy_DeclinesAdvisoriesPath(t *testing.T) {
	// Parent now lists "advisories" as reserved-top-level so its classifier
	// won't pretend /advisories/* is a profile/repo.
	s := NewGitHubStrategy(Dependencies{})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/advisories/GHSA-1234"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parent should not probe /advisories/*; got %v", candidateTypes(got))
	}
}
