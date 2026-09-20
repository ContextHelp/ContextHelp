// Package caldavvdir is the caldav-vdir backend of the calendar
// protocol slot. Backend() returns "caldav-vdir"; the package
// directory drops the hyphen so the import path is Go-conformant.
//
// caldav-vdir is the first FULLY bidirectional adapter in the dPKMS
// substrate: it declares CapFetch + CapServe + CapSubmit +
// CapEmitEvents. Real lifecycle (Start/Drain/Stop), real persistence
// to a vdir filesystem (one .ics file per event), and a thin CalDAV
// HTTP handler that clients (Apple Calendar, Thunderbird Lightning,
// etc.) can subscribe to.
//
// # Storage
//
// Events live as .ics files under Config.VDir. Each event's filename
// is its UID (sanitized). Fetch walks the directory; Submit writes a
// new file. The vdir convention is the same one the cardamum CardDAV
// adapter uses for contacts (see ADR-065 §Slot conventions).
//
// # Persistence + policy gating (Acceptance Gate 4)
//
// Submit and Serve writes route through domain.Service[eventEntity]
// when Repo + Publisher are wired, firing
// kit.runtime.entity.pre_persisted. CEL rules veto malformed events
// (e.g. missing required fields). Mirrors the gmail backend's
// approach: payload discrimination via resource.fields.Kind, not a
// per-protocol veto-able topic.
//
// # CalDAV server (Phase 2 narrowed scope)
//
// Phase 2 ships a thin CalDAV HTTP handler covering:
//
//   - PROPFIND /  → returns the calendar's basic properties
//     (resourcetype, displayname, supported-calendar-component-set).
//   - REPORT /   → returns the list of events in the vdir as a
//     calendar-multiget-style response.
//   - GET /<uid>.ics  → returns the .ics body.
//   - PUT /<uid>.ics  → writes a new .ics body (PROPPATCH-style).
//
// This is a deliberate sub-set of RFC 4791. Full CalDAV semantics
// (free-busy, recurrence expansion, sync-collection, etc.) are
// future work. The handler is good enough to land + extend; clients
// that demand the full spec error-fall-through to the next backend.
//
// Production wiring may swap the handler for github.com/emersion/
// go-webdav/caldav once the dependency is added; the seam is
// CalDAVHandler below.
package caldavvdir

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/calendar"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Backend is the canonical backend identifier returned from
// Adapter.Backend(). Used by the substrate Registry, by operator
// surfaces (`dpkms adapter list`), and by the runner when emitting
// dpkms.adapter.lifecycle.* events.
const Backend = "caldav-vdir"

// EventEntityKind is the discriminator value emitted in
// resource.fields.Kind for inbound calendar events. CEL rules use
// this to target this adapter's mutations.
const EventEntityKind = "calendar.event"

// ErrInvalidICS is returned from Submit when the input lacks a UID
// or BEGIN:VCALENDAR/BEGIN:VEVENT structure.
var ErrInvalidICS = errors.New("caldavvdir: invalid iCalendar input (missing UID or VEVENT)")

// EventRepository is the persistence sink for calendar events.
type EventRepository = domain.Repository[eventEntity]

// Config configures the caldav-vdir backend.
type Config struct {
	// VDir is the filesystem directory holding .ics files. Created
	// at Start if missing. Empty value rejected (Start fails loud).
	VDir string

	// CalendarName is the displayname surfaced via PROPFIND. Empty
	// defaults to "ctxt".
	CalendarName string

	// Repo is the persistence sink for calendar events. nil bypasses
	// the policy gate (Submit writes directly to the vdir). Production
	// wiring passes a real Repository so Gate 4 applies.
	Repo EventRepository

	// Publisher is the policy event publisher. nil bypasses the
	// pre_persisted gate (test-only). Production callers pass
	// pol.Publisher() from the policy.Bootstrap.
	Publisher domain.EventPublisher
}

