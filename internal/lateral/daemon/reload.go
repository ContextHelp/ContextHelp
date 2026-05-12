package daemon

import (
	"github.com/ideacrafterslabs/ctxt/internal/lateral/rollout"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/arxiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/beehiiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/google"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/linkedin"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/medium"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/substack"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/wikipedia"
	x "github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/x"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/youtube"
)

// GateConfig translates the daemon's typed Config gates into the
// flat strategyID→enabled map the rollout.StrategyGate consumes.
//
// Mapping:
//
//   - JIT.Enabled               → "jit"
//   - GitHub.EnableParent        → "github"
//   - GitHub.EnableGist          → "github.gist"
//   - GitHub.EnableSecurityAdvisory → "github.advisory"
//   - Roster.<each>             → matching IDXxxx const from
//                                  the strategy package (e.g.
//                                  "GoogleSearchStrategy", "XStrategy")
//
// Pointer-bool roster gates: nil = default-enabled (true). Operators
// set explicit `false` in YAML to disable.
//
// The result feeds rollout.StrategyGate.SetAll on every reload, so a
// strategy missing from the YAML reverts to its default (the gate is
// fully replaced, not merged).
func (cfg Config) GateConfig() rollout.GateConfig {
	out := rollout.GateConfig{}

	// JIT — opt-in, default false.
	out[jit.StrategyID] = cfg.JIT.Enabled

	// GitHub family.
	out[github.StrategyIDGitHub] = cfg.GitHub.EnableParent
	out[github.StrategyIDGist] = cfg.GitHub.EnableGist
	out[github.StrategyIDSecurityAdvisory] = cfg.GitHub.EnableSecurityAdvisory

	// Roster — pointer-bool defaults to true when nil.
	enabled := func(p *bool) bool {
		if p == nil {
			return true
		}
		return *p
	}
	out[google.IDParent] = enabled(cfg.Roster.GoogleStrategy)
	out[google.IDSearch] = enabled(cfg.Roster.GoogleSearchStrategy)
	out[google.IDScholar] = enabled(cfg.Roster.GoogleScholarStrategy)
	out[google.IDTrends] = enabled(cfg.Roster.GoogleTrendsStrategy)
	out[google.IDNews] = enabled(cfg.Roster.GoogleNewsStrategy)

	out[x.ID] = enabled(cfg.Roster.XStrategy)
	out[linkedin.ID] = enabled(cfg.Roster.LinkedInStrategy)
	out[arxiv.ID] = enabled(cfg.Roster.ArxivStrategy)
	out[wikipedia.ID] = enabled(cfg.Roster.WikipediaStrategy)
	out[youtube.ID] = enabled(cfg.Roster.YouTubeStrategy)

	out[medium.IDParent] = enabled(cfg.Roster.MediumStrategy)
	out[medium.IDPublication] = enabled(cfg.Roster.MediumPublicationStrategy)
	out[medium.IDProfile] = enabled(cfg.Roster.MediumProfileStrategy)

	out[substack.IDParent] = enabled(cfg.Roster.SubstackStrategy)
	out[substack.IDPublication] = enabled(cfg.Roster.SubstackPublicationStrategy)
	out[substack.IDPost] = enabled(cfg.Roster.SubstackPostStrategy)
	out[substack.IDNotes] = enabled(cfg.Roster.SubstackNotesStrategy)

	out[beehiiv.IDParent] = enabled(cfg.Roster.BeehiivStrategy)
	out[beehiiv.IDPublication] = enabled(cfg.Roster.BeehiivPublicationStrategy)
	out[beehiiv.IDPost] = enabled(cfg.Roster.BeehiivPostStrategy)

	return out
}

// ApplyGate updates lc's runtime gate from cfg. Call on initial boot
// (so the gate matches the YAML at start-of-day) and again on every
// SIGHUP-driven reload (so flips land mid-process).
func (lc *Lifecycle) ApplyGate(cfg Config) {
	cfg.GateConfig().Apply(lc.gate)
}

// ApplySampler swaps the lifecycle's sampler with one built from
// cfg.SamplePercent. SIGHUP reload drives this alongside ApplyGate;
// initial boot calls it once. Empty cfg.SamplePercent is a valid
// "no shaping" state — the sampler still installs but every Allow
// returns true.
func (lc *Lifecycle) ApplySampler(cfg Config) {
	lc.sampler.Store(rollout.NewSampler(cfg.SamplePercent))
}
