package steward

import (
	"context"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Every transition of steward access reaches the security emitter
// (ADR-083 §2, §3), so notification hooks can attach: the request's
// (ApprovalNotices), a grant's revocation (Service.Revoke), and a
// browser session's elevation (ElevationEvents, RecordElevationRefused).
// The alerts carry ids and stable codes, never the request's reason or
// scope. A nil emitter records nothing.

// requestEvents maps the mechanism's transitions of a steward.grant
// request to their security events. A decision short of N is no
// transition; expiry is computed, never recorded, so it has no event.
var requestEvents = map[string]string{
	approval.EventFiled:     security.EventStewardRequestFiled,
	approval.EventApproved:  security.EventStewardRequestApproved,
	approval.EventRejected:  security.EventStewardRequestRejected,
	approval.EventWithdrawn: security.EventStewardRequestWithdrawn,
	approval.EventStale:     security.EventStewardRequestStale,
}

// ApprovalNotices returns the approval mechanism's Notify hook that
// records each transition of a steward.grant request on sec, its actor
// the principal: filed, approved (naming the grant it wrote), rejected,
// withdrawn and stale. Requests of other kinds pass by.
func ApprovalNotices(sec *security.Emitter) func(context.Context, approval.Notice) {
	return func(ctx context.Context, n approval.Notice) {
		event, ok := requestEvents[n.Event]
		if !ok || n.Request.Kind != KindName {
			return
		}
		meta := map[string]string{
			"request_id":         n.Request.ID,
			"steward_id":         n.Request.SubjectID,
			"requested_by":       n.Request.RequestedBy,
			"payload_digest":     n.Request.PayloadDigest,
			"approvals_required": strconv.Itoa(n.Request.ApprovalsRequired),
		}
		if n.Request.ResultRef != "" {
			meta["grant_id"] = n.Request.ResultRef
		}
		sec.RecordSteward(ctx, event, n.Actor, meta)
	}
}

// ElevationEvents returns the session manager's OnElevation hook that
// records each elevation that starts or ends on sec, the steward the
// principal.
func ElevationEvents(sec *security.Emitter) func(context.Context, auth.ElevationEvent) {
	return func(ctx context.Context, e auth.ElevationEvent) {
		meta := map[string]string{"grant_id": e.GrantID, "session_id": e.SessionID}
		event := security.EventStewardElevationStarted
		if !e.Started {
			event = security.EventStewardElevationEnded
			meta["reason"] = e.Reason
		}
		sec.RecordSteward(ctx, event, e.PrincipalID, meta)
	}
}

// RecordElevationRefused records on sec that principal could not read
// under grantID: an elevation asked for or a request made under one,
// refused with reason (a sharing.StewardReason code, or the transport's
// own), deferred when the cause was unavailability.
func RecordElevationRefused(ctx context.Context, sec *security.Emitter, principal, grantID, reason string, deferred bool) {
	sec.RecordSteward(ctx, security.EventStewardElevationRefused, principal, map[string]string{
		"grant_id": grantID, "reason": reason, "deferred": strconv.FormatBool(deferred),
	})
}

// recordRevoked records g's revocation by actor on sec.
func recordRevoked(ctx context.Context, sec *security.Emitter, actor string, g *storage.StewardGrant) {
	sec.RecordSteward(ctx, security.EventStewardGrantRevoked, actor, map[string]string{
		"grant_id": g.ID, "request_id": g.RequestID, "steward_id": g.StewardID,
	})
}
