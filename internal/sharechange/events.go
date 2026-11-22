package sharechange

import (
	"context"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// requestEvents maps the mechanism's events of a share.change request
// to their security events. A decision short of N is reported as
// decided; expiry is computed, never recorded, so it has no event.
var requestEvents = map[string]string{
	approval.EventFiled:     security.EventShareChangeFiled,
	approval.EventDecided:   security.EventShareChangeDecided,
	approval.EventApproved:  security.EventShareChangeApplied,
	approval.EventRejected:  security.EventShareChangeRejected,
	approval.EventWithdrawn: security.EventShareChangeWithdrawn,
	approval.EventStale:     security.EventShareChangeStale,
}

// ApprovalNotices returns the approval mechanism's Notify hook that
// records each transition of a share.change request on sec, its actor
// the principal: filed, decided, applied (approved), rejected,
// withdrawn and stale. Requests of other kinds pass by. The alerts
// carry ids and the action, never the payload.
func ApprovalNotices(sec *security.Emitter) func(context.Context, approval.Notice) {
	return func(ctx context.Context, n approval.Notice) {
		event, ok := requestEvents[n.Event]
		if !ok || n.Request.Kind != KindName {
			return
		}
		meta := map[string]string{
			"request_id":         n.Request.ID,
			"action":             n.Request.Action,
			"requested_by":       n.Request.RequestedBy,
			"payload_digest":     n.Request.PayloadDigest,
			"approvals_required": strconv.Itoa(n.Request.ApprovalsRequired),
		}
		if n.Request.SubjectID != "" {
			meta["subject_id"] = n.Request.SubjectID
		}
		if n.Request.ResultRef != "" {
			meta["result_ref"] = n.Request.ResultRef
		}
		if n.Event == approval.EventDecided {
			if ds := n.Request.Decisions; len(ds) > 0 {
				meta["decision"] = string(ds[len(ds)-1].Decision)
			}
		}
		sec.RecordShareChange(ctx, event, n.Actor, meta)
	}
}
