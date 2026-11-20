package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// sseBuffer is how many events one connection holds before it drops.
const sseBuffer = 64

// HandleSSE streams bus events as Server-Sent Events to the client.
// Each event is a CloudEvents JSON envelope in the SSE `data:` field.
// The connection is held open until the client disconnects; its bus
// subscription ends with it.
//
// The owner (and every caller on a private instance) gets every event.
// Any other principal gets the object events for objects its viewer may
// read, filtered per event (see viewerEvents); a viewer that may read
// nothing gets an empty stream, never a 403, so the answer does not
// reveal whether anything exists. While the stream is idle the handler
// writes an SSE comment every server.events_heartbeat.
//
// The stream follows the caller: each event (at most every
// sseRecheckTTL) and each heartbeat authenticates the credential again
// and resolves the viewer as it is now, so a revoked grant applies to
// the next event, and a revoked or expired key ends the stream with a
// final comment.
//
// Route: GET /api/v1/events
func HandleSSE(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the client supports streaming.
		flusher, ok := w.(http.Flusher)
		if !ok {
			WriteError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "streaming not supported")
			return
		}

		// A caller with no viewer attached is the zero viewer: nothing
		// visible. Only an explicit Unrestricted viewer sees everything.
		viewer, _ := sharing.FromContext(r.Context())
		callerView := newCallerView(r.Context(), viewer)
		filter := newViewerEvents(svc)
		// The owner's stream subscribes to everything. Anyone else
		// subscribes to object events only, and always, even with no
		// grant yet: a grant made later then applies without a reconnect.
		pattern := "ctxt.runtime.object.*"
		if viewer.IsUnrestricted() {
			pattern = "#"
		}

		// Set SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		// Send the headers now: until they arrive, a browser EventSource
		// stays "connecting" and never fires open.
		flusher.Flush()

		// Channel for events delivered to this connection. A sync handler
		// runs on the publisher's goroutine, so it only hands off, and it
		// never returns an error: that would veto the publish. Unsubscribe
		// on disconnect, or the subscriber outlives the connection.
		ch := make(chan bus.Event, sseBuffer)
		unsub := svc.Bus.Subscribe(pattern, func(_ context.Context, e bus.Event) error {
			select {
			case ch <- e:
			default:
				// Drop on full buffer rather than blocking the bus.
			}
			return nil
		})
		defer unsub()

		heartbeat := time.NewTicker(sseHeartbeat(svc.Cfg))
		defer heartbeat.Stop()

		// Stream events until client disconnects.
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				// Re-check the caller on every tick, so a revoked key
				// ends an idle stream too.
				if _, ok := callerView.current(true); !ok {
					closeStream(w, flusher)
					return
				}
				fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			case e := <-ch:
				v, ok := callerView.current(false)
				if !ok {
					closeStream(w, flusher)
					return
				}
				var ce events.Event
				var err error
				if v.IsUnrestricted() {
					ce, err = events.CloudEvent(e)
				} else {
					var send bool
					ce, send, err = filter.project(sharing.WithViewer(ctx, v), e)
					if !send && err == nil {
						continue
					}
				}
				if err != nil {
					slog.Warn("events: skipping event", "topic", e.Topic, "err", err)
					continue
				}
				data, err := json.Marshal(ce)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

// sseHeartbeat is the interval between heartbeat comments:
// server.events_heartbeat, or the default when it is unset. A negative
// value is treated as unset too, so the ticker is always valid.
func sseHeartbeat(cfg config.Config) time.Duration {
	if d := cfg.Server.EventsHeartbeat; d > 0 {
		return d
	}
	return config.DefaultEventsHeartbeat
}

// sseRecheckTTL is how long the stream trusts the caller's last check:
// each event re-authenticates the credential and re-resolves the viewer
// unless the last check is younger than this. A revoked key or grant
// therefore takes effect within this long (and at the next heartbeat at
// the latest). The check is a few indexed reads and a cache lookup, so
// the TTL only bounds the cost of an event burst. A package variable so
// tests can make every event check.
var sseRecheckTTL = 2 * time.Second

// closeStream ends a stream whose caller no longer authenticates, with a
// final comment line so the client sees why. An EventSource reconnects
// and is then answered 401.
func closeStream(w http.ResponseWriter, f http.Flusher) {
	fmt.Fprint(w, ": closed: credential no longer valid\n\n")
	f.Flush()
}

// callerView is the caller's current viewer for one stream.
type callerView struct {
	ctx     context.Context
	recheck Recheck
	v       sharing.Viewer
	at      time.Time
}

func newCallerView(ctx context.Context, v sharing.Viewer) *callerView {
	return &callerView{ctx: ctx, recheck: recheckFrom(ctx), v: v, at: time.Now()}
}

// current returns the caller's viewer, checking again when force is set
// or the last check is older than sseRecheckTTL. false means the caller
// no longer authenticates, or its viewer cannot be resolved (fail
// closed): the stream must end. With no Recheck (a private instance)
// the viewer never changes.
func (c *callerView) current(force bool) (sharing.Viewer, bool) {
	if c.recheck == nil {
		return c.v, true
	}
	if !force && time.Since(c.at) < sseRecheckTTL {
		return c.v, true
	}
	v, err := c.recheck(c.ctx)
	if err != nil {
		return sharing.Viewer{}, false
	}
	c.v, c.at = v, time.Now()
	return v, true
}
