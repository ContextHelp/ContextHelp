package repl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterh/liner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// The history file lives in the data dir the session's config resolved.
func TestHistoryPath_InDataDir(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, "repl_history"), HistoryPath(dir))
}

// Without a loaded config the built-in (XDG) data dir applies.
func TestHistoryPath_NoDataDirUsesDefault(t *testing.T) {
	xdgData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgData)
	t.Setenv(config.EnvDataDir, "")
	assert.Equal(t, filepath.Join(xdgData, "contexthelp", "repl_history"), HistoryPath(""))
}

// A session whose config carries storage.data_dir (CTXT_DATA_DIR)
// keeps its history there, not in the XDG data dir.
func TestSession_HistoryFollowsConfigDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := NewSession(&config.Config{Storage: config.StorageConfig{DataDir: dataDir}})
	assert.Equal(t, filepath.Join(dataDir, "repl_history"), HistoryPath(s.dataDir()))
	assert.Equal(t, "", NewSession(nil).dataDir())
}

func TestLoadSaveHistory_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	lines := []string{"find auth flow", "list --type url", "show 1"}

	l := liner.NewLiner()
	for _, line := range lines {
		l.AppendHistory(line)
	}
	require.NoError(t, SaveHistory(l, dir))
	l.Close()

	l2 := liner.NewLiner()
	require.NoError(t, LoadHistory(l2, dir))
	l2.Close()

	_, err := os.Stat(filepath.Join(dir, "repl_history"))
	assert.NoError(t, err)
}
