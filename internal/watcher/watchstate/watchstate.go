// Package watchstate is ctxt's own record of what `ctxt watch` handed to
// each dpkms instance, and of the running watcher. dpkms never sees it:
// the paths are private to this machine (ADR-077 §7).
//
// # Files
//
// Both live in $XDG_STATE_HOME/ctxt/watch/, one pair per dpkms instance,
// named by the resolver's instance key (the server.urls entry's name,
// else the normalized URL), path-escaped:
//
//	home.state                          dedup records, clipboard hash, last errors
//	home.run                            the running `ctxt watch start`
//	http:%2F%2F127.0.0.1:8080.state     an unnamed endpoint
//
// A state file is versioned JSON:
//
//	{
//	  "version": 1,
//	  "instance": "home",
//	  "clipboard": {"last_hash": "<sha256>", "updated_at": "..."},
//	  "dirs": {
//	    "/Users/me/notes": {
//	      "last_enqueue_at": "...",
//	      "last_error": "enqueue a.md: dpkms at ... unreachable: ...",
//	      "last_error_at": "...",
//	      "files": {
//	        "/Users/me/notes/a.md": {"hash": "<sha256>", "job_id": "...", "object_id": "", "seen_at": "..."}
//	      }
//	    }
//	  }
//	}
//
// Raw content is never stored, only hashes. A missing file is an empty
// state. A file that doesn't parse, has another version, or names
// another instance is ErrCorrupt and is never rewritten: resetting it
// would send every watched file again.
//
// # Writes
//
// One process writes a state file: the `ctxt watch start` holding the
// instance's run lock. It keeps the state in memory and writes it back
// shortly after a change and on Close, so a scan of thousands of files
// rewrites the file a few times, not once per file. Every write holds an
// exclusive flock on "<file>.lock" and replaces the file atomically
// (temp file in the same directory, fsync, rename), as
// internal/ambient/position does. Readers (`ctxt watch status`) take no
// lock. A crash loses at most the last FlushDelay of records; those files
// are enqueued again on the next start, and dpkms's content-hash dedup
// is the safety net.
//
// # Run file
//
// `ctxt watch start` holds an exclusive flock on the run file for its
// whole life and writes its pid, endpoint and directories into it. The
// kernel drops the lock when the process dies, so liveness is "the lock
// is held", never "a process with that pid exists": a reused pid is
// never mistaken for the watcher.
package watchstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"hop.top/kit/go/core/xdg"

	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

const (
	schemaVersion = 1
	dirPerm       = 0o700
	filePerm      = 0o600
)

// FlushDelay is how long after a change the state is written back.
const FlushDelay = 500 * time.Millisecond

// ErrCorrupt marks a state or run file that exists but cannot be trusted.
var ErrCorrupt = errors.New("watch state file is corrupt")

// Dir returns $XDG_STATE_HOME/ctxt/watch.
func Dir() (string, error) {
	dir, err := xdg.RawStateDir("ctxt")
	if err != nil {
		return "", fmt.Errorf("watch state: resolve state dir: %w", err)
	}
	return filepath.Join(dir, "watch"), nil
}

// fileStem turns an instance key into a file name stem: path-escaped, so
// a URL key or a name with a slash stays one file directly in dir.
func fileStem(key string) string { return url.PathEscape(key) }

// StatePath is the state file for instance key under dir.
func StatePath(dir, key string) string { return filepath.Join(dir, fileStem(key)+".state") }

// RunPath is the run file for instance key under dir.
func RunPath(dir, key string) string { return filepath.Join(dir, fileStem(key)+".run") }

// Snapshot is a state file's content.
type Snapshot struct {
	Version   int                  `json:"version"`
	Instance  string               `json:"instance"`
	Clipboard ClipboardState       `json:"clipboard"`
	Dirs      map[string]*DirState `json:"dirs"`
}

