package grpc

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
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
// configured provider. Provider-agnostic: it only lifts credentials out
// of metadata and forwards them. Failures are recorded on the security
// emitter (nil-safe), keyed by peer address since no principal exists.
func authUnaryInterceptor(provider authn.Provider, sec *security.Emitter) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
			return handler(ctx, req)
		}
		princ, err := provider.Authenticate(ctx, credentialFromMD(ctx))
		if err != nil {
			recordAuthFailure(ctx, sec)
			return nil, unauthenticatedStatus(err)
		}
		return handler(authn.Attach(ctx, princ), req)
	}
}

// authStreamInterceptor mirrors authUnaryInterceptor for streaming RPCs
// (e.g. JobService.WatchJob).
func authStreamInterceptor(provider authn.Provider, sec *security.Emitter) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
			return handler(srv, ss)
		}
		princ, err := provider.Authenticate(ss.Context(), credentialFromMD(ss.Context()))
		if err != nil {
			recordAuthFailure(ss.Context(), sec)
			return unauthenticatedStatus(err)
		}
		return handler(srv, &authenticatedStream{
			ServerStream: ss,
			ctx:          authn.Attach(ss.Context(), princ),
		})
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

// unauthenticatedStatus maps provider errors to codes.Unauthenticated
// without leaking provider internals to the caller.
func unauthenticatedStatus(err error) error {
	if errors.Is(err, authn.ErrNoCredential) {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	return status.Error(codes.Unauthenticated, "invalid credentials")
}
