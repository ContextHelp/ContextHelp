package s3

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

func TestNew_RequiresBucket(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("New with empty bucket: expected error")
	}
}

func TestKeyForEvent_LayoutMatchesDocs(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:      "clipboard",
		Fingerprint: "fp-abc123",
		OccurredAt:  when,
	}
	got := keyForEvent(ev)
	want := "events/clipboard/2026/05/05/fp-abc123.json"
	if got != want {
		t.Errorf("keyForEvent = %q, want %q", got, want)
	}
}

func TestKeyForEvent_EmptyFingerprintFallsBackToTimestamp(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:     "clipboard",
		OccurredAt: when,
	}
	got := keyForEvent(ev)
	if got == "events/clipboard/2026/05/05/.json" {
		t.Error("empty fingerprint produced empty id segment")
	}
}

func TestStats_ReportsBackendAndCounters(t *testing.T) {
	t.Parallel()
	// Construct directly without going through New (which would try to
	// load AWS credentials).
	b := &Buffer{bucket: "test-bucket", prefix: "ctxt"}
	stats := b.Stats()
	if stats.Backend != "s3" {
		t.Errorf("Backend = %q, want s3", stats.Backend)
	}
	if got := stats.Extra["bucket"]; got != "test-bucket" {
		t.Errorf("Extra[bucket] = %v, want test-bucket", got)
	}
}

// Compile-time check.
var _ ambient.Buffer = (*Buffer)(nil)
