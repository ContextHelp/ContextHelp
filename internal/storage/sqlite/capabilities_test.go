package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// openProbeDB opens a throwaway database on the canonical build and registers
// a checked close. It registers sqlite-vec exactly as New does, so the probe is
// independent of test order.
func openProbeDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return db
}

// TestProbeCapabilities_Succeeds asserts the canonical build (fts5 tag + CGo)
// satisfies every required capability.
func TestProbeCapabilities_Succeeds(t *testing.T) {
	db := openProbeDB(t)

	if err := probeCapabilities(t.Context(), db); err != nil {
		t.Fatalf("probeCapabilities on canonical build: %v", err)
	}
}

// TestProbeCapabilities_MissingCapability asserts the error shape when a probe
// fails: it wraps the sentinel and names both capability and remedy.
func TestProbeCapabilities_MissingCapability(t *testing.T) {
	db := openProbeDB(t)

	// Stand in for a degraded engine by probing a function that cannot exist.
	missing := capability{
		name:   "phantom",
		probe:  "SELECT phantom_version()",
		remedy: "rebuild with -tags fts5",
	}
	restore := requiredCapabilities
	requiredCapabilities = []capability{missing}
	defer func() { requiredCapabilities = restore }()

	err := probeCapabilities(t.Context(), db)
	if err == nil {
		t.Fatal("expected error for missing capability, got nil")
	}
	if !errors.Is(err, storage.ErrCapabilityMissing) {
		t.Errorf("error does not wrap ErrCapabilityMissing: %v", err)
	}
	for _, want := range []string{missing.name, missing.remedy} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

// TestNew_ProbesBeforeMigrations asserts the probe runs on the real open path.
func TestNew_ProbesBeforeMigrations(t *testing.T) {
	d, err := New(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("New on canonical build: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}
