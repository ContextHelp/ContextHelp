// Package accessmarker records an instance's access class (server.access)
// in a "<dbpath>.access" sidecar beside its SQLite database, where any
// process can read it with the daemon down.
//
// dpkms serve writes the marker for a protected or public instance before
// it opens the database, and never removes it on shutdown. A process
// that would open the database directly (a CLI command, housekeeping, a
// local federation push) reads the marker first and refuses when the
// class is not private: the daemon's API, with its authentication and
// policy, is the only way into such an instance. The marker lives
// outside the database on purpose: reading a row would mean opening the
// database, the very act being gated.
//
// No marker means the instance has never been served protected or
// public: it is private. A marker that cannot be read or parsed refuses
// (fail closed). Only `dpkms instance set-access` lowers a recorded
// class.
//
// The marker is cooperative enforcement: it stops well-behaved tools,
// not a process that opens the file itself. File modes are the
// mandatory tier (see CheckPermissions).
package accessmarker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/safefs"
)

// suffix is appended to the database path to form the marker path.
const suffix = ".access"

// filePerm restricts the marker to the owning user, as the database.
const filePerm = 0o600

// Marker is the sidecar body.
type Marker struct {
	// Access is the instance's access class: private, protected or public.
	Access string `json:"access"`
	// Instance is the instance name the daemon serves the database as.
	Instance string `json:"instance,omitempty"`
	// DaemonAddr is the HTTP address the daemon last listened on; empty
	// until it has bound its listener.
	DaemonAddr string `json:"daemon_addr,omitempty"`
	// UpdatedAt is when the marker was last written.
	UpdatedAt time.Time `json:"updated_at"`
}

// Private reports whether m allows direct opens.
func (m Marker) Private() bool { return m.Access == config.AccessPrivate }

// Path returns the marker path for dbPath, beside its canonical
// spelling (dblock.Canonical): every spelling of one database file,
// symlinks included, reads the same marker.
func Path(dbPath string) string { return dblock.Canonical(dbPath) + suffix }

// validAccess reports whether a is one of the three access classes.
func validAccess(a string) bool {
	switch a {
	case config.AccessPrivate, config.AccessProtected, config.AccessPublic:
		return true
	}
	return false
}

// Write atomically replaces dbPath's marker with m (temporary file,
// fsync, rename, directory fsync), so a reader sees the old marker or
// the new one, never a torn one. UpdatedAt defaults to now.
func Write(dbPath string, m Marker) error {
	if !validAccess(m.Access) {
		return fmt.Errorf("access marker: invalid access class %q", m.Access)
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("access marker: %w", err)
	}
	p := Path(dbPath)
	if err := safefs.ReplaceFile(filepath.Dir(p), filepath.Base(p), bytes.NewReader(data), filePerm); err != nil {
		return fmt.Errorf("access marker: write %s: %w", p, err)
	}
	return nil
}

// Read returns dbPath's marker. found is false when there is none, and
// the marker is then private. A marker that cannot be read, does not
// parse or names no valid class is an error: callers refuse on it.
func Read(dbPath string) (m Marker, found bool, err error) {
	p := Path(dbPath)
	data, err := os.ReadFile(p) // #nosec G304 -- caller-supplied DB path
	if errors.Is(err, fs.ErrNotExist) {
		return Marker{Access: config.AccessPrivate}, false, nil
	}
	if err != nil {
		return Marker{}, false, fmt.Errorf("access marker: read %s: %w", p, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, false, fmt.Errorf("access marker: parse %s: %w", p, err)
	}
	if !validAccess(m.Access) {
		return Marker{}, false, fmt.Errorf("access marker: %s names invalid access class %q", p, m.Access)
	}
	return m, true, nil
}

// ErrRefused is the sentinel every posture refusal wraps.
var ErrRefused = errors.New("direct database access refused by access class")

// RefusedError reports a direct open refused because the instance is
// not private. It wraps ErrRefused.
type RefusedError struct {
	// DBPath is the database whose open was refused.
	DBPath string
	// Marker is the marker that refused it; Access is empty when the
	// marker could not be read (Cause says why).
	Marker Marker
	// Cause is the read error of an unreadable marker.
	Cause error
}

// Error names the instance and its class.
func (e *RefusedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("database %s: unreadable access marker, direct database access refused: %v", e.DBPath, e.Cause)
	}
	name := e.Marker.Instance
	if name == "" {
		name = e.DBPath
	}
	return fmt.Sprintf("instance %q is access-controlled (%s): direct database access is disabled (%s)",
		name, e.Marker.Access, e.DBPath)
}

// Unwrap makes errors.Is(err, ErrRefused) true, and keeps the read cause
// reachable.
func (e *RefusedError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrRefused, e.Cause}
	}
	return []error{ErrRefused}
}

// Fix states what the operator can do instead.
func (e *RefusedError) Fix() string {
	daemon := "use the daemon API"
	if e.Marker.DaemonAddr != "" {
		daemon += " (daemon last listened on " + e.Marker.DaemonAddr + ")"
	}
	return daemon + "; daemon not running? start it with `dpkms serve`; " +
		"maintenance override: `dpkms instance set-access private` with the daemon stopped"
}

// Gate refuses a direct open of dbPath unless its marker is private or
// absent. The refusal is a *RefusedError; so is an unreadable marker.
func Gate(dbPath string) error {
	m, _, err := Read(dbPath)
	if err != nil {
		return &RefusedError{DBPath: dbPath, Cause: err}
	}
	if !m.Private() {
		return &RefusedError{DBPath: dbPath, Marker: m}
	}
	return nil
}
