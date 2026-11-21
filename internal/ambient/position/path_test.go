package position

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// A relative path names the same file after the working directory moves:
// New resolves it once.
func TestNewResolvesRelativePathOnce(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	s := New(filepath.Join("rel", DefaultFileName))
	require.True(t, filepath.IsAbs(s.Path()), "path is absolute: %s", s.Path())

	_, err := s.Advance(key, t0)
	require.NoError(t, err)

	t.Chdir(t.TempDir())
	got, ok, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.Equal(t0))
	_, err = os.Stat(filepath.Join(root, "rel", DefaultFileName))
	assert.NoError(t, err, "state file sits where the relative path pointed at construction")
}

// ".." elements are cleaned away; the store reads and writes the cleaned
// location only.
func TestNewCleansDotDot(t *testing.T) {
	root := t.TempDir()
	s := New(filepath.Join(root, "a", "..", "b", DefaultFileName))
	assert.Equal(t, filepath.Join(root, "b", DefaultFileName), s.Path())

	_, err := s.Advance(key, t0)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "b", DefaultFileName))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "a"))
	assert.True(t, os.IsNotExist(err), "the cleaned-away component is never created")
}

// A symlinked state directory is followed; files land in its target with
// the usual modes.
func TestSymlinkedDir(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))

	s := New(filepath.Join(link, DefaultFileName))
	_, err := s.Advance(key, t0)
	require.NoError(t, err)

	for _, name := range []string{DefaultFileName, DefaultFileName + ".lock"} {
		fi, err := os.Stat(filepath.Join(real, name))
		require.NoError(t, err, name)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), name)
	}
	got, ok, err := New(filepath.Join(real, DefaultFileName)).Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.Equal(t0))
}

// A symlinked state file is read through, and a write replaces the link
// with a regular file, leaving the old target as it was.
func TestSymlinkedStateFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.state")
	_, err := New(target).Advance(key, t0)
	require.NoError(t, err)
	before, err := os.ReadFile(target)
	require.NoError(t, err)

	link := filepath.Join(root, DefaultFileName)
	require.NoError(t, os.Symlink(target, link))
	s := New(link)

	got, ok, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, ok, "read goes through the link")
	assert.True(t, got.Equal(t0))

	t1 := t0.Add(time.Hour)
	_, err = s.Advance(key, t1)
	require.NoError(t, err)

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Zero(t, fi.Mode()&os.ModeSymlink, "the write replaced the link with a regular file")
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	after, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the old target is untouched")
	got, _, err = s.Get(key)
	require.NoError(t, err)
	assert.True(t, got.Equal(t1))
}

// The lock is a ".lock" sibling, kept after release, mode 0600; the
// directory is 0700; no temp file survives a write.
func TestLockSidecarAndModes(t *testing.T) {
	s := tempStore(t)
	_, err := s.Advance(key, t0)
	require.NoError(t, err)

	lockInfo, err := os.Stat(s.Path() + ".lock")
	require.NoError(t, err, "the lock file is never removed")
	assert.Equal(t, os.FileMode(0o600), lockInfo.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(s.Path()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	// Re-taking the lock after release works (it was released).
	release, err := lock(s.Path() + ".lock")
	require.NoError(t, err)
	release()
}
