package sharing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Check ids and reason codes of the steward audit (ADR-083 §5), beside
// the receipt's (receipt.go) and the steward_grant check's own codes
// (steward.go). Stable snake_case, never reworded.
const (
	// CheckStewardGrant: the steward grant the request named. Passed
	// with StewardReasonGrantActive; failed (or UNKNOWN when the store
	// could not be read) with the resolution's code otherwise.
	CheckStewardGrant = "steward_grant"
	// CheckAudit: the durable record of an elevated request. It appears
	// only when the record could not be committed: UNKNOWN,
	// ReasonAuditUnavailable, and the request was refused.
	CheckAudit = "audit"
	// ReasonAuditUnavailable: the entry could not be committed, so the
	// answer was withheld (CodeAuditUnavailable).
	ReasonAuditUnavailable = "audit_unavailable"
)

// CodeAuditUnavailable is the API error code (503 over HTTP,
// Unavailable over gRPC) of an elevated request whose audit entry
// could not be committed: the answer is withheld and nothing is
// disclosed (ADR-083 §5, option B).
const CodeAuditUnavailable = "AUDIT_UNAVAILABLE"

// ErrAuditUnavailable is Commit's error when there is no audit store to
// commit to.
var ErrAuditUnavailable = errors.New("read audit: no store to record the steward entry in")

// stewardCommitTimeout bounds one durable append. It is detached from
// the request's cancellation: a client that hangs up must not leave a
// released read unrecorded, and over-recording is the safe error.
const stewardCommitTimeout = 5 * time.Second

// Elevated reports whether the request named a steward grant, used or
// refused: its entry is durable and chained, committed before the
// answer is released.
func (r *Receipt) Elevated() bool { return r != nil && r.op.StewardGrant != "" }

// StewardRefused records the refusal of the elevation the request
// named (ADR-083 §3): the steward_grant check fails with the
// resolution's code and the request is denied, or the check is UNKNOWN
// and the request deferred when the cause was a store that could not
// be read. Nothing follows it in the trail: the request goes no further.
func (r *Receipt) StewardRefused(refusal *StewardRefusal) {
	if r == nil || refusal == nil {
		return
	}
	if refusal.Deferred {
		r.Defer(CheckStewardGrant, refusal.Reason)
		return
	}
	r.Refuse(CheckStewardGrant, refusal.Reason)
}

// AppendDurable writes one steward entry synchronously and chained
// (storage.AccessLogStore.AppendSteward) and returns once it is
// committed. A nil writer has no store: ErrAuditUnavailable.
func (w *AuditWriter) AppendDurable(ctx context.Context, e *storage.AccessEntry) error {
	if w == nil {
		return ErrAuditUnavailable
	}
	return w.store.AppendSteward(ctx, e)
}

// Commit records the request's entry, once. An elevated request's entry
// is written durably and chained before Commit returns (ADR-083 §5,
// option B): the transport calls Commit with the answer held back and
// releases the answer only when Commit returns nil. On an error the
// answer must be replaced by CodeAuditUnavailable and nothing
// disclosed; the receipt then records the audit check UNKNOWN and tries
// once to commit that deferred entry in its place, so a store that
// recovers keeps the trace of the refusal. The first error is returned
// either way.
//
// An ordinary request's entry goes to the buffer, as Finish does, and
// Commit returns nil. Later calls, and a Finish after a Commit, do
// nothing and return the first result.
func (r *Receipt) Commit(ctx context.Context, w *AuditWriter) error {
	if r == nil {
		return nil
	}
	// Once: concurrent callers wait for the first one and share its
	// result.
	r.commitOnce.Do(func() { r.commitErr = r.commit(ctx, w) })
	return r.commitErr
}

// commit is the one real Commit.
func (r *Receipt) commit(ctx context.Context, w *AuditWriter) error {
	r.mu.Lock()
	r.done = true
	e := r.entry()
	elevated := r.op.StewardGrant != ""
	r.mu.Unlock()
	if !elevated {
		if e != nil {
			w.Record(e)
		}
		return nil
	}
	if e == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stewardCommitTimeout)
	defer cancel()
	err := w.AppendDurable(ctx, e)
	if err == nil {
		return nil
	}
	slog.Error("steward audit: entry not committed, the answer is withheld",
		"route", e.Route, "principal", e.PrincipalID, "steward_grant", e.StewardGrantID, "err", err)
	if e.Decision != storage.AccessDecisionAllowed {
		// Already a refusal or a deferral: its entry would say the same,
		// so a store that is down is not asked twice.
		return err
	}
	r.mu.Lock()
	r.auditFailed = true
	r.add(CheckAudit, storage.AccessOutcomeUnknown, ReasonAuditUnavailable)
	deferred := r.entry()
	r.mu.Unlock()
	if derr := w.AppendDurable(ctx, deferred); derr != nil {
		slog.Error("steward audit: the withheld answer could not be recorded either",
			"route", e.Route, "principal", e.PrincipalID, "steward_grant", e.StewardGrantID, "err", derr)
	}
	return err
}
