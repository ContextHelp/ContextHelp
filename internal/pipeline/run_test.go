package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// scratchStep creates a scratch dir with a file in it, records the dir,
// and names a metadata key pointing into it.
type scratchStep struct {
	pipeline.BaseContract
	dir *string
	err error
}

func newScratchStep(dir *string, err error) scratchStep {
	return scratchStep{pipeline.NewBaseContract(pipeline.StepContract{Binary: true}), dir, err}
}

func (scratchStep) Name() string { return "scratchy" }
func (s scratchStep) Run(ctx context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	dir, err := pipeline.ScratchDir(ctx, "ctxt-runtest-*", "scratch_file")
	if err != nil {
		return nil, err
	}
	*s.dir = dir
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		return nil, err
	}
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	d.Metadata["scratch_file"] = f
	// A step may write anything into metadata, including a path the runner
	// must never touch; the runner does not read metadata for cleanup.
	d.Metadata["scratch_dirs"] = []string{*s.dir + "-victim"}
	return d, s.err
}

func TestRunStepsRemovesScratchOnSuccessAndFailure(t *testing.T) {
	for name, stepErr := range map[string]error{"success": nil, "failure": errors.New("boom")} {
		t.Run(name, func(t *testing.T) {
			victim := t.TempDir()
			var dir string
			step := newScratchStep(&dir, stepErr)
			draft := &storage.KnowledgeObject{}
			out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{step}}, draft)
			if (err != nil) != (stepErr != nil) {
				t.Fatalf("err = %v", err)
			}
			if _, statErr := os.Stat(dir); statErr == nil {
				t.Errorf("scratch dir %s survived", dir)
			}
			if out != nil {
				if _, ok := out.Metadata["scratch_file"]; ok {
					t.Error("metadata key naming a scratch file survived")
				}
			}
			if _, statErr := os.Stat(victim); statErr != nil {
				t.Errorf("a path named in metadata was removed: %v", statErr)
			}
		})
	}
}

func TestRunStepsNeverRemovesWhatMetadataNames(t *testing.T) {
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var dir string
	step := newScratchStep(&dir, nil)
	draft := &storage.KnowledgeObject{Metadata: map[string]any{"scratch_dirs": []string{victim}}}
	if _, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{step}}, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(victim, "keep")); err != nil {
		t.Errorf("RunSteps removed a directory named in draft metadata: %v", err)
	}
}

func TestScratchDirOutsideARunIsRefused(t *testing.T) {
	if _, err := pipeline.ScratchDir(context.Background(), "x-*"); !errors.Is(err, pipeline.ErrNoScratch) {
		t.Fatalf("err = %v", err)
	}
}

// scratchTestRoot points the per-user cache at a temp dir and returns the
// scratch root path inside it (not yet created).
func scratchTestRoot(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	return filepath.Join(cache, "ctxt", "scratch")
}

func TestScratchRootIsPerUserPrivateAndNotInTemp(t *testing.T) {
	root := scratchTestRoot(t)
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	dir, err := pipeline.ScratchDir(ctx, "x-*")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != root {
		t.Errorf("scratch dir %s is not under %s", dir, root)
	}
	if info, err := os.Lstat(root); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Errorf("root mode: %v, %v", info, err)
	}
}

func TestSweepStaleScratchRemovesOnlyOldScratchDirs(t *testing.T) {
	root := scratchTestRoot(t)
	ctx, scratch := pipeline.WithScratch(context.Background())
	old, err := pipeline.ScratchDir(ctx, "old-*")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := pipeline.ScratchDir(ctx, "fresh-*")
	if err != nil {
		t.Fatal(err)
	}
	defer scratch.Cleanup(nil)
	// A plain file in the root is not a scratch dir either.
	stray := filepath.Join(root, "stray")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{old, stray} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	pipeline.SweepStaleScratch(24 * time.Hour)
	if _, err := os.Stat(old); err == nil {
		t.Error("stale scratch dir survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh scratch dir was swept")
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a non-directory entry was swept")
	}
}