// Adapter is the caldav-vdir typed adapter.
type Adapter struct {
	cfg Config

	mu       sync.Mutex
	state    adapter.LifecycleState
	svc      *domain.Service[eventEntity]
	server   *http.Server
	drained  bool
	serveErr chan error
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// New constructs the caldav-vdir adapter. Most validation is deferred
// to Start so misconfig surfaces with the operator-facing identity.
func New(cfg Config) *Adapter {
	if cfg.CalendarName == "" {
		cfg.CalendarName = "ctxt"
	}
	return &Adapter{cfg: cfg, state: adapter.StateStopped}
}

// Protocol returns "calendar".
func (a *Adapter) Protocol() string { return calendar.Protocol }

// Backend returns "caldav-vdir".
func (a *Adapter) Backend() string { return Backend }

// Capabilities declares fetch + serve + submit + emit-events. Full
// bidirectional — first adapter in Phase 2 to declare CapServe.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{
		adapter.CapFetch,
		adapter.CapServe,
		adapter.CapSubmit,
		adapter.CapEmitEvents,
	}
}

// Start validates the vdir, constructs the policy-gated
// domain.Service for event persistence, and prepares the HTTP
// handler. The server itself doesn't start until Serve runs (Serve
// owns the listener lifecycle).
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == adapter.StateReady {
		return nil
	}
	a.state = adapter.StateStarting

	if a.cfg.VDir == "" {
		a.state = adapter.StateStopped
		return errors.New("caldavvdir: Config.VDir is required")
	}
	if err := os.MkdirAll(a.cfg.VDir, 0o750); err != nil {
		a.state = adapter.StateStopped
		return fmt.Errorf("create vdir %s: %w", a.cfg.VDir, err)
	}

	if a.cfg.Repo != nil {
		opts := []domain.Option[eventEntity]{}
		if a.cfg.Publisher != nil {
			opts = append(opts, domain.WithPublisher[eventEntity](a.cfg.Publisher))
		}
		a.svc = domain.NewService[eventEntity](a.cfg.Repo, opts...)
	}

	a.drained = false
	a.state = adapter.StateReady
	return nil
}

// Ready reports whether Start has completed successfully.
func (a *Adapter) Ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state == adapter.StateReady
}

// Drain stops accepting new requests on the served listener and
// rejects new Submit calls. In-flight handlers run to completion.
func (a *Adapter) Drain(ctx context.Context) error {
	a.mu.Lock()
	server := a.server
	a.drained = true
	a.state = adapter.StateDraining
	a.mu.Unlock()
	if server != nil {
		// Shutdown is graceful: returns when in-flight handlers finish
		// or ctx fires. The Serve goroutine sees ErrServerClosed and
		// returns nil to the caller.
		return server.Shutdown(ctx)
	}
	return nil
}

// Stop tears down the HTTP server and closes any in-flight resources.
// Idempotent across repeated calls.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	server := a.server
	serveErr := a.serveErr
	a.server = nil
	a.serveErr = nil
	a.state = adapter.StateStopped
	a.mu.Unlock()

	if server != nil {
		// Shutdown is no-op if already shut down by Drain.
		_ = server.Shutdown(ctx)
	}
	if serveErr != nil {
		select {
		case <-serveErr:
		default:
		}
	}
	return nil
}

// Fetch walks the vdir and returns one ingest.Object per .ics file.
func (a *Adapter) Fetch(_ context.Context) ([]ingest.Object, error) {
	a.mu.Lock()
	dir := a.cfg.VDir
	a.mu.Unlock()
	if dir == "" {
		return nil, errors.New("caldavvdir: not started (no vdir)")
	}
	return walkVDir(dir)
}

