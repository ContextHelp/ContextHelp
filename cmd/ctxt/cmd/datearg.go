package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"hop.top/kit/go/console/output"
	"hop.top/kit/go/core/util"

	"github.com/ideacrafterslabs/ctxt/internal/timeframe"
)

// dateFlagForms is the grammar every created-date and mentioned-date flag
// takes.
const dateFlagForms = "3d, 42h, 1w, 30min (m = minute), 1mo (M = month), 1y, yesterday, today, YYYY-MM-DD, YYYY-MM-DDTHH:MM:SS (local) or an RFC 3339 timestamp"

var (
	relativeDateRe = regexp.MustCompile(`^[0-9]{1,6}(h|d|w|m|M|y|min|mo)$`)
	plainDateRe    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// resolveDate turns a date flag value into an instant. Relative spans
// (3d, 42h, 1w, 30m, 1M, 1y) are now minus the span, read as kit reads
// them: m or min is a minute, M or mo a month. yesterday, today and
// YYYY-MM-DD name whole local days: a lower bound is the day's start, an
// upper bound (upper) the start of the next day, so bounds are half-open
// and --until 2026-01-31 takes the 31st whole when the caller compares
// with <. An RFC 3339 timestamp or a local YYYY-MM-DDTHH:MM:SS is taken as
// it is. Anything else is a usage error naming the flag.
func resolveDate(flag, v string, upper bool, now time.Time) (time.Time, error) {
	v = strings.TrimSpace(v)
	day := func(t time.Time) time.Time {
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		if upper {
			t = t.AddDate(0, 0, 1)
		}
		return t
	}
	switch {
	case v == "today":
		return day(now), nil
	case v == "yesterday":
		return day(now.AddDate(0, 0, -1)), nil
	case relativeDateRe.MatchString(v):
		if t, err := util.ParseSinceAt(kitSpan(v), now); err == nil {
			return t, nil
		}
	default:
		if t, err := timeframe.ParseTime(v, now, now.Location()); err == nil {
			if plainDateRe.MatchString(v) {
				return day(t), nil
			}
			return t, nil
		}
	}
	e := output.UsageError(fmt.Sprintf("--%s %q: want %s", flag, v, dateFlagForms))
	e.SuggestedFix = "use a relative span like 3d, a day like 2026-01-31, or yesterday/today"
	return time.Time{}, e
}

// kitSpan is a relative span spelled as kit's short form: min is its
// m (minute), mo its M (month); the rest already match.
func kitSpan(v string) string {
	switch {
	case strings.HasSuffix(v, "min"):
		return strings.TrimSuffix(v, "min") + "m"
	case strings.HasSuffix(v, "mo"):
		return strings.TrimSuffix(v, "mo") + "M"
	}
	return v
}

// resolveInstant is resolveDate as the RFC 3339 UTC text the API takes.
func resolveInstant(flag, v string, upper bool) (string, error) {
	t, err := resolveDate(flag, v, upper, dateNow())
	if err != nil {
		return "", err
	}
	return t.UTC().Format(time.RFC3339), nil
}

// resolveDay is a date flag value as the YYYY-MM-DD day the mentioned
// dates compare against: a plain date as written, anything else the
// local day of its instant (an RFC 3339 timestamp, the UTC day).
func resolveDay(flag, v string, now time.Time) (string, error) {
	v = strings.TrimSpace(v)
	if plainDateRe.MatchString(v) {
		if _, err := time.Parse(time.DateOnly, v); err == nil {
			return v, nil
		}
	}
	t, err := resolveDate(flag, v, false, now)
	if err != nil {
		return "", err
	}
	if _, perr := time.Parse(time.RFC3339, v); perr == nil {
		t = t.UTC()
	}
	return t.Format(time.DateOnly), nil
}

// dateNow is the clock date flags resolve against; tests replace it.
var dateNow = time.Now
