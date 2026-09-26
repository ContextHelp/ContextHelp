// Package timeframe turns --since / --until / --range flag values into a
// normalized set of half-open time ranges.
//
// # Grammar
//
// Every time value is parsed in a caller-supplied *time.Location
// (default time.Local) and is one of:
//
//   - RFC 3339 with an offset: 2026-09-10T12:00:00Z,
//     2026-09-10T08:00:00.5-04:00. The offset wins over the location.
//   - Local date-time without an offset: 2026-09-10T08:00:00 (wall clock
//     in the location; fractional seconds allowed).
//   - Bare date: 2026-09-10 (a whole calendar day in the location; see
//     below for how each bound uses it).
//   - Relative span: Nm (minutes), Nh (hours), Nd (days), Nw (weeks),
//     meaning now minus the span. N is a positive integer of up to six
//     digits. Nm and Nh are elapsed durations; Nd and Nw are calendar
//     spans in the location (same wall-clock time N days earlier), so
//     "1d" across a DST change is 23 or 25 elapsed hours. Arithmetic is
//     delegated to hop.top/kit/go/core/util.ParseSinceAt.
//
// Flags:
//
//   - --since X yields [X, +inf).
//   - --until Y yields [-inf, Y).
//   - --since X --until Y yields [X, Y).
//   - --range FROM..TO (repeatable) yields [FROM, TO). Either end may be
//     omitted (2026-09-01.., ..2026-09-10) but not both.
//   - --range DATE (bare date only, no "..") is shorthand for
//     DATE..DATE: that one full day.
//   - --range cannot be combined with --since or --until.
//
// # Bare dates cover whole days
//
// Ranges are half-open: From is included, To is not. A bare-date lower
// bound is the start of that day. A bare-date upper bound INCLUDES that
// day: its exclusive end is the start of the next day in the location,
// computed with calendar arithmetic, so DST days are 23 or 25 hours
// long. Hence 2026-09-01..2026-09-10 and --until 2026-09-10 both include
// all of September 10, and 2026-09-10..2026-09-10 (or --range
// 2026-09-10) is exactly September 10. Every other upper-bound form is
// taken literally.
//
// # Output
//
// Resolve returns ranges sorted by From with overlapping and adjacent
// ranges merged. An open end is explicit: a zero From means "from the
// beginning" and a zero To means "no upper bound".
//
// # Errors
//
// Every rejection is a *Error wrapping one of the Err* sentinels; IsUsage
// reports whether an error is one, so a command can map it to its usage
// exit code.
package timeframe

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"hop.top/kit/go/core/util"
)

// Sentinel error kinds. Match with errors.Is.
var (
	// ErrUnparseable marks a value outside the accepted grammar.
	ErrUnparseable = errors.New("unparseable time value")
	// ErrEmptyRange marks a range whose lower and upper bounds are equal.
	ErrEmptyRange = errors.New("empty range: start equals end")
	// ErrInvertedRange marks a range whose upper bound precedes its lower
	// bound.
	ErrInvertedRange = errors.New("inverted range: end is before start")
	// ErrConflictingFlags marks --range combined with --since or --until.
	ErrConflictingFlags = errors.New("--range cannot be combined with --since or --until")
	// ErrUnboundedRange marks a --range value with both ends omitted.
	ErrUnboundedRange = errors.New("range needs at least one bound")
)

// Error is the typed error returned for every invalid input.
type Error struct {
	// Kind is one of the Err* sentinels.
	Kind error
	// Flag names the offending flag without dashes ("since", "until",
	// "range"); empty for flag-combination errors and for ParseTime. An
	// ordering error on --since/--until names --until.
	Flag string
	// Value is the offending raw value; empty for flag-combination errors.
	Value string
	// Hint is optional guidance appended to the message.
	Hint string
}

