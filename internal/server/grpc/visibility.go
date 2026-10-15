package grpc

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// visibilityConfig is what the visibility interceptors need beyond the
// method table.
type visibilityConfig struct {
	strict   bool
	security *security.Emitter
	audit    *sharing.AuditWriter
}

// visibilityUnaryInterceptor wraps a restricted viewer's call (ADR-080
// §8): it attaches the request's ledger ahead of the handler and
// checks the answer against it after, through the method's declared
// shape, stripping (or, strictly, refusing) anything the ledger does
// not hold. An unrestricted viewer passes untouched. A method with no
// declared shape fails closed.
func visibilityUnaryInterceptor(cfg visibilityConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		v, ok := sharing.FromContext(ctx)
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
		v, ok := sharing.FromContext(ss.Context())
		if ok && v.IsUnrestricted() {
			return handler(srv, ss)
		}
		ledger := sharing.NewLedger()
		ctx := sharing.WithLedger(ss.Context(), ledger)
		err := handler(srv, &checkedStream{
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
	out, err := s.cfg.check(s.ctx, s.method, s.viewer, s.ledger, m)
	if err != nil {
		return err
	}
	return s.ServerStream.SendMsg(out)
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
		if shape == sharing.ShapeNone {
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
