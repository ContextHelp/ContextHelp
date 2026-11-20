package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seenCap bounds the object IDs one connection remembers.
const seenCap = 10000

// viewerEvents decides, per event, what a restricted viewer's stream
// may carry (ADR-080 §6, "Events"). One per connection; used by the
// connection's goroutine only.
//
//   - Only object events pass: ingested, updated, deleted, raw_stored.
//     Everything else (jobs, upgrade, inbox, embeddings, ...) is
//     owner-only.
//   - An object event passes when the object is readable by the
//     connection's viewer: the same scoped read find/list/get use
//     (Service.Store), looked up by the event's object ID. Hidden and
//     missing objects are dropped alike.
//   - The payload is rebuilt from the object as the viewer reads it, so
//     the policy's field projection applies: no field list on updates;
//     source, source_key and tags only when the policy projects them
//     (source_key follows source).
//   - A delete leaves nothing to read, so it passes only for an object
//     this connection already delivered (the viewer saw it exist). A
//     delete of any other ID is dropped, since delivering it would
//     confirm an ID the viewer never saw.
//   - The viewer is the caller's current one, passed in per event (see
//     callerView), so a revoked grant applies to the next event.
type viewerEvents struct {
	svc  *service.Service
	seen *idSet
}

func newViewerEvents(svc *service.Service) *viewerEvents {
	return &viewerEvents{svc: svc, seen: newIDSet(seenCap)}
}

// project returns the CloudEvent the viewer may receive for e, and
// whether to send it. A false send with a nil error is a drop.
func (f *viewerEvents) project(ctx context.Context, e bus.Event) (events.Event, bool, error) {
	var send bool
	var payload any
	switch e.Topic {
	case events.TopicObjectDeleted:
		id := objectID(e)
		if id == "" || !f.seen.has(id) {
			return events.Event{}, false, nil
		}
		f.seen.remove(id)
		payload, send = events.ObjectDeletedPayload{ObjectID: id}, true
	case events.TopicObjectIngested, events.TopicObjectUpdated, events.TopicObjectRawStored:
		id := objectID(e)
		if id == "" {
			return events.Event{}, false, nil
		}
		obj, err := f.svc.Store(ctx).Objects().Get(ctx, id)
		switch {
		case errors.Is(err, storage.ErrNotFound) || (err == nil && obj == nil):
			f.seen.remove(id)
			return events.Event{}, false, nil
		case err != nil:
			// Fail closed: an unreadable object is not delivered.
			slog.Warn("events: viewer read failed", "object", id, "err", err)
			return events.Event{}, false, nil
		}
		f.seen.add(id)
		payload, send = viewerPayload(e, obj), true
	}
	if !send {
		return events.Event{}, false, nil
	}
	ce, err := events.CloudEvent(bus.Event{Topic: e.Topic, Source: e.Source, Timestamp: e.Timestamp, Payload: payload})
	return ce, err == nil, err
}

// viewerPayload is the payload of an object event as the viewer reads
// the object (obj is the rendered copy).
func viewerPayload(e bus.Event, obj *storage.KnowledgeObject) any {
	switch e.Topic {
	case events.TopicObjectIngested:
		p := events.ObjectIngestedPayload{
			ObjectID:  obj.ID,
			Type:      obj.Type,
			Source:    obj.Source,
			SourceKey: obj.SourceKey,
			Pipeline:  obj.Pipeline,
			ProfileID: obj.ProfileID,
		}
		// The owner payload's duration, whether the event is the
		// publisher's value (a bus event) or the stored JSON (an
		// outbox row).
		switch orig := e.Payload.(type) {
		case events.ObjectIngestedPayload:
			p.DurationMs = orig.DurationMs
		case json.RawMessage:
			var stored events.ObjectIngestedPayload
			if json.Unmarshal(orig, &stored) == nil {
				p.DurationMs = stored.DurationMs
			}
		}
		for _, t := range obj.Tags {
			p.Tags = append(p.Tags, t.Label)
		}
		return p
	case events.TopicObjectUpdated:
		return viewerUpdated{ObjectID: obj.ID, Type: obj.Type}
	default: // raw_stored carries the stored object
		return obj
	}
}

// viewerUpdated is an update as a viewer gets it: the object and its
// type, without the owner payload's list of changed fields.
type viewerUpdated struct {
	ObjectID string `json:"object_id"`
	Type     string `json:"type"`
}

// objectID is the ID of the object an event announces, whatever form
// its payload is in; empty when it names none.
func objectID(e bus.Event) string {
	switch p := e.Payload.(type) {
	case events.ObjectIngestedPayload:
		return p.ObjectID
	case events.ObjectUpdatedPayload:
		return p.ObjectID
	case events.ObjectDeletedPayload:
		return p.ObjectID
	case *storage.KnowledgeObject:
		if p != nil {
			return p.ID
		}
		return ""
	}
	raw, err := events.PayloadJSON(e)
	if err != nil || raw == nil {
		return ""
	}
	var p struct {
		ObjectID string `json:"object_id"`
		ID       string `json:"id"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	if p.ObjectID != "" {
		return p.ObjectID
	}
	return p.ID
}

// idSet is a bounded set of IDs that forgets the oldest first.
type idSet struct {
	max   int
	order []string
	in    map[string]struct{}
}

func newIDSet(max int) *idSet { return &idSet{max: max, in: map[string]struct{}{}} }

func (s *idSet) has(id string) bool { _, ok := s.in[id]; return ok }

func (s *idSet) add(id string) {
	if s.has(id) {
		return
	}
	if len(s.order) >= s.max {
		delete(s.in, s.order[0])
		s.order = s.order[1:]
	}
	s.in[id] = struct{}{}
	s.order = append(s.order, id)
}

func (s *idSet) remove(id string) {
	if !s.has(id) {
		return
	}
	delete(s.in, id)
	for i, v := range s.order {
		if v == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}
