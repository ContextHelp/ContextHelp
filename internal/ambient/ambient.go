// Package ambient is the local-side ambient capture substrate per ADR-066.
//
// Ambient sources (clipboard, file-watch, browser-history, foreground-window,
// screenshot, meeting) run inside the ctxd local daemon and emit a stream of
// RawEvent values into a shared Runner. The Runner multiplexes sources, applies
// fingerprint dedup at the enqueue boundary, tags events with the active
// SessionID (per ADR-067), buffers when dpkms is unreachable, and posts to the
// existing /api/v1/analyze HTTP path.
//
// This package defines the core types (Source interface, RawEvent envelope,
// lifecycle states, capture event topics) and the Runner. Concrete sources
// live under internal/ambient/<source>/. The buffer (with FS, S3, and memory
// backends) lives under internal/ambient/buffer/. The session cutter lives
// under internal/ambient/session/.
//
// Per ADR-066, the substrate runs CLIENT-SIDE: dpkms is often deployed remote
// and cannot read local-machine signals (clipboard contents, foreground
// windows, system audio). The Runner enqueues against whichever dpkms is
// configured via the existing HTTP path — dpkms remains a pure pipeline +
// storage worker.
package ambient

import (
	"context"
	"errors"
	"time"
)

// Kind is the type of payload a RawEvent carries. Used by the Runner (and any
// downstream policy guards) to choose pipelines and shape of redaction.
type Kind string

const (
	KindText        Kind = "text"
	KindURL         Kind = "url"
	KindImage       Kind = "image"
	KindFile        Kind = "file"
	KindWindowFocus Kind = "window-focus"
	KindMeeting     Kind = "meeting"
)

// RawEvent is the envelope every ambient Source emits. The Runner consumes
// these, applies redaction → policy filter → fingerprint dedup → compression
// → session-tag → buffer → enqueue, emitting a kit/bus event at every stage
// per the ADR-066 §Decision taxonomy.
//
// Sources MUST populate Source, OccurredAt, Kind, Payload, Fingerprint, and
// SuggestedPipeline. The Runner populates SessionID from the active cutter.
// Metadata is open-ended per source (e.g. clipboard sources may carry the
// foreground bundle id; file-watch sources carry the file path).
type RawEvent struct {
	// Source matches the emitting Source's Name() (e.g. "clipboard", "filewatch").
	Source string
	// OccurredAt is the monotonic-safe wall time the event was captured.
	OccurredAt time.Time
	// Kind is the payload's shape; chooses pipeline and redaction.
	Kind Kind
	// Payload is the captured bytes. Sources MUST apply source-side redaction
	// (passwords, OAuth tokens) BEFORE emission — privacy enforcement happens
	// before the event leaves the source goroutine.
	Payload []byte
	// Fingerprint is a SHA-256 hex string over the normalised Payload. Used by
	// the Runner for cheap pre-enqueue dedup. Empty fingerprint disables dedup
	// for this event (rare; only when payload semantics resist normalisation).
	Fingerprint string
	// SuggestedPipeline is the dpkms pipeline the Runner should request when
	// enqueueing (e.g. "text.short", "url.generic", "audio.transcribe"). The
	// Runner forwards as-is; pipeline selection ultimately rests with dpkms.
	SuggestedPipeline string
	// SessionID is populated by the Runner from the active cutter snapshot
	// (per ADR-067). Empty when no session is active or the cutter is absent.
	SessionID string
	// Metadata is open-ended per-source data. Common keys:
	//
	//	"foreground_bundle_id"   string  — for clipboard / screenshot
	//	"file_path"              string  — for filewatch
	//	"app_name", "window_title" string — for foreground-window
	Metadata map[string]any
}

