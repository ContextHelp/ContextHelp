// Package grpc implements the gRPC API server for dPKMS.
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// Server wraps a gRPC server with all registered services.
type Server struct {
	grpc   *grpc.Server
	addr   string
	health *health.Server
}

// Option configures the gRPC server at construction time.
type Option func(*serverOptions)

type serverOptions struct {
	auth          *authn.Chain
	security      *security.Emitter
	entitlements  *registry.InboundGate
	reflection    bool
	strictSharing bool
}

// WithStrictSharing makes the visibility backstop answer Internal on a
// hit instead of stripping it (dpkms serve --strict-sharing; every
// test instance).
func WithStrictSharing(strict bool) Option {
	return func(o *serverOptions) { o.strictSharing = strict }
}

// WithSecurity wires the security event emitter into the auth
// interceptors so failed authentications are recorded. A nil emitter is
// a no-op.
func WithSecurity(e *security.Emitter) Option {
	return func(o *serverOptions) { o.security = e }
}

// WithAuth guards every RPC (unary and stream) behind the configured
// authentication chain; grpc.health.v1 probes stay exempt. A nil chain
// is a no-op (private instance).
func WithAuth(c *authn.Chain) Option {
	return func(o *serverOptions) { o.auth = c }
}

// WithEntitlements gates the entity-serving RPCs behind per-principal
// namespace grants and metering quotas — the same InboundGate the HTTP
// router enforces, so the two transports can never drift. A nil gate is
// a no-op (private instance).
func WithEntitlements(g *registry.InboundGate) Option {
	return func(o *serverOptions) { o.entitlements = g }
}

// WithReflection toggles server reflection. Reflection is a discovery
// aid for grpcurl/tooling and stays enabled by default; non-private
// instances disable it to avoid advertising the API surface.
func WithReflection(enabled bool) Option {
	return func(o *serverOptions) { o.reflection = enabled }
}

// New creates a gRPC Server with all service handlers registered.
func New(addr string, svc *service.Service, opts ...Option) *Server {
	o := serverOptions{reflection: true}
	for _, opt := range opts {
		opt(&o)
	}

	unary := []grpc.UnaryServerInterceptor{recoveryInterceptor}
	var stream []grpc.StreamServerInterceptor
	if o.auth != nil {
		unary = append(unary, authUnaryInterceptor(o.auth, o.security, svc.Viewers()))
		stream = append(stream, authStreamInterceptor(o.auth, o.security, svc.Viewers()))
	} else {
		// Private instance: no principal, but every caller is the
		// owner and reads unscoped. Attached where the auth
		// interceptors would attach it, so no RPC runs without a viewer.
		unary = append(unary, viewerUnaryInterceptor(sharing.Unrestricted))
		stream = append(stream, viewerStreamInterceptor(sharing.Unrestricted))
	}

	// Visibility runs after the viewer is attached and before the
	// handler: it attaches the ledger, then checks the answer (ADR-080
	// §8). The owner passes it untouched.
	vis := visibilityConfig{strict: o.strictSharing, security: o.security}
	unary = append(unary, visibilityUnaryInterceptor(vis))
	stream = append(stream, visibilityStreamInterceptor(vis))

	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)

	// Register domain services.
	pb.RegisterAnalyzeServiceServer(gs, newAnalyzeHandler(svc))
	pb.RegisterJobServiceServer(gs, newJobHandler(svc))
	pb.RegisterQueryServiceServer(gs, newQueryHandler(svc))
	pb.RegisterEntityServiceServer(gs, newEntityHandler(svc, o.entitlements, o.security))

	// Register health service (grpc.health.v1).
	hs := health.NewServer()
	grpc_health_v1.RegisterHealthServer(gs, hs)
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	if o.reflection {
		reflection.Register(gs)
	}

	return &Server{grpc: gs, addr: addr, health: hs}
}

// Start listens on s.addr and blocks until ctx is cancelled or a fatal error occurs.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpc: listen %s: %w", s.addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on an already-bound listener and blocks until ctx is
// cancelled or a fatal error occurs. The server owns ln from here on.
// Callers that must hold the port from the moment they choose it bind
// first and pass the listener, rather than letting Start re-bind.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	slog.Info("grpc: listening", "addr", ln.Addr().String())

	// Stop when context cancelled.
	go func() {
		<-ctx.Done()
		s.health.Shutdown()
		s.grpc.GracefulStop()
	}()

	if err := s.grpc.Serve(ln); err != nil {
		return fmt.Errorf("grpc: serve: %w", err)
	}
	return nil
}

// recoveryInterceptor catches panics in handlers and returns an error.
func recoveryInterceptor(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("grpc: panic in handler", "panic", r)
			err = fmt.Errorf("internal server error")
		}
	}()
	return handler(ctx, req)
}
