package banner

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// writeShadow is a small fixture helper — keeps test bodies focused on
// behaviour rather than JSON marshalling boilerplate.
func writeShadow(t *testing.T, path string, st upgrade.Status, modTime time.Time) {
	t.Helper()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

// TestInjectInProgressEmitsBanner: a fresh in_progress shadow produces the
// canonical banner format on stderr.
func TestInjectInProgressEmitsBanner(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	now := time.Date(2026, 5, 7, 13, 42, 32, 0, time.UTC)

	writeShadow(t, shadow, upgrade.Status{
		State:      upgrade.StateInProgress,
		Bucket:     upgrade.BucketReingestSelective,
		Done:       47,
		Total:      120,
		Progress:   0.391,
		EtaSeconds: 32,
		StartedAt:  now.Add(-30 * time.Second),
	}, now)

	var buf bytes.Buffer
	if err := injectFromPath(&buf, shadow, now); err != nil {
		t.Fatalf("inject: %v", err)
	}
	got := buf.String()
	for _, frag := range []string{
		"upgrading reingest_selective",
		"47/120 objects",
		"39%",
		"ETA 32s",
		"ctxt upgrade status",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("banner missing %q; got:\n%s", frag, got)
		}
	}
}

// TestInjectMissingFileNoOp: idle (no shadow file) means no output. Banner
// must NOT noise up every command.
func TestInjectMissingFileNoOp(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "upgrade-state.json")

	var buf bytes.Buffer
	if err := injectFromPath(&buf, missing, time.Now()); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

// TestInjectStaleFileNoOp: a shadow file older than ShadowStaleAfter is
// ignored — the daemon probably died mid-upgrade.
func TestInjectStaleFileNoOp(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	now := time.Date(2026, 5, 7, 13, 0, 0, 0, time.UTC)

	// Mod time = now - (StaleAfter + 1m). Definitely stale.
	staleModTime := now.Add(-(upgrade.ShadowStaleAfter + time.Minute))
	writeShadow(t, shadow, upgrade.Status{
		State:    upgrade.StateInProgress,
		Bucket:   upgrade.BucketReingestSelective,
		Total:    100,
		Done:     50,
		Progress: 0.5,
	}, staleModTime)

	var buf bytes.Buffer
	if err := injectFromPath(&buf, shadow, now); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected stale file → no banner, got %q", buf.String())
	}
}

// TestInjectMalformedFileNoOp: the banner must never break the CLI even
// when the shadow file is corrupted.
func TestInjectMalformedFileNoOp(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	if err := os.WriteFile(shadow, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	var buf bytes.Buffer
	if err := injectFromPath(&buf, shadow, time.Now()); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no banner on malformed input, got %q", buf.String())
	}
}

// TestFormatStates verifies each state renders with the expected sigil and
// keywords. We assert on substrings so future polish (e.g. ANSI colour) does
// not destabilise the test.
func TestFormatStates(t *testing.T) {
	cases := []struct {
		name string
		st   upgrade.Status
		want []string
	}{
		{
			"in_progress",
			upgrade.Status{
				State:      upgrade.StateInProgress,
				Bucket:     upgrade.BucketReindexAuto,
				Done:       3,
				Total:      10,
				Progress:   0.3,
				EtaSeconds: 7,
			},
			[]string{"upgrading reindex_auto", "3/10 objects", "30%", "ETA 7s"},
		},
		{
			"failed",
			upgrade.Status{
				State:     upgrade.StateFailed,
				Bucket:    upgrade.BucketReingestSelective,
				LastError: "disk full",
			},
			[]string{"upgrade failed", "reingest_selective", "disk full"},
		},
		{
			"awaiting_consent",
			upgrade.Status{
				State:  upgrade.StateAwaitingConsent,
				Bucket: upgrade.BucketReingestAll,
			},
			[]string{"awaiting_consent", "reingest_all"},
		},
		{
			"idle_renders_empty",
			upgrade.Status{State: upgrade.StateIdle},
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Format(tc.st)
			if tc.want == nil {
				if out != "" {
					t.Fatalf("idle should render empty, got %q", out)
				}
				return
			}
			for _, frag := range tc.want {
				if !strings.Contains(out, frag) {
					t.Errorf("missing %q in %q", frag, out)
				}
			}
		})
	}
}
