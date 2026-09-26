package chromiumtest

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // register the "sqlite" driver
)

// HistorySchema is the urls and visits schema of a Chromium History
// database, identical to ../testdata/history/schema.sql (a test keeps the
// two in sync).
const HistorySchema = `-- urls and visits tables of a Chromium History database: schema only, no rows.
-- Dumped with: sqlite3 <copy of History> '.schema urls' '.schema visits'
CREATE TABLE urls(id INTEGER PRIMARY KEY AUTOINCREMENT,url LONGVARCHAR,title LONGVARCHAR,visit_count INTEGER DEFAULT 0 NOT NULL,typed_count INTEGER DEFAULT 0 NOT NULL,last_visit_time INTEGER NOT NULL,hidden INTEGER DEFAULT 0 NOT NULL);
CREATE INDEX urls_url_index ON urls (url);
CREATE TABLE visits(id INTEGER PRIMARY KEY AUTOINCREMENT,url INTEGER NOT NULL,visit_time INTEGER NOT NULL,from_visit INTEGER,external_referrer_url TEXT,transition INTEGER DEFAULT 0 NOT NULL,segment_id INTEGER,visit_duration INTEGER DEFAULT 0 NOT NULL,incremented_omnibox_typed_score BOOLEAN DEFAULT FALSE NOT NULL,opener_visit INTEGER,originator_cache_guid TEXT,originator_visit_id INTEGER,originator_from_visit INTEGER,originator_opener_visit INTEGER,is_known_to_sync BOOLEAN DEFAULT FALSE NOT NULL,consider_for_ntp_most_visited BOOLEAN DEFAULT FALSE NOT NULL,visited_link_id INTEGER DEFAULT 0 NOT NULL,app_id TEXT);
CREATE INDEX visits_url_index ON visits (url);
CREATE INDEX visits_from_index ON visits (from_visit);
CREATE INDEX visits_time_index ON visits (visit_time);
CREATE INDEX visits_originator_id_index ON visits (originator_visit_id);
`

// HistoryFileName is the per-profile History database file name.
const HistoryFileName = "History"

// TransitionTyped is a typed top-level navigation that was not
// redirected (core type TYPED plus CHAIN_START and CHAIN_END), the
// default for a Visit with a zero Transition.
const TransitionTyped uint32 = 0x1 | 0x10000000 | 0x20000000

// TransitionAutoSubframe is content loaded into a frame automatically;
// history readers drop it.
const TransitionAutoSubframe uint32 = 0x3 | 0x10000000 | 0x20000000

// Visit is one row pair (urls + visits) for WriteHistory.
type Visit struct {
	URL   string
	Title string
	At    time.Time
	// Transition is the raw page transition; zero means TransitionTyped.
	Transition uint32
}

// webKitOffsetMicros is the Unix epoch in microseconds since 1601-01-01.
const webKitOffsetMicros int64 = 11_644_473_600 * 1_000_000

// WebKitTime converts t to a Chromium timestamp (microseconds since
// 1601-01-01 UTC), truncating below the microsecond.
func WebKitTime(t time.Time) int64 { return t.UnixMicro() + webKitOffsetMicros }

// WriteHistory adds visits to <profileDir>/History, creating the
// database with the real schema when it does not exist yet. Calling it
// again appends, which is how a test simulates browsing between runs.
func WriteHistory(t testing.TB, profileDir string, visits ...Visit) {
	t.Helper()
	path := filepath.Join(profileDir, HistoryFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("chromiumtest: open History: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'visits'`).Scan(&n); err != nil {
		t.Fatalf("chromiumtest: inspect History: %v", err)
	}
	if n == 0 {
		if _, err := db.Exec(HistorySchema); err != nil {
			t.Fatalf("chromiumtest: apply History schema: %v", err)
		}
	}
	for _, v := range visits {
		tr := v.Transition
		if tr == 0 {
			tr = TransitionTyped
		}
		vt := WebKitTime(v.At)
		res, err := db.Exec(`INSERT INTO urls(url, title, last_visit_time) VALUES (?, ?, ?)`, v.URL, v.Title, vt)
		if err != nil {
			t.Fatalf("chromiumtest: insert url: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("chromiumtest: url id: %v", err)
		}
		// Chromium stores the transition as a signed 32-bit integer.
		stored := int64(int32(tr)) // #nosec G115 -- mirrors Chromium's int32 storage
		if _, err := db.Exec(`INSERT INTO visits(url, visit_time, transition) VALUES (?, ?, ?)`,
			id, vt, stored); err != nil {
			t.Fatalf("chromiumtest: insert visit: %v", err)
		}
	}
}
