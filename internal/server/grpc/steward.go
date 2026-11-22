package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
)

// auditUnavailable is the answer of an elevated call whose entry could
// not be committed (ADR-083 §5, option B): Unavailable with the code,
// and nothing of the answer.
func auditUnavailable() error {
	return status.Error(codes.Unavailable, sharing.CodeAuditUnavailable+": the request could not be recorded; nothing was released")
}

// refuseElevation answers a call whose steward grant failed a check
// (ADR-083 §3, §5): the refusal is a read audit entry committed durably
// before the status leaves, then one security.steward.elevation.refused
// event. gRPC carries no browser session, so there is no elevation to
// end. If the entry cannot be committed the call is answered
// AUDIT_UNAVAILABLE instead, as every elevated call whose record
// failed, and the event says the refusal was deferred. req is the
// request message of a unary call, nil for a stream.
func refuseElevation(ctx context.Context, fullMethod string, req any, princ *authn.Principal, refusal *sharing.StewardRefusal, sec *security.Emitter, audit *sharing.AuditWriter) error {
	op := sharing.Operation{
		Route: fullMethod, Params: protoParams(req),
		Principal: princ.ID, Identity: princ.Meta[authn.MetaIdentityID],
		StewardGrant: princ.Meta[authn.MetaStewardGrantID],
	}
	if princ.Provider != "" && princ.Provider != "none" {
		op.AuthMethod = princ.Provider
	}
	rcpt := sharing.NewReceipt(op)
	rcpt.StewardRefused(refusal)
	committed := rcpt.Commit(ctx, audit) == nil
	steward.RecordElevationRefused(ctx, sec, princ.ID, op.StewardGrant, refusal.Reason, refusal.Deferred || !committed)
	if !committed {
		return auditUnavailable()
	}
	return stewardRefusalStatus(refusal)
}

// commitElevated commits an elevated call's entry before its answer
// leaves: the answer stands when the entry is committed, and is
// replaced by AUDIT_UNAVAILABLE when it is not. A call that is not
// elevated passes as it is; its entry is finished on the way out.
func commitElevated(ctx context.Context, rcpt *sharing.Receipt, audit *sharing.AuditWriter, resp any, err error) (any, error) {
	if !rcpt.Elevated() {
		return resp, err
	}
	if cerr := rcpt.Commit(ctx, audit); cerr != nil {
		return nil, auditUnavailable()
	}
	return resp, err
}

// refuseElevatedStream closes a stream to an elevated call (ADR-083 §3,
// §4: no streams for stewards in v1). A stream answers message by
// message, so no entry could be committed before its data leaves; the
// refusal itself is committed before the status does.
func refuseElevatedStream(ctx context.Context, rcpt *sharing.Receipt, audit *sharing.AuditWriter) error {
	rcpt.Refuse(sharing.CheckRouteAccess, sharing.ReasonStewardNoStream)
	if err := rcpt.Commit(ctx, audit); err != nil {
		return auditUnavailable()
	}
	return status.Error(codes.PermissionDenied, "NOT_SHARED: streams are not open to a steward elevation")
}
