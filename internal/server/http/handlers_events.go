package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"hop.top/kit/go/runtime/bus"

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
// reveal whether anything exists.
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
		var filter *viewerEvents
		pattern := "#"
		if !viewer.IsUnrestricted() {
			filter = newViewerEvents(svc)
			pattern = "ctxt.runtime.object.*"
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

		// Channel for events delivered to this connection. A viewer that
		// matches nothing needs no subscription at all.
		ch := make(chan bus.Event, sseBuffer)
		if filter == nil || !viewer.MatchesNothing() {
			// A sync handler runs on the publisher's goroutine, so it only
			// hands off, and it never returns an error: that would veto
			// the publish. Unsubscribe on disconnect, or the subscriber
			// outlives the connection.
			unsub := svc.Bus.Subscribe(pattern, func(_ context.Context, e bus.Event) error {
				select {
				case ch <- e:
				default:
					// Drop on full buffer rather than blocking the bus.
				}
				return nil
			})
			defer unsub()
		}

		// Stream events until client disconnects.
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				var ce events.Event
				var err error
				if filter == nil {
					ce, err = events.CloudEvent(e)
				} else {
					var send bool
					ce, send, err = filter.project(ctx, e)
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
