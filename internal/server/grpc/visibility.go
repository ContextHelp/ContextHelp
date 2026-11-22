package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// visibilityConfig is what the visibility interceptors need beyond the
// method table.
type visibilityConfig struct {
	strict   bool
	security *security.Emitter
	audit    *sharing.AuditWriter
	viewers  *sharing.Resolver
}

// visibilityUnaryInterceptor runs behind the auth interceptor: it
// applies an owner's view-as swap, then the capability check against
// the viewer that will answer (ADR-080 §4), and for a restricted
// viewer attaches the request's ledger ahead of the handler and checks
// the answer against it after (§8), through the method's declared
// shape, stripping (or, strictly, refusing) anything the ledger does
// not hold. An unrestricted viewer passes untouched. A method with no
// declared shape fails closed. A restricted caller's call (or the
// owner's preview) carries a sharing.Receipt that every check appends
// to and that is finished once on every way out, early refusals
// included (§9).
func visibilityUnaryInterceptor(cfg visibilityConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// One receipt per audited call, finished once on every way out,
		// early refusals included (ADR-080 §9).
		if !audited(ctx) {
			return cfg.serveUnary(ctx, req, info, handler)
		}
		ctx, rcpt := ensureReceipt(ctx, info.FullMethod, req)
		// A panic below still leaves the entry; after a commit this is
		// a no-op.
		defer func() { rcpt.Finish(cfg.audit) }()
		resp, err := cfg.serveUnary(ctx, req, info, handler)
		return commitElevated(ctx, rcpt, cfg.audit, resp, err)
	}
}

// serveUnary runs a unary call behind the view-as swap, the capability
// check and, for a restricted viewer, the ledger and the backstop.
func (cfg visibilityConfig) serveUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	restricted := callerRestricted(ctx)
	ctx, v, ok, err := cfg.viewAs(ctx, info.FullMethod, restricted)
	if err != nil {
		return nil, err
	}
	ctx, err = authorizeCapability(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	if ok && v.IsUnrestricted() {
		// The owner previewing the owner is the owner: not logged.
		sharing.ReceiptFrom(ctx).Discard()
		return handler(ctx, req)
	}
	ledger := sharing.NewLedger()
	sharing.ReceiptFrom(ctx).SetLedger(ledger)
	ctx = sharing.WithLedger(ctx, ledger)
	resp, err := handler(ctx, req)
	sharing.ReceiptFrom(ctx).SettlePolicy()
	if err != nil {
		return resp, err
	}
	return cfg.check(ctx, info.FullMethod, v, ledger, resp)
}

// visibilityStreamInterceptor mirrors visibilityUnaryInterceptor for
// streams: every message sent is checked against the stream's ledger.
func visibilityStreamInterceptor(cfg visibilityConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		restricted := callerRestricted(ctx)
		if audited(ctx) {
			var rcpt *sharing.Receipt
			ctx, rcpt = ensureReceipt(ctx, info.FullMethod, nil)
			defer func() { rcpt.Finish(cfg.audit) }()
			if rcpt.Elevated() {
				return refuseElevatedStream(ctx, rcpt, cfg.audit)
			}
		}
		ctx, v, ok, err := cfg.viewAs(ctx, info.FullMethod, restricted)
		if err != nil {
			return err
		}
		ctx, err = authorizeCapability(ctx, info.FullMethod)
		if err != nil {
			return err
		}
		if ok && v.IsUnrestricted() {
			sharing.ReceiptFrom(ctx).Discard()
			return handler(srv, &checkedStream{ServerStream: ss, ctx: ctx})
		}
		ledger := sharing.NewLedger()
		sharing.ReceiptFrom(ctx).SetLedger(ledger)
		ctx = sharing.WithLedger(ctx, ledger)
		err = handler(srv, &checkedStream{
			ServerStream: ss, ctx: ctx,
			cfg: cfg, method: info.FullMethod, viewer: v, ledger: ledger,
		})
		// The stream's receipt is written when it ends, stamped with when
		// it began; a process that dies mid-stream writes none.
		sharing.ReceiptFrom(ctx).SettleStream()
		return err
	}
}

// checkedStream checks every message a stream handler sends.
type checkedStream struct {
	grpc.ServerStream
	ctx    context.Context
	cfg    visibilityConfig
	method string
	viewer sharing.Viewer
	ledger *sharing.Ledger
}

func (s *checkedStream) Context() context.Context { return s.ctx }

func (s *checkedStream) SendMsg(m any) error {
	if s.ledger == nil {
		return s.ServerStream.SendMsg(m)
	}
	out, err := s.cfg.check(s.ctx, s.method, s.viewer, s.ledger, m)
	if err != nil {
		return err
	}
	return s.ServerStream.SendMsg(out)
}

