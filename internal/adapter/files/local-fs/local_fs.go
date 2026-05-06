// Package localfs is the local-fs backend of the files protocol slot.
//
// It is a sensor (per ADR-065 §Amendment 2026-05-06): a passive
// observer that walks one or more local roots on each Fetch and emits
// one ingest.Object per file. File bodies are NOT loaded — sensors
// emit metadata only; downstream pipelines load bodies on demand.
//
// Declared capabilities: fetch + emit-events.
package localfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/files"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Root configures a single directory to scan during Fetch.
//
//   - Path is the absolute directory to walk.
//   - Recursive controls whether subdirectories are traversed.
//   - Ignore is a list of filepath.Match glob patterns evaluated
//     against the file's basename; matching files are skipped.
type Root struct {
	Path      string
	Recursive bool
	Ignore    []string
}

// Config carries the local-fs sensor settings. Zero-value Config is
// valid (Fetch returns no objects); operators populate Roots via
// policy/ambient.yaml.
type Config struct {
	Roots []Root
}

// New constructs a typed local-fs sensor Adapter.
func New(cfg Config) *Adapter {
	return &Adapter{cfg: cfg}
}

// Adapter is the typed local-fs sensor. Fetch is the only meaningful
// method; lifecycle methods are no-op (no persistent connections to
// manage — Fetch walks the trees on demand).
type Adapter struct {
	cfg   Config
	ready bool
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "files" — the slot identity defined in
// internal/adapter/files/slot.go.
func (a *Adapter) Protocol() string { return files.Protocol }

// Backend returns "local-fs" — the canonical backend name surfaced in
// operator UIs (`dpkms adapter list`, `policy/ambient.yaml`'s
// `name: files.local-fs`).
func (a *Adapter) Backend() string { return "local-fs" }

// Capabilities returns the local-fs sensor's declared capabilities:
// fetch (walks configured roots) and emit-events (lifecycle topics on
// the bus, driven by the Runner). Sensors are fetch-only per ADR-065
// §Amendment 2026-05-06.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op for the local-fs sensor — Fetch walks the trees on
// demand, so there's no persistent connection to set up.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error {
	a.ready = true
	return nil
}

// Ready reports whether Start has completed successfully.
func (a *Adapter) Ready() bool { return a.ready }

// Drain is a no-op (no in-flight state).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop transitions the sensor back to not-ready. No resources held.
func (a *Adapter) Stop(_ context.Context) error {
	a.ready = false
	return nil
}

// Fetch walks every configured Root and returns one ingest.Object per
// regular file. File bodies are NOT loaded; downstream pipelines load
// bodies on demand. The Object.Metadata map carries:
//
//   - "path"     — absolute file path
//   - "size"     — file size in bytes (int64)
//   - "modtime"  — modification time (time.Time)
//   - "root"     — the Root.Path under which the file was found
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	var objs []ingest.Object
	for _, root := range a.cfg.Roots {
		if err := ctx.Err(); err != nil {
			return objs, err
		}
		if err := walkRoot(ctx, root, &objs); err != nil {
			return objs, fmt.Errorf("local-fs: walk %s: %w", root.Path, err)
		}
	}
	return objs, nil
}

func walkRoot(ctx context.Context, root Root, objs *[]ingest.Object) error {
	return filepath.WalkDir(root.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if path == root.Path {
				return nil
			}
			if !root.Recursive {
				return filepath.SkipDir
			}
			if matchAny(root.Ignore, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if matchAny(root.Ignore, d.Name()) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		*objs = append(*objs, ingest.Object{
			ID:   path,
			Type: "file",
			Metadata: map[string]any{
				"path":    path,
				"size":    info.Size(),
				"modtime": info.ModTime(),
				"root":    root.Path,
			},
		})
		return nil
	})
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Submit returns ErrCapabilityNotDeclared — local-fs is a sensor
// (fetch-only). Writing back to local filesystem would be a different
// adapter shape (e.g. a sink adapter), not this one.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — local-fs does not expose a
// wire protocol.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
