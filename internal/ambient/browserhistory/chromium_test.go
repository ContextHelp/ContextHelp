package browserhistory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
)

var (
	_ BrowserClient = (*ChromiumClient)(nil)
	_ RangeReader   = (*ChromiumClient)(nil)
)

// plainVisit is a typed top-level navigation (TYPED | CHAIN_START |
// CHAIN_END), the transition of an ordinary visit.
const plainVisit = 1 | 0x10000000 | 0x20000000

const localState = `{"profile": {
  "info_cache": {
    "Default": {"name": "Personal"},
    "Profile 3": {"name": "Research"}
  },
  "last_used": "Default",
  "profiles_order": ["Default", "Profile 3"]
}}`

var t0 = time.Date(2026, 5, 1, 9, 0, 0, 123456000, time.UTC)

// newUserDataDir builds a user data dir whose "Profile 3" ("Research")
// holds a History database with one plain visit per entry in times.
func newUserDataDir(t *testing.T, times ...time.Time) string {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, chromium.LocalStateFile), []byte(localState), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"Default", "Profile 3"} {
		if err := os.Mkdir(filepath.Join(base, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "browser", "chromium", "testdata", "history", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(base, "Profile 3", chromium.HistoryFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	for i, ts := range times {
		vt := ts.UnixMicro() + 11_644_473_600_000_000
		res, err := db.Exec(`INSERT INTO urls(url, title, last_visit_time) VALUES (?, ?, ?)`,
			fmt.Sprintf("https://example.com/%d", i), fmt.Sprintf("Page %d", i), vt)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO visits(url, visit_time, transition) VALUES (?, ?, ?)`, id, vt, plainVisit); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func visitURLs(vs []Visit) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.URL
	}
	return out
}

func TestChromiumClientName(t *testing.T) {
	base := newUserDataDir(t)
	for _, query := range []string{"Research", "Profile 3"} {
		c, err := NewChromiumClient(chromium.Brave, query, chromium.WithUserDataDir(base))
		if err != nil {
			t.Fatalf("NewChromiumClient(%q): %v", query, err)
		}
		if got := c.Name(); got != "brave:Profile 3" {
			t.Fatalf("Name() = %q, want %q", got, "brave:Profile 3")
		}
	}
}

func TestChromiumClientUnknownProfile(t *testing.T) {
	base := newUserDataDir(t)
	_, err := NewChromiumClient(chromium.Brave, "Nope", chromium.WithUserDataDir(base))
	if !errors.Is(err, chromium.ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
}

func TestChromiumClientVisitsSincePages(t *testing.T) {
	times := make([]time.Time, 5)
	for i := range times {
		times[i] = t0.Add(time.Duration(i) * time.Minute)
	}
	base := newUserDataDir(t, times...)
	c, err := NewChromiumClient(chromium.Brave, "Research", chromium.WithUserDataDir(base))
	if err != nil {
		t.Fatal(err)
	}
	c.BatchSize = 2
	ctx := context.Background()

	var (
		since   time.Time
		got     []string
		batches int
	)
	for {
		vs, err := c.VisitsSince(ctx, since)
		if err != nil {
			t.Fatalf("VisitsSince: %v", err)
		}
		if len(vs) == 0 {
			break
		}
		if len(vs) > c.BatchSize {
			t.Fatalf("batch of %d exceeds BatchSize %d", len(vs), c.BatchSize)
		}
		batches++
		if batches > 10 {
			t.Fatal("polling made no progress")
		}
		for _, v := range vs {
			if v.Browser != "brave" {
				t.Fatalf("Browser = %q, want brave", v.Browser)
			}
			if !v.VisitedAt.After(since) {
				t.Fatalf("visit at %v not after since %v", v.VisitedAt, since)
			}
		}
		got = append(got, visitURLs(vs)...)
		since = vs[len(vs)-1].VisitedAt
	}
	want := []string{"https://example.com/0", "https://example.com/1", "https://example.com/2", "https://example.com/3", "https://example.com/4"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged visits = %q, want %q", got, want)
	}
	if batches != 3 {
		t.Fatalf("batches = %d, want 3", batches)
	}
}

func TestChromiumClientVisitsCarrySource(t *testing.T) {
	base := newUserDataDir(t, t0, t0.Add(time.Minute))
	c, err := NewChromiumClient(chromium.Brave, "Research", chromium.WithUserDataDir(base))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	since, err := c.VisitsSince(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	between, err := c.VisitsBetween(ctx, t0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for name, vs := range map[string][]Visit{"VisitsSince": since, "VisitsBetween": between} {
		if len(vs) != 2 {
			t.Fatalf("%s returned %d visits, want 2", name, len(vs))
		}
		for _, v := range vs {
			if v.Source != "brave:Profile 3" {
				t.Fatalf("%s: Source = %q, want %q", name, v.Source, "brave:Profile 3")
			}
			if v.Browser != "brave" {
				t.Fatalf("%s: Browser = %q, want brave", name, v.Browser)
			}
		}
	}
}

func TestSourceEventMetadataCarriesVisitSource(t *testing.T) {
	base := newUserDataDir(t, t0)
	c, err := NewChromiumClient(chromium.Brave, "Research", chromium.WithUserDataDir(base))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Browsers: []BrowserClient{c}, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	var ev ambient.RawEvent
	select {
	case ev = <-s.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	if ev.Metadata["source"] != "brave:Profile 3" {
		t.Fatalf("Metadata[source] = %v, want %q", ev.Metadata["source"], "brave:Profile 3")
	}
	if ev.Metadata["browser"] != "brave" {
		t.Fatalf("Metadata[browser] = %v, want brave", ev.Metadata["browser"])
	}
}

func TestSourceEventMetadataOmitsEmptySource(t *testing.T) {
	s, err := New(Config{Browsers: []BrowserClient{&fakeBrowser{name: "chrome"}}})
	if err != nil {
		t.Fatal(err)
	}
	ev := s.toRawEvent(Visit{URL: "https://example.com/", VisitedAt: t0, Browser: "chrome"})
	if _, ok := ev.Metadata["source"]; ok {
		t.Fatalf("Metadata[source] set for a visit without Source: %v", ev.Metadata["source"])
	}
}

func TestChromiumClientVisitsSinceIsStrict(t *testing.T) {
	base := newUserDataDir(t, t0, t0.Add(time.Microsecond))
	c, err := NewChromiumClient(chromium.Brave, "Research", chromium.WithUserDataDir(base))
	if err != nil {
		t.Fatal(err)
	}
	vs, err := c.VisitsSince(context.Background(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if got := visitURLs(vs); fmt.Sprint(got) != "[https://example.com/1]" {
		t.Fatalf("VisitsSince(t0) = %q, want only the visit after t0", got)
	}
}

func TestChromiumClientVisitsBetween(t *testing.T) {
	base := newUserDataDir(t, t0, t0.Add(time.Hour), t0.Add(2*time.Hour))
	c, err := NewChromiumClient(chromium.Brave, "Profile 3", chromium.WithUserDataDir(base))
	if err != nil {
		t.Fatal(err)
	}
	c.BatchSize = 1 // range reads are not batched
	ctx := context.Background()

	vs, err := c.VisitsBetween(ctx, t0, t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := visitURLs(vs); fmt.Sprint(got) != "[https://example.com/0 https://example.com/1]" {
		t.Fatalf("VisitsBetween = %q", got)
	}
	if vs[0].Title != "Page 0" || !vs[0].VisitedAt.Equal(t0) || vs[0].Browser != "brave" {
		t.Fatalf("visit = %+v", vs[0])
	}

	// Overlapping ranges agree on the shared part.
	a, err := c.VisitsBetween(ctx, t0.Add(time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got := visitURLs(a); fmt.Sprint(got) != "[https://example.com/1 https://example.com/2]" {
		t.Fatalf("open-ended VisitsBetween = %q", got)
	}
}
