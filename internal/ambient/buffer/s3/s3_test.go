package s3

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

func TestNew_RequiresBucket(t *testing.T) {
	t.Parallel()
	// Construct a Buffer struct directly to avoid loading default AWS
	// credentials in test env. New() returns error early when bucket is
	// empty so we test that path explicitly via the public constructor.
	_, err := New(t.Context(), Config{})
	if err == nil {
		t.Fatal("New with empty bucket: expected error")
	}
}

func TestKeyForEvent_LayoutMatchesDocs(t *testing.T) {
	t.Parallel()
	b := &Buffer{bucket: "test-bucket", prefix: ""}
	when := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:      "clipboard",
		Fingerprint: "fp-abc123",
		OccurredAt:  when,
	}
	got := b.keyForEvent(ev)
	want := "events/clipboard/2026/05/05/fp-abc123.json"
	if got != want {
		t.Errorf("keyForEvent = %q, want %q", got, want)
	}
}

func TestKeyForEvent_HonoursPrefix(t *testing.T) {
	t.Parallel()
	b := &Buffer{bucket: "test-bucket", prefix: "ctxt-ambient"}
	when := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:      "clipboard",
		Fingerprint: "fp-1",
		OccurredAt:  when,
	}
	got := b.keyForEvent(ev)
	want := "ctxt-ambient/events/clipboard/2026/05/05/fp-1.json"
	if got != want {
		t.Errorf("keyForEvent with prefix = %q, want %q", got, want)
	}
}

func TestKeyForEvent_EmptyFingerprintFallsBackToTimestamp(t *testing.T) {
	t.Parallel()
	b := &Buffer{bucket: "test-bucket"}
	when := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	ev := ambient.RawEvent{
		Source:     "clipboard",
		OccurredAt: when,
	}
	got := b.keyForEvent(ev)
	// Should not contain "/.json" (i.e. id segment must be non-empty).
	if got == "events/clipboard/2026/05/05/.json" {
		t.Error("empty fingerprint produced empty id segment")
	}
}

func TestEventsPrefix_HonoursOptionalPrefix(t *testing.T) {
	t.Parallel()
	b1 := &Buffer{bucket: "x"}
	if got := b1.eventsPrefix(); got != "events/" {
		t.Errorf("eventsPrefix() = %q, want events/", got)
	}
	b2 := &Buffer{bucket: "x", prefix: "ctxt"}
	if got := b2.eventsPrefix(); got != "ctxt/events/" {
		t.Errorf("eventsPrefix() = %q, want ctxt/events/", got)
	}
}

func TestStats_ReportsBackendAndCounters(t *testing.T) {
	t.Parallel()
	b := &Buffer{bucket: "test-bucket", prefix: "ctxt"}
	stats := b.Stats()
	if stats.Backend != "s3" {
		t.Errorf("Backend = %q, want s3", stats.Backend)
	}
	if got := stats.Extra["bucket"]; got != "test-bucket" {
		t.Errorf("Extra[bucket] = %v, want test-bucket", got)
	}
	if got := stats.Extra["prefix"]; got != "ctxt" {
		t.Errorf("Extra[prefix] = %v, want ctxt", got)
	}
}

// Compile-time check.
var _ ambient.Buffer = (*Buffer)(nil)