// ClipboardState is the clipboard watcher's dedup record.
type ClipboardState struct {
	LastHash  string    `json:"last_hash,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// DirState is one watched directory's records and last outcome.
type DirState struct {
	LastEnqueueAt time.Time              `json:"last_enqueue_at,omitempty"`
	LastError     string                 `json:"last_error,omitempty"`
	LastErrorAt   time.Time              `json:"last_error_at,omitempty"`
	Files         map[string]*FileRecord `json:"files"`
}

// FileRecord is one file's last hand-off.
type FileRecord struct {
	Hash     string    `json:"hash"`
	JobID    string    `json:"job_id"`
	ObjectID string    `json:"object_id,omitempty"`
	SeenAt   time.Time `json:"seen_at"`
}

func emptySnapshot(key string) *Snapshot {
	return &Snapshot{Version: schemaVersion, Instance: key, Dirs: map[string]*DirState{}}
}

// Read loads the state file at path without locking it. A missing file
// is an empty snapshot.
func Read(path string) (*Snapshot, error) {
	return load(path, "")
}

// load reads path; wantKey, when set, must match the file's instance.
func load(path, wantKey string) (*Snapshot, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the state file under the ctxt state dir
	if errors.Is(err, os.ErrNotExist) {
		return emptySnapshot(wantKey), nil
	}
	if err != nil {
		return nil, fmt.Errorf("watch state: read %s: %w", path, err)
	}
	corrupt := func(detail error) error {
		return fmt.Errorf("%w: %s: %w (inspect it, or delete it to send every watched file again)", ErrCorrupt, path, detail)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, corrupt(err)
	}
	if s.Version != schemaVersion {
		return nil, corrupt(fmt.Errorf("version %d, want %d", s.Version, schemaVersion))
	}
	if wantKey != "" && s.Instance != wantKey {
		return nil, corrupt(fmt.Errorf("records instance %q, want %q", s.Instance, wantKey))
	}
	if s.Dirs == nil {
		s.Dirs = map[string]*DirState{}
	}
	for _, d := range s.Dirs {
		if d.Files == nil {
			d.Files = map[string]*FileRecord{}
		}
	}
	return &s, nil
}

// Store is one instance's state, held in memory by the process that owns
// the instance's run lock and written back after changes. It implements
// watcher.FileRecords and watcher.ClipboardDedup, and is safe for
// concurrent use.
type Store struct {
	path string

	mu       sync.Mutex
	snap     *Snapshot
	dirty    bool
	timer    *time.Timer
	flushErr error
	closed   bool
}

var (
	_ watcher.FileRecords    = (*Store)(nil)
	_ watcher.ClipboardDedup = (*Store)(nil)
)

// Open loads instance key's state from path. A corrupt file is
// ErrCorrupt.
func Open(path, key string) (*Store, error) {
	if key == "" {
		return nil, errors.New("watch state: empty instance key")
	}
	snap, err := load(path, key)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, snap: snap}, nil
}

// Path returns the state file path.
func (s *Store) Path() string { return s.path }

func (s *Store) dir(watchID string) *DirState {
	d := s.snap.Dirs[watchID]
	if d == nil {
		d = &DirState{Files: map[string]*FileRecord{}}
		s.snap.Dirs[watchID] = d
	}
	return d
}

// changed marks the state dirty and schedules a write. Callers hold mu.
func (s *Store) changed() {
	s.dirty = true
	if s.timer == nil && !s.closed {
		s.timer = time.AfterFunc(FlushDelay, func() {
			s.mu.Lock()
			s.timer = nil
			s.mu.Unlock()
			_ = s.Flush()
		})
	}
}

// GetFile implements watcher.FileRecords.
func (s *Store) GetFile(_ context.Context, watchID, path string) (watcher.FileRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.snap.Dirs[watchID]
	if d == nil {
		return watcher.FileRecord{}, false, nil
	}
	r := d.Files[path]
	if r == nil {
		return watcher.FileRecord{}, false, nil
	}
	return watcher.FileRecord{Path: path, ContentHash: r.Hash, JobID: r.JobID, ObjectID: r.ObjectID, LastSeen: r.SeenAt}, true, nil
}

// PutFile implements watcher.FileRecords.
func (s *Store) PutFile(_ context.Context, watchID string, rec watcher.FileRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := rec.LastSeen
	if seen.IsZero() {
		seen = time.Now().UTC()
	}
	d := s.dir(watchID)
	d.Files[rec.Path] = &FileRecord{Hash: rec.ContentHash, JobID: rec.JobID, ObjectID: rec.ObjectID, SeenAt: seen}
	d.LastEnqueueAt = seen
	s.changed()
	return nil
}

// DeleteFile implements watcher.FileRecords.
func (s *Store) DeleteFile(_ context.Context, watchID, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.snap.Dirs[watchID]
	if d == nil || d.Files[path] == nil {
		return nil
	}
	delete(d.Files, path)
	s.changed()
	return nil
}

// SetLastError implements watcher.FileRecords.
func (s *Store) SetLastError(_ context.Context, watchID, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dir(watchID)
	d.LastError, d.LastErrorAt = msg, time.Now().UTC()
	s.changed()
	return nil
}

// ClipboardHash implements watcher.ClipboardDedup.
func (s *Store) ClipboardHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap.Clipboard.LastHash
}

// SetClipboardHash implements watcher.ClipboardDedup.
func (s *Store) SetClipboardHash(h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap.Clipboard = ClipboardState{LastHash: h, UpdatedAt: time.Now().UTC()}
	s.changed()
	return nil
}

// Flush writes pending changes now. It returns the error of any earlier
// background write that failed, too.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty {
		data, err := json.MarshalIndent(s.snap, "", "  ")
		if err != nil {
			return fmt.Errorf("watch state: encode: %w", err)
		}
		if err := writeLocked(s.path, append(data, '\n')); err != nil {
			s.flushErr = err
		} else {
			s.dirty = false
		}
	}
	err := s.flushErr
	s.flushErr = nil
	return err
}

// Close writes pending changes and stops background writes.
func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	return s.Flush()
}

// writeLocked replaces path with data under the sidecar flock.
func writeLocked(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("watch state: create %s: %w", dir, err)
	}
	release, err := flock(path+".lock", syscall.LOCK_EX)
	if err != nil {
		return err
	}
	defer release()
	return replaceFile(path, data)
}

// replaceFile writes data to a temp file in path's directory, fsyncs it
// and renames it over path.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("watch state: create temp: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(step string, err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("watch state: %s %s: %w", step, path, err)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		return fail("chmod temp for", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write temp for", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("fsync temp for", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("watch state: close temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("watch state: replace %s: %w", path, err)
	}
	return nil
}

// flock takes an advisory lock of kind on path, blocking, and returns
// the release func. The lock file is never removed (see
// internal/ambient/position).
func flock(path string, kind int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- derived from the state path
	if err != nil {
		return nil, fmt.Errorf("watch state: open lock %s: %w", path, err)
	}
	fd := int(f.Fd()) // #nosec G115 -- fd is a small positive int
	if err := syscall.Flock(fd, kind); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("watch state: flock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