// Error renders the kind with the flag and value that caused it.
func (e *Error) Error() string {
	var msg string
	switch {
	case e.Flag != "":
		msg = fmt.Sprintf("--%s %q: %s", e.Flag, e.Value, e.Kind)
	case e.Value != "":
		msg = fmt.Sprintf("%q: %s", e.Value, e.Kind)
	default:
		msg = e.Kind.Error()
	}
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

// Unwrap exposes Kind to errors.Is.
func (e *Error) Unwrap() error { return e.Kind }

// IsUsage reports whether err is (or wraps) a timeframe input error.
func IsUsage(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

// Range is a half-open interval [From, To). A zero From is unbounded
// below; a zero To is unbounded above.
type Range struct {
	From time.Time
	To   time.Time
}

// Contains reports whether t lies in [From, To).
func (r Range) Contains(t time.Time) bool {
	if !r.From.IsZero() && t.Before(r.From) {
		return false
	}
	if !r.To.IsZero() && !t.Before(r.To) {
		return false
	}
	return true
}

// String renders the range in --range syntax (RFC 3339 ends, To
// exclusive).
func (r Range) String() string {
	var from, to string
	if !r.From.IsZero() {
		from = r.From.Format(time.RFC3339Nano)
	}
	if !r.To.IsZero() {
		to = r.To.Format(time.RFC3339Nano)
	}
	return from + ".." + to
}

// Contains reports whether any range in rs contains t.
func Contains(rs []Range, t time.Time) bool {
	for _, r := range rs {
		if r.Contains(t) {
			return true
		}
	}
	return false
}

// Flags holds the raw timeframe flag values.
type Flags struct {
	Since  string
	Until  string
	Ranges []string
}

// IsSet reports whether any timeframe flag carries a value. No timeframe
// means incremental mode; any timeframe means an explicit backfill.
func (f Flags) IsSet() bool {
	return f.Since != "" || f.Until != "" || len(f.Ranges) > 0
}

// Options controls parsing.
type Options struct {
	// Now returns the reference instant for relative spans. Default
	// time.Now.
	Now func() time.Time
	// Location interprets bare dates, offset-less date-times and calendar
	// spans. Default time.Local.
	Location *time.Location
}

// Resolve validates f and returns its normalized ranges. It returns nil,
// nil when no flag is set; callers choose incremental mode via IsSet
// before calling.
func Resolve(f Flags, o Options) ([]Range, error) {
	if !f.IsSet() {
		return nil, nil
	}
	if len(f.Ranges) > 0 && (f.Since != "" || f.Until != "") {
		return nil, &Error{Kind: ErrConflictingFlags}
	}
	now, loc := o.now(), o.location()

	out := make([]Range, 0, len(f.Ranges)+1)
	if len(f.Ranges) == 0 {
		r, err := bounds("until", f.Since, f.Until, now, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	for _, raw := range f.Ranges {
		r, err := parseRange(raw, now, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return normalize(out), nil
}

// ParseTime parses one value of the grammar relative to now in loc (nil
// means time.Local) as a point in time; a bare date yields the start of
// that day. Resolve applies the end-of-day rule for upper bounds on top.
// The result is expressed in loc.
func ParseTime(s string, now time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	t, _, ok := parseTime(s, now, loc)
	if !ok {
		return time.Time{}, &Error{Kind: ErrUnparseable, Value: s}
	}
	return t, nil
}

var (
	relativeRe = regexp.MustCompile(`^[0-9]{1,6}[mhdw]$`)
	dateRe     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

const localDateTime = "2006-01-02T15:04:05"

// parseTime returns the instant, whether s was a bare date, and success.
func parseTime(s string, now time.Time, loc *time.Location) (t time.Time, bareDate, ok bool) {
	switch {
	case relativeRe.MatchString(s):
		t, err := util.ParseSinceAt(s, now.In(loc))
		if err != nil {
			return time.Time{}, false, false
		}
		return t.In(loc), false, true
	case dateRe.MatchString(s):
		t, err := time.ParseInLocation(time.DateOnly, s, loc)
		return t, true, err == nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), false, true
	}
	if t, err := time.ParseInLocation(localDateTime, s, loc); err == nil {
		return t, false, true
	}
	return time.Time{}, false, false
}

const rangeHint = "use from..to, or a bare YYYY-MM-DD for one whole day"

func parseRange(raw string, now time.Time, loc *time.Location) (Range, error) {
	var from, to string
	switch strings.Count(raw, "..") {
	case 0:
		day := strings.TrimSpace(raw)
		if !dateRe.MatchString(day) {
			return Range{}, &Error{Kind: ErrUnparseable, Flag: "range", Value: raw, Hint: rangeHint}
		}
		from, to = day, day
	case 1:
		from, to, _ = strings.Cut(raw, "..")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if from == "" && to == "" {
			return Range{}, &Error{Kind: ErrUnboundedRange, Flag: "range", Value: raw}
		}
	default:
		return Range{}, &Error{Kind: ErrUnparseable, Flag: "range", Value: raw, Hint: rangeHint}
	}

	r, err := bounds("range", from, to, now, loc)
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			e.Flag, e.Value = "range", raw
		}
		return Range{}, err
	}
	return r, nil
}

// bounds parses an optional lower and upper bound and validates their
// order. Empty strings are open ends. toFlag names the flag blamed for
// ordering errors.
func bounds(toFlag, from, to string, now time.Time, loc *time.Location) (Range, error) {
	var r Range
	if from != "" {
		t, _, ok := parseTime(from, now, loc)
		if !ok {
			return Range{}, &Error{Kind: ErrUnparseable, Flag: "since", Value: from}
		}
		r.From = t
	}
	toBareDate := false
	if to != "" {
		t, bare, ok := parseTime(to, now, loc)
		if !ok {
			return Range{}, &Error{Kind: ErrUnparseable, Flag: "until", Value: to}
		}
		if bare {
			// A bare-date upper bound includes the whole day.
			t = t.AddDate(0, 0, 1)
		}
		r.To, toBareDate = t, bare
	}
	if r.From.IsZero() || r.To.IsZero() {
		return r, nil
	}
	switch {
	case r.To.Before(r.From):
		return Range{}, &Error{Kind: ErrInvertedRange, Flag: toFlag, Value: to}
	case r.To.Equal(r.From) && toBareDate:
		// The included day ends exactly where the range starts: the upper
		// date is the day before the lower bound.
		return Range{}, &Error{Kind: ErrInvertedRange, Flag: toFlag, Value: to}
	case r.To.Equal(r.From):
		return Range{}, &Error{Kind: ErrEmptyRange, Flag: toFlag, Value: to}
	}
	return r, nil
}

// normalize sorts rs by From (open start first) and merges overlapping or
// adjacent ranges.
func normalize(rs []Range) []Range {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i].From, rs[j].From
		if a.IsZero() || b.IsZero() {
			return a.IsZero() && !b.IsZero()
		}
		return a.Before(b)
	})
	out := []Range{rs[0]}
	for _, next := range rs[1:] {
		cur := &out[len(out)-1]
		if cur.To.IsZero() {
			continue // open end absorbs everything after it
		}
		if next.From.IsZero() || !next.From.After(cur.To) {
			if next.To.IsZero() || next.To.After(cur.To) {
				cur.To = next.To
			}
			continue
		}
		out = append(out, next)
	}
	return out
}

func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

func (o Options) location() *time.Location {
	if o.Location == nil {
		return time.Local
	}
	return o.Location
}
