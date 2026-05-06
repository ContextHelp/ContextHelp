// Package mic is the `mic` protocol slot under internal/adapter/. It
// hosts a single canonical backend that records audio from the
// default macOS input device by shelling out to ffmpeg's avfoundation
// driver.
//
// Per ADR-065 §Amendment 2026-05-06 the mic sensor is its own
// per-device slot (separate from clipboard + screen) so ambient
// capture can run all three in parallel without violating
// one-platform-per-protocol.
//
// Phase 2 requires ffmpeg to be installed and on $PATH; a native
// AVFoundation binding (no CGo dependency) is Phase 3 work. The
// ambient.yaml schema documents the ffmpeg requirement.
package mic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Protocol is the slot identity.
const Protocol = "mic"

// Backend is the canonical backend identifier.
const Backend = "mic"

// CapabilityPlatformDarwin signals the OS-platform constraint.
const CapabilityPlatformDarwin adapter.Capability = "platform:darwin"

// DefaultSampleRate is sufficient for downstream transcription
// (per docs/ctxt/ambient.md §mic).
const DefaultSampleRate = 16000

// DefaultMaxWindow is the hard ceiling on a single capture window
// when the operator hasn't set one explicitly.
const DefaultMaxWindow = 5 * time.Minute

// CommandRunner runs an external command and returns combined output.
// Production wires defaultRunner; tests inject a stub.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// Config carries the per-capture knobs from ambient.yaml's `mic.*`
// keys. Zero-value uses 16kHz sample rate, 5-minute max window,
// `ffmpeg` on $PATH.
type Config struct {
	// SampleRate is the recording rate in Hz. Zero = DefaultSampleRate.
	SampleRate int
	// MaxWindow is a hard ceiling on a single capture window. A Fetch
	// call requesting longer is capped at this value. Zero =
	// DefaultMaxWindow.
	MaxWindow time.Duration
	// Binary overrides `ffmpeg` (test/escape hatch).
	Binary string
	// Runner overrides defaultRunner (tests inject a fake).
	Runner CommandRunner
	// OutputDir is where capture files land. Empty = os.TempDir().
	OutputDir string
	// Device is the avfoundation input index (default ":0" = default
	// audio input). avfoundation accepts "<video>:<audio>"; mic-only
	// captures use ":<audio>".
	Device string
}

// fetchOptions are runtime options on a single Fetch call. The
// ambient runner threads them via context (see WithWindow); zero
// value falls back to MaxWindow.
type fetchOptions struct {
	window time.Duration
}

type fetchOptionsKey struct{}

// WithWindow attaches a request to record for d. The adapter caps
// d at Config.MaxWindow.
func WithWindow(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, fetchOptionsKey{}, fetchOptions{window: d})
}

func optsFromCtx(ctx context.Context) fetchOptions {
	if v := ctx.Value(fetchOptionsKey{}); v != nil {
		if o, ok := v.(fetchOptions); ok {
			return o
		}
	}
	return fetchOptions{}
}

// Adapter is the typed mic sensor.
type Adapter struct {
	cfg       Config
	mu        sync.Mutex
	ready     bool
	runner    CommandRunner
	binary    string
	device    string
	rate      int
	maxWindow time.Duration
	tempDir   string
}

// New constructs a mic Adapter.
func New(cfg Config) *Adapter {
	a := &Adapter{cfg: cfg}
	a.runner = cfg.Runner
	if a.runner == nil {
		a.runner = defaultRunner
	}
	a.binary = cfg.Binary
	if a.binary == "" {
		a.binary = "ffmpeg"
	}
	a.device = cfg.Device
	if a.device == "" {
		a.device = ":0"
	}
	a.rate = cfg.SampleRate
	if a.rate == 0 {
		a.rate = DefaultSampleRate
	}
	a.maxWindow = cfg.MaxWindow
	if a.maxWindow == 0 {
		a.maxWindow = DefaultMaxWindow
	}
	a.tempDir = cfg.OutputDir
	if a.tempDir == "" {
		a.tempDir = os.TempDir()
	}
	return a
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns the slot identity.
func (a *Adapter) Protocol() string { return Protocol }

// Backend returns the canonical backend name.
func (a *Adapter) Backend() string { return Backend }

// Capabilities declares fetch + emit-events + platform:darwin.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{
		adapter.CapFetch,
		adapter.CapEmitEvents,
		CapabilityPlatformDarwin,
	}
}

// Start marks the adapter ready. macOS Microphone access
// (tcc:Microphone) is enforced by the OS at first ffmpeg invocation;
// the substrate-level skip-on-permission-gap handling lives upstream.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ready = true
	return nil
}

// Ready reports whether Start has run.
func (a *Adapter) Ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ready
}

// Drain is a no-op (each Fetch is a one-shot ffmpeg invocation).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop clears ready state.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ready = false
	return nil
}

// Fetch records audio for the requested window (or MaxWindow,
// whichever is smaller) and returns one ingest.Object pointing at the
// resulting WAV file. Type = "audio".
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	opts := optsFromCtx(ctx)
	window := opts.window
	if window <= 0 {
		window = a.maxWindow
	}
	if window > a.maxWindow {
		window = a.maxWindow
	}

	now := time.Now().UTC()
	stamp := now.Format("20060102T150405.000")
	outPath := filepath.Join(a.tempDir, "ctxt-mic-"+stamp+".wav")

	args := buildArgs(a.device, a.rate, window, outPath)
	if _, err := a.runner(ctx, a.binary, args...); err != nil {
		return nil, fmt.Errorf("mic: %s: %w", a.binary, err)
	}

	info, statErr := os.Stat(outPath)
	var size int64
	if statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("mic: stat output: %w", statErr)
		}
		// Stub mode: file may not exist; fall through with size=0.
	} else {
		size = info.Size()
	}

	sum := sha256.Sum256([]byte(outPath + now.Format(time.RFC3339Nano)))
	id := "mic:" + hex.EncodeToString(sum[:])[:32]
	obj := ingest.Object{
		ID:      id,
		Type:    "audio",
		Content: outPath,
		Metadata: map[string]any{
			"source":      "mic",
			"path":        outPath,
			"sample_rate": a.rate,
			"window_sec":  window.Seconds(),
			"byte_count":  size,
			"captured":    now.Format(time.RFC3339Nano),
		},
	}
	return []ingest.Object{obj}, nil
}

// buildArgs translates device + rate + window + outPath into ffmpeg
// avfoundation flags. Equivalent to:
//
//	ffmpeg -y -f avfoundation -i :0 -t <secs> -ar 16000 <out>.wav
//
// `-y` overwrites any existing file at outPath (the path is
// timestamped, so collisions are unlikely; the flag prevents ffmpeg
// blocking on a stdin prompt).
func buildArgs(device string, rate int, window time.Duration, outPath string) []string {
	secs := window.Seconds()
	return []string{
		"-y",
		"-f", "avfoundation",
		"-i", device,
		"-t", strconv.FormatFloat(secs, 'f', -1, 64),
		"-ar", strconv.Itoa(rate),
		outPath,
	}
}

// Submit returns ErrCapabilityNotDeclared — mic is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — mic is a sensor, not a
// wire-protocol server.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
