package steward

import (
	"context"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Every transition of steward access reaches the security emitter
// (ADR-083 §2, §9), so notification hooks can attach: the request's
// (ApprovalNotices) and a grant's revocation (Service.Revoke). The
// alerts carry ids and stable codes, never the request's reason or
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

// recordRevoked records g's revocation by actor on sec.
func recordRevoked(ctx context.Context, sec *security.Emitter, actor string, g *storage.StewardGrant) {
	sec.RecordSteward(ctx, security.EventStewardGrantRevoked, actor, map[string]string{
		"grant_id": g.ID, "request_id": g.RequestID, "steward_id": g.StewardID,
	})
}
