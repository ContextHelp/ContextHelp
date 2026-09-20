package daemon

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/roster"
)

// stubAPIClient is a no-op github.APIClient for wiring tests.
type stubAPIClient struct{}

func (stubAPIClient) ListRepoSiblings(_ context.Context, _, _ string) ([]github.RepoSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListOwnerStarred(_ context.Context, _ string) ([]github.RepoSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListOwnerPinned(_ context.Context, _ string) ([]github.RepoSummary, error) {
	return nil, nil
}
func (stubAPIClient) HasSponsorPage(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (stubAPIClient) ListAuthoredPRs(_ context.Context, _ string, _ int) ([]github.PullRequestSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListPRReviewers(_ context.Context, _, _ string, _ int) ([]github.UserSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListAuthoredIssues(_ context.Context, _ string, _ int) ([]github.IssueSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListIssueLabels(_ context.Context, _, _ string, _ int) ([]github.LabelSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListSponsored(_ context.Context, _ string) ([]github.UserSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListContributionOrgs(_ context.Context, _ string) ([]github.OrgSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListSimilarSponsors(_ context.Context, _ string) ([]github.UserSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListOwnerGists(_ context.Context, _ string) ([]github.GistSummary, error) {
	return nil, nil
}
func (stubAPIClient) ListGlobalAdvisories(_ context.Context, _, _ string, _ int) ([]github.AdvisorySummary, error) {
	return nil, nil
}
func (stubAPIClient) RateSnapshot(_ context.Context) github.RateSnapshot {
	return github.RateSnapshot{}
}

type stubProposer struct{ reply []string }

func (s *stubProposer) Propose(_ context.Context, _, _ string) ([]string, error) {
	return s.reply, nil
}

type stubFetcher struct{}

func (stubFetcher) Fetch(_ context.Context, _ string) (string, error) { return "", nil }

func TestBuild_DefaultsRegistersRoster_GitHubOff_JITOff(t *testing.T) {
	// Default cfg: GitHub.EnableParent=true requires APIClient (panic).
	// Disable github explicitly here; the default Roster (zero value)
	// should register everything.
	cfg := Config{
		GitHub: github.Config{}, // EnableParent=false
		Roster: roster.Gates{},
		JIT:    jit.Config{Enabled: false},
	}
	reg, snap := Build(cfg, Deps{})
	if snap.JIT {
		t.Errorf("JIT registered with cfg.JIT.Enabled=false")
	}
	if snap.GitHub != 0 {
		t.Errorf("GitHub count = %d; want 0", snap.GitHub)
	}
	if snap.Roster <= 0 {
		t.Errorf("Roster count = %d; want > 0 (defaults all on)", snap.Roster)
	}
	// Sanity: a github-shaped URL has no platform-claiming strategy
	// (github family disabled), so JIT would fire — but JIT isn't on
	// either, so dispatch returns empty.
	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://github.com/torvalds/linux",
	})
	if len(chosen) != 0 {
		t.Errorf("Dispatch returned %d strategies; want 0 (github off, jit off)", len(chosen))
	}
}

func TestBuild_GitHubEnabledRegistersFamily(t *testing.T) {
	cfg := Config{
		GitHub: github.DefaultConfig(),
		Roster: roster.Gates{},
		JIT:    jit.Config{Enabled: false},
	}
	deps := Deps{GitHubAPI: stubAPIClient{}}
	reg, snap := Build(cfg, deps)
	if snap.GitHub == 0 {
		t.Errorf("github count = 0; want > 0")
	}

	// A github-shaped URL must dispatch to a github strategy.
	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://github.com/torvalds/linux",
	})
	if len(chosen) == 0 {
		t.Fatal("github URL dispatched no strategies")
	}
	got := chosen[0].ID()
	if got == jit.StrategyID {
		t.Errorf("first strategy = %q; want a github strategy (jit shouldn't fire when github family on)", got)
	}
}

func TestBuild_JITEnabled_RegisteredLast(t *testing.T) {
	cfg := Config{
		GitHub: github.Config{}, // off
		Roster: roster.Gates{
			// Disable everything to give JIT clean fall-through.
			GoogleStrategy: ptrFalse(), GoogleSearchStrategy: ptrFalse(),
			GoogleScholarStrategy: ptrFalse(), GoogleTrendsStrategy: ptrFalse(),
			GoogleNewsStrategy: ptrFalse(), XStrategy: ptrFalse(),
			LinkedInStrategy: ptrFalse(), ArxivStrategy: ptrFalse(),
			WikipediaStrategy: ptrFalse(), MediumStrategy: ptrFalse(),
			MediumPublicationStrategy: ptrFalse(), MediumProfileStrategy: ptrFalse(),
			SubstackStrategy: ptrFalse(), SubstackPublicationStrategy: ptrFalse(),
			SubstackPostStrategy: ptrFalse(), SubstackNotesStrategy: ptrFalse(),
			BeehiivStrategy: ptrFalse(), BeehiivPublicationStrategy: ptrFalse(),
			BeehiivPostStrategy: ptrFalse(), YouTubeStrategy: ptrFalse(),
		},
		JIT: jit.Config{Enabled: true},
	}
	deps := Deps{
		JITProposer: &stubProposer{reply: []string{"/about"}},
		JITCache:    jit.NewMemoryProposalCache(),
		JITFetcher:  stubFetcher{},
	}
	reg, snap := Build(cfg, deps)
	if !snap.JIT {
		t.Errorf("JIT not registered")
	}

	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://random-domain.example/post/123",
	})
	if len(chosen) != 1 || chosen[0].ID() != jit.StrategyID {
		t.Fatalf("Dispatch = %v; want [jit]", chosen)
	}
}

func TestBuild_GitHubMissingAPIPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when GitHub enabled but APIClient nil")
		}
	}()
	cfg := Config{GitHub: github.DefaultConfig()}
	_, _ = Build(cfg, Deps{})
}

func TestBuild_JITMissingProposerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when JIT enabled but Proposer nil")
		}
	}()
	cfg := Config{
		GitHub: github.Config{},
		JIT:    jit.Config{Enabled: true},
	}
	_, _ = Build(cfg, Deps{})
}

func TestBuild_DefaultJITCacheAppliedOnNil(t *testing.T) {
	// When cfg.JIT.Enabled but Deps.JITCache is nil, Build supplies
	// MemoryProposalCache rather than panicking — caches are pure
	// optimisations, not safety-critical.
	cfg := Config{
		GitHub: github.Config{},
		JIT:    jit.Config{Enabled: true},
	}
	deps := Deps{
		JITProposer: &stubProposer{},
		JITFetcher:  stubFetcher{},
		// JITCache: nil — should be supplied internally
	}
	reg, snap := Build(cfg, deps)
	if !snap.JIT {
		t.Fatal("JIT not registered")
	}
	if reg == nil {
		t.Fatal("registry nil")
	}
}

func ptrFalse() *bool { v := false; return &v }
