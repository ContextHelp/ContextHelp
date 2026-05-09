package daemon

import (
	kitdomain "hop.top/kit/go/runtime/domain"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/arxiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/beehiiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/google"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/medium"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/roster"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/substack"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/wikipedia"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/youtube"
)

// Deps bundles the daemon-side collaborators all strategies share.
// The daemon constructs each adapter once at boot and threads them
// through Build to per-strategy Register calls.
//
// Required:
//   - Publisher: shared bus publisher (T-0318 adapter). All emit
//     helpers across jit, github, jobs use it.
//
// Per-strategy fetchers (T-0315 adapter) are passed via JITFetcher and
// GitHubFetcher. A single adapter instance can satisfy both interfaces
// — the daemon typically constructs one ibr-backed fetcher and assigns
// it to both fields — but the struct keeps them separate so a future
// per-platform fetcher (e.g. higher-budget for github) can drop in
// without changing the wiring shape.
//
// Optional (zero value tolerated, defaults applied):
//   - JITProposer / JITCache / JITClassifier: jit-specific (T-0314,
//     T-0317). When JITProposer is nil and cfg.JIT.Enabled is true,
//     Build panics — JIT can't run without an LLM.
//   - GitHubAPI: REST adapter (T-0319). When nil and any github
//     strategy is enabled, Build panics — github family requires an
//     API client.
//   - GitHubBreaker: kit/core/breaker adapter (T-0316) for github API
//     guarding. Optional; nil → no gating.
//   - GitHubFloor: github-package-internal rate-limit floor tracker.
//     Optional; nil → no throttle.
//   - Roster clients: per-platform (google, arxiv, etc.). Each is
//     optional — the corresponding strategy degrades to URL-only
//     output when its client is nil (per roster.Deps contract).
type Deps struct {
	Publisher kitdomain.EventPublisher

	// jit
	JITProposer   jit.Proposer
	JITCache      jit.ProposalCache
	JITFetcher    jit.Fetcher
	JITOutage     jit.OutageDetector
	JITClassifier jit.PageTypeClassifier

	// github
	GitHubAPI     github.APIClient
	GitHubFetcher github.Fetcher
	GitHubBreaker github.Breaker
	GitHubFloor   *github.FloorTracker
	GitHubFailure github.FailureRecorder

	// roster (P4 platforms)
	GoogleClient    google.GoogleClient
	ArxivClient     arxiv.ArxivClient
	WikipediaClient wikipedia.WikipediaClient
	MediumClient    medium.MediumClient
	SubstackClient  substack.SubstackClient
	BeehiivClient   beehiiv.BeehiivClient
	YouTubeClient   youtube.YouTubeClient
}

// Build constructs a lateral.Registry populated with every strategy
// the cfg gates allow. Returns the registry and a snapshot of which
// strategies were registered (used by the status subcommand).
//
// Registration order:
//
//  1. Tier-A platform strategies (github family, google family, x,
//     linkedin, arxiv, wikipedia, medium, substack, beehiiv, youtube)
//     via roster.Register + github.Register.
//  2. JIT catch-all last so its specificity-0 only fires when no
//     Tier-A strategy claimed the event (lateral.Registry.Dispatch
//     enforces this; ordering is for readability + log clarity).
//
// Panics on missing required deps when the corresponding strategy
// is enabled — fail loud at boot rather than first-event nil-pointer.
func Build(cfg Config, deps Deps) (*lateral.Registry, RegistrationSnapshot) {
	reg := lateral.NewRegistry()
	snap := RegistrationSnapshot{}

	// 1. github family.
	if cfg.GitHub.EnableParent {
		if deps.GitHubAPI == nil {
			panic("daemon.Build: GitHub.EnableParent=true but Deps.GitHubAPI is nil")
		}
		shared := github.SharedDeps{
			APIClient:       deps.GitHubAPI,
			Fetcher:         deps.GitHubFetcher,
			Breaker:         deps.GitHubBreaker,
			Floor:           deps.GitHubFloor,
			FailureRecorder: deps.GitHubFailure,
		}
		count := github.Register(reg, cfg.GitHub, shared)
		snap.GitHub = count
	}

	// 2. roster (P4 platforms).
	rosterDeps := roster.Deps{
		Google:    deps.GoogleClient,
		Arxiv:     deps.ArxivClient,
		Wikipedia: deps.WikipediaClient,
		Medium:    deps.MediumClient,
		Substack:  deps.SubstackClient,
		Beehiiv:   deps.BeehiivClient,
		YouTube:   deps.YouTubeClient,
	}
	roster.Register(reg, cfg.Roster, rosterDeps)
	snap.Roster = countRoster(cfg.Roster)

	// 3. JIT (catch-all, registered last by convention).
	if cfg.JIT.Enabled {
		if deps.JITProposer == nil {
			panic("daemon.Build: JIT.Enabled=true but Deps.JITProposer is nil")
		}
		if deps.JITCache == nil {
			deps.JITCache = jit.NewMemoryProposalCache()
		}
		if deps.JITFetcher == nil {
			panic("daemon.Build: JIT.Enabled=true but Deps.JITFetcher is nil")
		}
		jitDeps := jit.Deps{
			Proposer:   deps.JITProposer,
			Cache:      deps.JITCache,
			Fetcher:    deps.JITFetcher,
			Publisher:  deps.Publisher,
			Outage:     deps.JITOutage,
			Classifier: deps.JITClassifier,
		}
		if _, ok := jit.Register(reg, cfg.JIT, jitDeps); ok {
			snap.JIT = true
		}
	}

	return reg, snap
}

// RegistrationSnapshot captures what Build registered. Cheap to
// produce; the status subcommand renders this for operators.
type RegistrationSnapshot struct {
	JIT    bool
	GitHub int
	Roster int
}

// countRoster mirrors roster.Register's gate logic to count what was
// registered. Defensive: if the roster package adds new flags, this
// underscores it (and the count is approximate until they ship).
func countRoster(g roster.Gates) int {
	count := 0
	enabled := func(p *bool) bool {
		if p == nil {
			return true
		}
		return *p
	}
	flags := []*bool{
		g.GoogleStrategy, g.GoogleSearchStrategy, g.GoogleScholarStrategy,
		g.GoogleTrendsStrategy, g.GoogleNewsStrategy,
		g.XStrategy, g.LinkedInStrategy,
		g.ArxivStrategy, g.WikipediaStrategy,
		g.MediumStrategy, g.MediumPublicationStrategy, g.MediumProfileStrategy,
		g.SubstackStrategy, g.SubstackPublicationStrategy, g.SubstackPostStrategy, g.SubstackNotesStrategy,
		g.BeehiivStrategy, g.BeehiivPublicationStrategy, g.BeehiivPostStrategy,
		g.YouTubeStrategy,
	}
	for _, f := range flags {
		if enabled(f) {
			count++
		}
	}
	return count
}
