package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Events the sharing routes append to the audit log (ADR-080 §9): who
// changed what a viewer may see, in the order it happened. Each entry
// names the caller as its actor and carries the ids and names of what
// changed, never a secret: an invite's link is not in its entry. A
// change to the registry is recorded by sharechange, in its
// transaction, under these names; the invite mint is recorded here.
const (
	EventAudienceCreated = sharechange.EventAudienceCreated
	EventMemberAdded     = sharechange.EventMemberAdded
	EventMemberRemoved   = sharechange.EventMemberRemoved
	EventPolicyCreated   = sharechange.EventPolicyCreated
	EventPolicyUpdated   = sharechange.EventPolicyUpdated
	EventGrantCreated    = sharechange.EventGrantCreated
	EventGrantRevoked    = sharechange.EventGrantRevoked
	EventCeilingSet      = sharechange.EventCeilingSet
	EventKeyPolicyBound  = sharechange.EventKeyPolicyBound
	EventInviteMinted    = "share.invite.minted"
)

// changeLog appends sharing changes to the audit log. A nil store logs
// nothing, as on a router built without one.
type changeLog struct{ store storage.AuditStore }

// record appends one entry for a change r just made, as r's caller.
// The change is already made when this runs, so a failed append is
// reported to the operator and never to the caller, as the security
// events are.
func (c changeLog) record(r *http.Request, event string, payload map[string]any) {
	c.recordAs(r.Context(), caller(r).ID, event, payload)
}

// recordAs appends one entry for a change made on behalf of actor, for
// a change with no authenticated caller of its own: an invite's
// redemption adds the redeemer to the audience as the invite's minter.
func (c changeLog) recordAs(ctx context.Context, actor, event string, payload map[string]any) {
	if c.store == nil {
		return
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: event,
		Actor:     actor,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	if err := c.store.Append(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit: sharing change not recorded", "event", event, "actor", entry.Actor, "err", err)
	}
}
