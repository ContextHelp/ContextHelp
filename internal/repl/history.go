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
func HistoryPath() string {
	dir, err := xdg.RawDataDir("contexthelp")
	if err != nil {
		// xdg.RawDataDir only fails if neither $XDG_DATA_HOME nor $HOME is set.
		return filepath.Join("contexthelp", "repl_history")
	}
	return filepath.Join(dir, "repl_history")
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
