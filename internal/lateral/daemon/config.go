package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	lateralconfig "github.com/ideacrafterslabs/ctxt/internal/lateral/config"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/roster"
)

// Config is the daemon-side bundle the lateral subcommand assembles
// from the layered config. It composes:
//
//   - Substrate: the core scoring/lifecycle/jobs block. Owned by
//     internal/lateral/config and reload-aware via kit/core/config.
//     Reloadable.
//   - JIT, GitHub, Roster: per-strategy gates and tuning knobs the
//     wiring helpers consume.
//
// The substrate config root-keys at the YAML document root (mirrors
// the existing internal/lateral/config tests). The daemon adds a
// peer-level `strategies` key for per-strategy gates, all in the
// same file:
//
//	scoring:
//	  weights: { session_topic: 0.5, capture_window: 0.3, interest_registry: 0.2 }
//	lifecycle: { cold_cycle_days: 30, soft_delete_days: 30, p3_reference_threshold: 3 }
//	jobs: { engine_kind: memory }
//	strategies:
//	  jit: { enabled: false }
//	  github:
//	    enable_parent: true
//	    enable_gist: true
//	    enable_security_advisory: true
//	  google: { enabled: true }
//	  google_search: { enabled: true }
//	  # ... per-strategy gates per the lateral spec amendment
//
// The substrate.Config struct already round-trips scoring/lifecycle/
// jobs via its YAML tags. `strategies` is the daemon-only addition;
// nothing inside internal/lateral/config knows about it (substrate is
// strategy-agnostic by design).
type Config struct {
	// Substrate is the lateral substrate's own config (scoring,
	// lifecycle, jobs). Loaded via internal/lateral/config.Load.
	Substrate lateralconfig.Config

	// JIT gates the catch-all FamilyJIT strategy. Default disabled
	// (zero value) — JIT is opt-in per the spec.
	JIT jit.Config

	// GitHub gates the github family (parent / gist / advisory) plus
	// floor tracker tuning. Default DefaultConfig() — production
	// posture is "enabled."
	GitHub github.Config

	// Roster gates the P4 platform roster (google + children, x,
	// linkedin, arxiv, wikipedia, medium + children, substack +
	// children, beehiiv + children, youtube). Zero-value Gates means
	// every strategy enabled (matches the spec's v1 posture).
	Roster roster.Gates
}

