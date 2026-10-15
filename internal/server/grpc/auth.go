package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// Metadata keys carrying inbound credentials. gRPC metadata keys are
// lowercase by convention.
const (
	mdAuthorization = "authorization"
	mdAPIKey        = "x-api-key"
)

// healthMethodPrefix exempts grpc.health.v1 probes from authentication,
// mirroring the open /health endpoints on the HTTP surface.
const healthMethodPrefix = "/grpc.health.v1.Health/"

// authUnaryInterceptor authenticates every unary call through the
// method chain, then checks the method's scope (methodScopes). The
// capability check (authorizeCapability) runs in the visibility
// interceptor behind it, after an owner's view-as swap.
// Method-agnostic: it only lifts credentials out of metadata and
// forwards them; gRPC carries no cookie, so only carried methods can
// match. Authentication failures are recorded on the security emitter
// (nil-safe), keyed by peer address since no principal exists; scope
// denials are recorded against the principal.
func authUnaryInterceptor(chain *authn.Chain, sec *security.Emitter, viewers *sharing.Resolver) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
			return handler(ctx, req)
		}
		res, err := chain.Authenticate(ctx, credentialFromMD(ctx))
		if err != nil {
			recordAuthFailure(ctx, sec)
			return nil, unauthenticatedStatus(err)
		}
		ctx, err = attachCaller(ctx, res.Principal, viewers)
		if err != nil {
			return nil, err
		}
		if err := authorizeMethod(ctx, info.FullMethod, sec); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// attachCaller binds an authenticated principal and its viewer to ctx
// in one step, so no RPC reaches a handler authenticated but unscoped.
// The owner is Unrestricted; anyone else gets the viewer viewers
// compiles from its grants, under the identity that authenticated it
// (ADR-080 §4). A resolution failure is Internal and the RPC refused:
// a caller is never attached unscoped by mistake.
func attachCaller(ctx context.Context, princ *authn.Principal, viewers *sharing.Resolver) (context.Context, error) {
	v, err := viewers.Resolve(ctx, princ.ID, princ.Kind, princ.Meta[authn.MetaIdentityID])
	if err != nil {
		slog.Error("grpc auth: viewer resolution failed", "principal", princ.ID, "err", err)
		return nil, status.Error(codes.Internal, "viewer resolution failed")
	}
	return sharing.WithViewer(authn.Attach(ctx, princ), v), nil
}

// viewerUnaryInterceptor attaches v to every unary call. It is the
// private instance's counterpart of authUnaryInterceptor: with no
// provider there is no principal, every caller is the owner (ADR-079
// §1) and reads unscoped (sharing.Unrestricted).
func viewerUnaryInterceptor(v sharing.Viewer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(sharing.WithViewer(ctx, v), req)
	}
}

// viewerStreamInterceptor mirrors viewerUnaryInterceptor for streams.
func viewerStreamInterceptor(v sharing.Viewer) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &authenticatedStream{ServerStream: ss, ctx: sharing.WithViewer(ss.Context(), v)})
	}
}

// authStreamInterceptor mirrors authUnaryInterceptor for streaming RPCs
// (e.g. JobService.WatchJob).
func authStreamInterceptor(chain *authn.Chain, sec *security.Emitter, viewers *sharing.Resolver) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
			return handler(srv, ss)
		}
		res, err := chain.Authenticate(ss.Context(), credentialFromMD(ss.Context()))
		if err != nil {
			recordAuthFailure(ss.Context(), sec)
			return unauthenticatedStatus(err)
		}
		ctx, err := attachCaller(ss.Context(), res.Principal, viewers)
		if err != nil {
			return err
		}
		if err := authorizeMethod(ctx, info.FullMethod, sec); err != nil {
			return err
		}
		return handler(srv, &authenticatedStream{ServerStream: ss, ctx: ctx})
	}
}

// recordAuthFailure emits a security event for a failed authentication,
// keyed by the peer address (per-source sliding window). nil-safe.
func recordAuthFailure(ctx context.Context, sec *security.Emitter) {
	if sec == nil {
		return
	}
	source := "unknown"
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		source = p.Addr.String()
	}
	sec.RecordAuthFailure(ctx, source)
}

// authenticatedStream overrides Context so stream handlers see the
// principal-carrying context.
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context { return s.ctx }

// credentialFromMD lifts credentials out of incoming metadata:
// authorization: Bearer <token> first, x-api-key fallback.
func credentialFromMD(ctx context.Context) authn.Credential {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authn.Credential{}
	}
	if vals := md.Get(mdAuthorization); len(vals) > 0 {
		h := vals[0]
		if len(h) >= len("Bearer ") && strings.EqualFold(h[:len("Bearer ")], "Bearer ") {
			return authn.Credential{
				Scheme: authn.SchemeBearer,
				Token:  strings.TrimSpace(h[len("Bearer "):]),
			}
		}
	}
	if vals := md.Get(mdAPIKey); len(vals) > 0 && vals[0] != "" {
		return authn.Credential{Scheme: authn.SchemeAPIKey, Token: vals[0]}
	}
	return authn.Credential{}
}

// unauthenticatedStatus maps chain errors to codes.Unauthenticated
// without leaking method internals to the caller.
func unauthenticatedStatus(err error) error {
	if errors.Is(err, authn.ErrNoCredential) {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	return status.Error(codes.Unauthenticated, "invalid credentials")
}
