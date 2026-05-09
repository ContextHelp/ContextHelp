package github

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// stubAPIClient is a fully-controllable APIClient for probe unit tests.
// Each Func field, when non-nil, replaces the default zero-value return.
type stubAPIClient struct {
	listRepoSiblingsFn     func(ctx context.Context, owner, exclude string) ([]RepoSummary, error)
	listOwnerStarredFn     func(ctx context.Context, login string) ([]RepoSummary, error)
	listOwnerPinnedFn      func(ctx context.Context, login string) ([]RepoSummary, error)
	hasSponsorPageFn       func(ctx context.Context, login string) (bool, error)
	listAuthoredPRsFn      func(ctx context.Context, login string, limit int) ([]PullRequestSummary, error)
	listPRReviewersFn      func(ctx context.Context, owner, repo string, number int) ([]UserSummary, error)
	listAuthoredIssuesFn   func(ctx context.Context, login string, limit int) ([]IssueSummary, error)
	listIssueLabelsFn      func(ctx context.Context, owner, repo string, number int) ([]LabelSummary, error)
	listSponsoredFn        func(ctx context.Context, login string) ([]UserSummary, error)
	listContributionOrgsFn func(ctx context.Context, login string) ([]OrgSummary, error)
	listSimilarSponsorsFn  func(ctx context.Context, login string) ([]UserSummary, error)
	listOwnerGistsFn       func(ctx context.Context, login string) ([]GistSummary, error)
	listGlobalAdvisoriesFn func(ctx context.Context, ecosystem, severity string, limit int) ([]AdvisorySummary, error)
}

func (s *stubAPIClient) ListRepoSiblings(ctx context.Context, owner, exclude string) ([]RepoSummary, error) {
	if s.listRepoSiblingsFn != nil {
		return s.listRepoSiblingsFn(ctx, owner, exclude)
	}
	return nil, nil
}

func (s *stubAPIClient) ListOwnerStarred(ctx context.Context, login string) ([]RepoSummary, error) {
	if s.listOwnerStarredFn != nil {
		return s.listOwnerStarredFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) ListOwnerPinned(ctx context.Context, login string) ([]RepoSummary, error) {
	if s.listOwnerPinnedFn != nil {
		return s.listOwnerPinnedFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) HasSponsorPage(ctx context.Context, login string) (bool, error) {
	if s.hasSponsorPageFn != nil {
		return s.hasSponsorPageFn(ctx, login)
	}
	return false, nil
}

func (s *stubAPIClient) ListAuthoredPRs(ctx context.Context, login string, limit int) ([]PullRequestSummary, error) {
	if s.listAuthoredPRsFn != nil {
		return s.listAuthoredPRsFn(ctx, login, limit)
	}
	return nil, nil
}

func (s *stubAPIClient) ListPRReviewers(ctx context.Context, owner, repo string, number int) ([]UserSummary, error) {
	if s.listPRReviewersFn != nil {
		return s.listPRReviewersFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (s *stubAPIClient) ListAuthoredIssues(ctx context.Context, login string, limit int) ([]IssueSummary, error) {
	if s.listAuthoredIssuesFn != nil {
		return s.listAuthoredIssuesFn(ctx, login, limit)
	}
	return nil, nil
}

func (s *stubAPIClient) ListIssueLabels(ctx context.Context, owner, repo string, number int) ([]LabelSummary, error) {
	if s.listIssueLabelsFn != nil {
		return s.listIssueLabelsFn(ctx, owner, repo, number)
	}
	return nil, nil
}

func (s *stubAPIClient) ListSponsored(ctx context.Context, login string) ([]UserSummary, error) {
	if s.listSponsoredFn != nil {
		return s.listSponsoredFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) ListContributionOrgs(ctx context.Context, login string) ([]OrgSummary, error) {
	if s.listContributionOrgsFn != nil {
		return s.listContributionOrgsFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) ListSimilarSponsors(ctx context.Context, login string) ([]UserSummary, error) {
	if s.listSimilarSponsorsFn != nil {
		return s.listSimilarSponsorsFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) ListOwnerGists(ctx context.Context, login string) ([]GistSummary, error) {
	if s.listOwnerGistsFn != nil {
		return s.listOwnerGistsFn(ctx, login)
	}
	return nil, nil
}

func (s *stubAPIClient) ListGlobalAdvisories(ctx context.Context, ecosystem, severity string, limit int) ([]AdvisorySummary, error) {
	if s.listGlobalAdvisoriesFn != nil {
		return s.listGlobalAdvisoriesFn(ctx, ecosystem, severity, limit)
	}
	return nil, nil
}

func (s *stubAPIClient) RateSnapshot(_ context.Context) RateSnapshot { return RateSnapshot{} }

// candidateTypes returns the multiset of CandidateType values across cs.
func candidateTypes(cs []lateral.Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.CandidateType
	}
	return out
}

// hasType reports whether any candidate in cs has CandidateType t.
func hasType(cs []lateral.Candidate, t string) bool {
	for _, c := range cs {
		if c.CandidateType == t {
			return true
		}
	}
	return false
}

// findFirst returns the first candidate matching t, or nil.
func findFirst(cs []lateral.Candidate, t string) *lateral.Candidate {
	for i := range cs {
		if cs[i].CandidateType == t {
			return &cs[i]
		}
	}
	return nil
}

func TestProbeRepo_Skeleton_NoClient(t *testing.T) {
	s := NewGitHubStrategy(Dependencies{})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].CandidateType != TypeOwnerProfile {
		t.Errorf("skeleton mode should emit only owner_profile; got %v", candidateTypes(got))
	}
	owner := findFirst(got, TypeOwnerProfile)
	if owner == nil || owner.URL != "https://github.com/samber" {
		t.Errorf("owner candidate URL = %v, want https://github.com/samber", owner)
	}
	if owner.Preview[PreviewKeyIdentityKey] != "@github.user.samber" {
		t.Errorf("identity_key = %v, want @github.user.samber", owner.Preview[PreviewKeyIdentityKey])
	}
}

func TestProbeRepo_FullClient_EmitsAllCandidateTypes(t *testing.T) {
	api := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, owner, exclude string) ([]RepoSummary, error) {
			if owner != "samber" || exclude != "lo" {
				t.Errorf("ListRepoSiblings(%q, %q): unexpected args", owner, exclude)
			}
			return []RepoSummary{
				{Owner: "samber", Name: "do", URL: "https://github.com/samber/do", Stars: 1500},
				{Owner: "samber", Name: "mo", URL: "https://github.com/samber/mo", Stars: 800},
			}, nil
		},
		listOwnerPinnedFn: func(_ context.Context, login string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: login, Name: "lo", Stars: 12000}}, nil
		},
		hasSponsorPageFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
		listOwnerStarredFn: func(_ context.Context, _ string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: "thanos-io", Name: "thanos", URL: "https://github.com/thanos-io/thanos"}}, nil
		},
	}
	s := NewGitHubStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		TypeSiblingRepo, TypeOwnerProfile, TypePinnedRepo, TypeSponsorPage, TypeStarredRepo,
	} {
		if !hasType(got, want) {
			t.Errorf("missing %q in %v", want, candidateTypes(got))
		}
	}

	// Sibling identity_key is canonical repo key.
	sib := findFirst(got, TypeSiblingRepo)
	if got, want := sib.Preview[PreviewKeyIdentityKey], "@github.repo.samber/do"; got != want {
		t.Errorf("sibling identity_key = %v, want %v", got, want)
	}
}

