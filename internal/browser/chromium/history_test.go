package chromium

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// testRow is one synthetic visit written into a fixture History database.
type testRow struct {
	url        string
	title      string
	at         time.Time
	transition uint32
}

// typed is a plain top-level navigation: its own one-visit chain.
const typed = transitionTyped | transitionChainStart | transitionChainEnd

// base is a fixed instant with microsecond precision.
var base = time.Date(2026, 3, 14, 15, 9, 26, 535897000, time.UTC)

func at(d time.Duration) time.Time { return base.Add(d) }

// createHistoryDB creates path as a History database from the dumped
// schema and inserts rows, storing transitions as Chromium does (int32).
func createHistoryDB(t *testing.T, path string, rows []testRow) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join("testdata", "history", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	insertRows(t, db, rows)
}

func insertRows(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, rows []testRow,
) {
	t.Helper()
	for _, r := range rows {
		vt := toWebKit(r.at)
		res, err := db.Exec(`INSERT INTO urls(url, title, last_visit_time) VALUES (?, ?, ?)`, r.url, r.title, vt)
		if err != nil {
			t.Fatalf("insert url: %v", err)
		}
		urlID, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO visits(url, visit_time, transition) VALUES (?, ?, ?)`,
			urlID, vt, int64(int32(r.transition))); err != nil {
			t.Fatalf("insert visit: %v", err)
		}
	}
}

// newProfile returns a profile dir holding a History database with rows.
func newProfile(t *testing.T, rows []testRow) string {
	t.Helper()
	dir := t.TempDir()
	createHistoryDB(t, filepath.Join(dir, HistoryFile), rows)
	return dir
}

func urls(vs []HistoryVisit) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.URL
	}
	return out
}

func assertURLs(t *testing.T, got []HistoryVisit, want ...string) {
	t.Helper()
	g := urls(got)
	if fmt.Sprint(g) != fmt.Sprint(want) {
		t.Fatalf("urls = %q, want %q", g, want)
	}
}

func TestWebKitTimeConversion(t *testing.T) {
	epoch := time.Date(1601, 1, 1, 0, 0, 0, 0, time.UTC)
	unix := time.Unix(0, 0).UTC()
	tests := []struct {
		name string
		vt   int64
		want time.Time
	}{
		{"webkit epoch", 0, epoch},
		{"one microsecond after epoch", 1, epoch.Add(time.Microsecond)},
		{"unix epoch", 11_644_473_600_000_000, unix},
		{"fixed instant", 11_644_473_600_000_000 + base.UnixMicro(), base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromWebKit(tt.vt)
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Fatalf("fromWebKit(%d) = %v, want %v UTC", tt.vt, got, tt.want)
			}
			if back := toWebKit(got); back != tt.vt {
				t.Fatalf("toWebKit(fromWebKit(%d)) = %d", tt.vt, back)
			}
		})
	}
}

func TestToWebKitRoundsSubMicrosecondUp(t *testing.T) {
	// A range bound between two microseconds must exclude the earlier one:
	// [t+1ns, ...) starts at the next representable visit time.
	if got, want := toWebKit(base.Add(time.Nanosecond)), toWebKit(base)+1; got != want {
		t.Fatalf("toWebKit(base+1ns) = %d, want %d", got, want)
	}
	if got, want := toWebKit(base.Add(999*time.Nanosecond)), toWebKit(base)+1; got != want {
		t.Fatalf("toWebKit(base+999ns) = %d, want %d", got, want)
	}
	// Before 1601 the value is negative; rounding must still go up.
	pre := time.Date(1600, 12, 31, 23, 59, 59, 999_999_001, time.UTC)
	if got := toWebKit(pre); got != 0 {
		t.Fatalf("toWebKit(1ns-ish before epoch rounded up) = %d, want 0", got)
	}
}

func TestHistoryVisitsRange(t *testing.T) {
	dir := newProfile(t, []testRow{
		{"https://example.com/a", "A", at(0), typed},
		{"https://example.com/b", "B", at(time.Second), typed},
		{"https://example.com/c", "C", at(2 * time.Second), typed},
	})
	ctx := context.Background()

	tests := []struct {
		name     string
		from, to time.Time
		want     []string
	}{
		{"from inclusive, to exclusive", at(0), at(2 * time.Second), []string{"https://example.com/a", "https://example.com/b"}},
		{"to equal to a visit excludes it", at(time.Second), at(2 * time.Second), []string{"https://example.com/b"}},
		{"to one microsecond later includes it", at(2 * time.Second), at(2*time.Second + time.Microsecond), []string{"https://example.com/c"}},
		{"from one nanosecond after a visit excludes it", at(time.Nanosecond), time.Time{}, []string{"https://example.com/b", "https://example.com/c"}},
		{"empty range", at(time.Second), at(time.Second), nil},
		{"inverted range", at(2 * time.Second), at(0), nil},
		{"zero to is unbounded", at(time.Second), time.Time{}, []string{"https://example.com/b", "https://example.com/c"}},
		{"zero from and to is everything", time.Time{}, time.Time{}, []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"}},
		{"after last visit", at(3 * time.Second), time.Time{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HistoryVisits(ctx, dir, tt.from, tt.to, 0)
			if err != nil {
				t.Fatalf("HistoryVisits: %v", err)
			}
			assertURLs(t, got, tt.want...)
		})
	}
}

func TestHistoryVisitsFields(t *testing.T) {
	dir := newProfile(t, []testRow{{"https://example.com/x", "Example X", at(0), typed}})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	want := HistoryVisit{URL: "https://example.com/x", Title: "Example X", VisitedAt: base, Transition: typed}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
	if got[0].VisitedAt.Location() != time.UTC {
		t.Fatalf("VisitedAt location = %v, want UTC", got[0].VisitedAt.Location())
	}
}

func TestHistoryVisitsOrdersOldestFirst(t *testing.T) {
	dir := newProfile(t, []testRow{
		{"https://example.com/late", "", at(time.Hour), typed},
		{"https://example.com/early", "", at(0), typed},
		{"https://example.com/mid", "", at(time.Minute), typed},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/early", "https://example.com/mid", "https://example.com/late")
}

func TestHistoryVisitsDropsSubframes(t *testing.T) {
	chain := transitionChainStart | transitionChainEnd
	dir := newProfile(t, []testRow{
		{"https://example.com/top", "", at(0), transitionLink | chain},
		{"https://example.com/ad-frame", "", at(1), transitionAutoSubframe | chain},
		{"https://example.com/clicked-frame", "", at(2), transitionManualSubframe | chain},
		{"https://example.com/form", "", at(3), transitionFormSubmit | chain},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/top", "https://example.com/form")
}

func TestHistoryVisitsCollapsesRedirectChains(t *testing.T) {
	dir := newProfile(t, []testRow{
		// Server redirect chain: short link -> tracker -> landing page.
		{"https://example.com/short", "", at(0), transitionLink | transitionChainStart},
		{"https://example.com/track", "", at(0), transitionLink | transitionServerRedirect},
		{"https://example.com/landing", "Landing", at(0), transitionLink | transitionServerRedirect | transitionChainEnd},
		// Client redirect: page loads, then script navigates away.
		{"https://example.com/old", "", at(time.Second), transitionTyped | transitionChainStart},
		{"https://example.com/new", "New", at(2 * time.Second), transitionTyped | transitionClientRedirect | transitionChainEnd},
		// Plain visit, no redirect.
		{"https://example.com/plain", "", at(3 * time.Second), typed},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/landing", "https://example.com/new", "https://example.com/plain")
}

func TestHistoryVisitsLimit(t *testing.T) {
	dir := newProfile(t, []testRow{
		{"https://example.com/1", "", at(0), typed},
		{"https://example.com/2", "", at(1 * time.Second), typed},
		{"https://example.com/3", "", at(2 * time.Second), typed},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 2)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/1", "https://example.com/2")
}

func TestHistoryVisitsLimitCountsKeptVisitsOnly(t *testing.T) {
	// A batch must not fill up with filtered rows, or a poller making
	// progress by the last returned time would stall on an empty batch.
	dir := newProfile(t, []testRow{
		{"https://example.com/frame1", "", at(0), transitionManualSubframe | transitionChainStart | transitionChainEnd},
		{"https://example.com/hop", "", at(1), transitionLink | transitionChainStart},
		{"https://example.com/kept", "", at(2), typed},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 1)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/kept")
}

func TestHistoryVisitsLimitKeepsTimestampTies(t *testing.T) {
	// Visits can share a visit_time. A batch that ends on a tie returns
	// every tied visit, so a strictly-after cursor never skips one.
	dir := newProfile(t, []testRow{
		{"https://example.com/1", "", at(0), typed},
		{"https://example.com/2a", "", at(time.Second), typed},
		{"https://example.com/2b", "", at(time.Second), typed},
		{"https://example.com/3", "", at(2 * time.Second), typed},
	})
	ctx := context.Background()
	got, err := HistoryVisits(ctx, dir, time.Time{}, time.Time{}, 2)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/1", "https://example.com/2a", "https://example.com/2b")

	next, err := HistoryVisits(ctx, dir, got[len(got)-1].VisitedAt.Add(time.Nanosecond), time.Time{}, 2)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, next, "https://example.com/3")
}

func TestHistoryVisitsTieExtensionRespectsFilters(t *testing.T) {
	dir := newProfile(t, []testRow{
		{"https://example.com/1", "", at(0), typed},
		{"https://example.com/frame", "", at(0), transitionAutoSubframe | transitionChainStart | transitionChainEnd},
		{"https://example.com/hop", "", at(0), transitionLink | transitionChainStart},
		{"https://example.com/1b", "", at(0), typed},
	})
	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 1)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/1", "https://example.com/1b")
}

func TestHistoryVisitsMissingDB(t *testing.T) {
	_, err := HistoryVisits(context.Background(), t.TempDir(), time.Time{}, time.Time{}, 0)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestHistoryVisitsCanceledContext(t *testing.T) {
	dir := newProfile(t, []testRow{{"https://example.com/a", "", at(0), typed}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := HistoryVisits(ctx, dir, time.Time{}, time.Time{}, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// snapshotDir maps each file in dir to its bytes and mtime.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		out[e.Name()] = fmt.Sprintf("%x|%d", data, fi.ModTime().UnixNano())
	}
	return out
}

func TestHistoryVisitsCopiesAndCleansUp(t *testing.T) {
	// The temp dir name has a space to exercise file: URI escaping.
	tmp := filepath.Join(t.TempDir(), "tmp dir")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)

	dir := filepath.Join(t.TempDir(), "Profile 3")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	createHistoryDB(t, filepath.Join(dir, HistoryFile), []testRow{{"https://example.com/a", "", at(0), typed}})
	// An idle Chromium journal: persisted, header zeroed.
	if err := os.WriteFile(filepath.Join(dir, HistoryFile+"-journal"), make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, dir)

	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/a")

	if after := snapshotDir(t, dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("profile dir changed by a read")
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temp dir not cleaned up: %v", left)
	}
}

// journalMagic opens every valid rollback journal header.
var journalMagic = []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// newHotJournalProfile snapshots a History database mid-transaction, the
// way a copy taken while the browser writes can land: uncommitted pages
// already spilled into History, originals in a hot History-journal.
func newHotJournalProfile(t *testing.T, committed, uncommitted []testRow) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work.db")
	createHistoryDB(t, work, committed)

	db, err := sql.Open("sqlite", work)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=DELETE", "PRAGMA locking_mode=EXCLUSIVE", "PRAGMA cache_size=1"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	insertRows(t, tx, uncommitted)
	// Rewrite existing pages too so the spill touches the file.
	if _, err := tx.Exec(`UPDATE urls SET title = hex(randomblob(400))`); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	copyFile(t, work, filepath.Join(dir, HistoryFile))
	copyFile(t, work+"-journal", filepath.Join(dir, HistoryFile+"-journal"))

	hdr := make([]byte, len(journalMagic))
	f, err := os.Open(filepath.Join(dir, HistoryFile+"-journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.ReadFull(f, hdr); err != nil || !bytes.Equal(hdr, journalMagic) {
		t.Fatalf("fixture journal is not hot: header %x, err %v", hdr, err)
	}
	return dir
}

func TestHistoryVisitsRollsBackHotJournalInCopy(t *testing.T) {
	committed := make([]testRow, 0, 50)
	for i := range 50 {
		committed = append(committed, testRow{fmt.Sprintf("https://example.com/c%02d", i), "", at(time.Duration(i) * time.Second), typed})
	}
	uncommitted := make([]testRow, 0, 200)
	for i := range 200 {
		uncommitted = append(uncommitted, testRow{fmt.Sprintf("https://example.com/u%03d", i), "", at(time.Hour + time.Duration(i)*time.Second), typed})
	}
	dir := newHotJournalProfile(t, committed, uncommitted)
	before := snapshotDir(t, dir)

	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	if len(got) != len(committed) {
		t.Fatalf("got %d visits, want the %d committed ones", len(got), len(committed))
	}
	for i, v := range got {
		if v.URL != committed[i].url || v.Title != "" {
			t.Fatalf("visit %d = %+v, want committed row %q with its original title", i, v, committed[i].url)
		}
	}
	if after := snapshotDir(t, dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("hot journal rolled back in the profile dir instead of the copy")
	}
}

func TestHistoryVisitsRetriesCopyWhileWritten(t *testing.T) {
	dir := newProfile(t, []testRow{{"https://example.com/a", "", at(0), typed}})
	writes := 0
	restore := setAfterCopyHook(func() {
		if writes < 2 {
			writes++
			// Simulate the browser writing during the copy.
			mt := time.Now().Add(time.Duration(writes) * time.Second)
			if err := os.Chtimes(filepath.Join(dir, HistoryFile), mt, mt); err != nil {
				t.Fatal(err)
			}
		}
	})
	defer restore()

	got, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatalf("HistoryVisits: %v", err)
	}
	assertURLs(t, got, "https://example.com/a")
	if writes != 2 {
		t.Fatalf("hook saw %d writes, want 2", writes)
	}
}

func TestHistoryVisitsGivesUpOnBusyDB(t *testing.T) {
	dir := newProfile(t, []testRow{{"https://example.com/a", "", at(0), typed}})
	n := 0
	restore := setAfterCopyHook(func() {
		n++
		mt := time.Now().Add(time.Duration(n) * time.Second)
		if err := os.Chtimes(filepath.Join(dir, HistoryFile), mt, mt); err != nil {
			t.Fatal(err)
		}
	})
	defer restore()

	if _, err := HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0); !errors.Is(err, ErrHistoryBusy) {
		t.Fatalf("err = %v, want ErrHistoryBusy", err)
	}
}
