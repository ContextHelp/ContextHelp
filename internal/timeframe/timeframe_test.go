package timeframe

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	require.NoError(t, err)
	return loc
}

func fixedNow(ts time.Time) func() time.Time { return func() time.Time { return ts } }

func opts(t *testing.T, now time.Time) Options {
	t.Helper()
	return Options{Now: fixedNow(now), Location: toronto(t)}
}

func sept(loc *time.Location, day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, loc) }

func TestFlagsIsSet(t *testing.T) {
	assert.False(t, Flags{}.IsSet())
	assert.False(t, Flags{Ranges: []string{}}.IsSet())
	assert.True(t, Flags{Since: "1d"}.IsSet())
	assert.True(t, Flags{Until: "2026-09-01"}.IsSet())
	assert.True(t, Flags{Ranges: []string{"2026-09-01.."}}.IsSet())
}

func TestResolve_NoFlagsReturnsNil(t *testing.T) {
	got, err := Resolve(Flags{}, Options{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParseTime_Grammar(t *testing.T) {
	loc := toronto(t)
	now := time.Date(2026, 9, 26, 15, 30, 0, 0, loc)

	cases := []struct {
		in   string
		want time.Time
	}{
		{"2026-09-10", sept(loc, 10)},
		{"2026-09-10T23:59:59", time.Date(2026, 9, 10, 23, 59, 59, 0, loc)},
		{"2026-09-10T12:00:00Z", time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)},
		{"2026-09-10T12:00:00.123456-04:00", time.Date(2026, 9, 10, 16, 0, 0, 123456000, time.UTC)},
		{"30m", now.Add(-30 * time.Minute)},
		{"6h", now.Add(-6 * time.Hour)},
		{"7d", time.Date(2026, 9, 19, 15, 30, 0, 0, loc)},
		{"2w", time.Date(2026, 9, 12, 15, 30, 0, 0, loc)},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseTime(tc.in, now, loc)
			require.NoError(t, err)
			assert.True(t, tc.want.Equal(got), "got %s want %s", got, tc.want)
			assert.Equal(t, loc, got.Location(), "results are expressed in the given location")
		})
	}
}

func TestParseTime_Rejects(t *testing.T) {
	loc := toronto(t)
	now := time.Date(2026, 9, 26, 15, 30, 0, 0, loc)
	for _, in := range []string{
		"", "yesterday", "0d", "7", "d", "7y", "3M", "-7d", "+7d", "7 d",
		"2026-9-1", "2026-09-10 12:00", "2026/09/10", "May 1", "2026-13-01",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := ParseTime(in, now, loc)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnparseable)
		})
	}
}

func TestParseTime_DefaultsToLocal(t *testing.T) {
	got, err := ParseTime("2026-09-10", time.Now(), nil)
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local).Equal(got))
}

func TestResolve_SinceUntil(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location

	got, err := Resolve(Flags{Since: "2026-09-01"}, o)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].From.Equal(sept(loc, 1)), "bare-date lower bound is start of day")
	assert.True(t, got[0].To.IsZero(), "no --until means no upper bound")

	got, err = Resolve(Flags{Until: "2026-09-10"}, o)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].From.IsZero(), "no --since means from the beginning")
	assert.True(t, got[0].To.Equal(sept(loc, 11)))

	got, err = Resolve(Flags{Since: "7d", Until: "1d"}, o)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].From.Equal(time.Date(2026, 9, 19, 12, 0, 0, 0, loc)))
	assert.True(t, got[0].To.Equal(time.Date(2026, 9, 25, 12, 0, 0, 0, loc)))
}

