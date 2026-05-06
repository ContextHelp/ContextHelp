package screen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol + backend identifiers.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "screen" {
		t.Errorf("Protocol = %q, want screen", got)
	}
	if got := a.Backend(); got != "screen" {
		t.Errorf("Backend = %q, want screen", got)
	}
}

// TestAdapterDeclaresFetchEmitDarwin pins the capability set:
// fetch + emit-events + platform:darwin. Submit + Serve MUST NOT be
// declared on this fetch-only sensor.
func TestAdapterDeclaresFetchEmitDarwin(t *testing.T) {
	a := New(Config{})
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
// contract.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestStartReadyStop walks the lifecycle.
func TestStartReadyStop(t *testing.T) {
	a := New(Config{Runner: stubRunnerOK(t)})
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

// TestFetchInvokesScreencapture confirms the runner is called with
// the configured binary and the screencapture flags are present.
func TestFetchInvokesScreencapture(t *testing.T) {
	var captured struct {
		name string
		args []string
	}
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		captured.name = name
		captured.args = append([]string(nil), args...)
		return nil, nil
	}
	a := New(Config{Runner: runner, Binary: "/usr/sbin/screencapture"})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch returned %d, want 1", len(objs))
	}
	if captured.name != "/usr/sbin/screencapture" {
		t.Errorf("runner.name = %q, want /usr/sbin/screencapture", captured.name)
	}
	// -x silent + -t png + outpath are required.
	want := []string{"-x", "-t", "png"}
	for _, w := range want {
		if !contains(captured.args, w) {
			t.Errorf("args missing %q: got %v", w, captured.args)
		}
	}
	if objs[0].Type != "image" {
		t.Errorf("Type = %q, want image", objs[0].Type)
	}
	if objs[0].Metadata["source"] != "screen" {
		t.Errorf("Metadata.source = %v, want screen", objs[0].Metadata["source"])
	}
}

// TestFetchHonorsRegionFlags confirms RegionWindow appends -W and
// rect:x,y,w,h appends -R <coords>.
func TestFetchHonorsRegionFlags(t *testing.T) {
	cases := []struct {
		name   string
		region Region
		check  func(args []string) bool
	}{
		{"window", RegionWindow, func(a []string) bool { return contains(a, "-W") }},
		{"rect", "rect:0,0,800,600", func(a []string) bool {
			for i, v := range a {
				if v == "-R" && i+1 < len(a) && a[i+1] == "0,0,800,600" {
					return true
				}
			}
			return false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				got = append([]string(nil), args...)
				return nil, nil
			}
			a := New(Config{Runner: runner, Region: tc.region})
			if _, err := a.Fetch(context.Background()); err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if !tc.check(got) {
				t.Errorf("region %q args = %v", tc.region, got)
			}
		})
	}
}

// TestFetchFormatPropagates confirms the -t flag picks up the
// configured format.
func TestFetchFormatPropagates(t *testing.T) {
	var got []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	}
	a := New(Config{Runner: runner, Format: FormatJPEG})
	if _, err := a.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for i, v := range got {
		if v == "-t" && i+1 < len(got) {
			if got[i+1] != "jpeg" {
				t.Errorf("-t = %q, want jpeg", got[i+1])
			}
			return
		}
	}
	t.Errorf("no -t in args: %v", got)
}

// TestFetchPropagatesRunnerError confirms screencapture failures
// surface (wrapped) — typical TCC permission denial path.
func TestFetchPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("permission denied")
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, wantErr
	}
	a := New(Config{Runner: runner})
	_, err := a.Fetch(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("Fetch error = %v, want wrapping %v", err, wantErr)
	}
}

// TestFetchEnforcesMaxSizeMB stubs the runner to write a 2 MiB file
// and configures MaxSizeMB=1 — the fetch returns no objects (skip).
func TestFetchEnforcesMaxSizeMB(t *testing.T) {
	dir := t.TempDir()
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// Last arg is the output path.
		out := args[len(args)-1]
		// Write 2 MiB of zeros.
		blob := make([]byte, 2*1024*1024)
		return nil, os.WriteFile(out, blob, 0o600)
	}
	a := New(Config{Runner: runner, MaxSizeMB: 1, OutputDir: dir})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("oversize capture: got %d objects, want 0 (skipped)", len(objs))
	}
	// File should have been removed.
	matches, _ := filepath.Glob(filepath.Join(dir, "ctxt-screen-*"))
	if len(matches) != 0 {
		t.Errorf("oversize capture left files: %v", matches)
	}
}

// TestFetchOutputDirHonored verifies the path the runner is asked to
// write to lives under cfg.OutputDir.
func TestFetchOutputDirHonored(t *testing.T) {
	dir := t.TempDir()
	var outArg string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outArg = args[len(args)-1]
		return nil, nil
	}
	a := New(Config{Runner: runner, OutputDir: dir})
	if _, err := a.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.HasPrefix(outArg, dir) {
		t.Errorf("output path %q not under %q", outArg, dir)
	}
}

// stubRunnerOK returns a runner that succeeds and creates a small
// file at the requested output path so size-stat works.
func stubRunnerOK(t *testing.T) CommandRunner {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		return nil, os.WriteFile(out, []byte("png"), 0o600)
	}
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
