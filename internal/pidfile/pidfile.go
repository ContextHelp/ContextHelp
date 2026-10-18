// Package pidfile manages per-instance pidfiles for dpkms processes.
//
// Each running dpkms instance writes a JSON pidfile at:
//
//	$XDG_DATA_HOME/contexthelp/run/<port>.pid
//
// On clean shutdown the file is removed. Readers (dpkms ps, shutdown,
// reboot, ctxt) validate each pidfile by checking whether the recorded
// pid is running (kill -0 on Unix, the process handle on Windows) and,
// when the OS reports it, that the process started no later than the
// entry's started_at: a later start means the pid was reused by an
// unrelated process. Stale files (from crashes or reboots) are silently
// removed. At startup, serve also treats an entry recording its own pid
// as stale: it has not registered yet, so the entry is a leftover from
// an earlier process that had the same pid (a container's PID 1).
//
// Write replaces a pidfile atomically, so a reader sees the old file or
// the new one, never a partial write. A file a reader cannot parse is
// skipped, never removed. Write and Scan take the run dir's lock
// (lock.go), so a scan removes only the stale file it judged, never one
// a new instance writes meanwhile.
//
// The run dir (<data dir>/run, mode 0750) is not always one user's: an
// instance started with sudo, or from a --data-dir several users share,
// runs as another user. A pid this user may not signal is therefore a
// running process (alive); a pid the OS reused is told apart by its
// start time (sameProcess).
package pidfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/safefs"
)

// validName matches URI-safe instance names: lowercase alphanumeric and hyphens.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateName reports whether name is a legal instance name.
func ValidateName(name string) bool { return validName.MatchString(name) }

// Info holds the metadata written to a pidfile.
type Info struct {
	PID              int    `json:"pid"`
	Name             string `json:"name"`        // unique instance name (URI-safe slug)
	ConfigPath       string `json:"config_path"` // config file used at startup
	Port             int    `json:"port"`
	GRPCPort         int    `json:"grpc_port"`
	CookieBridgePort int    `json:"cookie_bridge_port"`
	BrowserPort      int    `json:"browser_port,omitempty"`
	DBPath           string `json:"db_path"`
	// Access is the instance's access class (server.access), for
	// discovery only (dpkms ps). The access marker beside the database
	// is what direct opens are gated on, not this field.
	Access    string    `json:"access,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Write creates or replaces the pidfile for the given port in dir. The
// content goes to a temporary file that is renamed over the pidfile, so
// a concurrent Scan reads the old entry or the new one, never a partial
// write it would take for a corrupt file.
func Write(dir string, info Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	release, _ := lockDir(dir, writeWait)
	defer release()
	return safefs.ReplaceFile(dir, fmt.Sprintf("%d.pid", info.Port), bytes.NewReader(data), 0o600)
}

// Remove deletes the pidfile for the given port in dir, under the run
// dir's lock like Write.
func Remove(dir string, port int) error {
	release, _ := lockDir(dir, writeWait)
	defer release()
	return os.Remove(filepath.Join(dir, fmt.Sprintf("%d.pid", port)))
}

// Scan reads all pidfiles in dir, removes stale ones, and returns live entries.
func Scan(dir string) ([]Info, error) { return scan(dir, 0) }

// scan is Scan, also treating entries that record pid self as stale
// (self 0 disables this).
func scan(dir string, self int) ([]Info, error) {
	release, locked := lockDir(dir, scanWait)
	defer release()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	live := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := readEntry(path)
		if err != nil {
			// Unreadable or unparseable: not a file to judge, so not
			// one to remove. The next Write for its port replaces it.
			continue
		}
		if (self > 0 && info.PID == self) || !alive(info.PID) || !sameProcess(info) {
			// Pruned only under the lock: unlocked, a writer may be
			// renaming a live entry over this very path.
			if locked {
				_ = os.Remove(path)
			}
			continue
		}
		live = append(live, info)
	}
	return live, nil
}

// readEntry decodes the pidfile at path.
func readEntry(path string) (Info, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a pidfile in the run dir
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

// FindByName scans dir and returns the live Info whose Name matches, or
// (Info{}, false) if not found. Stale pidfiles are pruned as usual.
func FindByName(dir, name string) (Info, bool, error) {
	infos, err := Scan(dir)
	if err != nil {
		return Info{}, false, err
	}
	for _, info := range infos {
		if info.Name == name {
			return info, true, nil
		}
	}
	return Info{}, false, nil
}

// CheckNameConflict returns an error if any live instance in dir already
// uses name. It is the startup check, run before the caller writes its
// own pidfile, so an entry recording the caller's pid cannot be a running
// instance: it is pruned as stale, letting the caller's write replace it.
func CheckNameConflict(dir, name string) error {
	infos, err := scan(dir, os.Getpid())
	if err != nil {
		return err
	}
	for _, info := range infos {
		if info.Name == name {
			return fmt.Errorf("instance name %q is already in use by a running dpkms process", name)
		}
	}
	return nil
}

// PortFromFilename extracts the port number from a pidfile name like "8080.pid".
func PortFromFilename(name string) (int, bool) {
	base := strings.TrimSuffix(name, ".pid")
	n, err := strconv.Atoi(base)
	return n, err == nil
}
