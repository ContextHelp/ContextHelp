package watchstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// RunInfo describes a running `ctxt watch start`. It never holds a token.
type RunInfo struct {
	Version   int       `json:"version"`
	PID       int       `json:"pid"`
	Instance  string    `json:"instance"`
	Endpoint  string    `json:"endpoint"`
	Dirs      []string  `json:"dirs"`
	Clipboard bool      `json:"clipboard"`
	StartedAt time.Time `json:"started_at"`
}

// RunningError is AcquireRun's answer when another process holds the
// instance's run lock.
type RunningError struct {
	Path string
	Info RunInfo
}

func (e *RunningError) Error() string {
	if e.Info.PID > 0 {
		return fmt.Sprintf("a watcher for instance %q is already running (pid %d)", e.Info.Instance, e.Info.PID)
	}
	return fmt.Sprintf("a watcher is already running (run file %s)", e.Path)
}

// Run is a held run lock.
type Run struct {
	path string
	f    *os.File
}

// AcquireRun takes the run lock at path for this process and writes info
// (with this pid) into the file. It fails with *RunningError while
// another process holds it. A run file left by a process that died is
// taken over.
func AcquireRun(path string, info RunInfo) (*Run, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("watch run: create %s: %w", filepath.Dir(path), err)
	}
	for range 5 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePerm) // #nosec G304 -- the run file under the ctxt state dir
		if err != nil {
			return nil, fmt.Errorf("watch run: open %s: %w", path, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { // #nosec G115 -- fd is a small positive int
			_ = f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				held, _, _ := ReadRun(path)
				return nil, &RunningError{Path: path, Info: held}
			}
			return nil, fmt.Errorf("watch run: flock %s: %w", path, err)
		}
		// The previous holder may have removed the file between our open
		// and our lock: then the lock is on an unlinked inode. Retry.
		if !samePath(f, path) {
			_ = f.Close()
			continue
		}
		info.Version, info.PID = schemaVersion, os.Getpid()
		if info.StartedAt.IsZero() {
			info.StartedAt = time.Now().UTC()
		}
		data, err := json.Marshal(info)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("watch run: encode: %w", err)
		}
		if err := writeAll(f, append(data, '\n')); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("watch run: write %s: %w", path, err)
		}
		return &Run{path: path, f: f}, nil
	}
	return nil, fmt.Errorf("watch run: %s keeps changing under the lock", path)
}

func samePath(f *os.File, path string) bool {
	a, err := f.Stat()
	if err != nil {
		return false
	}
	b, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

func writeAll(f *os.File, data []byte) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return err
	}
	return f.Sync()
}

// Release removes the run file and drops the lock.
func (r *Run) Release() error {
	err := os.Remove(r.path)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	_ = syscall.Flock(int(r.f.Fd()), syscall.LOCK_UN) // #nosec G115 -- fd is a small positive int
	if cerr := r.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// ReadRun reads the run file at path and reports whether its watcher is
// alive (some process holds the lock). A missing file is not alive and
// not an error. A file whose lock is held but whose content is not
// written yet reads as alive with an empty RunInfo.
func ReadRun(path string) (RunInfo, bool, error) {
	f, err := os.Open(path) // #nosec G304 -- the run file under the ctxt state dir
	if errors.Is(err, os.ErrNotExist) {
		return RunInfo{}, false, nil
	}
	if err != nil {
		return RunInfo{}, false, fmt.Errorf("watch run: open %s: %w", path, err)
	}
	defer f.Close()

	alive := false
	switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); { // #nosec G115 -- fd is a small positive int
	case err == nil:
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // #nosec G115 -- fd is a small positive int
	case errors.Is(err, syscall.EWOULDBLOCK):
		alive = true
	default:
		return RunInfo{}, false, fmt.Errorf("watch run: flock %s: %w", path, err)
	}

	var info RunInfo
	data, err := os.ReadFile(path) // #nosec G304 -- the run file under the ctxt state dir
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RunInfo{}, alive, fmt.Errorf("watch run: read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &info); err != nil && !alive {
			return RunInfo{}, false, fmt.Errorf("%w: %s: %w", ErrCorrupt, path, err)
		}
	}
	return info, alive, nil
}

// RemoveStale deletes the run file at path when no process holds its
// lock. It reports whether it removed one.
func RemoveStale(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, filePerm) // #nosec G304 -- the run file under the ctxt state dir
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("watch run: open %s: %w", path, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { // #nosec G115 -- fd is a small positive int
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("watch run: flock %s: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }() // #nosec G115 -- fd is a small positive int
	if !samePath(f, path) {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("watch run: remove %s: %w", path, err)
	}
	return true, nil
}
