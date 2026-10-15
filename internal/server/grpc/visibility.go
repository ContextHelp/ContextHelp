package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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
// declared shape fails closed.
func visibilityUnaryInterceptor(cfg visibilityConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, v, ok, err := cfg.viewAs(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		if err := authorizeCapability(ctx, info.FullMethod); err != nil {
			return nil, err
		}
		if ok && v.IsUnrestricted() {
			return handler(ctx, req)
		}
		ledger := sharing.NewLedger()
		ctx = sharing.WithLedger(ctx, ledger)
		resp, err := handler(ctx, req)
		cfg.audit.Record(sharing.AccessEntryFor(ctx, v, info.FullMethod, ledger, time.Now()))
		if err != nil {
			return resp, err
		}
		return cfg.check(ctx, info.FullMethod, v, ledger, resp)
	}
}

// visibilityStreamInterceptor mirrors visibilityUnaryInterceptor for
// streams: every message sent is checked against the stream's ledger.
func visibilityStreamInterceptor(cfg visibilityConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, v, ok, err := cfg.viewAs(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		if err := authorizeCapability(ctx, info.FullMethod); err != nil {
			return err
		}
		if ok && v.IsUnrestricted() {
			return handler(srv, &checkedStream{ServerStream: ss, ctx: ctx})
		}
		ledger := sharing.NewLedger()
		ctx = sharing.WithLedger(ctx, ledger)
		err = handler(srv, &checkedStream{
			ServerStream: ss, ctx: ctx,
			cfg: cfg, method: info.FullMethod, viewer: v, ledger: ledger,
		})
		cfg.audit.Record(sharing.AccessEntryFor(ctx, v, info.FullMethod, ledger, time.Now()))
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
func (cfg visibilityConfig) viewAs(ctx context.Context, fullMethod string) (context.Context, sharing.Viewer, bool, error) {
	v, ok := sharing.FromContext(ctx)
	target := ""
	if md, found := metadata.FromIncomingContext(ctx); found {
		if vals := md.Get(sharing.MetadataViewAs); len(vals) > 0 {
			target = strings.TrimSpace(vals[0])
		}
	}
	if target == "" {
		return ctx, v, ok, nil
	}
	if !ok || !v.IsUnrestricted() {
		return ctx, v, ok, status.Error(codes.PermissionDenied, "VIEW_AS_OWNER_ONLY: "+sharing.MetadataViewAs+" is for the owner")
	}
	if scope, known := methodScopes[fullMethod]; !known || scope.Verb() != "read" {
		return ctx, v, ok, status.Error(codes.InvalidArgument, "VIEW_AS_READ_ONLY: "+sharing.MetadataViewAs+" previews read methods only")
	}
	pv, err := cfg.viewers.ViewAs(ctx, target)
	switch {
	case errors.Is(err, sharing.ErrViewAsUnknown):
		return ctx, v, ok, status.Errorf(codes.NotFound, "no principal or share policy named %q", target)
	case err != nil:
		slog.Error("view-as: resolve failed", "target", target, "err", err)
		return ctx, v, ok, status.Error(codes.Internal, "view-as resolution failed")
	}
	p := sharing.Preview{Target: target, Principal: storage.OwnerPrincipal}
	if owner, found := authn.FromContext(ctx); found {
		p.Principal, p.Identity = owner.ID, owner.Meta[authn.MetaIdentityID]
	}
	return sharing.WithPreview(sharing.WithViewer(ctx, pv), p), pv, true, nil
}

// check runs the backstop over resp for fullMethod: the message is
// taken through protojson with proto names, walked and searched, and
// rebuilt from what passed.
func (cfg visibilityConfig) check(ctx context.Context, fullMethod string, v sharing.Viewer, ledger *sharing.Ledger, resp any) (any, error) {
	shape, known := MethodShape(fullMethod)
	if !known {
		slog.Error("visibility: method declares no response shape", "method", fullMethod)
		return nil, status.Error(codes.Internal, "method declares no response shape")
	}
	msg, ok := resp.(proto.Message)
	if !ok || msg == nil {
		// Not a message: nothing the walker can read. Fine while the
		// reads released nothing; a released row with no way to check
		// the answer fails closed.
		if ledger.Empty() {
			return resp, nil
		}
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	if shape == sharing.ShapeNone && ledger.Empty() {
		return resp, nil
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	res, err := sharing.Backstop(raw, sharing.GRPCPaths(shape), ledger)
	if err != nil {
		slog.Error("visibility: backstop cannot check the response", "method", fullMethod, "err", err)
		return nil, status.Error(codes.Internal, "response cannot be checked for visibility")
	}
	if len(res.Hits) > 0 {
		cfg.record(ctx, fullMethod, v, res.Hits)
		if cfg.strict {
			return nil, status.Error(codes.Internal, "VISIBILITY_BACKSTOP: the response carried data outside the viewer's visibility and was refused")
		}
	}
	if res.Absent {
		return nil, status.Error(codes.NotFound, "resource not found")
	}
	out := msg.ProtoReflect().New().Interface()
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(res.Body, out); err != nil {
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
