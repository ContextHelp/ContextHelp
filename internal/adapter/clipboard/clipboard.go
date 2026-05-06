// Package clipboard is the `clipboard` protocol slot under
// internal/adapter/. It hosts a single canonical backend reading the
// system pasteboard.
//
// Per ADR-065 §Amendment 2026-05-06, OS-platform sensors get their
// own per-device protocol slot (one of: clipboard, screen, mic) so
// ambient capture (`ctxt capture --ambient`) can run all three in
// parallel without violating the one-platform-per-protocol invariant.
//
// Phase 2 ships the darwin implementation only. The adapter declares
// `Capability("platform:darwin")` so a future Registry platform-gate
// (Phase 3, when Linux + Windows backends arrive) can reject mismatched
// backends. The substrate doesn't enforce that constraint yet — the
// declaration is a forward-compatible convention.
package clipboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Protocol is the slot identity. The substrate's Registry routes
// registration by this value; per ADR-065 there is one platform per
// protocol per dPKMS instance.
const Protocol = "clipboard"

// Backend is the canonical backend identifier. There is exactly one
// backend per OS-platform sensor slot — the package itself IS the
// backend, named after the slot.
const Backend = "clipboard"

// CapabilityPlatformDarwin is the platform-constraint capability
// string used by OS-platform sensors per ADR-065 §Amendment 2026-05-06.
// Capability is `type Capability string` so arbitrary strings work
// without a typed const.
const CapabilityPlatformDarwin adapter.Capability = "platform:darwin"

// readFunc is the indirection over clipboard.ReadAll so tests can
// inject a deterministic fake instead of touching the real pasteboard.
// Production assigns clipboard.ReadAll directly; tests override.
var readFunc = clipboard.ReadAll

// Adapter is the typed clipboard sensor. Lifecycle methods are no-op:
// the pasteboard has no persistent connection. Each Fetch reads the
// current pasteboard contents; the adapter dedups via SHA-256 of the
// last content seen.
type Adapter struct {
	mu       sync.Mutex
	lastHash string
	ready    bool
}

// New constructs a clipboard Adapter. The adapter is not Ready until
// Start has been called; Fetch before Start returns an empty slice.
func New() *Adapter { return &Adapter{} }

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns the slot identity.
func (a *Adapter) Protocol() string { return Protocol }

// Backend returns the canonical backend name.
func (a *Adapter) Backend() string { return Backend }

// Capabilities declares fetch + emit-events + platform:darwin.
// Clipboard is fetch-only; lifecycle events emit via the Runner.
// platform:darwin signals the OS-platform constraint per ADR-065
// §Amendment 2026-05-06.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{
		adapter.CapFetch,
		adapter.CapEmitEvents,
		CapabilityPlatformDarwin,
	}
}

// Start marks the adapter ready. macOS pasteboard reads do not require
// TCC permission, so there's no permission gate here.
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

// Drain is a no-op (no in-flight pasteboard reads).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop clears ready state.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ready = false
	return nil
}

// Fetch reads the current pasteboard. If the content hash matches the
// previously-fetched value, returns an empty slice (dedup). Otherwise
// returns one ingest.Object with the pasteboard content; Type=text.
func (a *Adapter) Fetch(_ context.Context) ([]ingest.Object, error) {
	content, err := readFunc()
	if err != nil {
		return nil, fmt.Errorf("clipboard: read: %w", err)
	}
	if content == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])

	a.mu.Lock()
	defer a.mu.Unlock()
	if hash == a.lastHash {
		return nil, nil
	}
	a.lastHash = hash

	now := time.Now().UTC().Format(time.RFC3339Nano)
	obj := ingest.Object{
		ID:      "clipboard:" + hash,
		Type:    "text",
		Content: content,
		Metadata: map[string]any{
			"source":     "clipboard",
			"hash":       hash,
			"captured":   now,
			"byte_count": len(content),
		},
	}
	return []ingest.Object{obj}, nil
}

// Submit returns ErrCapabilityNotDeclared — clipboard is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — the clipboard adapter is a
// passive sensor, not a wire-protocol server.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
