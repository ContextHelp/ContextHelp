package service

import (
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/ranking"
)

// CurrentVersionForFamily returns the highest registered version for the
// pipeline family `name` (the bare name without a `@vN` suffix). Returns
// (0, false) when no registration exists for the family.
//
// "Currently installed" is defined as max(version) across all registered
// names that resolve to the family — both bare ("text.short") and explicitly
// versioned ("text.short@v1") registrations are considered. This matches the
// registry's resolution semantics: a bare registration is implicitly @v0
// (per pipeline.ParseVersionedName) so a registry with both "text.short" and
// "text.short@v1" registered surfaces v1 as current.
func CurrentVersionForFamily(reg pipeline.Registry, name string) (int, bool) {
	if reg == nil {
		return 0, false
	}
	maxVer := -1
	for _, registered := range reg.List() {
		fam, ver, ok := pipeline.ParseVersionedName(registered)
		if !ok || fam != name {
			continue
		}
		if ver > maxVer {
			maxVer = ver
		}
	}
	if maxVer < 0 {
		return 0, false
	}
	return maxVer, true
}

// countStaleCandidates returns the number of candidates whose `Object.Pipeline`
// parses to a version strictly older than the registry's currently-installed
// version for the same family. Candidates with an unparseable pipeline value
// (e.g. legacy malformed strings, plugins that pre-date the @vN convention)
// are not counted — they're a different problem than staleness.
//
// Cached per family within the call so a result set with N candidates from
// the same family makes one registry lookup, not N.
func countStaleCandidates(reg pipeline.Registry, candidates map[string]ranking.Candidate) int {
	type famKey struct {
		family  string
		current int
		known   bool
	}
	cache := map[string]famKey{}
	stale := 0
	for _, c := range candidates {
		if c.Object == nil || c.Object.Pipeline == "" {
			continue
		}
		fam, ver, ok := pipeline.ParseVersionedName(c.Object.Pipeline)
		if !ok {
			continue
		}
		entry, cached := cache[fam]
		if !cached {
			cur, known := CurrentVersionForFamily(reg, fam)
			entry = famKey{family: fam, current: cur, known: known}
			cache[fam] = entry
		}
		if !entry.known {
			continue
		}
		if ver < entry.current {
			stale++
		}
	}
	return stale
}