// Source is the contract every ambient capture source implements. Sources run
// concurrently inside the Runner; multiple sources of distinct Names() may be
// active simultaneously (in contrast to ADR-065 typed adapters, which enforce
// one-platform-per-protocol).
//
// Lifecycle:
//
//	Start  — register the source on the Runner; subscribe to OS signals;
//	         begin emitting on Events(). The Source receives the ctxt
//	         daemon's bus so it can publish lifecycle topics directly.
//	Events — channel of RawEvent values. The Runner reads from this channel
//	         and is responsible for consuming events promptly. Sources should
//	         buffer modestly (a few events) but never block indefinitely.
//	Drain  — stop accepting new OS signals; finish emitting any in-flight
//	         events. Drain returns once Events() will produce no more.
//	Stop   — hard-stop. Releases all resources (file watchers, hotkeys, etc.).
type Source interface {
	// Name returns the source identifier (e.g. "clipboard"). Must be unique
	// across registered sources in a Runner.
	Name() string
	// Start begins emitting events. Returns once the source is ready or with
	// the start error. The bus is the ctxd daemon's kit/runtime/bus; sources
	// publish their lifecycle topics on it (e.g.
	// ctxt.ambient.source.<name>.started).
	Start(ctx context.Context, b Publisher) error
	// Events returns the source's outbound event channel. Callers must read
	// promptly to avoid backpressure; the Runner does this for production use.
	Events() <-chan RawEvent
	// Drain stops accepting new OS signals and flushes pending events. After
	// Drain returns, Events() will close once the in-flight events have been
	// emitted.
	Drain(ctx context.Context) error
	// Stop is the hard-stop. Releases resources. Idempotent.
	Stop(ctx context.Context) error
}

// Publisher is the minimal interface a Source uses to emit lifecycle and
// per-event topics on the daemon bus. Defined as an interface (not bus.Bus
// directly) so tests and constrained sources can pass a mock.
//
// Implementations MUST forward Publish to a kit/runtime/bus.Bus. The runner
// constructs a Publisher around the daemon bus and passes it to each Source's
// Start.
type Publisher interface {
	Publish(ctx context.Context, topic, source string, payload any) error
}

// LifecycleState is the source's current state in the Source lifecycle.
// Exposed so observers (status CLI, MCP health tool) can report what each
// source is doing without inspecting goroutine internals.
type LifecycleState string

const (
	StateStopped  LifecycleState = "stopped"
	StateStarting LifecycleState = "starting"
	StateReady    LifecycleState = "ready"
	StateDraining LifecycleState = "draining"
	StateFailed   LifecycleState = "failed"
)

// Topic builders for the kit/runtime/bus 4-segment past-tense convention used
// throughout ADR-066. Source authors should call these instead of hand-rolling
// strings; tests assert the resulting strings match expected shapes.
//
// Shape: ctxt.ambient.<object>.<action>.

// SourceLifecycleTopic returns the topic for source lifecycle transitions:
// started, ready, drained, stopped, failed.
func SourceLifecycleTopic(action string) string {
	return "ctxt.ambient.source." + action
}

// EventTopic returns the topic for per-event transformations: captured,
// redacted, filtered, deduped, debounced, compressed.
func EventTopic(action string) string {
	return "ctxt.ambient.event." + action
}

// BufferTopic returns the topic for buffer transitions: appended, evicted,
// replayed.
func BufferTopic(action string) string {
	return "ctxt.ambient.buffer." + action
}

// EnqueueTopic returns the topic for enqueue transitions: queued, waiting,
// attempted, succeeded, failed.
func EnqueueTopic(action string) string {
	return "ctxt.ambient.enqueue." + action
}

// SessionTopic returns the topic for session transitions per ADR-067: opened,
// closed, event_joined, cut_evaluated.
func SessionTopic(action string) string {
	return "ctxt.ambient.session." + action
}

// ErrSourceAlreadyRegistered indicates an attempt to register a second source
// under a name that is already registered. The Runner enforces unique names.
var ErrSourceAlreadyRegistered = errors.New("ambient: source already registered")

// ErrSourceNotFound indicates a lookup against the Runner for a name that has
// not been registered.
var ErrSourceNotFound = errors.New("ambient: source not found")
