package chromium

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// HistoryFile is the per-profile SQLite database holding browsing history.
const HistoryFile = "History"

// historySidecars are the files SQLite may keep next to History. Chromium
// uses a rollback journal (journal_mode=DELETE, exclusive locking, so an
// idle journal persists with a zeroed header); -wal is copied too in case
// a fork switches to WAL.
var historySidecars = []string{"-journal", "-wal"}

// copyAttempts bounds how often the copy is retried while the browser
// writes to History mid-copy.
const copyAttempts = 3

// ErrHistoryBusy reports that History kept changing while it was copied.
var ErrHistoryBusy = errors.New("history database changed during copy")

// HistoryVisit is one visit read from a profile's History database.
type HistoryVisit struct {
	URL   string
	Title string
	// VisitedAt is the visit time in UTC, exact to the microsecond.
	VisitedAt time.Time
	// Transition is the raw page transition (core type + qualifier bits).
	Transition uint32
}

// HistoryVisits reads the visits in profileDir's History database whose
// time is in the half-open range [from, to), oldest first. A zero to means
// no upper bound; a zero from starts at the beginning of history.
//
// Only one visit per user navigation is returned: subframe loads are
// dropped and a redirect chain collapses to its final visit.
//
// limit caps the batch when > 0. Visits sharing the last returned
// visit's time are all included (the batch may exceed limit), so a caller
// paging with from = last.VisitedAt + 1ns never skips a visit.
//
// The live database is never opened: History and its journal are copied
// to a temporary directory, which is removed before returning.
func HistoryVisits(ctx context.Context, profileDir string, from, to time.Time, limit int) ([]HistoryVisit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "ctxt-chromium-history-*")
	if err != nil {
		return nil, fmt.Errorf("history: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	dbPath, err := copyHistory(profileDir, tmp)
	if err != nil {
		return nil, err
	}
	db, err := openHistoryCopy(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	return queryVisits(ctx, db, from, to, limit)
}

// fromWebKit converts a Chromium timestamp (microseconds since
// 1601-01-01 UTC) to a UTC time.
func fromWebKit(us int64) time.Time {
	return time.UnixMicro(us - webKitUnixOffsetMicros).UTC()
}

// toWebKit converts t to a Chromium timestamp, rounding a sub-microsecond
// remainder up: the result is the first representable instant >= t, which
// is what an inclusive lower (or exclusive upper) bound needs.
func toWebKit(t time.Time) int64 {
	us := t.UnixMicro() // floor for all t: Nanosecond() is non-negative
	if t.Nanosecond()%int(time.Microsecond) != 0 {
		us++
	}
	return us + webKitUnixOffsetMicros
}

// webKitUnixOffsetMicros is the Unix epoch in microseconds since
// 1601-01-01 UTC. Computed via Unix time: a time.Duration spanning 1601 to
// today overflows int64 nanoseconds.
const webKitUnixOffsetMicros int64 = 11_644_473_600 * 1_000_000

// afterCopyHook runs between copying and re-checking the source files.
// Tests use it to simulate a browser write during the copy.
var afterCopyHook = func() {}

// setAfterCopyHook installs fn and returns a function restoring the
// previous hook.
func setAfterCopyHook(fn func()) func() {
	prev := afterCopyHook
	afterCopyHook = fn
	return func() { afterCopyHook = prev }
}

// fileSig identifies a file's content version well enough to notice a
// write: size and modification time. A missing file has a zero sig.
type fileSig struct {
	size  int64
	mtime int64
	exist bool
}

func statSigs(base string) ([]fileSig, error) {
	names := append([]string{""}, historySidecars...)
	out := make([]fileSig, len(names))
	for i, suffix := range names {
		fi, err := os.Stat(base + suffix)
		switch {
		case err == nil:
			out[i] = fileSig{size: fi.Size(), mtime: fi.ModTime().UnixNano(), exist: true}
		case errors.Is(err, os.ErrNotExist) && suffix != "":
		default:
			return nil, fmt.Errorf("history: %w", err)
		}
	}
	return out, nil
}

func equalSigs(a, b []fileSig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyHistory copies History and its sidecars from profileDir into dst,
// retrying while the source changes underneath the copy. It returns the
// path of the copied database.
func copyHistory(profileDir, dst string) (string, error) {
	src := filepath.Join(profileDir, HistoryFile)
	out := filepath.Join(dst, HistoryFile)
	for range copyAttempts {
		before, err := statSigs(src)
		if err != nil {
			return "", err
		}
		for i, suffix := range append([]string{""}, historySidecars...) {
			if !before[i].exist {
				_ = os.Remove(out + suffix)
				continue
			}
			if err := copyFileTo(src+suffix, out+suffix); err != nil {
				return "", err
			}
		}
		afterCopyHook()
		after, err := statSigs(src)
		if err != nil {
			return "", err
		}
		if equalSigs(before, after) {
			return out, nil
		}
	}
	return "", fmt.Errorf("history: %s: %w", src, ErrHistoryBusy)
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- path under a resolved browser profile dir
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- path in our temp dir
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("history: copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("history: copy %s: %w", src, err)
	}
	return nil
}

// sqliteURI builds a file: URI for path with the given query.
func sqliteURI(path, query string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths: file:///C:/...
	}
	return (&url.URL{Scheme: "file", Path: p, RawQuery: query}).String()
}

// openHistoryCopy opens the copied database read-only. A copy taken while
// the browser was mid-transaction carries a hot journal; SQLite must roll
// it back before reading, which a read-only connection cannot do
// (SQLITE_READONLY_ROLLBACK). The copy is private, so it is then reopened
// read-write to let SQLite roll back, and kept query-only.
func openHistoryCopy(ctx context.Context, path string) (*sql.DB, error) {
	db, err := openProbe(ctx, sqliteURI(path, "mode=ro"))
	if err == nil {
		return db, nil
	}
	var se *sqlite.Error
	if !errors.As(err, &se) || se.Code() != sqlite3.SQLITE_READONLY_ROLLBACK {
		return nil, fmt.Errorf("history: open copy: %w", err)
	}
	db, err = openProbe(ctx, sqliteURI(path, "mode=rw&_pragma=query_only(1)"))
	if err != nil {
		return nil, fmt.Errorf("history: open copy for rollback: %w", err)
	}
	return db, nil
}

// openProbe opens dsn on a single connection and reads the schema, which
// is where a hot journal surfaces.
func openProbe(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

const visitColumns = `SELECT v.id, COALESCE(u.url, ''), COALESCE(u.title, ''), v.visit_time, v.transition
FROM visits v JOIN urls u ON u.id = v.url
WHERE `

// queryVisits selects kept visits in [from, to), then, when the batch is
// full, the remaining visits tied with the last one's time.
func queryVisits(ctx context.Context, db *sql.DB, from, to time.Time, limit int) ([]HistoryVisit, error) {
	lo := toWebKit(from)
	hi := int64(math.MaxInt64)
	if !to.IsZero() {
		hi = toWebKit(to)
	}
	sqlLimit := -1 // SQLite: no limit
	if limit > 0 {
		sqlLimit = limit
	}
	q := visitColumns + keptVisitSQL +
		" AND v.visit_time >= ? AND v.visit_time < ? ORDER BY v.visit_time, v.id LIMIT ?"
	visits, lastID, err := scanVisits(ctx, db, q, lo, hi, sqlLimit)
	if err != nil || limit <= 0 || len(visits) < limit {
		return visits, err
	}

	lastTime := toWebKit(visits[len(visits)-1].VisitedAt)
	tq := visitColumns + keptVisitSQL +
		" AND v.visit_time = ? AND v.id > ? ORDER BY v.id"
	ties, _, err := scanVisits(ctx, db, tq, lastTime, lastID)
	if err != nil {
		return nil, err
	}
	return append(visits, ties...), nil
}

// scanVisits runs q and returns its visits plus the id of the last one.
func scanVisits(ctx context.Context, db *sql.DB, q string, args ...any) ([]HistoryVisit, int64, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("history: query visits: %w", err)
	}
	defer rows.Close()
	out := make([]HistoryVisit, 0)
	var lastID int64
	for rows.Next() {
		var (
			v     HistoryVisit
			vt    int64
			trans int64
		)
		if err := rows.Scan(&lastID, &v.URL, &v.Title, &vt, &trans); err != nil {
			return nil, 0, fmt.Errorf("history: scan visit: %w", err)
		}
		v.VisitedAt = fromWebKit(vt)
		v.Transition = uint32(trans) // #nosec G115 -- stored as int32; keep the low 32 bits
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("history: read visits: %w", err)
	}
	return out, lastID, nil
}
