package chromium

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// A copy with a hot journal is reopened read-write so SQLite can roll it
// back; the connection must still refuse writes.
func TestOpenHistoryCopyAfterRollbackIsQueryOnly(t *testing.T) {
	committed := make([]testRow, 0, 50)
	for i := range 50 {
		committed = append(committed, testRow{fmt.Sprintf("https://example.com/c%02d", i), "", at(time.Duration(i) * time.Second), typed})
	}
	uncommitted := make([]testRow, 0, 200)
	for i := range 200 {
		uncommitted = append(uncommitted, testRow{fmt.Sprintf("https://example.com/u%03d", i), "", at(time.Hour + time.Duration(i)*time.Second), typed})
	}
	dir := newHotJournalProfile(t, committed, uncommitted)
	dbPath, err := copyHistory(dir, t.TempDir())
	if err != nil {
		t.Fatalf("copyHistory: %v", err)
	}

	db, err := openHistoryCopy(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("openHistoryCopy: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM urls`).Scan(&n); err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if n != len(committed) {
		t.Fatalf("copy has %d urls, want the %d committed ones", n, len(committed))
	}
	if _, err := db.Exec(`DELETE FROM urls`); err == nil {
		t.Fatal("write to the reopened copy succeeded, want it refused (query-only)")
	}
}