func TestProbeRepo_PartialFailure_OtherSubpathsContinue(t *testing.T) {
	api := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			return nil, errors.New("rate limited")
		},
		listOwnerPinnedFn: func(_ context.Context, _ string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: "samber", Name: "lo"}}, nil
		},
	}
	s := NewGitHubStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Owner profile + pinned should still come through.
	if !hasType(got, TypeOwnerProfile) {
		t.Errorf("missing owner_profile after sibling failure")
	}
	if !hasType(got, TypePinnedRepo) {
		t.Errorf("missing pinned_repo after sibling failure")
	}
	if hasType(got, TypeSiblingRepo) {
		t.Errorf("sibling_repo should be empty after sibling failure")
	}
}

func TestProbePR_Skeleton_NoClient(t *testing.T) {
	s := NewGitHubStrategy(Dependencies{})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo/pull/42"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].CandidateType != TypeOwnerProfile {
		t.Errorf("skeleton mode should emit only owner_profile; got %v", candidateTypes(got))
	}
}

func TestProbePR_FullClient_EmitsAllTypes(t *testing.T) {
	api := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, owner, exclude string) ([]RepoSummary, error) {
			if owner != "owner" || exclude != "repo" {
				t.Errorf("ListRepoSiblings(%q, %q) unexpected args", owner, exclude)
			}
			return []RepoSummary{{Owner: "owner", Name: "other"}}, nil
		},
		listPRReviewersFn: func(_ context.Context, owner, repo string, n int) ([]UserSummary, error) {
			if owner != "owner" || repo != "repo" || n != 42 {
				t.Errorf("ListPRReviewers(%q,%q,%d) unexpected args", owner, repo, n)
			}
			return []UserSummary{
				{Login: "alice", Type: "User"},
				{Login: "acme", Type: "Organization"},
			}, nil
		},
	}
	s := NewGitHubStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo/pull/42"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{TypeOwnerProfile, TypeSiblingRepo, TypeReviewer} {
		if !hasType(got, want) {
			t.Errorf("missing %q in %v", want, candidateTypes(got))
		}
	}

	// Reviewer that's an Org should carry @github.org.* identity key.
	for _, c := range got {
		if c.CandidateType != TypeReviewer {
			continue
		}
		if c.Preview["login"] == "acme" {
			if got, want := c.Preview[PreviewKeyIdentityKey], "@github.org.acme"; got != want {
				t.Errorf("org reviewer identity_key = %v, want %v", got, want)
			}
		}
		if c.Preview["login"] == "alice" {
			if got, want := c.Preview[PreviewKeyIdentityKey], "@github.user.alice"; got != want {
				t.Errorf("user reviewer identity_key = %v, want %v", got, want)
			}
		}
	}
}

func TestProbePR_PullsListView_NoNumber_SkipsReviewers(t *testing.T) {
	reviewerCalled := false
	api := &stubAPIClient{
		listPRReviewersFn: func(_ context.Context, _, _ string, _ int) ([]UserSummary, error) {
			reviewerCalled = true
			return nil, nil
		},
	}
	s := NewGitHubStrategy(Dependencies{APIClient: api})
	_, _ = s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/owner/repo/pulls"},
		lateral.ActiveContext{})
	if reviewerCalled {
		t.Error("ListPRReviewers should not be called for /pulls list view (no number)")
	}
}

func TestRepoIdentityKey(t *testing.T) {
	if got, want := repoIdentityKey("samber", "lo"), "@github.repo.samber/lo"; got != want {
		t.Errorf("repoIdentityKey = %q, want %q", got, want)
	}
	if got, want := userIdentityKey("jadb"), "@github.user.jadb"; got != want {
		t.Errorf("userIdentityKey = %q, want %q", got, want)
	}
	if got, want := orgIdentityKey("hashicorp"), "@github.org.hashicorp"; got != want {
		t.Errorf("orgIdentityKey = %q, want %q", got, want)
	}
}
