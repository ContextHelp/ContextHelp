// Package tagfilter is the tag-selection grammar of --tagged and its REST
// and MCP twins, in the Lucene/Elasticsearch must/should style.
//
//	a,b,c      any: at least one of the tags (plain tags are optional)
//	+a,+b,+c   all: every tag required
//	+a,b,+c    a and c required; b optional, it only ranks
//
// A list is a comma list, repeated, or both; values concatenate in order
// and duplicates collapse, a required mark winning over a plain one. A
// leading "+" marks a tag required. With any "+" tag, every "+" tag must
// match and the plain tags only rank; with none, one plain tag must match.
// Only commas separate tags: a label may hold spaces and, past the first
// character, "+", and matching is case-sensitive. A leading "-" is
// reserved for exclusion and refused.
//
// Ranking counts every selected tag an object carries, required or plain
// alike: results matching more of the listed tags rank first.
package tagfilter

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Tag is one label of a Filter.
type Tag struct {
	Label    string
	Required bool // a leading "+": the object must carry it
}

// Filter selects objects by tag label. The zero Filter selects all.
type Filter struct {
	Tags []Tag
}

// Error is a malformed tag list.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func bad(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

// One is the filter for a single plain tag.
func One(label string) Filter { return Filter{Tags: []Tag{{Label: label}}} }

// Parse reads tag list values as flags or parameters hand them over. No
// values is the zero Filter.
func Parse(vals []string) (Filter, error) {
	if len(vals) == 0 {
		return Filter{}, nil
	}
	var f Filter
	for _, v := range vals {
		// Commas only: a label may hold spaces ("machine learning").
		for _, seg := range strings.Split(v, ",") {
			tok := strings.TrimSpace(seg)
			if tok == "" {
				return Filter{}, bad("empty tag in %q", v)
			}
			if strings.HasPrefix(tok, "-") {
				return Filter{}, bad("%q: exclusion (a leading \"-\") is not supported yet", tok)
			}
			label, required := strings.CutPrefix(tok, "+")
			if label = strings.TrimSpace(label); label == "" {
				return Filter{}, bad("empty tag %q: \"+\" needs a label", tok)
			}
			if strings.HasPrefix(label, "-") && required {
				return Filter{}, bad("%q: exclusion (a leading \"-\") is not supported yet", tok)
			}
			f.add(label, required)
		}
	}
	return f, nil
}

// add appends a tag; a repeat keeps the first position, and required
// wins over plain.
func (f *Filter) add(label string, required bool) {
	for i := range f.Tags {
		if f.Tags[i].Label == label {
			f.Tags[i].Required = f.Tags[i].Required || required
			return
		}
	}
	f.Tags = append(f.Tags, Tag{label, required})
}

// IsZero reports a filter that selects everything.
func (f Filter) IsZero() bool { return len(f.Tags) == 0 }

// Labels is every label, in order.
func (f Filter) Labels() []string { return f.pick(func(Tag) bool { return true }) }

// Required is the labels an object must all carry: the "+" tags. Empty
// when none is marked, and then one of Labels suffices.
func (f Filter) Required() []string { return f.pick(func(t Tag) bool { return t.Required }) }

// HasRequired reports at least one "+" tag.
func (f Filter) HasRequired() bool {
	return slices.ContainsFunc(f.Tags, func(t Tag) bool { return t.Required })
}

func (f Filter) pick(keep func(Tag) bool) []string {
	var out []string
	for _, t := range f.Tags {
		if keep(t) {
			out = append(out, t.Label)
		}
	}
	return out
}

// Match reports whether an object carrying labels passes the filter: every
// required tag, or with none marked, at least one tag.
func (f Filter) Match(labels []string) bool {
	if f.IsZero() {
		return true
	}
	if f.HasRequired() {
		for _, l := range f.Required() {
			if !slices.Contains(labels, l) {
				return false
			}
		}
		return true
	}
	return slices.ContainsFunc(f.Tags, func(t Tag) bool { return slices.Contains(labels, t.Label) })
}

// Matched counts the selected tags labels carry, required and plain alike.
// It is the ranking key: results matching more of the listed tags rank
// first.
func (f Filter) Matched(labels []string) int {
	n := 0
	for _, t := range f.Tags {
		if slices.Contains(labels, t.Label) {
			n++
		}
	}
	return n
}

// Ranks reports whether ordering by Matched can reorder results. It cannot
// when every selected object has the same count: a single tag, or a list
// of required tags only. Cursors that need a fixed order refuse a filter
// that Ranks (two or more tags with none required, or a plain tag beside a
// required one).
func (f Filter) Ranks() bool {
	if f.HasRequired() {
		return slices.ContainsFunc(f.Tags, func(t Tag) bool { return !t.Required })
	}
	return len(f.Tags) >= 2
}

// String is the filter in the grammar Parse reads, the REST and MCP wire
// form.
func (f Filter) String() string {
	parts := make([]string, len(f.Tags))
	for i, t := range f.Tags {
		parts[i] = t.Label
		if t.Required {
			parts[i] = "+" + parts[i]
		}
	}
	return strings.Join(parts, ",")
}

// ErrInvalid matches every *Error under errors.Is.
var ErrInvalid = errors.New("invalid tag filter")

// Is makes errors.Is(err, ErrInvalid) true for a malformed list.
func (e *Error) Is(target error) bool { return target == ErrInvalid }
