// Package events defines ctxt's domain event topics and payloads, the
// CloudEvents wire envelope the SSE stream carries, a Publisher onto
// kit's runtime bus, and the Subscriber that keeps focus profiles in
// step with profile events. Domain events travel on kit's runtime bus
// (hop.top/kit/go/runtime/bus); this package owns no bus of its own.
package events

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"hop.top/kit/go/runtime/bus"
)

// Event is the CloudEvents v1.0 envelope the SSE stream emits for a bus
// event.
type Event struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	SpecVersion     string          `json:"specversion"`
	Type            string          `json:"type"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Time            time.Time       `json:"time"`
	Data            json.RawMessage `json:"data,omitempty"`
}

// PayloadJSON returns the event payload as JSON, whatever form it
// arrives in: the publisher's Go value in process, or the decoded form
// (map, slice) after a hop across a process boundary.
func PayloadJSON(e bus.Event) (json.RawMessage, error) {
	if e.Payload == nil {
		return nil, nil
	}
	if raw, ok := e.Payload.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CloudEvent wraps a bus event in its CloudEvents envelope. The id is a
// UUIDv5 over topic, source, timestamp and payload, so the same bus event
// carries the same id on every connection and can be deduplicated.
func CloudEvent(e bus.Event) (Event, error) {
	data, err := PayloadJSON(e)
	if err != nil {
		return Event{}, err
	}
	ts := e.Timestamp.UTC()
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00", e.Topic, e.Source, ts.UnixNano())
	h.Write(data)
	id := uuid.NewSHA1(uuid.NameSpaceOID, h.Sum(nil))
	out := Event{
		ID:          id.String(),
		Source:      e.Source,
		SpecVersion: "1.0",
		Type:        string(e.Topic),
		Time:        ts,
		Data:        data,
	}
	if data != nil {
		out.DataContentType = "application/json"
	}
	return out, nil
}
