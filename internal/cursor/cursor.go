// Package cursor implements named, persistent cursors over ctxt list
// results (US-0409).
//
// A cursor is a per-viewer marker storing last_seen_at, last_object_id,
// and a query snapshot. `ctxt list --cursor <name>` returns only items
// added after the cursor's last_seen_at; `--advance` updates the marker
// to the most-recent returned item after a successful list.
//
// Storage is a single YAML file at $CTXT_CURSOR_FILE (default
// ~/.config/contexthelp/cursors.yaml). Concurrent --advance is safe
// via advisory POSIX flock on a sidecar .lock file plus atomic rename.
package cursor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// SchemaVersion is the on-disk format version; bump on incompatible changes.
	SchemaVersion = 1

	// DefaultFileName is the cursor file name relative to the config dir.
	DefaultFileName = "cursors.yaml"

	// EnvFile overrides the cursor file path.
	EnvFile = "CTXT_CURSOR_FILE"

	dirPerm  = 0o700
	filePerm = 0o600
)

// ErrUnknownCursor signals a name not present in the file.
var ErrUnknownCursor = errors.New("unknown cursor")

// QuerySnapshot captures the filter flags at the time of cursor creation
// or last advance. Drift detection compares current flags to this snapshot.
type QuerySnapshot struct {
	Mention []string `yaml:"mention,omitempty" json:"mention,omitempty"`
	Tag     []string `yaml:"tag,omitempty"     json:"tag,omitempty"`
	Profile string   `yaml:"profile,omitempty" json:"profile,omitempty"`
	Q       string   `yaml:"q,omitempty"       json:"q,omitempty"`
	Type    string   `yaml:"type,omitempty"    json:"type,omitempty"`
	After   string   `yaml:"after,omitempty"   json:"after,omitempty"`
}

// Equal reports whether two snapshots match.
func (q QuerySnapshot) Equal(other QuerySnapshot) bool {
	if q.Profile != other.Profile || q.Q != other.Q ||
		q.Type != other.Type || q.After != other.After {
		return false
	}
	return slicesEqual(q.Mention, other.Mention) && slicesEqual(q.Tag, other.Tag)
}

// Cursor is a single named position marker.
type Cursor struct {
	Name         string        `yaml:"-"                          json:"name"`
	LastSeenAt   time.Time     `yaml:"last_seen_at"               json:"last_seen_at"`
	LastObjectID string        `yaml:"last_object_id,omitempty"   json:"last_object_id,omitempty"`
	Query        QuerySnapshot `yaml:"query,omitempty"            json:"query,omitempty"`
	CreatedAt    time.Time     `yaml:"created_at"                 json:"created_at"`
	UpdatedAt    time.Time     `yaml:"updated_at"                 json:"updated_at"`
}

// File is the on-disk representation; cursors keyed by name.
type File struct {
	SchemaVersion int                `yaml:"schema_version"     json:"schema_version"`
	Cursors       map[string]*Cursor `yaml:"cursors,omitempty"  json:"cursors,omitempty"`
}

// Path returns the resolved cursor file path.
//
// Resolution: $CTXT_CURSOR_FILE > $XDG_CONFIG_HOME/contexthelp/cursors.yaml
// > ~/.config/contexthelp/cursors.yaml.
func Path() (string, error) {
	if p := os.Getenv(EnvFile); p != "" {
		return p, nil
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "contexthelp", DefaultFileName), nil
}

// Load reads the cursor file from path; missing file returns an empty File.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is user-configured
	if errors.Is(err, os.ErrNotExist) {
		return &File{SchemaVersion: SchemaVersion, Cursors: map[string]*Cursor{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cursor file: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse cursor file: %w", err)
	}
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}
	if f.Cursors == nil {
		f.Cursors = map[string]*Cursor{}
	}
	for name, c := range f.Cursors {
		if c != nil {
			c.Name = name
		}
	}
	return &f, nil
}

// Save writes the cursor file atomically with restrictive permissions.
//
// Strategy: create parent dir 0700; write to <path>.<pid>.tmp; fsync;
// rename(2) over the target. Final file mode is 0600.
func Save(path string, f *File) error {
	if f == nil {
		return errors.New("cursor: nil file")
	}
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}
	if f.Cursors == nil {
		f.Cursors = map[string]*Cursor{}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir cursor dir: %w", err)
	}

	out, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal cursor file: %w", err)
	}

	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	tf, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm) // #nosec G304
	if err != nil {
		return fmt.Errorf("create temp cursor file: %w", err)
	}
	if _, err := tf.Write(out); err != nil {
		_ = tf.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write temp cursor file: %w", err)
	}
	if err := tf.Sync(); err != nil {
		_ = tf.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync cursor file: %w", err)
	}
	if err := tf.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp cursor file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename cursor file: %w", err)
	}
	// umask may have widened mode; tighten back.
	if err := os.Chmod(path, filePerm); err != nil {
		return fmt.Errorf("chmod cursor file: %w", err)
	}
	return nil
}

