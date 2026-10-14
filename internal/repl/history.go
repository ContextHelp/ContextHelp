package repl

import (
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/peterh/liner"
)

// HistoryPath returns the absolute path to the REPL history file:
// <dataDir>/repl_history, where dataDir is cfg.Storage.DataDir
// (CTXT_DATA_DIR, storage.data_dir, else the XDG data dir).
//
// An empty dataDir (no loaded config) falls back to the built-in data
// dir, and, when even that cannot be resolved (no $HOME, no
// $XDG_DATA_HOME), to os.TempDir/contexthelp. The caller can always
// trust the returned path to be absolute.
func HistoryPath(dataDir string) string {
	if dataDir == "" {
		dir, err := config.DefaultDataDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "contexthelp", historyFile)
		}
		dataDir = dir
	}
	return filepath.Join(dataDir, historyFile)
}

const historyFile = "repl_history"

// LoadHistory reads previously saved lines from HistoryPath(dataDir) into l.
// If the history file does not exist, LoadHistory returns nil (not an error).
func LoadHistory(l *liner.State, dataDir string) error {
	path := HistoryPath(dataDir)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	_, err = l.ReadHistory(f)
	return err
}

// SaveHistory writes the current history from l to HistoryPath(dataDir).
// It creates the parent directory if needed.
func SaveHistory(l *liner.State, dataDir string) error {
	path := HistoryPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = l.WriteHistory(f)
	return err
}
