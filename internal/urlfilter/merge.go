package urlfilter

import (
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML merges the decoded document into c instead of replacing
// c, so every config layer that is decoded onto the same value (config
// files, -c files, -c key=value overrides) adds to the filter; see Merge.
// Decoding into a zero Config is unaffected.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type plain Config // same fields, no UnmarshalYAML: no recursion
	var layer plain
	if err := n.Decode(&layer); err != nil {
		return err
	}
	c.Merge(Config(layer))
	return nil
}

// Merge folds a later config layer into c. Deny is a privacy control, so
// no layer can remove a rule: deny lists at global, browser and profile
// scope become the union of both layers, in order, without duplicates.
// allow_only keeps its narrowing meaning: each layer's non-empty list at
// a scope stays a separate list the URL must match, so a later layer can
// narrow what gets captured but never widen it. Empty or missing lists
// in layer change nothing.
func (c *Config) Merge(layer Config) {
	c.Rules = c.merged(layer.Rules)
	if len(layer.Browsers) == 0 {
		return
	}
	browsers := maps.Clone(c.Browsers)
	if browsers == nil {
		browsers = make(map[string]BrowserConfig, len(layer.Browsers))
	}
	for bk, lb := range layer.Browsers {
		b := browsers[bk]
		b.Rules = b.merged(lb.Rules)
		if len(lb.Profiles) > 0 {
			profiles := maps.Clone(b.Profiles)
			if profiles == nil {
				profiles = make(map[string]Rules, len(lb.Profiles))
			}
			for pk, lp := range lb.Profiles {
				profiles[pk] = profiles[pk].merged(lp)
			}
			b.Profiles = profiles
		}
		browsers[bk] = b
	}
	c.Browsers = browsers
}

// merged returns r with layer's rules added. It never writes to the
// backing arrays of r or layer.
func (r Rules) merged(layer Rules) Rules {
	out := Rules{Deny: appendUnique(nil, r.Deny, layer.Deny)}
	for _, list := range append(r.allowOnlyLists(), layer.allowOnlyLists()...) {
		out.addAllowOnly(list)
	}
	return out
}

// allowOnlyLists returns every non-empty allow_only list of r.
func (r Rules) allowOnlyLists() [][]string {
	var lists [][]string
	if len(r.AllowOnly) > 0 {
		lists = append(lists, r.AllowOnly)
	}
	for _, l := range r.moreAllowOnly {
		if len(l) > 0 {
			lists = append(lists, l)
		}
	}
	return lists
}

// addAllowOnly adds list as another allow_only gate unless an existing
// gate holds the same rules.
func (r *Rules) addAllowOnly(list []string) {
	list = appendUnique(nil, list)
	if len(list) == 0 {
		return
	}
	for _, have := range r.allowOnlyLists() {
		if sameSet(have, list) {
			return
		}
	}
	if len(r.AllowOnly) == 0 {
		r.AllowOnly = list
		return
	}
	r.moreAllowOnly = append(slices.Clip(r.moreAllowOnly), list)
}

// appendUnique returns a new slice holding dst followed by every string
// of lists not already present.
func appendUnique(dst []string, lists ...[]string) []string {
	n := len(dst)
	for _, l := range lists {
		n += len(l)
	}
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	for _, l := range append([][]string{dst}, lists...) {
		for _, s := range l {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
