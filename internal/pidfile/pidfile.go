// Package pidfile manages per-instance pidfiles for dpkms processes.
//
// Each running dpkms instance writes a JSON pidfile at:
//
//	$XDG_DATA_HOME/contexthelp/run/<port>.pid
//
// On clean shutdown the file is removed. On startup, dpkms ps validates
// each pidfile by checking whether the recorded pid is alive (kill -0);
// stale files (from crashes or reboots) are silently removed.
package pidfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// validName matches URI-safe instance names: lowercase alphanumeric and hyphens.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateName reports whether name is a legal instance name.
func ValidateName(name string) bool { return validName.MatchString(name) }

// Info holds the metadata written to a pidfile.
type Info struct {
	PID              int       `json:"pid"`
	Name             string    `json:"name"`              // unique instance name (URI-safe slug)
	ConfigPath       string    `json:"config_path"`       // config file used at startup
	Port             int       `json:"port"`
	GRPCPort         int       `json:"grpc_port"`
	CookieBridgePort int       `json:"cookie_bridge_port"`
	BrowserPort      int       `json:"browser_port,omitempty"`
	DBPath           string    `json:"db_path"`
	StartedAt        time.Time `json:"started_at"`
}

// Write creates or overwrites the pidfile for the given port in dir.
func Write(dir string, info Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.pid", info.Port))
	return os.WriteFile(path, data, 0600)
}

// Remove deletes the pidfile for the given port in dir.
func Remove(dir string, port int) error {
	return os.Remove(filepath.Join(dir, fmt.Sprintf("%d.pid", port)))
}

// Scan reads all pidfiles in dir, removes stale ones, and returns live entries.
func Scan(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var live []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pid") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var info Info
		if err := json.Unmarshal(data, &info); err != nil {
			// Corrupt file — remove it.
			os.Remove(path)
			continue
		}
		if !alive(info.PID) {
			os.Remove(path)
			continue
		}
		live = append(live, info)
	}
	return live, nil
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

// CheckNameConflict returns an error if any live instance in dir already uses name.
func CheckNameConflict(dir, name string) error {
	_, found, err := FindByName(dir, name)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("instance name %q is already in use by a running dpkms process", name)
	}
	return nil
}

// alive reports whether the process with the given pid is running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// kill -0: no signal sent, just checks existence.
	return proc.Signal(syscall.Signal(0)) == nil
}

// PortFromFilename extracts the port number from a pidfile name like "8080.pid".
func PortFromFilename(name string) (int, bool) {
	base := strings.TrimSuffix(name, ".pid")
	n, err := strconv.Atoi(base)
	return n, err == nil
}
