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
)

// sseBuffer is how many events one connection holds before it drops.
const sseBuffer = 64

// HandleSSE streams every bus event as Server-Sent Events to the client.
// Each event is a CloudEvents JSON envelope in the SSE `data:` field.
// The connection is held open until the client disconnects; its bus
// subscription ends with it.
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

		// Set SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		// Send the headers now: until they arrive, a browser EventSource
		// stays "connecting" and never fires open.
		flusher.Flush()

		// Channel for events delivered to this connection.
		ch := make(chan bus.Event, sseBuffer)
		// A sync handler runs on the publisher's goroutine, so it only
		// hands off, and it never returns an error: that would veto the
		// publish. Unsubscribe on disconnect, or the subscriber outlives
		// the connection.
		unsub := svc.Bus.Subscribe("#", func(_ context.Context, e bus.Event) error {
			select {
			case ch <- e:
			default:
				// Drop on full buffer rather than blocking the bus.
			}
			return nil
		})
		defer unsub()

		// Stream events until client disconnects.
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				ce, err := events.CloudEvent(e)
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
