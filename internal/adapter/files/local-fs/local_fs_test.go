package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol and backend identifiers the
// substrate's Registry uses for slot routing.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "files" {
		t.Errorf("Protocol = %q, want files", got)
	}
	if got := a.Backend(); got != "local-fs" {
		t.Errorf("Backend = %q, want local-fs", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms the local-fs sensor is
// capability-honest: only fetch + emit-events. Sensors are passive
// observers per ADR-065 §Amendment 2026-05-06.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract:
// methods for capabilities NOT declared MUST return
// ErrCapabilityNotDeclared.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit on sensor: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve on sensor: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestFetchRecursive walks a temp tree with subdirs and returns one
// Object per file (no bodies; metadata only).
func TestFetchRecursive(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "a.txt"), "alpha")
	mkfile(t, filepath.Join(root, "sub", "b.md"), "bravo")
	mkfile(t, filepath.Join(root, "sub", "deep", "c.log"), "charlie")

	a := New(Config{Roots: []Root{{Path: root, Recursive: true}}})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := len(objs), 3; got != want {
		t.Fatalf("Fetch returned %d objects, want %d (%v)", got, want, paths(objs))
	}
	for _, o := range objs {
		if o.Content != "" {
			t.Errorf("object %s carries body content; sensor should emit metadata only", o.ID)
		}
		if o.Metadata == nil || o.Metadata["path"] == nil {
			t.Errorf("object %s missing metadata.path", o.ID)
		}
	}
}

// TestFetchNonRecursive walks only the immediate directory.
func TestFetchNonRecursive(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "a.txt"), "alpha")
	mkfile(t, filepath.Join(root, "b.txt"), "bravo")
	mkfile(t, filepath.Join(root, "sub", "c.txt"), "charlie")

	a := New(Config{Roots: []Root{{Path: root, Recursive: false}}})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := len(objs), 2; got != want {
		t.Fatalf("Fetch returned %d objects, want %d (%v)", got, want, paths(objs))
	}
}

// TestFetchIgnoreGlobs respects per-root ignore patterns.
func TestFetchIgnoreGlobs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "keep.md"), "keep")
	mkfile(t, filepath.Join(root, ".DS_Store"), "junk")
	mkfile(t, filepath.Join(root, "scratch.tmp"), "junk")

	a := New(Config{Roots: []Root{{Path: root, Recursive: true, Ignore: []string{".DS_Store", "*.tmp"}}}})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, want := len(objs), 1; got != want {
		t.Fatalf("Fetch returned %d objects, want %d (%v)", got, want, paths(objs))
	}
	if filepath.Base(objs[0].Metadata["path"].(string)) != "keep.md" {
		t.Errorf("expected keep.md, got %v", objs[0].Metadata["path"])
	}
}

func mkfile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func paths(objs []ingest.Object) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		if o.Metadata != nil {
			if p, ok := o.Metadata["path"].(string); ok {
				out = append(out, p)
				continue
			}
		}
		out = append(out, o.ID)
	}
	return out
}
