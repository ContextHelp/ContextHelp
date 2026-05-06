package policy_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
)

// TestMigration_LegacyFileRelocates confirms that when only the
// pre-Phase-2 path ($XDG_CONFIG_HOME/contexthelp/policies.yaml) exists
// and the new path (policy/ctxt.yaml) doesn't, Init relocates the file.
// The old path must be gone after the move; the new path must contain
// the original bytes.
func TestMigration_LegacyFileRelocates(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CTXT_POLICY_FILE", "") // ensure resolution falls through to xdg path

	legacyPath := filepath.Join(xdg, "contexthelp", "policies.yaml")
	newPath := filepath.Join(xdg, "contexthelp", "policy", "ctxt.yaml")

	// Seed the legacy path with a valid (minimal) policy.
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o750))
	body := []byte("policies: []\n")
	require.NoError(t, os.WriteFile(legacyPath, body, 0o600))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err, "Init must succeed when migrating from legacy path")
	t.Cleanup(pol.Close)

	// Legacy path must be gone.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy path %s still exists after migration: stat err=%v", legacyPath, err)
	}
	// New path must contain the original bytes.
	got, err := os.ReadFile(newPath)
	require.NoError(t, err, "new path must exist after migration")
	assert.Equal(t, body, got, "migrated bytes don't match original")
}

// TestMigration_NewPathPreferredOverLegacy confirms that when both
// paths exist, the new path wins and the legacy is left alone (no
// destructive overwrite). This is the expected state after a previous
// migration ran successfully — a stale legacy file shouldn't clobber
// adopter-authored rules at the new path.
func TestMigration_NewPathPreferredOverLegacy(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CTXT_POLICY_FILE", "")

	legacyPath := filepath.Join(xdg, "contexthelp", "policies.yaml")
	newPath := filepath.Join(xdg, "contexthelp", "policy", "ctxt.yaml")

	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Dir(newPath), 0o750))

	legacyBody := []byte("# legacy stale\npolicies: []\n")
	newBody := []byte("# current\npolicies: []\n")
	require.NoError(t, os.WriteFile(legacyPath, legacyBody, 0o600))
	require.NoError(t, os.WriteFile(newPath, newBody, 0o600))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	// Both paths must still exist — no migration ran (new is the source
	// of truth; legacy is left alone for the operator to clean up).
	got, err := os.ReadFile(newPath)
	require.NoError(t, err)
	assert.Equal(t, newBody, got, "new path must be unchanged")
	got, err = os.ReadFile(legacyPath)
	require.NoError(t, err)
	assert.Equal(t, legacyBody, got, "legacy path must be left alone when new path already exists")
}

// TestMigration_NoLegacyNoIssue confirms the fresh-install case:
// neither path exists, so Init seeds the bundled default at the new
// path. No migration runs.
func TestMigration_NoLegacyNoIssue(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CTXT_POLICY_FILE", "")

	newPath := filepath.Join(xdg, "contexthelp", "policy", "ctxt.yaml")

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	// New path created with bundled default.
	got, err := os.ReadFile(newPath)
	require.NoError(t, err, "new path must be seeded on fresh install")
	assert.NotEmpty(t, got)
	assert.Contains(t, string(got), "policies:", "seeded file must be the bundled default")
}
