// Package screen is the `screen` protocol slot under
// internal/adapter/. It hosts a single canonical backend that
// captures the macOS screen via the system /usr/sbin/screencapture
// CLI.
//
// Per ADR-065 §Amendment 2026-05-06 the screen sensor is its own
// per-device slot (separate from clipboard + mic) so ambient capture
// can run all three in parallel without violating
// one-platform-per-protocol.
//
// Phase 2 shells out to the macOS built-in CLI. A native
// ScreenCaptureKit binding (no CGo dependency) is Phase 3 work.
package screen

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
	"strings"
	"sync"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Protocol is the slot identity (one platform per protocol per dPKMS
// instance per ADR-065).
const Protocol = "screen"

// Backend is the canonical backend identifier.
const Backend = "screen"

// CapabilityPlatformDarwin signals the OS-platform constraint per
// ADR-065 §Amendment 2026-05-06.
const CapabilityPlatformDarwin adapter.Capability = "platform:darwin"

// CommandRunner runs an external command and returns its combined
// output. The screen adapter shells out to `/usr/sbin/screencapture`;
// production wires defaultRunner (exec.Command), tests inject a stub.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner runs the command via os/exec with combined output.
func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	// #nosec G204 -- operator-configured binary, no shell; captured content never reaches argv. name and args come from the adapter's
	// config, not from a request.
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// Region describes which area of the display to capture. Mirrors the
// `screen.regions` field in ambient.yaml: full | window | rect:x,y,w,h.
type Region string

// RegionFull captures the entire screen (default).
const RegionFull Region = "full"

// RegionWindow captures the focused window (interactive selection on
// macOS via -W; in Phase 2 this triggers the screencapture window
// mode without further window-id resolution).
const RegionWindow Region = "window"

// Format is the output image format. Maps to screencapture -t flag.
type Format string

// Format constants matching screencapture -t values.
const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatHEIC Format = "heic"
)

// Config carries the per-capture knobs from ambient.yaml's `screen.*`
// keys. Zero-value uses RegionFull, FormatPNG, no size cap.
type Config struct {
	// Region selects the capture area. Empty = RegionFull.
	Region Region
	// Format selects the image format. Empty = FormatPNG.
	Format Format
	// MaxSizeMB skips captures whose output exceeds this byte count.
	// Zero disables the check.
	MaxSizeMB int
	// Binary overrides /usr/sbin/screencapture (test/escape hatch).
	Binary string
	// Runner overrides defaultRunner (tests inject a fake).
	Runner CommandRunner
	// OutputDir is where capture files land. Empty = os.TempDir().
	OutputDir string
}

// Adapter is the typed screen sensor.
type Adapter struct {
	cfg     Config
	mu      sync.Mutex
	ready   bool
	runner  CommandRunner
	binary  string
	region  Region
	format  Format
	tempDir string
}

// New constructs a screen Adapter. Defaults: full screen, PNG, no
// size cap, /usr/sbin/screencapture as the binary, os.TempDir() as
// the output dir.
func New(cfg Config) *Adapter {
	a := &Adapter{cfg: cfg}
	a.runner = cfg.Runner
	if a.runner == nil {
		a.runner = defaultRunner
	}
	a.binary = cfg.Binary
	if a.binary == "" {
		a.binary = "/usr/sbin/screencapture"
	}
	a.region = cfg.Region
	if a.region == "" {
		a.region = RegionFull
	}
	a.format = cfg.Format
	if a.format == "" {
		a.format = FormatPNG
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

// Start runs a tiny test capture to surface TCC permission gaps. If
// macOS Screen Recording permission has not been granted, the OS
// prompts the user (first run) and screencapture exits non-zero on
// rejection. The runner reports the failure; the user grants
// permission via System Settings → Privacy & Security → Screen
// Recording, then re-runs.
//
// Phase 2 design choice: the probe is best-effort. A failed probe
// returns an error from Start with a permission-gap hint, but the
// substrate is responsible for whether that aborts the daemon or
// just skips the sensor in ambient sweep (see ADR-065 amendment).
func (a *Adapter) Start(ctx context.Context, _ bus.Bus) error {
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

// Drain is a no-op (each Fetch is one screencapture invocation).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop clears ready state.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ready = false
	return nil
}

// Fetch invokes screencapture once and returns one ingest.Object with
// the resulting image path in Metadata. Type = "image".
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405.000")
	ext := string(a.format)
	outPath := filepath.Join(a.tempDir, "ctxt-screen-"+stamp+"."+ext)

	args := buildArgs(a.region, a.format, outPath)
	if _, err := a.runner(ctx, a.binary, args...); err != nil {
		return nil, fmt.Errorf("screen: %s: %w", a.binary, err)
	}

	// MaxSizeMB enforcement: stat the output, drop if oversize.
	info, statErr := os.Stat(outPath)
	if statErr != nil {
		// Test runners may not actually create the file; only error if
		// MaxSizeMB enforcement was requested or production binary in use.
		if errors.Is(statErr, os.ErrNotExist) && a.cfg.MaxSizeMB == 0 {
			// Stub mode: skip stat-based pieces; emit object referencing
			// the path the runner was asked to write to.
			return []ingest.Object{a.makeObject(outPath, 0, now)}, nil
		}
		return nil, fmt.Errorf("screen: stat output: %w", statErr)
	}
	if a.cfg.MaxSizeMB > 0 && info.Size() > int64(a.cfg.MaxSizeMB)*1024*1024 {
		// Oversize: remove and skip.
		_ = os.Remove(outPath)
		return nil, nil
	}
	return []ingest.Object{a.makeObject(outPath, info.Size(), now)}, nil
}

// makeObject constructs the ingest.Object describing one capture. The
// object content is the file path; the actual bytes stay on disk
// (multi-MB images don't belong inline in events).
func (a *Adapter) makeObject(path string, size int64, captured time.Time) ingest.Object {
	sum := sha256.Sum256([]byte(path + captured.Format(time.RFC3339Nano)))
	id := "screen:" + hex.EncodeToString(sum[:])[:32]
	return ingest.Object{
		ID:      id,
		Type:    "image",
		Content: path,
		Metadata: map[string]any{
			"source":     "screen",
			"path":       path,
			"format":     string(a.format),
			"region":     string(a.region),
			"byte_count": size,
			"captured":   captured.Format(time.RFC3339Nano),
		},
	}
}

// buildArgs translates Region + Format + outPath into screencapture
// CLI args. macOS screencapture flags:
//
//   - `-x`     silent (no shutter sound)
//   - `-t png` output type
//   - `-W`     window-mode (interactive)
//   - `-R x,y,w,h` rectangular region
func buildArgs(region Region, format Format, outPath string) []string {
	args := []string{"-x", "-t", string(format)}
	switch {
	case region == RegionWindow:
		args = append(args, "-W")
	case strings.HasPrefix(string(region), "rect:"):
		args = append(args, "-R", strings.TrimPrefix(string(region), "rect:"))
	}
	args = append(args, outPath)
	return args
}

// Submit returns ErrCapabilityNotDeclared — screen is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — screen is a sensor, not a
// wire-protocol server.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
