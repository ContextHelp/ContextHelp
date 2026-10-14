package postgres

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"testing"
)

// captureWarnings routes slog to a buffer for the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestWarnUnsnapshottedMigration(t *testing.T) {
	latest := pgMigrations[len(pgMigrations)-1].Version
	t.Run("pending on an existing database warns", func(t *testing.T) {
		buf := captureWarnings(t)
		warnUnsnapshottedMigration(latest-1, latest)
		out := buf.String()
		for _, want := range []string{"level=WARN", "pg_dump",
			"v" + strconv.Itoa(latest-1) + " to v" + strconv.Itoa(latest)} {
			if !strings.Contains(out, want) {
				t.Errorf("warning %q does not contain %q", out, want)
			}
		}
	})
	t.Run("nothing pending is silent", func(t *testing.T) {
		buf := captureWarnings(t)
		warnUnsnapshottedMigration(latest, latest)
		if buf.Len() != 0 {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})
	t.Run("fresh database is silent", func(t *testing.T) {
		buf := captureWarnings(t)
		warnUnsnapshottedMigration(0, latest)
		if buf.Len() != 0 {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})
}
