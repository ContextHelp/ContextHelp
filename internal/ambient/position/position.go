// Package position persists, per dpkms instance and ambient source, the
// last event time already handed off, so an incremental run resumes where
// the previous one stopped. The browser-history source is the first
// consumer; a Key pairs the instance key (the server.urls entry name,
// else the normalized URL) with a source key like "brave:Profile 3".
// Positions are per instance so that switching instances never skips
// history the other instance did not receive.
//
// # Incremental, since and backfill contract
//
// The store cannot enforce this, so every caller must. ctxt capture
// history picks one of three modes from its time flags (see
// internal/timeframe Flags):
//
//   - No time flag: incremental. Start from the saved position (or the
//     initial lookback when there is none), then advance it.
//   - --since alone: capture from --since to now regardless of the saved
//     position, then advance it. Advance is monotonic, so a --since
//     earlier than the position never moves it back.
//   - --until or --range: backfill. The store is neither read nor
//     advanced, so replaying an old window never moves a position and
//     never hides newer, not-yet-emitted history.
//
// Advance a key only after the events up to that time were handed off
// successfully.
//
// # Storage
//
// One JSON file, default $XDG_STATE_HOME/ctxt/ambient/browserhistory.state
// (override: $CTXT_AMBIENT_HISTORY_STATE_FILE), keyed by instance, then
// source:
//
//	{"version":2,"instances":{"home":{"brave:Profile 3":"2026-09-10T12:00:00.123456Z"}}}
//
// Timestamps are UTC with exactly six fractional digits. Stored values
// are truncated to the microsecond (the resolution of Chromium and
// Firefox history), so a sub-microsecond remainder may be re-emitted
// but is never skipped.
//
// A missing file is an empty store. An unreadable, unparseable or
// unknown-version file is ErrCorrupt and is never rewritten: silently
// resetting would re-send the whole history. A version-1 file (positions
// not yet per instance) is ErrUnsupportedVersion: there is no migration,
// the operator deletes it. Reset clears one key.
//
// # Concurrency
//
// Every mutation holds an exclusive advisory flock on the sidecar
// "<file>.lock" for its whole read-modify-write, then replaces the file
// atomically (temp file in the same directory, fsync, rename). Readers
// take no lock: rename guarantees they see either the old or the new
// complete file. The lock is what keeps Advance monotonic across
// processes; whole-file replace alone would let a slower writer roll a
// position back.
package position

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hop.top/kit/go/core/util"
	"hop.top/kit/go/core/xdg"
)

const (
	// DefaultFileName is the state file name under the ambient state dir.
	DefaultFileName = "browserhistory.state"
	// EnvFile overrides the state file path.
	EnvFile = "CTXT_AMBIENT_HISTORY_STATE_FILE"

	schemaVersion = 2
	timeLayout    = "2006-01-02T15:04:05.000000Z07:00"
	dirPerm       = 0o700
	filePerm      = 0o600
)

var (
	// ErrCorrupt marks a state file that exists but cannot be trusted.
	ErrCorrupt = errors.New("position state file is corrupt")
	// ErrUnsupportedVersion marks a state file written in another
	// format version. It is also ErrCorrupt.
	ErrUnsupportedVersion = errors.New("unsupported position file version")
	// ErrEmptyKey rejects a key with an empty instance or source.
	ErrEmptyKey = errors.New("position: empty instance or source key")
	// ErrZeroTime rejects advancing to the zero time.
	ErrZeroTime = errors.New("position: zero time")
)

// DefaultPath resolves the state file path: $CTXT_AMBIENT_HISTORY_STATE_FILE,
// else $XDG_STATE_HOME/ctxt/ambient/browserhistory.state (via kit xdg).
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvFile); p != "" {
		return p, nil
	}
	dir, err := xdg.RawStateDir("ctxt")
	if err != nil {
		return "", fmt.Errorf("position: resolve state dir: %w", err)
	}
	return filepath.Join(dir, "ambient", DefaultFileName), nil
}

// Key names one position: a source on one dpkms instance.
type Key struct {
	// Instance is the dpkms instance key the events were handed to.
	Instance string
	// Source is the ambient source, e.g. "brave:Profile 3".
	Source string
}

