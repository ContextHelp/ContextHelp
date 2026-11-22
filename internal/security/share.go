package security

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Share change events (ADR-083 §9, §10 B): every transition of a
// share.change request, so notification hooks can attach. Each alert
// names its actor as the principal and carries ids and stable codes in
// Meta, never the payload. The approval mechanism's own audit entries
// are separate and unchanged.
const (
	EventShareChangeFiled     = "security.share_change.filed"
	EventShareChangeDecided   = "security.share_change.decided"
	EventShareChangeApplied   = "security.share_change.applied"
	EventShareChangeRejected  = "security.share_change.rejected"
	EventShareChangeWithdrawn = "security.share_change.withdrawn"
	EventShareChangeStale     = "security.share_change.stale"
)

// ShareChangeEvents lists the share change events.
var ShareChangeEvents = []string{
	EventShareChangeFiled, EventShareChangeDecided, EventShareChangeApplied,
	EventShareChangeRejected, EventShareChangeWithdrawn, EventShareChangeStale,
}

var shareChangeMessages = map[string]string{
	EventShareChangeFiled:     "sharing change waits for approval",
	EventShareChangeDecided:   "sharing change decision recorded",
	EventShareChangeApplied:   "sharing change approved and applied",
	EventShareChangeRejected:  "sharing change rejected",
	EventShareChangeWithdrawn: "sharing change withdrawn",
	EventShareChangeStale:     "sharing change ended stale: nothing was applied",
}

// RecordShareChange fires the share change event kind (EventShareChange*)
// for principal, the actor, with meta naming its ids. Every occurrence
// fires. A nil emitter records nothing.
func (e *Emitter) RecordShareChange(ctx context.Context, kind, principal string, meta map[string]string) {
	if e == nil {
		return
	}
	msg := shareChangeMessages[kind]
	if msg == "" {
		msg = kind
	}
	e.dispatch(ctx, Alert{
		ID:        uuid.New().String(),
		Kind:      kind,
		Level:     AlertLevelWarn,
		Principal: principal,
		Message:   msg,
		Count:     1,
		Meta:      meta,
		OccuredAt: time.Now().UTC(),
	})
}
