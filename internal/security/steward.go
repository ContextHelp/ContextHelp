package security

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Steward access events (ADR-083 §2, §3, §9): every transition of a
// steward grant request, of a grant and of a browser session's
// elevation, so notification hooks (webhook, SMTP, the audit log) can
// attach. Each alert names its actor as the principal and carries ids
// and stable codes in Meta, never the request's reason text or a
// scope. The approval mechanism's own audit entries are separate and
// unchanged.
const (
	EventStewardRequestFiled     = "security.steward.request.filed"
	EventStewardRequestApproved  = "security.steward.request.approved"
	EventStewardRequestRejected  = "security.steward.request.rejected"
	EventStewardRequestWithdrawn = "security.steward.request.withdrawn"
	EventStewardRequestStale     = "security.steward.request.stale"
	EventStewardGrantRevoked     = "security.steward.grant.revoked"
	EventStewardElevationStarted = "security.steward.elevation.started"
	EventStewardElevationEnded   = "security.steward.elevation.ended"
	EventStewardElevationRefused = "security.steward.elevation.refused"
)

// StewardEvents lists the steward events.
var StewardEvents = []string{
	EventStewardRequestFiled, EventStewardRequestApproved, EventStewardRequestRejected,
	EventStewardRequestWithdrawn, EventStewardRequestStale, EventStewardGrantRevoked,
	EventStewardElevationStarted, EventStewardElevationEnded, EventStewardElevationRefused,
}

// stewardMessages is the alert text of each steward event.
var stewardMessages = map[string]string{
	EventStewardRequestFiled:     "steward access requested",
	EventStewardRequestApproved:  "steward access request approved: a steward grant was written",
	EventStewardRequestRejected:  "steward access request rejected",
	EventStewardRequestWithdrawn: "steward access request withdrawn",
	EventStewardRequestStale:     "steward access request ended stale: nothing was granted",
	EventStewardGrantRevoked:     "steward grant revoked",
	EventStewardElevationStarted: "steward elevation started",
	EventStewardElevationEnded:   "steward elevation ended",
	EventStewardElevationRefused: "steward elevation refused",
}

// RecordSteward fires the steward event kind (EventSteward*) for
// principal, the actor, with meta naming its ids. Every occurrence
// fires: there is no threshold. A nil emitter records nothing.
func (e *Emitter) RecordSteward(ctx context.Context, kind, principal string, meta map[string]string) {
	if e == nil {
		return
	}
	msg := stewardMessages[kind]
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