// viewAs applies the x-ctxt-view-as metadata of the owner's call
// (ADR-080 §2): the viewer of the named principal or policy replaces
// the owner's, and the call is marked a preview for the audit. It
// returns the viewer on ctx, whether one is attached, and the refusal
// of a non-owner sender (PermissionDenied), a method that writes
// (InvalidArgument) or an unknown target (NotFound).
func (cfg visibilityConfig) viewAs(ctx context.Context, fullMethod string, restricted bool) (context.Context, sharing.Viewer, bool, error) {
	v, ok := sharing.FromContext(ctx)
	target := viewAsTarget(ctx)
	if target == "" {
		return ctx, v, ok, nil
	}
	rcpt := sharing.ReceiptFrom(ctx)
	// refuse ends the call: a restricted sender's attempt is an entry
	// that failed view_as; the owner's own refused preview is not logged.
	refuse := func(reason string, err error) (context.Context, sharing.Viewer, bool, error) {
		if restricted {
			rcpt.Refuse(sharing.CheckViewAs, reason)
		} else {
			rcpt.Discard()
		}
		return ctx, v, ok, err
	}
	if !ok || !v.IsUnrestricted() {
		return refuse(sharing.ReasonOwnerOnly, status.Error(codes.PermissionDenied, "VIEW_AS_OWNER_ONLY: "+sharing.MetadataViewAs+" is for the owner"))
	}
	if scope, known := methodScopes[fullMethod]; !known || scope.Verb() != "read" {
		return refuse(sharing.ReasonViewAsReadOnly, status.Error(codes.InvalidArgument, "VIEW_AS_READ_ONLY: "+sharing.MetadataViewAs+" previews read methods only"))
	}
	pv, err := cfg.viewers.ViewAs(ctx, target)
	switch {
	case errors.Is(err, sharing.ErrViewAsUnknown):
		return refuse(sharing.ReasonViewAsUnknownTarget, status.Errorf(codes.NotFound, "no principal or share policy named %q", target))
	case err != nil:
		slog.Error("view-as: resolve failed", "target", target, "err", err)
		return refuse(sharing.ReasonViewAsResolutionFailed, status.Error(codes.Internal, "view-as resolution failed"))
	}
	p := sharing.Preview{Target: target, Principal: storage.OwnerPrincipal}
	if owner, found := authn.FromContext(ctx); found {
		p.Principal, p.Identity = owner.ID, owner.Meta[authn.MetaIdentityID]
	}
	rcpt.SetPreview(p, pv)
	return sharing.WithPreview(sharing.WithViewer(ctx, pv), p), pv, true, nil
}

// check runs the backstop over resp for fullMethod: the message is
// taken through protojson with proto names, walked and searched, and
// rebuilt from what passed.
func (cfg visibilityConfig) check(ctx context.Context, fullMethod string, v sharing.Viewer, ledger *sharing.Ledger, resp any) (any, error) {
	rcpt := sharing.ReceiptFrom(ctx)
	shape, known := MethodShape(fullMethod)
	if !known {
		slog.Error("visibility: method declares no response shape", "method", fullMethod)
		rcpt.Refuse(sharing.CheckRouteAccess, sharing.ReasonRouteUnclassified)
		return nil, status.Error(codes.Internal, "method declares no response shape")
	}
	msg, ok := resp.(proto.Message)
	if !ok || msg == nil {
		// Not a message: nothing the walker can read. Fine while the
		// reads released nothing; a released row with no way to check
		// the answer fails closed.
		if ledger.Empty() {
			rcpt.NotApplicable(sharing.CheckBackstop, sharing.ReasonNothingReleased)
			return resp, nil
		}
		rcpt.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	if shape == sharing.ShapeNone && ledger.Empty() {
		rcpt.NotApplicable(sharing.CheckBackstop, sharing.ReasonNothingReleased)
		return resp, nil
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		rcpt.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	res, err := sharing.Backstop(raw, sharing.GRPCPaths(shape), ledger)
	if err != nil {
		slog.Error("visibility: backstop cannot check the response", "method", fullMethod, "err", err)
		rcpt.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	if len(res.Hits) > 0 {
		cfg.record(ctx, fullMethod, v, res.Hits)
		// An elevated request is strict whatever the instance says
		// (ADR-083 §3).
		if cfg.strict || v.StrictBackstop() {
			rcpt.Refuse(sharing.CheckBackstop, sharing.ReasonHitRefused)
			return nil, status.Error(codes.Internal, "VISIBILITY_BACKSTOP: the response carried data outside the viewer's visibility and was refused")
		}
	}
	if len(res.Hits) > 0 || res.Absent {
		rcpt.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped)
	} else {
		rcpt.Pass(sharing.CheckBackstop, sharing.ReasonClean)
	}
	if res.Absent {
		return nil, status.Error(codes.NotFound, "resource not found")
	}
	out := msg.ProtoReflect().New().Interface()
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(res.Body, out); err != nil {
		rcpt.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		return nil, status.Error(codes.Internal, "response cannot be rebuilt after the visibility check")
	}
	return out, nil
}

// record logs and emits the security.visibility_backstop event.
func (cfg visibilityConfig) record(ctx context.Context, fullMethod string, v sharing.Viewer, hits []string) {
	principal := ""
	if p, ok := authn.FromContext(ctx); ok {
		principal = p.ID
	}
	slog.Warn(security.EventVisibilityBackstop, "method", fullMethod, "principal", principal, "policy_versions", v.PolicyKey(), "hits", hits)
	if cfg.security != nil {
		cfg.security.RecordVisibilityBackstop(ctx, principal, map[string]string{
			"route": fullMethod, "policy_versions": v.PolicyKey(), "hits": strconv.Itoa(len(hits)), "first_hit": hits[0],
		})
	}
}
