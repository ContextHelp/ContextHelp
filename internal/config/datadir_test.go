package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hermeticDataDir points HOME and every XDG base at a fresh tempdir and
// clears CTXT_DATA_DIR / CTXT_CONFIG, so no test here can resolve the
// developer's real data directory. Returns the tempdir.
func hermeticDataDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CONFIG_DIRS", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdgdata"))
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvDataDir, "")
	withChdir(t, home)
	return root
}

// requireUnder fails unless path sits inside root: the guard that a
// data-dir test never resolved a real path.
func requireUnder(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	require.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel),
		"%q escapes the test tempdir %q", path, root)
}

// writeUserConfig writes $XDG_CONFIG_HOME/contexthelp/<bin>.yaml.
func writeUserConfig(t *testing.T, root, bin, body string) {
	t.Helper()
	dir := filepath.Join(root, "config", "contexthelp")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, bin+".yaml"), []byte(body), 0o600))
}

// CTXT_DATA_DIR names a directory: the database is a file inside it,
// never the directory itself.
func TestLoad_DataDirEnvIsDirectory(t *testing.T) {
	root := hermeticDataDir(t)
	dataDir := filepath.Join(root, "env-data")
	t.Setenv(EnvDataDir, dataDir)

	cfg, err := Load("dpkms", "")
	require.NoError(t, err)

	assert.Equal(t, dataDir, cfg.Storage.DataDir, "storage.data_dir")
	assert.Equal(t, filepath.Join(dataDir, DBFileName), cfg.Storage.Path,
		"storage.path must default to a file inside CTXT_DATA_DIR, not the dir itself")
	assert.Equal(t, filepath.Join(dataDir, BlobDirName), cfg.Storage.Blob.Local.Path,
		"storage.blob.local.path must default inside CTXT_DATA_DIR")
	requireUnder(t, root, cfg.Storage.Path)
}

// With nothing set, the data dir is the XDG data dir and every derived
// default sits under it.
func TestLoad_DataDirDefaultsToXDG(t *testing.T) {
	root := hermeticDataDir(t)
	want := filepath.Join(root, "xdgdata", "contexthelp")

	cfg, err := Load("ctxt", "")
	require.NoError(t, err)

	assert.Equal(t, want, cfg.Storage.DataDir)
	assert.Equal(t, filepath.Join(want, "db.sqlite"), cfg.Storage.Path,
		"default database filename stays db.sqlite")
	assert.Equal(t, filepath.Join(want, "blobs"), cfg.Storage.Blob.Local.Path)
	requireUnder(t, root, cfg.Storage.DataDir)
}

// storage.data_dir in a config file moves every derived default.
func TestLoad_DataDirConfigKey(t *testing.T) {
	root := hermeticDataDir(t)
	dataDir := filepath.Join(root, "file-data")
	writeUserConfig(t, root, "dpkms", "storage:\n  data_dir: "+dataDir+"\n")

	cfg, err := Load("dpkms", "")
	require.NoError(t, err)

	assert.Equal(t, dataDir, cfg.Storage.DataDir)
	assert.Equal(t, filepath.Join(dataDir, DBFileName), cfg.Storage.Path)
	assert.Equal(t, filepath.Join(dataDir, BlobDirName), cfg.Storage.Blob.Local.Path)
}

// Precedence: -c override > env > file > default.
func TestLoad_DataDirPrecedence(t *testing.T) {
	root := hermeticDataDir(t)
	fileDir := filepath.Join(root, "file-data")
	envDir := filepath.Join(root, "env-data")
	overrideDir := filepath.Join(root, "override-data")
	writeUserConfig(t, root, "dpkms", "storage:\n  data_dir: "+fileDir+"\n")

	t.Setenv(EnvDataDir, envDir)
	cfg, err := Load("dpkms", "")
	require.NoError(t, err)
	assert.Equal(t, envDir, cfg.Storage.DataDir, "env beats file")
	assert.Equal(t, filepath.Join(envDir, DBFileName), cfg.Storage.Path)

	overrides := WithOverride(nil, "storage.data_dir", overrideDir)
	cfg, err = LoadWithOverrides("dpkms", "", nil, overrides)
	require.NoError(t, err)
	assert.Equal(t, overrideDir, cfg.Storage.DataDir, "override beats env")
	assert.Equal(t, filepath.Join(overrideDir, DBFileName), cfg.Storage.Path)
}

