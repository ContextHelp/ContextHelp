package config

// T-0482: Load should consult the kit-canonical 4-layer cascade —
// project (walk-up from cwd) → user (~/.config/ctxt/) → system
// (/etc/ctxt/) → defaults — when no explicit cfgFile or
// CTXT_CONFIG env is set.
//
// Before T-0482, only the user XDG layer was consulted, which
// meant tools that drop a .ctxt.yaml at a project root never had
// it picked up. After T-0482, kit's OptionsForToolWithMarkers
// supplies the cascade and Load merges all available layers.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withChdir restores cwd at the end of the test.
func withChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// hermeticHome rewires HOME / XDG_CONFIG_HOME / EnvConfigPath / EnvDataDir
// into a tempdir so the test can't be polluted by the developer's real
// config or data dir.
func hermeticHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CONFIG_DIRS", "")
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvDataDir, filepath.Join(root, "data"))
	return home
}

func TestLoad_PicksUpProjectMarker(t *testing.T) {
	home := hermeticHome(t)
	project := filepath.Join(home, "work", "myproj")
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".contexthelp"), 0o755))

	// Drop a .contexthelp/ctxt.yaml at the project root with a
	// distinguishing value.
	cfg := filepath.Join(project, ".contexthelp", "ctxt.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(`server:
  port: 9876
`), 0o644))

	// Run from a subdirectory; the walk-up should find the marker.
	deep := filepath.Join(project, "internal", "events")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	withChdir(t, deep)

	got, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, 9876, got.Server.Port,
		"Load should walk up from cwd and merge .contexthelp/ctxt.yaml")
}

func TestLoad_NoProjectMarker_FallsBackToDefaults(t *testing.T) {
	home := hermeticHome(t)
	cwd := filepath.Join(home, "work", "myproj")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	withChdir(t, cwd)

	// No .ctxt/, .ctxt.yaml, or ctxt.yaml anywhere in the chain.
	got, err := Load("dpkms", "")
	require.NoError(t, err)
	require.NotNil(t, got)
	// We don't pin a specific default port; just assert Load succeeds and
	// returns a non-zero default for a known field. setDefaults sets server.port.
	assert.NotZero(t, got.Server.Port,
		"absent project marker, Load should still return defaults")
}

// TestLoad_PicksUpUserConfigPerBinary is the T-0576 regression. The
// on-disk layout puts user config under
// $XDG_CONFIG_HOME/contexthelp/<bin>.yaml — the binary name selects
// the file, the project-namespace dir stays shared. T-0482 broke this
// by switching the cascade to look at $XDG_CONFIG_HOME/ctxt/config.yaml
// regardless of binary; existing dpkms users silently fell back to
// hardcoded defaults.
func TestLoad_PicksUpUserConfigPerBinary(t *testing.T) {
	home := hermeticHome(t)
	// CTXT_DATA_DIR is bound to viper's storage.path key, so unset it
	// here — otherwise the env var would shadow the file value the
	// test asserts against.
	t.Setenv(EnvDataDir, "")
	confDir := filepath.Join(home, ".config", "contexthelp")
	require.NoError(t, os.MkdirAll(confDir, 0o755))

	// dpkms.yaml carries a distinguishing value for server.port.
	const dpkmsPort = 7771
	require.NoError(t, os.WriteFile(
		filepath.Join(confDir, "dpkms.yaml"),
		[]byte("server:\n  port: 7771\n"),
		0o644,
	))
	// ctxt.yaml has a different port so we can prove the bin selector
	// picks the right file (no accidental cross-load).
	const ctxtPort = 7772
	require.NoError(t, os.WriteFile(
		filepath.Join(confDir, "ctxt.yaml"),
		[]byte("server:\n  port: 7772\n"),
		0o644,
	))

	// Run from a directory with no project markers so only the user
	// layer fires.
	cwd := filepath.Join(home, "elsewhere")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	withChdir(t, cwd)

	gotDpkms, err := Load("dpkms", "")
	require.NoError(t, err)
	assert.Equal(t, dpkmsPort, gotDpkms.Server.Port,
		`Load("dpkms", "") must read $XDG_CONFIG_HOME/contexthelp/dpkms.yaml`)

	gotCtxt, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, ctxtPort, gotCtxt.Server.Port,
		`Load("ctxt", "") must read $XDG_CONFIG_HOME/contexthelp/ctxt.yaml`)
}