// strategiesFile is the on-disk shape of `lateral.strategies.*`. The
// daemon reads it once at boot and once on each SIGHUP-driven reload,
// then maps fields into the typed per-strategy Configs above.
//
// Entries default to "enabled" when absent (matches roster.Gates'
// nil-pointer convention). Operators turn a strategy off by setting
// enabled: false explicitly.
type strategiesFile struct {
	JIT struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"jit"`

	GitHub struct {
		EnableParent           *bool `yaml:"enable_parent"`
		EnableGist             *bool `yaml:"enable_gist"`
		EnableSecurityAdvisory *bool `yaml:"enable_security_advisory"`
	} `yaml:"github"`

	// Roster gates: one entry per spec-named strategy. Pointer-to-bool
	// distinguishes "absent" (default true) from "explicitly false."
	Google              *strategyGate `yaml:"google"`
	GoogleSearch        *strategyGate `yaml:"google_search"`
	GoogleScholar       *strategyGate `yaml:"google_scholar"`
	GoogleTrends        *strategyGate `yaml:"google_trends"`
	GoogleNews          *strategyGate `yaml:"google_news"`
	X                   *strategyGate `yaml:"x"`
	LinkedIn            *strategyGate `yaml:"linkedin"`
	Arxiv               *strategyGate `yaml:"arxiv"`
	Wikipedia           *strategyGate `yaml:"wikipedia"`
	Medium              *strategyGate `yaml:"medium"`
	MediumPublication   *strategyGate `yaml:"medium_publication"`
	MediumProfile       *strategyGate `yaml:"medium_profile"`
	Substack            *strategyGate `yaml:"substack"`
	SubstackPublication *strategyGate `yaml:"substack_publication"`
	SubstackPost        *strategyGate `yaml:"substack_post"`
	SubstackNotes       *strategyGate `yaml:"substack_notes"`
	Beehiiv             *strategyGate `yaml:"beehiiv"`
	BeehiivPublication  *strategyGate `yaml:"beehiiv_publication"`
	BeehiivPost         *strategyGate `yaml:"beehiiv_post"`
	YouTube             *strategyGate `yaml:"youtube"`
}

// strategyGate is the per-entry shape under lateral.strategies.<name>.
// Just an enabled flag for now; future fields (rate-limits, custom
// floors) layer in here without breaking the on-disk format.
type strategyGate struct {
	Enabled *bool `yaml:"enabled"`
}

// rootFile is the YAML root the daemon reads for strategy gates. The
// substrate config consumes the same file via kit/core/config.Load on
// the scoring/lifecycle/jobs keys (handled by yaml struct tags inside
// internal/lateral/config); the daemon only reads strategies.* through
// rootFile because the substrate doesn't model it.
type rootFile struct {
	Strategies strategiesFile `yaml:"strategies"`
}

// LoadOptions mirrors lateralconfig.LoadOptions plus an explicit list
// of strategy-override paths. Paths are passed through to substrate
// config and re-read for the daemon's strategies.* slice.
type LoadOptions struct {
	SystemConfigPath  string
	UserConfigPath    string
	ProjectConfigPath string
	ExtraConfigPaths  []string
	EnvPrefix         string // e.g. "CTXT_LATERAL"
}

// LoadConfig resolves the layered config into a daemon Config. It
// runs the substrate loader first (so scoring/lifecycle/jobs honor
// kit's full layer precedence) and then reads strategies.* from each
// configured layer, applying later layers on top of earlier ones.
//
// The roster.Gates default is "all enabled"; explicit `enabled: false`
// in any layer flips a flag off. JIT defaults to disabled; only an
// explicit `enabled: true` turns it on. GitHub starts from
// github.DefaultConfig() and overrides individual flags as set.
func LoadConfig(opts LoadOptions) (Config, error) {
	substrate, err := lateralconfig.Load(lateralconfig.LoadOptions{
		SystemConfigPath:  opts.SystemConfigPath,
		UserConfigPath:    opts.UserConfigPath,
		ProjectConfigPath: opts.ProjectConfigPath,
		ExtraConfigPaths:  opts.ExtraConfigPaths,
		EnvPrefix:         opts.EnvPrefix,
	})
	if err != nil {
		return Config{}, fmt.Errorf("substrate config: %w", err)
	}

	cfg := Config{
		Substrate: substrate,
		JIT:       jit.Config{},
		GitHub:    github.DefaultConfig(),
		Roster:    roster.Gates{}, // zero-value = every strategy enabled
	}

	// Apply layers in the canonical order. Later layers win.
	layers := []string{}
	for _, p := range []string{opts.SystemConfigPath, opts.UserConfigPath, opts.ProjectConfigPath} {
		if p != "" {
			layers = append(layers, p)
		}
	}
	layers = append(layers, opts.ExtraConfigPaths...)

	for _, p := range layers {
		if p == "" {
			continue
		}
		if err := applyStrategiesLayer(&cfg, p); err != nil {
			return Config{}, fmt.Errorf("strategies layer %s: %w", p, err)
		}
	}

	return cfg, nil
}

// applyStrategiesLayer reads path's strategies block (if any) and
// merges it into cfg. Missing files are silently skipped to mirror
// kit's System/User/Project precedence semantics. ExtraConfigPaths,
// however, are user-asserted — if a caller passes one and the file
// doesn't exist, we fail loud.
func applyStrategiesLayer(cfg *Config, path string) error {
	abs, _ := filepath.Abs(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// System/User/Project layers tolerate missing files. Extras
			// surface as errors via lateralconfig.Load (kit's loader
			// does the gating); we can safely no-op here for both.
			return nil
		}
		return fmt.Errorf("read %s: %w", abs, err)
	}

	var f rootFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("unmarshal %s: %w", abs, err)
	}
	mergeStrategies(cfg, f.Strategies)
	return nil
}

// mergeStrategies maps a parsed strategiesFile onto cfg. Each pointer
// field reflects "explicitly set in this layer"; absence preserves
// the prior layer's value (or the default when no layer touched it).
func mergeStrategies(cfg *Config, s strategiesFile) {
	if s.JIT.Enabled != nil {
		cfg.JIT.Enabled = *s.JIT.Enabled
	}

	if s.GitHub.EnableParent != nil {
		cfg.GitHub.EnableParent = *s.GitHub.EnableParent
	}
	if s.GitHub.EnableGist != nil {
		cfg.GitHub.EnableGist = *s.GitHub.EnableGist
	}
	if s.GitHub.EnableSecurityAdvisory != nil {
		cfg.GitHub.EnableSecurityAdvisory = *s.GitHub.EnableSecurityAdvisory
	}

	apply := func(target **bool, gate *strategyGate) {
		if gate == nil {
			return
		}
		// strategy.enabled with no value (e.g. just `google: {}`) leaves
		// the target untouched. Only an explicit boolean flips it.
		if gate.Enabled == nil {
			return
		}
		v := *gate.Enabled
		*target = &v
	}
	apply(&cfg.Roster.GoogleStrategy, s.Google)
	apply(&cfg.Roster.GoogleSearchStrategy, s.GoogleSearch)
	apply(&cfg.Roster.GoogleScholarStrategy, s.GoogleScholar)
	apply(&cfg.Roster.GoogleTrendsStrategy, s.GoogleTrends)
	apply(&cfg.Roster.GoogleNewsStrategy, s.GoogleNews)
	apply(&cfg.Roster.XStrategy, s.X)
	apply(&cfg.Roster.LinkedInStrategy, s.LinkedIn)
	apply(&cfg.Roster.ArxivStrategy, s.Arxiv)
	apply(&cfg.Roster.WikipediaStrategy, s.Wikipedia)
	apply(&cfg.Roster.MediumStrategy, s.Medium)
	apply(&cfg.Roster.MediumPublicationStrategy, s.MediumPublication)
	apply(&cfg.Roster.MediumProfileStrategy, s.MediumProfile)
	apply(&cfg.Roster.SubstackStrategy, s.Substack)
	apply(&cfg.Roster.SubstackPublicationStrategy, s.SubstackPublication)
	apply(&cfg.Roster.SubstackPostStrategy, s.SubstackPost)
	apply(&cfg.Roster.SubstackNotesStrategy, s.SubstackNotes)
	apply(&cfg.Roster.BeehiivStrategy, s.Beehiiv)
	apply(&cfg.Roster.BeehiivPublicationStrategy, s.BeehiivPublication)
	apply(&cfg.Roster.BeehiivPostStrategy, s.BeehiivPost)
	apply(&cfg.Roster.YouTubeStrategy, s.YouTube)
}
