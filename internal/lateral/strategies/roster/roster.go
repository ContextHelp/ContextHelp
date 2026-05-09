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
	// Google family.
	if enabled(g.GoogleStrategy) {
		reg.Register(google.NewParent(deps.Google))
	}
	if enabled(g.GoogleSearchStrategy) {
		reg.Register(google.NewSearch(deps.Google))
	}
	if enabled(g.GoogleScholarStrategy) {
		reg.Register(google.NewScholar(deps.Google))
	}
	if enabled(g.GoogleTrendsStrategy) {
		reg.Register(google.NewTrends(deps.Google))
	}
	if enabled(g.GoogleNewsStrategy) {
		reg.Register(google.NewNews(deps.Google))
	}

	// Single-strategy platforms.
	if enabled(g.XStrategy) {
		reg.Register(x.New())
	}
	if enabled(g.LinkedInStrategy) {
		reg.Register(linkedin.New())
	}
	if enabled(g.ArxivStrategy) {
		reg.Register(arxiv.New(deps.Arxiv))
	}
	if enabled(g.WikipediaStrategy) {
		reg.Register(wikipedia.New(deps.Wikipedia))
	}
	if enabled(g.YouTubeStrategy) {
		reg.Register(youtube.New(deps.YouTube))
	}

	// Medium family.
	if enabled(g.MediumStrategy) {
		reg.Register(medium.NewParent(deps.Medium))
	}
	if enabled(g.MediumPublicationStrategy) {
		reg.Register(medium.NewPublication(deps.Medium))
	}
	if enabled(g.MediumProfileStrategy) {
		reg.Register(medium.NewProfile(deps.Medium))
	}

	// Substack family.
	if enabled(g.SubstackStrategy) {
		reg.Register(substack.NewParent(deps.Substack))
	}
	if enabled(g.SubstackPublicationStrategy) {
		reg.Register(substack.NewPublication(deps.Substack))
	}
	if enabled(g.SubstackPostStrategy) {
		reg.Register(substack.NewPost(deps.Substack))
	}
	if enabled(g.SubstackNotesStrategy) {
		reg.Register(substack.NewNotes(deps.Substack))
	}

	// Beehiiv family.
	if enabled(g.BeehiivStrategy) {
		reg.Register(beehiiv.NewParent(deps.Beehiiv))
	}
	if enabled(g.BeehiivPublicationStrategy) {
		reg.Register(beehiiv.NewPublication(deps.Beehiiv))
	}
	if enabled(g.BeehiivPostStrategy) {
		reg.Register(beehiiv.NewPost(deps.Beehiiv))
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
