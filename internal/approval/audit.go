package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Events the mechanism appends to the audit log (ADR-082 §9), each in
// the transaction of the transition it records, so an entry exists if
// and only if its transition committed. Every entry names its actor,
// the request, its kind and action and the payload digest; none carries
// the payload or a secret. The change an approval applies is the
// kind's to record under its own event.
const (
	EventFiled     = "approval.request.filed"
	EventDecided   = "approval.decision.recorded"
	EventApproved  = "approval.request.approved"
	EventRejected  = "approval.request.rejected"
	EventWithdrawn = "approval.request.withdrawn"
	EventStale     = "approval.request.stale"
)

// record appends one entry for r by actor, in ctx's transaction. A
// failed append fails the transition: the record is part of it.
func (s *Service) record(ctx context.Context, event, actor string, r *storage.ApprovalRequest, at time.Time, extra map[string]any) error {
	payload := map[string]any{
		"request_id":         r.ID,
		"kind":               r.Kind,
		"action":             r.Action,
		"payload_digest":     r.PayloadDigest,
		"requested_by":       r.RequestedBy,
		"approvals_required": r.ApprovalsRequired,
	}
	if r.SubjectID != "" {
		payload["subject_id"] = r.SubjectID
	}
	for k, v := range extra {
		payload[k] = v
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: event,
		Actor:     actor,
		Payload:   payload,
		CreatedAt: at,
	}
	if err := s.audit.Append(ctx, entry); err != nil {
		return fmt.Errorf("audit %s: %w", event, err)
	}
	return nil
}

// approverIDs lists the approvers of r's approve decisions, in order.
func approverIDs(r *storage.ApprovalRequest) []string {
	out := []string{}
	for _, d := range r.Decisions {
		if d.Decision == storage.ApprovalVerdictApprove {
			out = append(out, d.ApproverID)
		}
	}
	return out
}
