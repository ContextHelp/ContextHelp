package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// HandleSSE streams all bus events as Server-Sent Events to the client.
// Each event is encoded as a JSON payload in the SSE `data:` field.
// The connection is held open until the client disconnects.
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

		// Channel for events delivered to this connection.
		ch := make(chan events.Event, 64)

		// Subscribe to all events via wildcard.
		svc.Bus.Subscribe("*", func(_ context.Context, e events.Event) error {
			select {
			case ch <- e:
			default:
				// Drop on full buffer rather than blocking the bus.
			}
			return nil
		})

		// Stream events until client disconnects.
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				data, err := json.Marshal(e)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
