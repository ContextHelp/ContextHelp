// Package roster wires the P4 platform-strategy roster into a
// lateral.Registry behind a single Register helper. The daemon calls
// Register at boot with its configured per-strategy gates and a Deps
// struct carrying daemon-side platform clients; nil clients are
// tolerated and degrade strategies to URL-only candidates.
//
// # Why a top-level helper
//
// Each platform package ships its own constructor (Strategy.New /
// NewParent / NewSearch …). The roster aggregates those calls so the
// daemon entry point doesn't need to know per-platform construction
// details. Adding a new platform requires touching this file plus the
// platform package — no daemon-side change.
//
// # Config gates
//
// EnabledFor(name) returns true when the per-strategy `enabled` flag is
// set in the substrate config. The default — no entry — is "enabled":
// operators turn strategies off by setting `enabled: false`. This
// matches the spec's posture (all strategies enabled in v1) without
// forcing the daemon to enumerate every strategy in its config.
package roster

import (
	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/arxiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/beehiiv"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/google"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/linkedin"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/medium"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/substack"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/wikipedia"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/x"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/youtube"
)

// Deps carries the daemon-side clients each strategy may use. All
// fields are optional; passing nil for a client makes the corresponding
// strategy degrade to URL-only output (no follow-up fetches).
//
// X and LinkedIn strategies are URL-structural and take no client.
type Deps struct {
	Google    google.GoogleClient
	Arxiv     arxiv.ArxivClient
	Wikipedia wikipedia.WikipediaClient
	Medium    medium.MediumClient
	Substack  substack.SubstackClient
	Beehiiv   beehiiv.BeehiivClient
	YouTube   youtube.YouTubeClient
}

// Gates carries the per-strategy enabled flags. The zero value enables
// every strategy (matches the spec's v1 posture). Set a field to false
// to turn off that strategy.
//
// The names match the spec's `lateral.strategies.<name>.enabled`
// keys 1:1.
type Gates struct {
	GoogleStrategy              *bool
	GoogleSearchStrategy        *bool
	GoogleScholarStrategy       *bool
	GoogleTrendsStrategy        *bool
	GoogleNewsStrategy          *bool
	XStrategy                   *bool
	LinkedInStrategy            *bool
	ArxivStrategy               *bool
	WikipediaStrategy           *bool
	MediumStrategy              *bool
	MediumPublicationStrategy   *bool
	MediumProfileStrategy       *bool
	SubstackStrategy            *bool
	SubstackPublicationStrategy *bool
	SubstackPostStrategy        *bool
	SubstackNotesStrategy       *bool
	BeehiivStrategy             *bool
	BeehiivPublicationStrategy  *bool
	BeehiivPostStrategy         *bool
	YouTubeStrategy             *bool
}

// enabled returns *flag if set; defaults true.
func enabled(flag *bool) bool {
	if flag == nil {
		return true
	}
	return *flag
}

// Register adds every roster strategy to reg, gated by g. Pure (no
// side-effect beyond reg mutation); safe to call once at boot or in
// tests.
func Register(reg *lateral.Registry, g Gates, deps Deps) {
	// Each strategy is built only when its gate is on; registration
	// follows table order.
	for _, s := range []struct {
		gate  *bool
		build func() lateral.LateralStrategy
	}{
		// Google family.
		{g.GoogleStrategy, func() lateral.LateralStrategy { return google.NewParent(deps.Google) }},
		{g.GoogleSearchStrategy, func() lateral.LateralStrategy { return google.NewSearch(deps.Google) }},
		{g.GoogleScholarStrategy, func() lateral.LateralStrategy { return google.NewScholar(deps.Google) }},
		{g.GoogleTrendsStrategy, func() lateral.LateralStrategy { return google.NewTrends(deps.Google) }},
		{g.GoogleNewsStrategy, func() lateral.LateralStrategy { return google.NewNews(deps.Google) }},

		// Single-strategy platforms.
		{g.XStrategy, func() lateral.LateralStrategy { return x.New() }},
		{g.LinkedInStrategy, func() lateral.LateralStrategy { return linkedin.New() }},
		{g.ArxivStrategy, func() lateral.LateralStrategy { return arxiv.New(deps.Arxiv) }},
		{g.WikipediaStrategy, func() lateral.LateralStrategy { return wikipedia.New(deps.Wikipedia) }},
		{g.YouTubeStrategy, func() lateral.LateralStrategy { return youtube.New(deps.YouTube) }},

		// Medium family.
		{g.MediumStrategy, func() lateral.LateralStrategy { return medium.NewParent(deps.Medium) }},
		{g.MediumPublicationStrategy, func() lateral.LateralStrategy { return medium.NewPublication(deps.Medium) }},
		{g.MediumProfileStrategy, func() lateral.LateralStrategy { return medium.NewProfile(deps.Medium) }},

		// Substack family.
		{g.SubstackStrategy, func() lateral.LateralStrategy { return substack.NewParent(deps.Substack) }},
		{g.SubstackPublicationStrategy, func() lateral.LateralStrategy { return substack.NewPublication(deps.Substack) }},
		{g.SubstackPostStrategy, func() lateral.LateralStrategy { return substack.NewPost(deps.Substack) }},
		{g.SubstackNotesStrategy, func() lateral.LateralStrategy { return substack.NewNotes(deps.Substack) }},

		// Beehiiv family.
		{g.BeehiivStrategy, func() lateral.LateralStrategy { return beehiiv.NewParent(deps.Beehiiv) }},
		{g.BeehiivPublicationStrategy, func() lateral.LateralStrategy { return beehiiv.NewPublication(deps.Beehiiv) }},
		{g.BeehiivPostStrategy, func() lateral.LateralStrategy { return beehiiv.NewPost(deps.Beehiiv) }},
	} {
		if enabled(s.gate) {
			reg.Register(s.build())
		}
	}
}

// AllEnabled returns a Gates value with every flag explicitly enabled.
// Useful for tests and for daemon code paths that want to express
// "every strategy on" without relying on the zero-value default.
//
// Each pointer field is allocated separately so callers can mutate one
// flag without affecting the others.
func AllEnabled() Gates {
	on := func() *bool { v := true; return &v }
	return Gates{
		GoogleStrategy:              on(),
		GoogleSearchStrategy:        on(),
		GoogleScholarStrategy:       on(),
		GoogleTrendsStrategy:        on(),
		GoogleNewsStrategy:          on(),
		XStrategy:                   on(),
		LinkedInStrategy:            on(),
		ArxivStrategy:               on(),
		WikipediaStrategy:           on(),
		MediumStrategy:              on(),
		MediumPublicationStrategy:   on(),
		MediumProfileStrategy:       on(),
		SubstackStrategy:            on(),
		SubstackPublicationStrategy: on(),
		SubstackPostStrategy:        on(),
		SubstackNotesStrategy:       on(),
		BeehiivStrategy:             on(),
		BeehiivPublicationStrategy:  on(),
		BeehiivPostStrategy:         on(),
		YouTubeStrategy:             on(),
	}
}
