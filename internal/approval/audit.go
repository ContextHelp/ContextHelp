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
	if c, ok := ctx.Value(noticesKey{}).(*notices); ok && c != nil {
		c.list = append(c.list, Notice{Event: event, Actor: actor, Request: *r, At: at})
	}
	return nil
}

// Notice is one event the mechanism recorded, reported to
// Options.Notify once its transaction returned (Options.Notify).
type Notice struct {
	// Event is the audit event (EventFiled, EventDecided, ...).
	Event string
	// Actor is who triggered it: the requester, the approver, the
	// withdrawer.
	Actor string
	// Request is the request as it stood when the event was recorded:
	// its status is the transition's target.
	Request storage.ApprovalRequest
	At      time.Time
}

// noticesKey carries the notices one call collects.
type noticesKey struct{}

type notices struct{ list []Notice }

// collect returns ctx carrying a fresh collector for one call's
// notices, when the service reports any.
func (s *Service) collect(ctx context.Context) (context.Context, *notices) {
	if s.notify == nil {
		return ctx, nil
	}
	// A call made inside another's collection (Collect) adds to that
	// collector, which reports once its own transaction has committed.
	if c, ok := ctx.Value(noticesKey{}).(*notices); ok && c != nil {
		return ctx, nil
	}
	c := &notices{}
	return context.WithValue(ctx, noticesKey{}, c), c
}

// Collect makes the notices of the Service calls made with the returned
// context wait for the returned function, which reports them: call it
// once the transaction that encloses those calls has committed, and
// never when it rolled back. A caller that files a request inside a
// transaction of its own uses this, so no notice leaves for a change
// that did not commit.
func (s *Service) Collect(ctx context.Context) (context.Context, func()) {
	ctx, c := s.collect(ctx)
	return ctx, func() { s.flush(ctx, c) }
}

// flush reports c's notices in order; the call's transaction returned
// without error. The result is already committed, so a hook that
// panics is logged and skipped: it never fails the call nor stops the
// notices after it.
func (s *Service) flush(ctx context.Context, c *notices) {
	if c == nil {
		return
	}
	for _, n := range c.list {
		s.notifyOne(ctx, n)
	}
}

// notifyOne reports n, recovering a panic of the hook.
func (s *Service) notifyOne(ctx context.Context, n Notice) {
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "approval: notify hook panicked", "event", n.Event, "request", n.Request.ID, "panic", fmt.Sprint(p))
		}
	}()
	s.notify(ctx, n)
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
	// The refusal is no transition: it reports nothing and collects
	// nothing into the call's notices, which are never flushed here.
	ctx = context.WithValue(ctx, noticesKey{}, (*notices)(nil))
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
