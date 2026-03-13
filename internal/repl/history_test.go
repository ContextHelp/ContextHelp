package repl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterh/liner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryPath_DefaultFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "contexthelp", "repl_history")
	assert.Equal(t, want, HistoryPath())
}

func TestHistoryPath_XDGOverride(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdgtest")
	want := "/tmp/xdgtest/contexthelp/repl_history"
	assert.Equal(t, want, HistoryPath())
}

func TestLoadSaveHistory_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	lines := []string{"find auth flow", "list --type url", "open 1"}

	l := liner.NewLiner()
	for _, line := range lines {
		l.AppendHistory(line)
	}
	require.NoError(t, SaveHistory(l))
	l.Close()

	l2 := liner.NewLiner()
	require.NoError(t, LoadHistory(l2))
	l2.Close()

	_, err := os.Stat(HistoryPath())
	assert.NoError(t, err)
}
