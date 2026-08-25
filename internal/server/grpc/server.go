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
	"github.com/ideacrafterslabs/ctxt/internal/security"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Server wraps a gRPC server with all registered services.
type Server struct {
	grpc    *grpc.Server
	addr    string
	health  *health.Server
}

// Option configures the gRPC server at construction time.
type Option func(*serverOptions)

type serverOptions struct {
	auth       authn.Provider
	security   *security.Emitter
	reflection bool
}

// WithSecurity wires the security event emitter into the auth
// interceptors so failed authentications are recorded. A nil emitter is
// a no-op.
func WithSecurity(e *security.Emitter) Option {
	return func(o *serverOptions) { o.security = e }
}

// WithAuth guards every RPC (unary and stream) behind the configured
// authentication provider; grpc.health.v1 probes stay exempt. A nil
// provider is a no-op (private instance).
func WithAuth(p authn.Provider) Option {
	return func(o *serverOptions) { o.auth = p }
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
		unary = append(unary, authUnaryInterceptor(o.auth, o.security))
		stream = append(stream, authStreamInterceptor(o.auth, o.security))
	}

	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)

	// Register domain services.
	pb.RegisterAnalyzeServiceServer(gs, newAnalyzeHandler(svc))
	pb.RegisterJobServiceServer(gs, newJobHandler(svc))
	pb.RegisterQueryServiceServer(gs, newQueryHandler(svc))
	pb.RegisterEntityServiceServer(gs, newEntityHandler(svc))

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
	slog.Info("grpc: listening", "addr", s.addr)

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
