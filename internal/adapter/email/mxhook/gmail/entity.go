package gmail

import "github.com/ideacrafterslabs/ctxt/internal/ingest"

// messageEntity satisfies domain.Entity for inbound mail. The
// substrate's ingest.Object carries ID as a struct field rather than
// a method; messageEntity flattens the relevant fields so CEL
// activation (kit/runtime/policy/subscriber.go::resourceFromPayload)
// sees them at predictable paths.
//
// JSON tags drive CEL field paths because kit's policy subscriber
// JSON-round-trips the entity into resource.fields. The shape exposed
// to operator-authored CEL rules is therefore:
//
//	resource.fields.id        — message ID
//	resource.fields.Kind      — "email" (discriminator)
//	resource.fields.From      — From header (or "" when absent)
//	resource.fields.To        — To header
//	resource.fields.Subject   — Subject header
//	resource.fields.MessageID — IMAP Message-ID
//
// Adopters MAY also reach into the wrapped Object via
// resource.fields.Object.metadata.<header>, but the flattened fields
// are the stable, gmail-specific contract.
//
// Kind == "email" lets CEL rules discriminate this adapter's
// mutations from others' (per ADR-065 §Amendment 2026-05-06
// §Acceptance Gate 4 — payload discrimination, not per-protocol
// veto-able topics).
type messageEntity struct {
	Object    ingest.Object `json:"Object"`
	ID        string        `json:"id"`
	Kind      string        `json:"Kind"`
	From      string        `json:"From,omitempty"`
	To        string        `json:"To,omitempty"`
	Subject   string        `json:"Subject,omitempty"`
	MessageID string        `json:"MessageID,omitempty"`
}

// GetID returns the message ID. The substrate's domain.Service[T]
// uses this to populate PreEntityPayload.EntityID.
func (m messageEntity) GetID() string { return m.ID }

// fromObject builds a messageEntity from an ingest.Object emitted by
// the IMAP client. Headers are extracted from Object.Metadata so CEL
// rules don't have to walk into the metadata map.
func fromObject(obj ingest.Object) messageEntity {
	from, _ := obj.Metadata["From"].(string)
	to, _ := obj.Metadata["To"].(string)
	subject, _ := obj.Metadata["Subject"].(string)
	messageID, _ := obj.Metadata["MessageID"].(string)
	return messageEntity{
		Object:    obj,
		ID:        obj.ID,
		Kind:      "email",
		From:      from,
		To:        to,
		Subject:   subject,
		MessageID: messageID,
	}
}