// Manager bundles file path + locking for higher-level cursor ops.
type Manager struct {
	Path string
}

// New creates a Manager bound to the resolved cursor path.
func New() (*Manager, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return &Manager{Path: p}, nil
}

// NewWithPath creates a Manager bound to an explicit path.
func NewWithPath(p string) *Manager { return &Manager{Path: p} }

// List returns all cursors sorted by name (deterministic for tests).
func (m *Manager) List() ([]*Cursor, error) {
	f, err := Load(m.Path)
	if err != nil {
		return nil, err
	}
	out := make([]*Cursor, 0, len(f.Cursors))
	for _, c := range f.Cursors {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get fetches a cursor by name; returns ErrUnknownCursor if absent.
func (m *Manager) Get(name string) (*Cursor, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	f, err := Load(m.Path)
	if err != nil {
		return nil, err
	}
	c, ok := f.Cursors[name]
	if !ok {
		return nil, ErrUnknownCursor
	}
	return c, nil
}

// GetOrInit returns the existing cursor or initializes a fresh one (epoch 0).
// The fresh cursor is NOT persisted by this call; caller advances/saves later.
func (m *Manager) GetOrInit(name string) (*Cursor, bool, error) {
	if err := ValidateName(name); err != nil {
		return nil, false, err
	}
	f, err := Load(m.Path)
	if err != nil {
		return nil, false, err
	}
	if c, ok := f.Cursors[name]; ok {
		return c, false, nil
	}
	now := time.Now().UTC()
	c := &Cursor{
		Name:       name,
		LastSeenAt: time.Time{}, // epoch 0
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return c, true, nil
}

// Delete removes name; returns ErrUnknownCursor if absent.
func (m *Manager) Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	return m.withLock(func(f *File) error {
		if _, ok := f.Cursors[name]; !ok {
			return ErrUnknownCursor
		}
		delete(f.Cursors, name)
		return nil
	})
}

// Reset rewinds a cursor to epoch 0 (creates if missing).
func (m *Manager) Reset(name string) (*Cursor, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	var out *Cursor
	err := m.withLock(func(f *File) error {
		now := time.Now().UTC()
		c, ok := f.Cursors[name]
		if !ok {
			c = &Cursor{Name: name, CreatedAt: now}
			f.Cursors[name] = c
		}
		c.LastSeenAt = time.Time{}
		c.LastObjectID = ""
		c.UpdatedAt = now
		out = c
		return nil
	})
	return out, err
}

// SetTo jumps a cursor to ts (creates if missing).
func (m *Manager) SetTo(name string, ts time.Time) (*Cursor, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	err := m.withLock(func(f *File) error {
		now := time.Now().UTC()
		c, ok := f.Cursors[name]
		if !ok {
			c = &Cursor{Name: name, CreatedAt: now}
			f.Cursors[name] = c
		}
		c.LastSeenAt = ts.UTC()
		c.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m.Get(name)
}

// Advance updates a cursor to (lastSeenAt, lastObjectID) and refreshes the
// query snapshot. Creates the cursor if missing. Atomic under flock.
func (m *Manager) Advance(name string, lastSeenAt time.Time, lastObjectID string, snap QuerySnapshot) (*Cursor, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	var out *Cursor
	err := m.withLock(func(f *File) error {
		now := time.Now().UTC()
		c, ok := f.Cursors[name]
		if !ok {
			c = &Cursor{Name: name, CreatedAt: now}
			f.Cursors[name] = c
		}
		// Only advance forward in time — never regress.
		if lastSeenAt.After(c.LastSeenAt) {
			c.LastSeenAt = lastSeenAt.UTC()
			c.LastObjectID = lastObjectID
		}
		c.Query = snap
		c.UpdatedAt = now
		out = c
		return nil
	})
	return out, err
}

// withLock runs op under an exclusive advisory lock + atomic rewrite.
func (m *Manager) withLock(op func(f *File) error) error {
	if err := os.MkdirAll(filepath.Dir(m.Path), dirPerm); err != nil {
		return fmt.Errorf("mkdir cursor dir: %w", err)
	}
	rel, err := acquireLock(m.Path + ".lock")
	if err != nil {
		return err
	}
	defer rel()

	f, err := Load(m.Path)
	if err != nil {
		return err
	}
	if err := op(f); err != nil {
		return err
	}
	return Save(m.Path, f)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
