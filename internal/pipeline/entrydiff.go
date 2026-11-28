package pipeline

import (
	"slices"
	"strings"
)

// EntryDiff is what changed between two signature entry lists.
type EntryDiff struct {
	// Added steps are in the new list only; Removed ones in the old only.
	Added   []string
	Removed []string
	// Versioned steps are in both with another version.
	Versioned []string
	// Reordered steps are in both but not in the same place among the
	// steps the two lists share.
	Reordered []string
	// Models are the model entries (resource=backend/model) that differ,
	// as resource names.
	Models []string
}

// Applies reports whether the change asks for any work: an added,
// re-versioned or reordered step, or another model. A change that only
// removed steps applies nothing.
func (d EntryDiff) Applies() bool {
	return len(d.Added)+len(d.Versioned)+len(d.Reordered)+len(d.Models) > 0
}

// Required lists the steps of the new list that must run for the change to
// be applied: the added and the re-versioned ones. A reordered step runs if
// the pipeline runs it, but its absence from a mode does not stop the
// change from being applied there; a removed step no longer runs.
func (d EntryDiff) Required() []string {
	out := slices.Clone(d.Added)
	for _, s := range d.Versioned {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// stepKey names one occurrence of a step: a pipeline may list a step more
// than once (an inserted second embedding step), so the n-th "embedding" of
// the old list is compared with the n-th of the new one.
type stepKey struct {
	name string
	n    int
}

// splitEntries separates "step@version" entries (as keyed occurrences, in
// order) from "resource=backend/model" ones.
func splitEntries(entries []string) (steps []stepKey, versions map[stepKey]string, models map[string]string) {
	versions, models = map[stepKey]string{}, map[string]string{}
	seen := map[string]int{}
	for _, e := range entries {
		if res, val, ok := strings.Cut(e, "="); ok && !strings.Contains(res, "@") {
			models[res] = val
			continue
		}
		name, ver := e, ""
		if i := strings.LastIndex(e, "@"); i >= 0 {
			name, ver = e[:i], e[i+1:]
		}
		k := stepKey{name, seen[name]}
		seen[name]++
		steps = append(steps, k)
		versions[k] = ver
	}
	return steps, versions, models
}

// DiffEntries compares the entry lists of two signatures of one pipeline.
// A step listed more than once is compared occurrence by occurrence: an
// extra occurrence in the new list is Added, a missing one Removed, a
// matched one with another version Versioned, and matched ones whose order
// among the matched changed are Reordered. Each group names a step once.
func DiffEntries(oldEntries, newEntries []string) EntryDiff {
	oldSteps, oldVer, oldModels := splitEntries(oldEntries)
	newSteps, newVer, newModels := splitEntries(newEntries)
	var d EntryDiff
	add := func(group *[]string, name string) {
		if !slices.Contains(*group, name) {
			*group = append(*group, name)
		}
	}
	var oldCommon, newCommon []stepKey
	for _, k := range newSteps {
		v, ok := oldVer[k]
		switch {
		case !ok:
			add(&d.Added, k.name)
		case v != newVer[k]:
			add(&d.Versioned, k.name)
			newCommon = append(newCommon, k)
		default:
			newCommon = append(newCommon, k)
		}
	}
	for _, k := range oldSteps {
		if _, ok := newVer[k]; !ok {
			add(&d.Removed, k.name)
		} else {
			oldCommon = append(oldCommon, k)
		}
	}
	for i := range newCommon {
		if oldCommon[i] != newCommon[i] {
			add(&d.Reordered, oldCommon[i].name)
			add(&d.Reordered, newCommon[i].name)
		}
	}
	seen := map[string]bool{}
	for res, val := range newModels {
		if old, ok := oldModels[res]; !ok || old != val {
			seen[res] = true
		}
	}
	for res := range oldModels {
		if _, ok := newModels[res]; !ok {
			seen[res] = true
		}
	}
	for res := range seen {
		d.Models = append(d.Models, res)
	}
	slices.Sort(d.Models)
	return d
}
