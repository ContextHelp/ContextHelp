package urlfilter

import (
	"slices"
	"sort"
	"strings"
)

// Scope names used in Rule.Scope.
const (
	ScopeBuiltin = "builtin"
	ScopeGlobal  = "global"
)

// builtinDeny is shipped with ctxt and always applied by Config.For. Only
// generic, never user- or organization-specific, entries belong here.
var builtinDeny = []string{
	"*://localhost/*",
	"*://*.localhost/*",
	"*://127.0.0.1/*",
	"*://[::1]/*",
	"file:*",
	"about:*",
	"chrome:*",
	"chrome-extension:*",
	"chrome-untrusted:*",
	"chrome-search:*",
	"brave:*",
	"edge:*",
	"opera:*",
	"vivaldi:*",
	"arc:*",
	"devtools:*",
	"view-source:*",
	"javascript:*",
	"data:*",
	"blob:*",
}

// BuiltinDeny returns a copy of the deny rules ctxt always applies.
func BuiltinDeny() []string { return slices.Clone(builtinDeny) }

// Config is the user-configured filter, read from the
// capture.url_filter config key. Global rules apply to every browser and
// profile; browser rules to every profile of that browser; profile rules
// to that one profile. Scoped rules add to the global ones: deny lists
// accumulate and every allow_only list must be satisfied. Config layers
// combine the same way: decoding YAML onto a populated Config merges
// into it (see Merge), so no layer can drop another layer's deny rule.
type Config struct {
	Rules    `mapstructure:",squash" yaml:",inline"`
	Browsers map[string]BrowserConfig `mapstructure:"browsers" yaml:"browsers,omitempty"`
}

// BrowserConfig holds rules for one browser and its profiles.
type BrowserConfig struct {
	Rules `mapstructure:",squash" yaml:",inline"`
	// Profiles is keyed by profile name as passed to --profile.
	Profiles map[string]Rules `mapstructure:"profiles" yaml:"profiles,omitempty"`
}

// For compiles the filter for one browser profile: builtin rules, global
// rules, then every browser entry whose key equals browser and every
// profile entry whose key equals one of profiles. Keys compare
// case-insensitively; pass every name a profile is known by (directory
// and display name) so no rule is missed. An empty browser yields the
// builtin and global layers only.
func (c Config) For(browser string, profiles ...string) (*Filter, error) {
	layers := []Layer{
		{Scope: ScopeBuiltin, Rules: Rules{Deny: builtinDeny}},
		{Scope: ScopeGlobal, Rules: c.Rules},
	}
	if browser != "" {
		for _, bk := range sortedKeys(c.Browsers) {
			if !strings.EqualFold(bk, browser) {
				continue
			}
			bc := c.Browsers[bk]
			layers = append(layers, Layer{Scope: "browser:" + bk, Rules: bc.Rules})
			for _, pk := range sortedKeys(bc.Profiles) {
				if slices.ContainsFunc(profiles, func(p string) bool { return p != "" && strings.EqualFold(p, pk) }) {
					layers = append(layers, Layer{Scope: "profile:" + bk + "/" + pk, Rules: bc.Profiles[pk]})
				}
			}
		}
	}
	return New(layers...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
