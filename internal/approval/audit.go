package approval

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Events the mechanism appends to the audit log (ADR-083 §9), each in
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
	// EventRefused records a decision the mechanism refused on a request
	// the decider could see: the requester, a beneficiary or an ineligible
	// principal deciding, or a digest other than the frozen one. It is
	// written after the refused transaction rolled back, as a security
	// row (event_class=security) like the security emitter's, so a
	// refused attempt leaves a trace; the request does not change.
	EventRefused = "approval.decision.refused"
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

// refusedCodes are the refusals recorded as EventRefused.
var refusedCodes = map[string]bool{
	CodeRequesterCannotDecide:   true,
	CodeBeneficiaryCannotDecide: true,
	CodeNotEligible:             true,
	CodeDigestMismatch:          true,
}

// recordRefusal appends the EventRefused entry for a decision of r by
// approver that err refused, outside the rolled-back transaction. Only
// the separation and digest refusals are recorded, and only for a
// request the approver could see: an invisible request is not found, and
// recording it would confirm it exists. Best effort, like the security
// emitter's rows: a failed append is logged, never masks the refusal.
func (s *Service) recordRefusal(ctx context.Context, r *storage.ApprovalRequest, approver *auth.Principal, err error) {
	code := CodeOf(err)
	if r == nil || !refusedCodes[code] {
		return
	}
	at := s.clock()
	entry := &storage.AuditEntry{
		ID: uuid.New().String(), EventType: EventRefused, Actor: approver.ID, CreatedAt: at,
		Payload: map[string]any{
			"event_class": "security", "code": code,
			"request_id": r.ID, "kind": r.Kind, "action": r.Action,
			"payload_digest": r.PayloadDigest, "requested_by": r.RequestedBy,
		},
	}
	if aerr := s.audit.Append(ctx, entry); aerr != nil {
		slog.WarnContext(ctx, "approval: audit append failed", "event", EventRefused, "err", aerr)
	}
}
