package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"hop.top/kit/go/core/xdg"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Scratch tracks the directories steps create for a pipeline run's
// intermediate files. The runner owns it and removes them when the run
// ends. It lives in the run's context, never in draft metadata: a step
// (an installed one included) can rewrite metadata, and the runner must
// never delete a path a step names.
type Scratch struct {
	mu       sync.Mutex
	names    []string // directories directly under the scratch root
	metaKeys []string
}

type scratchCtxKey struct{}

// WithScratch returns ctx carrying a new Scratch, and the Scratch.
func WithScratch(ctx context.Context) (context.Context, *Scratch) {
	s := &Scratch{}
	return context.WithValue(ctx, scratchCtxKey{}, s), s
}

// ErrNoScratch means a step asked for a scratch dir outside a run that
// provides one (see RunSteps and WithScratch).
var ErrNoScratch = errors.New("pipeline: no scratch scope in this context")

// scratchRoot is the per-user directory that holds every scratch dir:
// ctxt/scratch under the user's cache directory ($XDG_CACHE_HOME or
// ~/.cache on Linux, ~/Library/Caches on macOS, %LOCALAPPDATA% on
// Windows). It is never under a shared temp dir, where another user could
// plant it.
func scratchRoot() (string, error) {
	base, err := xdg.CacheDir("ctxt")
	if err != nil {
		return "", fmt.Errorf("scratch root: %w", err)
	}
	return filepath.Join(base, "scratch"), nil
}

// errUnsafeScratchRoot marks a scratch root that is not a private
// directory of ours.
var errUnsafeScratchRoot = errors.New("unsafe scratch root")

// checkRootInfo requires info, the root's own (Lstat) info, to describe a
// plain directory that private accepts: on unix, owned by the current
// user with no group or other access; elsewhere the per-user cache
// directory's ACL is the protection and private accepts everything.
func checkRootInfo(path string, info fs.FileInfo, private func(fs.FileInfo) bool) error {
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s is not a plain directory", errUnsafeScratchRoot, path)
	}
	if !private(info) {
		return fmt.Errorf("%w: %s is not private to this user (mode %o)", errUnsafeScratchRoot, path, info.Mode().Perm())
	}
	return nil
}

// openScratchRoot opens the scratch root as an *os.Root, so every later
// create, list and remove is relative to the directory opened here and
// cannot be redirected by swapping a path component afterwards. With
// create, a missing root is made (Mkdir, not MkdirAll, for the root
// itself: an existing symlink is an error, not followed).
//
// The root must be a plain directory, owned by the current user and
// private. A directory of ours with looser permissions, or without owner
// read, write and search, is tightened to 0700; anything else unsafe is a
// permanent error with a remedy.
func openScratchRoot(create bool) (*os.Root, string, error) {
	path, err := scratchRoot()
	if err != nil {
		return nil, "", err
	}
	if create {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return nil, "", fmt.Errorf("scratch root: %w", err)
			}
			if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return nil, "", fmt.Errorf("scratch root: %w", err)
			}
		}
	}
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if err := checkRootInfo(path, linfo, func(fs.FileInfo) bool { return true }); err != nil {
		return nil, "", Permanent(remedy(err, path))
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, "", fmt.Errorf("scratch root: %w", err)
	}
	// Bind the check to what was opened: a swap between Lstat and open
	// shows up as a different directory. The binding is strict on unix,
	// where SameFile compares device and inode; on Windows it compares the
	// volume and file index, which a replaced directory can reuse, so
	// there the per-user cache ACL is what protects the root.
	oinfo, err := r.Stat(".")
	if err != nil || !os.SameFile(linfo, oinfo) {
		_ = r.Close()
		return nil, "", Permanent(remedy(fmt.Errorf("%w: %s changed while opening", errUnsafeScratchRoot, path), path))
	}
	if !privateToUser(oinfo) && ownedByCurrentUser(oinfo) {
		// Ours but too open (a restored backup, a loose umask): tighten.
		if err := r.Chmod(".", 0o700); err == nil {
			if oinfo, err = r.Stat("."); err != nil {
				_ = r.Close()
				return nil, "", err
			}
		}
	}
	if err := checkRootInfo(path, oinfo, privateToUser); err != nil {
		_ = r.Close()
		return nil, "", Permanent(remedy(err, path))
	}
	return r, path, nil
}

func remedy(err error, path string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%w; remove it or make it a plain directory of yours", err)
	}
	return fmt.Errorf("%w; remove it or make it a directory of yours with mode 700 (chmod 700 %s)", err, path)
}

// SweepStaleScratch removes scratch dirs older than maxAge, best effort:
// leftovers of a run that was killed before it could clean up. Call it at
// start-up. It refuses, with a warning, a root that is not a private
// directory of ours, and skips any entry that is not a plain directory.
func SweepStaleScratch(maxAge time.Duration) {
	r, _, err := openScratchRoot(false)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("pipeline: scratch sweep skipped", "err", err)
		}
		return
	}
	defer func() { _ = r.Close() }()
	d, err := r.Open(".")
	if err != nil {
		return
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := r.Lstat(e.Name())
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = r.RemoveAll(e.Name())
		}
	}
}

// ScratchDir creates a directory under the scratch root that the run
// removes when it ends, and returns its path. metaKeys name draft
// metadata keys that point into it; they are removed from the draft the
// run returns, so no stale path is stored.
func ScratchDir(ctx context.Context, pattern string, metaKeys ...string) (string, error) {
	s, ok := ctx.Value(scratchCtxKey{}).(*Scratch)
	if !ok {
		return "", ErrNoScratch
	}
	r, root, err := openScratchRoot(true)
	if err != nil {
		return "", fmt.Errorf("scratch dir: %w", err)
	}
	defer func() { _ = r.Close() }()
	for range 100 {
		name := scratchName(pattern)
		err := r.Mkdir(name, 0o700)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("scratch dir: %w", err)
		}
		s.mu.Lock()
		s.names = append(s.names, name)
		s.metaKeys = append(s.metaKeys, metaKeys...)
		s.mu.Unlock()
		return filepath.Join(root, name), nil
	}
	return "", errors.New("scratch dir: no free name")
}

// scratchName fills the last "*" of pattern with random hex, like
// os.MkdirTemp; a pattern without one gets the hex appended.
func scratchName(pattern string) string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // never fails (Go 1.24+)
	n := hex.EncodeToString(b[:])
	if i := strings.LastIndex(pattern, "*"); i >= 0 {
		return pattern[:i] + n + pattern[i+1:]
	}
	return pattern + n
}

// Cleanup removes every tracked directory and drops the registered
// metadata keys from draft (which may be nil). Removal goes through the
// scratch root opened and checked again, so it never leaves the root.
func (s *Scratch) Cleanup(draft *storage.KnowledgeObject) {
	s.mu.Lock()
	names, keys := s.names, s.metaKeys
	s.names, s.metaKeys = nil, nil
	s.mu.Unlock()
	if len(names) > 0 {
		if r, _, err := openScratchRoot(false); err == nil {
			for _, n := range names {
				_ = r.RemoveAll(n)
			}
			_ = r.Close()
		} else {
			slog.Warn("pipeline: scratch cleanup skipped", "err", err)
		}
	}
	if draft != nil {
		for _, k := range keys {
			delete(draft.Metadata, k)
		}
	}
}
