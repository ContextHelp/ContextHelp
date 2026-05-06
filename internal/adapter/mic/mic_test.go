package mic

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol + backend identifiers.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "mic" {
		t.Errorf("Protocol = %q, want mic", got)
	}
	if got := a.Backend(); got != "mic" {
		t.Errorf("Backend = %q, want mic", got)
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
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil }
	a := New(Config{Runner: runner})
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

// TestFetchInvokesFFmpegWithDefaults confirms ffmpeg avfoundation
// flags are present with the documented defaults.
func TestFetchInvokesFFmpegWithDefaults(t *testing.T) {
	var captured struct {
		name string
		args []string
	}
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		captured.name = name
		captured.args = append([]string(nil), args...)
		return nil, nil
	}
	a := New(Config{Runner: runner})
	objs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Fetch returned %d, want 1", len(objs))
	}
	if captured.name != "ffmpeg" {
		t.Errorf("runner.name = %q, want ffmpeg", captured.name)
	}
	for _, want := range []string{"-f", "avfoundation", "-i", ":0", "-ar"} {
		if !contains(captured.args, want) {
			t.Errorf("args missing %q: got %v", want, captured.args)
		}
	}
	if objs[0].Type != "audio" {
		t.Errorf("Type = %q, want audio", objs[0].Type)
	}
	if objs[0].Metadata["source"] != "mic" {
		t.Errorf("Metadata.source = %v, want mic", objs[0].Metadata["source"])
	}
	if objs[0].Metadata["sample_rate"].(int) != DefaultSampleRate {
		t.Errorf("Metadata.sample_rate = %v, want %d", objs[0].Metadata["sample_rate"], DefaultSampleRate)
	}
}

// TestFetchHonorsCustomSampleRate confirms the -ar flag is set from
// Config.SampleRate.
func TestFetchHonorsCustomSampleRate(t *testing.T) {
	var got []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	}
	a := New(Config{Runner: runner, SampleRate: 44100})
	if _, err := a.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for i, v := range got {
		if v == "-ar" && i+1 < len(got) {
			if got[i+1] != "44100" {
				t.Errorf("-ar = %q, want 44100", got[i+1])
			}
			return
		}
	}
	t.Errorf("no -ar in args: %v", got)
}

// TestFetchEnforcesMaxWindow confirms a window request larger than
// MaxWindow caps at MaxWindow.
func TestFetchEnforcesMaxWindow(t *testing.T) {
	var got []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	}
	a := New(Config{Runner: runner, MaxWindow: 5 * time.Minute})
	ctx := WithWindow(context.Background(), 10*time.Minute)
	objs, err := a.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// -t value should be capped to 300 seconds (5 minutes).
	for i, v := range got {
		if v == "-t" && i+1 < len(got) {
			gotSecs, _ := strconv.ParseFloat(got[i+1], 64)
			if gotSecs != 300 {
				t.Errorf("-t = %s, want 300 (5m cap)", got[i+1])
			}
		}
	}
	if w, ok := objs[0].Metadata["window_sec"].(float64); !ok || w != 300 {
		t.Errorf("Metadata.window_sec = %v, want 300", objs[0].Metadata["window_sec"])
	}
}

// TestFetchUsesRequestedWindowWhenUnderCap confirms the window passes
// through unchanged when below MaxWindow.
func TestFetchUsesRequestedWindowWhenUnderCap(t *testing.T) {
	var got []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	}
	a := New(Config{Runner: runner, MaxWindow: 5 * time.Minute})
	ctx := WithWindow(context.Background(), 30*time.Second)
	if _, err := a.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for i, v := range got {
		if v == "-t" && i+1 < len(got) {
			gotSecs, _ := strconv.ParseFloat(got[i+1], 64)
			if gotSecs != 30 {
				t.Errorf("-t = %s, want 30", got[i+1])
			}
			return
		}
	}
	t.Errorf("no -t in args: %v", got)
}

// TestFetchPropagatesRunnerError confirms ffmpeg failures surface
// (wrapped) — typical TCC permission denial path on first run.
func TestFetchPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("input device not authorized")
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, wantErr
	}
	a := New(Config{Runner: runner})
	_, err := a.Fetch(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("Fetch error = %v, want wrapping %v", err, wantErr)
	}
}

// TestFetchHonorsCustomDevice confirms Config.Device threads into -i.
func TestFetchHonorsCustomDevice(t *testing.T) {
	var got []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return nil, nil
	}
	a := New(Config{Runner: runner, Device: ":1"})
	if _, err := a.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for i, v := range got {
		if v == "-i" && i+1 < len(got) {
			if got[i+1] != ":1" {
				t.Errorf("-i = %q, want :1", got[i+1])
			}
			return
		}
	}
	t.Errorf("no -i in args: %v", got)
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