// A bare-date upper bound includes that whole day: the exclusive end is
// the start of the next day.
func TestBareDateUpperBoundIncludesThatDay(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location
	for _, f := range []Flags{
		{Ranges: []string{"2026-09-01..2026-09-10"}},
		{Ranges: []string{"..2026-09-10"}},
		{Until: "2026-09-10"},
		{Since: "2026-09-01", Until: "2026-09-10"},
	} {
		got, err := Resolve(f, o)
		require.NoError(t, err, "%+v", f)
		require.Len(t, got, 1)
		assert.True(t, got[0].To.Equal(sept(loc, 11)), "%+v: To %s", f, got[0].To)
		assert.True(t, Contains(got, sept(loc, 10)))
		assert.True(t, Contains(got, time.Date(2026, 9, 10, 23, 59, 59, 999999000, loc)))
		assert.False(t, Contains(got, sept(loc, 11)))
	}
}

// Offset, offset-less date-time and relative upper bounds are literal.
func TestNonBareUpperBoundsAreLiteral(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t))
	o := opts(t, now)
	loc := o.Location

	got, err := Resolve(Flags{Ranges: []string{"..2026-09-10T12:00:00Z"}}, o)
	require.NoError(t, err)
	assert.True(t, got[0].To.Equal(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)))

	got, err = Resolve(Flags{Ranges: []string{"..2026-09-10T08:00:00"}}, o)
	require.NoError(t, err)
	assert.True(t, got[0].To.Equal(time.Date(2026, 9, 10, 8, 0, 0, 0, loc)))

	got, err = Resolve(Flags{Until: "1d"}, o)
	require.NoError(t, err)
	assert.True(t, got[0].To.Equal(now.AddDate(0, 0, -1)))
}

func TestSameBareDateBothEndsIsOneFullDay(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location
	for _, f := range []Flags{
		{Ranges: []string{"2026-09-10..2026-09-10"}},
		{Since: "2026-09-10", Until: "2026-09-10"},
	} {
		got, err := Resolve(f, o)
		require.NoError(t, err, "%+v", f)
		require.Len(t, got, 1)
		assert.Equal(t, Range{From: sept(loc, 10), To: sept(loc, 11)}, got[0])
	}
}