func (k Key) valid() bool { return k.Instance != "" && k.Source != "" }

// Store is a file-backed position store. The zero value is unusable; use
// New. A Store holds no open handles and is safe for concurrent use.
type Store struct {
	path string
}

// New returns a Store backed by path. Nothing is created until the first
// mutation.
func New(path string) *Store { return &Store{path: path} }

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Get returns the saved position for key and whether one exists.
func (s *Store) Get(key Key) (time.Time, bool, error) {
	if !key.valid() {
		return time.Time{}, false, ErrEmptyKey
	}
	m, err := s.load()
	if err != nil {
		return time.Time{}, false, err
	}
	t, ok := m[key]
	return t, ok, nil
}

// All returns every saved position.
func (s *Store) All() (map[Key]time.Time, error) {
	return s.load()
}

// Advance moves key's position to t, truncated to the microsecond, and
// returns the position now stored. It never moves a position backwards:
// a t at or before the saved position is a no-op that returns the saved
// value.
func (s *Store) Advance(key Key, t time.Time) (time.Time, error) {
	if !key.valid() {
		return time.Time{}, ErrEmptyKey
	}
	if t.IsZero() {
		return time.Time{}, ErrZeroTime
	}
	t = normalize(t)
	var stored time.Time
	err := s.update(func(m map[Key]time.Time) bool {
		cur, ok := m[key]
		if ok && !t.After(cur) {
			stored = cur
			return false
		}
		m[key] = t
		stored = t
		return true
	})
	return stored, err
}

// Reset forgets key's position, so the next incremental run starts from
// scratch for that source on that instance. Resetting an absent key is a
// no-op.
func (s *Store) Reset(key Key) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.update(func(m map[Key]time.Time) bool {
		if _, ok := m[key]; !ok {
			return false
		}
		delete(m, key)
		return true
	})
}

type fileFormat struct {
	Version int `json:"version"`
	// Instances maps instance key → source key → timestamp.
	Instances map[string]map[string]string `json:"instances"`
}

func normalize(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

func (s *Store) corrupt(detail error) error {
	return fmt.Errorf("%w: %s: %w (inspect it, or delete it to start over)", ErrCorrupt, s.path, detail)
}

// load reads the file; a missing file is an empty map.
func (s *Store) load() (map[Key]time.Time, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[Key]time.Time{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("position: read %s: %w", s.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, s.corrupt(err)
	}
	if f.Version != schemaVersion {
		return nil, s.corrupt(fmt.Errorf("%w: version %d, want %d", ErrUnsupportedVersion, f.Version, schemaVersion))
	}
	out := map[Key]time.Time{}
	for inst, sources := range f.Instances {
		for src, v := range sources {
			t, err := util.ParseStorageTime(v)
			if err != nil {
				return nil, s.corrupt(fmt.Errorf("instance %q key %q: %w", inst, src, err))
			}
			out[Key{Instance: inst, Source: src}] = normalize(t)
		}
	}
	return out, nil
}

// update runs op on the current positions under the file lock and writes
// the result back when op reports a change.
func (s *Store) update(op func(map[Key]time.Time) bool) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("position: create %s: %w", dir, err)
	}
	release, err := lock(s.path + ".lock")
	if err != nil {
		return err
	}
	defer release()

	m, err := s.load()
	if err != nil {
		return err
	}
	if !op(m) {
		return nil
	}
	return s.write(m)
}

// write replaces the file atomically: temp file in the same directory,
// fsync, rename.
func (s *Store) write(m map[Key]time.Time) error {
	f := fileFormat{Version: schemaVersion, Instances: map[string]map[string]string{}}
	for k, t := range m {
		if f.Instances[k.Instance] == nil {
			f.Instances[k.Instance] = map[string]string{}
		}
		f.Instances[k.Instance][k.Source] = t.UTC().Format(timeLayout)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("position: encode: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("position: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("position: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("position: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("position: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("position: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("position: replace %s: %w", s.path, err)
	}
	return nil
}