// An explicit storage.path / storage.blob.local.path is kept as-is; the
// data dir only supplies defaults.
func TestLoad_DataDirKeepsExplicitPaths(t *testing.T) {
	root := hermeticDataDir(t)
	dbPath := filepath.Join(root, "elsewhere", "my.db")
	blobs := filepath.Join(root, "elsewhere", "blobstore")
	writeUserConfig(t, root, "dpkms",
		"storage:\n  path: "+dbPath+"\n  blob:\n    local:\n      path: "+blobs+"\n")
	dataDir := filepath.Join(root, "env-data")
	t.Setenv(EnvDataDir, dataDir)

	cfg, err := Load("dpkms", "")
	require.NoError(t, err)

	assert.Equal(t, dataDir, cfg.Storage.DataDir)
	assert.Equal(t, dbPath, cfg.Storage.Path, "explicit storage.path wins over the data-dir default")
	assert.Equal(t, blobs, cfg.Storage.Blob.Local.Path)
}

// CTXT_DATA_DIR no longer writes storage.path: a file-set storage.path
// survives it.
func TestLoad_DataDirEnvDoesNotBindStoragePath(t *testing.T) {
	for _, b := range envBindings {
		if b.Env == EnvDataDir {
			assert.Equal(t, "storage.data_dir", b.Key, "%s must bind only the data dir", EnvDataDir)
		}
		if b.Key == "storage.path" {
			t.Errorf("storage.path must have no env binding, got %s", b.Env)
		}
	}
}

// RunDir is <data dir>/run, created on demand.
func TestRunDir_UnderDataDir(t *testing.T) {
	root := hermeticDataDir(t)
	dataDir := filepath.Join(root, "d")

	got, err := RunDir(dataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, "run"), got)
	info, err := os.Stat(got)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	state, err := CurrentInstanceFile(dataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, "run", "current-instance"), state)
}

// RunDirPath and CurrentInstanceFile name the run dir and its state
// file without creating anything: read paths (ps, restore, instance
// routing) must leave a data dir as they found it.
func TestRunDirPath_CreatesNothing(t *testing.T) {
	root := hermeticDataDir(t)
	dataDir := filepath.Join(root, "d")

	got, err := RunDirPath(dataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, "run"), got)

	state, err := CurrentInstanceFile(dataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, "run", "current-instance"), state)

	_, err = os.Stat(dataDir)
	assert.True(t, os.IsNotExist(err), "lookup created %s: %v", dataDir, err)
}

// An empty data dir is a caller bug, never a silent fallback to the
// process-wide default.
func TestRunDir_EmptyDataDirErrors(t *testing.T) {
	hermeticDataDir(t)
	_, err := RunDir("")
	require.Error(t, err)
	_, err = RunDirPath("")
	require.Error(t, err)
	require.Error(t, EnsureDataDir(""))
}

// EnsureDataDir creates the directory it is given.
func TestEnsureDataDir_CreatesDir(t *testing.T) {
	root := hermeticDataDir(t)
	dataDir := filepath.Join(root, "made", "here")

	require.NoError(t, EnsureDataDir(dataDir))
	info, err := os.Stat(dataDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// WithOverride layers one dotted key onto a nested override map
// without mutating the caller's map, and wins over an existing value.
func TestWithOverride_MergesNested(t *testing.T) {
	in := map[string]any{
		"storage": map[string]any{"data_dir": "/from/c", "type": "sqlite"},
		"server":  map[string]any{"port": 1},
	}
	out := WithOverride(in, "storage.data_dir", "/from/flag")

	assert.Equal(t, "/from/flag", out["storage"].(map[string]any)["data_dir"])
	assert.Equal(t, "sqlite", out["storage"].(map[string]any)["type"])
	assert.Equal(t, 1, out["server"].(map[string]any)["port"])
	assert.Equal(t, "/from/c", in["storage"].(map[string]any)["data_dir"], "input must not be mutated")
}