// --range DATE (no "..") is shorthand for DATE..DATE.
func TestSingleDayShorthand(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location

	short, err := Resolve(Flags{Ranges: []string{"2026-09-10"}}, o)
	require.NoError(t, err)
	long, err := Resolve(Flags{Ranges: []string{"2026-09-10..2026-09-10"}}, o)
	require.NoError(t, err)
	assert.Equal(t, long, short)
	assert.Equal(t, []Range{{From: sept(loc, 10), To: sept(loc, 11)}}, short)

	// Adjacent single days merge; a gap day keeps ranges apart.
	got, err := Resolve(Flags{Ranges: []string{"2026-09-11", "2026-09-10", "2026-09-13"}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{
		{From: sept(loc, 10), To: sept(loc, 12)},
		{From: sept(loc, 13), To: sept(loc, 14)},
	}, got)

	// Shorthand mixes with ordinary ranges.
	got, err = Resolve(Flags{Ranges: []string{"2026-09-12", "2026-09-01..2026-09-11"}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{{From: sept(loc, 1), To: sept(loc, 13)}}, got)
}

func TestSingleDayShorthand_RejectsNonDates(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	for _, v := range []string{"2026-09-10T12:00:00Z", "2026-09-10T12:00:00", "3d", "12h", "soon", ""} {
		t.Run(v, func(t *testing.T) {
			_, err := Resolve(Flags{Ranges: []string{v}}, o)
			require.ErrorIs(t, err, ErrUnparseable)
			assert.Contains(t, err.Error(), "from..to", "message points at the range form")
		})
	}
}

func TestRangeContains_HalfOpen(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	r := Range{From: from, To: to}

	assert.True(t, r.Contains(from), "From is inclusive")
	assert.True(t, r.Contains(to.Add(-time.Microsecond)))
	assert.False(t, r.Contains(to), "To is exclusive")
	assert.False(t, r.Contains(from.Add(-time.Nanosecond)))

	assert.True(t, Range{To: to}.Contains(time.Time{}.Add(time.Hour)), "zero From is unbounded")
	assert.True(t, Range{From: from}.Contains(from.AddDate(100, 0, 0)), "zero To is unbounded")
	assert.False(t, Range{To: to}.Contains(to))
}

func TestResolve_RangeOpenEnds(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location

	got, err := Resolve(Flags{Ranges: []string{"2026-09-01.."}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{{From: sept(loc, 1)}}, got)

	got, err = Resolve(Flags{Ranges: []string{"..2026-09-10"}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{{To: sept(loc, 11)}}, got)
}

func TestResolve_MergesSortsAndKeepsDisjoint(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location

	got, err := Resolve(Flags{Ranges: []string{
		"2026-09-20..2026-09-22",                   // disjoint, given first
		"2026-09-05..2026-09-07",                   // overlaps previous-in-order
		"2026-09-01..2026-09-05",                   // [1, 6)
		"2026-09-08T00:00:00..2026-09-10",          // adjacent to [5, 8)
		"2026-09-02..2026-09-03",                   // contained
		"2026-09-12T00:00:00..2026-09-12T06:00:00", // Sep 11 is a gap
	}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{
		{From: sept(loc, 1), To: sept(loc, 11)},
		{From: sept(loc, 12), To: time.Date(2026, 9, 12, 6, 0, 0, 0, loc)},
		{From: sept(loc, 20), To: sept(loc, 23)},
	}, got)
}

func TestResolve_MergesOpenEnds(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))
	loc := o.Location

	got, err := Resolve(Flags{Ranges: []string{
		"2026-09-15..",
		"2026-09-01..2026-09-03",
		"2026-09-20..2026-09-21",
		"..2026-08-01",
	}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{
		{To: time.Date(2026, 8, 2, 0, 0, 0, 0, loc)},
		{From: sept(loc, 1), To: sept(loc, 4)},
		{From: sept(loc, 15)}, // open end absorbs later ranges
	}, got)

	got, err = Resolve(Flags{Ranges: []string{"..2026-09-05", "2026-09-01.."}}, o)
	require.NoError(t, err)
	assert.Equal(t, []Range{{}}, got)
}

func TestResolve_Errors(t *testing.T) {
	o := opts(t, time.Date(2026, 9, 26, 12, 0, 0, 0, toronto(t)))

	cases := []struct {
		name  string
		flags Flags
		kind  error
	}{
		{"empty literal range", Flags{Ranges: []string{"2026-09-01T00:00:00..2026-09-01T00:00:00"}}, ErrEmptyRange},
		{"empty relative range", Flags{Ranges: []string{"3d..3d"}}, ErrEmptyRange},
		{"empty since/until", Flags{Since: "2026-09-01T10:00:00Z", Until: "2026-09-01T06:00:00-04:00"}, ErrEmptyRange},
		{"inverted range", Flags{Ranges: []string{"2026-09-10..2026-09-01"}}, ErrInvertedRange},
		{"inverted by one day", Flags{Ranges: []string{"2026-09-10..2026-09-09"}}, ErrInvertedRange},
		{"inverted since/until dates", Flags{Since: "2026-09-10", Until: "2026-09-09"}, ErrInvertedRange},
		{"inverted since/until relative", Flags{Since: "1d", Until: "7d"}, ErrInvertedRange},
		{"inverted literal", Flags{Ranges: []string{"2026-09-10T12:00:00..2026-09-10T11:00:00"}}, ErrInvertedRange},
		{"unparseable since", Flags{Since: "last tuesday"}, ErrUnparseable},
		{"unparseable until", Flags{Until: "soon"}, ErrUnparseable},
		{"unparseable range end", Flags{Ranges: []string{"2026-09-01..later"}}, ErrUnparseable},
		{"range with two separators", Flags{Ranges: []string{"1d..2d..3d"}}, ErrUnparseable},
		{"range plus since", Flags{Since: "1d", Ranges: []string{"2026-09-01.."}}, ErrConflictingFlags},
		{"range plus until", Flags{Until: "1d", Ranges: []string{"2026-09-01.."}}, ErrConflictingFlags},
		{"both ends open", Flags{Ranges: []string{".."}}, ErrUnboundedRange},
		{"both ends open with spaces", Flags{Ranges: []string{" .. "}}, ErrUnboundedRange},
		{"one bad range among good", Flags{Ranges: []string{"2026-09-01..", "3d..3d"}}, ErrEmptyRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.flags, o)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.ErrorIs(t, err, tc.kind)

			var fe *Error
			require.True(t, errors.As(err, &fe), "errors are *timeframe.Error")
			assert.True(t, IsUsage(err))
			assert.NotEmpty(t, fe.Error())
		})
	}
	assert.False(t, IsUsage(errors.New("other")))
	assert.False(t, IsUsage(nil))
}

// America/Toronto springs forward on 2026-03-08 (a 23-hour day) and falls
// back on 2026-11-01 (a 25-hour day). A bare-date upper bound ends at the
// next wall-clock midnight, not 24 elapsed hours later.
func TestDST_BareDatesSpanWallClockDays(t *testing.T) {
	loc := toronto(t)
	o := Options{Now: fixedNow(time.Date(2026, 12, 1, 0, 0, 0, 0, loc)), Location: loc}

	for _, v := range []string{"2026-03-08..2026-03-08", "2026-03-08"} {
		got, err := Resolve(Flags{Ranges: []string{v}}, o)
		require.NoError(t, err)
		assert.Equal(t, 23*time.Hour, got[0].To.Sub(got[0].From), v)
		assert.True(t, got[0].To.Equal(time.Date(2026, 3, 9, 0, 0, 0, 0, loc)), v)
	}
	for _, v := range []string{"2026-11-01..2026-11-01", "2026-11-01"} {
		got, err := Resolve(Flags{Ranges: []string{v}}, o)
		require.NoError(t, err)
		assert.Equal(t, 25*time.Hour, got[0].To.Sub(got[0].From), v)
		assert.True(t, got[0].To.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)), v)
	}

	got, err := Resolve(Flags{Until: "2026-11-01"}, o)
	require.NoError(t, err)
	assert.True(t, got[0].To.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)))
	assert.True(t, Contains(got, time.Date(2026, 11, 1, 23, 30, 0, 0, loc)))

	// Adjacent wall-clock days across the transition merge cleanly.
	got, err = Resolve(Flags{Ranges: []string{"2026-03-08", "2026-03-07"}}, o)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 47*time.Hour, got[0].To.Sub(got[0].From))
}

// Nd and Nw are calendar spans in the location (same wall-clock time N
// days earlier); Nh and Nm are elapsed durations.
func TestDST_RelativeSpans(t *testing.T) {
	loc := toronto(t)
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, loc) // noon EDT, DST began 02:00

	day, err := ParseTime("1d", now, loc)
	require.NoError(t, err)
	assert.True(t, day.Equal(time.Date(2026, 3, 7, 12, 0, 0, 0, loc)))
	assert.Equal(t, 23*time.Hour, now.Sub(day))

	hours, err := ParseTime("24h", now, loc)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, now.Sub(hours))
	assert.Equal(t, 11, hours.In(loc).Hour())

	// now given in UTC is still interpreted in loc for calendar math.
	day2, err := ParseTime("1d", now.UTC(), loc)
	require.NoError(t, err)
	assert.True(t, day.Equal(day2))

	fall := time.Date(2026, 11, 1, 12, 0, 0, 0, loc)
	week, err := ParseTime("1w", fall, loc)
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour+time.Hour, fall.Sub(week))
}

func TestRangeString(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-09-01T00:00:00Z..", Range{From: from}.String())
	assert.Equal(t, "..2026-09-01T00:00:00Z", Range{To: from}.String())
}
