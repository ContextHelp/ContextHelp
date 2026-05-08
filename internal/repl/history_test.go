package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peterh/liner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryPath_DefaultFallback(t *testing.T) {
	// When XDG_DATA_HOME is unset, kit/core/xdg picks the platform-correct
	// default — Linux: ~/.local/share, macOS: ~/Library/Application Support.
	// We assert only the suffix + that it lives under the user's home.
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	got := HistoryPath()
	assert.True(t, strings.HasPrefix(got, home),
		"expected path under home, got %q", got)
	assert.Equal(t, filepath.Join("contexthelp", "repl_history"),
		strings.Join([]string{filepath.Base(filepath.Dir(got)), filepath.Base(got)}, string(filepath.Separator)))
}

func TestHistoryPath_XDGOverride(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdgtest")
	want := "/tmp/xdgtest/contexthelp/repl_history"
	assert.Equal(t, want, HistoryPath())
}

func TestLoadSaveHistory_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	lines := []string{"find auth flow", "list --type url", "show 1"}

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