// A symlink planted at the scratch root must not be followed: the sweep
// would delete the target's contents, and a new scratch dir would land in it.
func TestScratchRootSymlinkIsRefused(t *testing.T) {
	root := scratchTestRoot(t)
	victim := t.TempDir()
	keep := filepath.Join(victim, "keep")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, root); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(keep, past, past); err != nil {
		t.Fatal(err)
	}

	pipeline.SweepStaleScratch(time.Hour)
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the sweep followed the symlink and removed %s", keep)
	}
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	if _, err := pipeline.ScratchDir(ctx, "x-*"); err == nil {
		t.Error("ScratchDir used a symlinked root")
	}
	if entries, _ := os.ReadDir(victim); len(entries) != 1 {
		t.Errorf("the symlink target was written to: %v", entries)
	}
}

// A symlink inside the root, even a stale-looking one, is skipped, never
// followed.
func TestSweepSkipsSymlinkedEntries(t *testing.T) {
	scratchTestRoot(t)
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	real, err := pipeline.ScratchDir(ctx, "real-*")
	if err != nil {
		t.Fatal(err)
	}
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(real), "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	// A negative age makes every entry stale.
	pipeline.SweepStaleScratch(-time.Hour)
	if _, err := os.Stat(filepath.Join(victim, "keep")); err != nil {
		t.Error("the sweep followed a symlinked entry")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("the symlink itself was removed; it is not ours to remove")
	}
	if _, err := os.Stat(real); err == nil {
		t.Error("a stale plain directory survived")
	}
}

// A root of ours with looser permissions (a restored backup, a loose
// umask) is tightened to 0700, not refused forever.
func TestScratchRootWithWrongModeIsTightened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory modes are synthesized on windows")
	}
	root := scratchTestRoot(t)
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	old, err := pipeline.ScratchDir(ctx, "x-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.ScratchDir(ctx, "y-*"); err != nil {
		t.Fatalf("ScratchDir on a root with mode 755: %v", err)
	}
	if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("root mode after use: %v, %v", info, err)
	}
	// The sweep tightens it too.
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	pipeline.SweepStaleScratch(-time.Hour)
	if info, _ := os.Stat(root); info.Mode().Perm() != 0o700 {
		t.Errorf("root mode after sweep: %v", info.Mode().Perm())
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the sweep did not run on a tightened root")
	}
}

// A root of ours the owner cannot write to (0500) is tightened to 0700
// too, rather than failing every job with a retried permission error.
func TestScratchRootWithoutOwnerWriteIsTightened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory modes are synthesized on windows")
	}
	root := scratchTestRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	if _, err := pipeline.ScratchDir(ctx, "x-*"); err != nil {
		t.Fatalf("ScratchDir on a root with mode 500: %v", err)
	}
	if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("root mode after use: %v, %v", info, err)
	}
}

// A root that cannot be made safe is a permanent error with a remedy, so
// the job is not retried.
func TestScratchRootThatCannotBeUsedIsPermanent(t *testing.T) {
	root := scratchTestRoot(t)
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, scratch := pipeline.WithScratch(context.Background())
	defer scratch.Cleanup(nil)
	_, err := pipeline.ScratchDir(ctx, "x-*")
	var perm *pipeline.PermanentError
	if !errors.As(err, &perm) || !strings.Contains(err.Error(), "remove it") {
		t.Fatalf("err = %v, want a permanent error with a remedy", err)
	}
}

// Cleanup removes through the opened root: a tracked name swapped for a
// symlink is unlinked, and what it points at is untouched.
func TestCleanupDoesNotFollowASwappedSymlink(t *testing.T) {
	scratchTestRoot(t)
	ctx, scratch := pipeline.WithScratch(context.Background())
	dir, err := pipeline.ScratchDir(ctx, "x-*")
	if err != nil {
		t.Fatal(err)
	}
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, dir); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	scratch.Cleanup(nil)
	if _, err := os.Stat(filepath.Join(victim, "keep")); err != nil {
		t.Errorf("Cleanup followed the swapped symlink: %v", err)
	}
}
