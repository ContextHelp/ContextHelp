package chromiumtest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

func TestHistorySchemaMatchesDump(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "testdata", "history", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != chromiumtest.HistorySchema {
		t.Fatalf("HistorySchema drifted from testdata/history/schema.sql")
	}
}

func TestWriteHistoryIsReadable(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Microsecond)
	chromiumtest.WriteHistory(t, dir,
		chromiumtest.Visit{URL: "https://example.com/a", Title: "A", At: now.Add(-2 * time.Hour)},
		chromiumtest.Visit{URL: "https://example.com/frame", At: now.Add(-90 * time.Minute), Transition: chromiumtest.TransitionAutoSubframe},
	)
	// A second call appends to the existing database.
	chromiumtest.WriteHistory(t, dir, chromiumtest.Visit{URL: "https://example.com/b", At: now.Add(-time.Hour)})

	got, err := chromium.HistoryVisits(context.Background(), dir, time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].URL != "https://example.com/a" || got[1].URL != "https://example.com/b" {
		t.Fatalf("visits = %+v", got)
	}
	if !got[0].VisitedAt.Equal(now.Add(-2*time.Hour)) || got[0].Title != "A" {
		t.Errorf("first visit = %+v", got[0])
	}
}