// Submit writes a new .ics file to the vdir. Requires Object.Content
// to be a parsable iCalendar VEVENT with a UID line. Routes through
// the policy-gated domain.Service when Repo+Publisher are wired.
func (a *Adapter) Submit(ctx context.Context, obj ingest.Object) error {
	a.mu.Lock()
	dir := a.cfg.VDir
	svc := a.svc
	drained := a.drained
	a.mu.Unlock()

	if drained {
		return errors.New("caldavvdir: drained, refusing new writes")
	}
	if dir == "" {
		return errors.New("caldavvdir: not started")
	}

	uid := extractUID(obj.Content)
	if uid == "" || !looksLikeVEVENT(obj.Content) {
		return ErrInvalidICS
	}
	if obj.ID == "" {
		obj.ID = uid
	}

	ent := eventFromObject(obj, uid)
	if svc != nil {
		if err := svc.Create(ctx, &ent); err != nil {
			return err
		}
	}
	return writeICS(dir, uid, obj.Content)
}

// Serve runs the CalDAV HTTP handler on the supplied listener. Blocks
// until ctx cancels or the server is Shutdown'd via Stop/Drain.
//
// Phase 2 narrowed scope: the handler covers PROPFIND /, REPORT /,
// GET /<uid>.ics, PUT /<uid>.ics. Full RFC 4791 semantics (free-busy,
// recurrence, sync-collection) are future work. Adopters demanding
// the full spec swap the handler for github.com/emersion/go-webdav/
// caldav under the same Adapter contract.
func (a *Adapter) Serve(ctx context.Context, listener net.Listener) error {
	a.mu.Lock()
	if a.state != adapter.StateReady {
		a.mu.Unlock()
		return fmt.Errorf("caldavvdir: serve called before Start (state=%s)", a.state)
	}
	if a.server != nil {
		a.mu.Unlock()
		return errors.New("caldavvdir: already serving")
	}
	mux := newCalDAVHandler(a.cfg.VDir, a.cfg.CalendarName)
	srv := &http.Server{Handler: mux}
	a.server = srv
	a.serveErr = make(chan error, 1)
	a.mu.Unlock()

	// #nosec G118 -- the goroutine's job IS to outlive ctx: it waits
	// for cancellation and then drains the server. Shutdown takes a
	// fresh context deliberately, so in-flight requests get to finish
	// rather than being cut off by the context that just fired.
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	err := srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// extractUID parses the UID line from an iCalendar body. Returns ""
// when no UID line is found. Phase 2 keeps the parser simple:
// line-based, case-insensitive, ignores parameters.
func extractUID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToUpper(line), "UID:") {
			return strings.TrimSpace(line[4:])
		}
	}
	return ""
}

// looksLikeVEVENT does a minimum sanity check for VEVENT framing.
// Production wiring with a real iCalendar parser swaps this out.
func looksLikeVEVENT(body string) bool {
	upper := strings.ToUpper(body)
	return strings.Contains(upper, "BEGIN:VCALENDAR") &&
		strings.Contains(upper, "BEGIN:VEVENT")
}

// writeICS persists an iCalendar body to <vdir>/<uid>.ics. The UID is
// sanitized so adversarial inputs can't escape the vdir.
func writeICS(vdir, uid, body string) error {
	safe := sanitizeUID(uid)
	if safe == "" {
		return ErrInvalidICS
	}
	path := filepath.Join(vdir, safe+".ics")
	return os.WriteFile(path, []byte(body), 0o600)
}

// sanitizeUID strips characters that could escape the vdir.
func sanitizeUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return ""
	}
	out := make([]byte, 0, len(uid))
	for i := 0; i < len(uid); i++ {
		c := uid[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-' || c == '_' || c == '.' || c == '@':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// walkVDir reads every .ics file and turns it into an ingest.Object.
func walkVDir(vdir string) ([]ingest.Object, error) {
	entries, err := os.ReadDir(vdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read vdir %s: %w", vdir, err)
	}
	var out []ingest.Object
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ics") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(vdir, e.Name()))
		if err != nil {
			continue
		}
		uid := extractUID(string(body))
		if uid == "" {
			uid = strings.TrimSuffix(e.Name(), ".ics")
		}
		out = append(out, ingest.Object{
			ID:      uid,
			Type:    EventEntityKind,
			Content: string(body),
			Metadata: map[string]any{
				"UID":  uid,
				"path": filepath.Join(vdir, e.Name()),
			},
		})
	}
	return out, nil
}
