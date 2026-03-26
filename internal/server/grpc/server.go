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

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Server wraps a gRPC server with all registered services.
type Server struct {
	grpc    *grpc.Server
	addr    string
	health  *health.Server
}

// New creates a gRPC Server with all service handlers registered.
func New(addr string, svc *service.Service) *Server {
	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(recoveryInterceptor),
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

	// Enable server reflection (useful for grpcurl / tooling).
	reflection.Register(gs)

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
