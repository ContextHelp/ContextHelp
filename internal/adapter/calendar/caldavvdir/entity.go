package caldavvdir

import "github.com/ideacrafterslabs/ctxt/internal/ingest"

// eventEntity satisfies domain.Entity for calendar events. Like the
// gmail backend's messageEntity, it flattens the relevant fields so
// CEL rules can read them at predictable paths under
// resource.fields.* (kit/runtime/policy/subscriber.go JSON-round-trips
// the entity into the activation map).
//
// CEL field paths exposed:
//
//	resource.fields.id     — event UID
//	resource.fields.Kind   — "calendar.event" (discriminator)
//	resource.fields.UID    — event UID (duplicate; some rules prefer it)
type eventEntity struct {
	Object ingest.Object `json:"Object"`
	ID     string        `json:"id"`
	Kind   string        `json:"Kind"`
	UID    string        `json:"UID"`
}

// GetID returns the event UID.
func (e eventEntity) GetID() string { return e.ID }

// eventFromObject builds an eventEntity from the raw ingest.Object +
// the parsed UID.
func eventFromObject(obj ingest.Object, uid string) eventEntity {
	return eventEntity{
		Object: obj,
		ID:     uid,
		Kind:   EventEntityKind,
		UID:    uid,
	}
}
