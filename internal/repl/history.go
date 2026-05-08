package repl

import (
	"os"
	"path/filepath"

	"github.com/peterh/liner"
	"hop.top/kit/go/core/xdg"
)

// HistoryPath returns the absolute path to the REPL history file.
// Follows XDG Base Directory specification via kit/core/xdg:
//   - $XDG_DATA_HOME/contexthelp/repl_history (when XDG_DATA_HOME is set)
//   - ~/.local/share/contexthelp/repl_history (fallback)
//
// Last-resort fallback (no $HOME, no $XDG_DATA_HOME — only happens in
// hermetic test envs): os.TempDir/contexthelp/repl_history. The caller
// can always trust the returned path to be absolute.
func HistoryPath() string {
	if dir, err := xdg.RawDataDir("contexthelp"); err == nil {
		return filepath.Join(dir, "repl_history")
	}
	return filepath.Join(os.TempDir(), "contexthelp", "repl_history")
}

// LoadHistory reads previously saved lines into l.
// If the history file does not exist, LoadHistory returns nil (not an error).
func LoadHistory(l *liner.State) error {
	path := HistoryPath()
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

// SaveHistory writes the current history from l to the history file.
// It creates the parent directory if needed.
func SaveHistory(l *liner.State) error {
	path := HistoryPath()
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
