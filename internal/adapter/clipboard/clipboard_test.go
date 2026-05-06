package clipboard

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol + backend identifiers the
// substrate's Registry uses to enforce one-platform-per-protocol.
func TestAdapterIdentity(t *testing.T) {
	a := New()
	if got := a.Protocol(); got != "clipboard" {
		t.Errorf("Protocol = %q, want clipboard", got)
	}
	if got := a.Backend(); got != "clipboard" {
		t.Errorf("Backend = %q, want clipboard", got)
	}
}

// TestAdapterDeclaresFetchEmitDarwin pins the capability set:
// fetch + emit-events + platform:darwin. Submit + Serve MUST NOT be
// declared on a fetch-only sensor.
func TestAdapterDeclaresFetchEmitDarwin(t *testing.T) {
	a := New()
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	if !adapter.HasCapability(a, CapabilityPlatformDarwin) {
		t.Error("missing platform:darwin Capability")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate
// contract: capability-gated methods for capabilities NOT declared
// MUST return ErrCapabilityNotDeclared via errors.Is.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New()
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestStartReadyStop walks the no-op lifecycle: Start sets Ready,
// Stop clears it. Drain is unconditionally fine.
func TestStartReadyStop(t *testing.T) {
	a := New()
	if a.Ready() {
		t.Fatal("Ready before Start: want false")
	}
	if err := a.Start(context.Background(), bus.New()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.Ready() {
		t.Error("Ready after Start: want true")
	}
	if err := a.Drain(context.Background()); err != nil {
		t.Errorf("Drain: %v", err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if a.Ready() {
		t.Error("Ready after Stop: want false")
	}
}

// TestFetchHappyPath stubs the pasteboard read with a known string and
// asserts a single Object emerges with that content + sha256 ID.
func TestFetchHappyPath(t *testing.T) {
	stubReader(t, "hello pasteboard", nil)
	a := New()
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch returned %d objects, want 1", len(objs))
	}
	if objs[0].Content != "hello pasteboard" {
		t.Errorf("Content = %q, want %q", objs[0].Content, "hello pasteboard")
	}
	if objs[0].Type != "text" {
		t.Errorf("Type = %q, want text", objs[0].Type)
	}
	if objs[0].Metadata["source"] != "clipboard" {
		t.Errorf("Metadata.source = %v, want clipboard", objs[0].Metadata["source"])
	}
}

// TestFetchDedupsIdenticalContent confirms the second Fetch with
// unchanged content returns no objects.
func TestFetchDedupsIdenticalContent(t *testing.T) {
	stubReader(t, "stable content", nil)
	a := New()
	first, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first Fetch returned %d, want 1", len(first))
	}
	second, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second Fetch (unchanged content) returned %d, want 0", len(second))
	}
}

// TestFetchEmitsAfterContentChange confirms the dedup is keyed on
// content: changing the pasteboard yields a fresh object.
func TestFetchEmitsAfterContentChange(t *testing.T) {
	current := "alpha"
	prev := readFunc
	readFunc = func() (string, error) { return current, nil }
	t.Cleanup(func() { readFunc = prev })

	a := New()
	if objs, _ := a.Fetch(context.Background()); len(objs) != 1 {
		t.Fatalf("Fetch alpha returned %d, want 1", len(objs))
	}
	current = "bravo"
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch bravo: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch bravo returned %d, want 1", len(objs))
	}
	if objs[0].Content != "bravo" {
		t.Errorf("Content = %q, want bravo", objs[0].Content)
	}
}

// TestFetchEmptyClipboard confirms an empty pasteboard yields no
// objects (and doesn't poison the dedup cache with the zero-hash).
func TestFetchEmptyClipboard(t *testing.T) {
	stubReader(t, "", nil)
	a := New()
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("Fetch on empty pasteboard returned %d, want 0", len(objs))
	}
}

// TestFetchPropagatesReadError confirms underlying ReadAll errors are
// surfaced (wrapped) — adapter authors don't swallow OS errors.
func TestFetchPropagatesReadError(t *testing.T) {
	wantErr := errors.New("pasteboard unavailable")
	stubReader(t, "", wantErr)
	a := New()
	if _, err := a.Fetch(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("Fetch error = %v, want wrapping %v", err, wantErr)
	}
}

// stubReader replaces readFunc for the duration of t with a closure
// that returns (text, err). Reset on cleanup.
func stubReader(t *testing.T, text string, err error) {
	t.Helper()
	prev := readFunc
	readFunc = func() (string, error) { return text, err }
	t.Cleanup(func() { readFunc = prev })
}
