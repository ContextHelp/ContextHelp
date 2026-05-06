package watchedfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol and backend identifiers.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "watched-fs" {
		t.Errorf("Protocol = %q, want watched-fs", got)
	}
	if got := a.Backend(); got != "watched-fs" {
		t.Errorf("Backend = %q, want watched-fs", got)
	}
}

// TestAdapterDeclaresFetchOnly: sensor is fetch + emit-events only.
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

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit on sensor: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve on sensor: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestLifecycleStartStop: Start registers a watcher, Stop closes it.
// Ready flips with the lifecycle.
func TestLifecycleStartStop(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Paths: []string{dir}})

	if a.Ready() {
		t.Error("Ready() = true before Start")
	}
	if err := a.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.Ready() {
		t.Error("Ready() = false after Start succeeded")
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if a.Ready() {
		t.Error("Ready() = true after Stop")
	}
}

// TestFetchEmitsCreatedFiles: write a file, Fetch returns one Object.
func TestFetchEmitsCreatedFiles(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Paths: []string{dir}})
	if err := a.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	path := filepath.Join(dir, "alpha.txt")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	objs := waitFetch(t, a, 1)
	if got, want := len(objs), 1; got != want {
		t.Fatalf("Fetch returned %d objects, want %d", got, want)
	}
	if objs[0].Metadata["path"] != path {
		t.Errorf("metadata.path = %v, want %s", objs[0].Metadata["path"], path)
	}
}

// TestFetchEmitsModifiedFiles: subsequent writes after a drained Fetch
// surface again on the next Fetch.
func TestFetchEmitsModifiedFiles(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Paths: []string{dir}})
	if err := a.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	path := filepath.Join(dir, "beta.txt")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitFetch(t, a, 1)

	// modify
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	objs := waitFetch(t, a, 1)
	if len(objs) == 0 {
		t.Fatalf("Fetch returned 0 objects after modify; want >=1")
	}
}

// TestFetchEmptyWhenIdle: with no events, Fetch returns nothing.
func TestFetchEmptyWhenIdle(t *testing.T) {
	dir := t.TempDir()
	a := New(Config{Paths: []string{dir}})
	if err := a.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	// drain any spurious initial events
	_, _ = a.Fetch(context.Background())

	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("Fetch with no changes returned %d objects, want 0", len(objs))
	}
}

// waitFetch polls Fetch up to ~2s waiting for at least min objects to
// surface. fsnotify delivery is asynchronous; tests need a short
// settle window.
func waitFetch(t *testing.T, a *Adapter, min int) []ingest.Object {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var collected []ingest.Object
	for time.Now().Before(deadline) {
		objs, err := a.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		collected = append(collected, objs...)
		if len(collected) >= min {
			return collected
		}
		time.Sleep(50 * time.Millisecond)
	}
	return collected
}
