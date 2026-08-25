package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
)

func newTestProvider(t *testing.T) authn.Provider {
	t.Helper()
	p, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-valid", Principal: "ops", Roles: []string{"admin"}},
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	return p
}

func unaryInfo(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

func TestAuthUnaryMissingCredential(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t))
	_, err := ic(context.Background(), nil, unaryInfo("/dpkms.v1.QueryService/Search"),
		func(context.Context, any) (any, error) { return "ok", nil })
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err: %v)", status.Code(err), err)
	}
}

func TestAuthUnaryInvalidToken(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t))
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer nope"))
	_, err := ic(ctx, nil, unaryInfo("/dpkms.v1.QueryService/Search"),
		func(context.Context, any) (any, error) { return "ok", nil })
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err: %v)", status.Code(err), err)
	}
}

func TestAuthUnaryValidTokenSetsPrincipal(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t))
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer tok-valid"))
	var seen *authn.Principal
	resp, err := ic(ctx, nil, unaryInfo("/dpkms.v1.QueryService/Search"),
		func(hctx context.Context, _ any) (any, error) {
			seen, _ = authn.FromContext(hctx)
			return "ok", nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("resp = %v", resp)
	}
	if seen == nil || seen.ID != "ops" {
		t.Fatalf("principal = %+v, want ops", seen)
	}
}

func TestAuthUnaryAPIKeyMetadata(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t))
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("x-api-key", "tok-valid"))
	_, err := ic(ctx, nil, unaryInfo("/dpkms.v1.QueryService/Search"),
		func(context.Context, any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
}

func TestAuthUnaryHealthExempt(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t))
	// No credentials at all — health checks must still pass.
	_, err := ic(context.Background(), nil, unaryInfo("/grpc.health.v1.Health/Check"),
		func(context.Context, any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("health must be exempt, got %v", err)
	}
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestAuthStreamMissingCredential(t *testing.T) {
	ic := authStreamInterceptor(newTestProvider(t))
	err := ic(nil, &fakeServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/dpkms.v1.JobService/WatchJob"},
		func(any, grpc.ServerStream) error { return nil })
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err: %v)", status.Code(err), err)
	}
}

func TestAuthStreamValidTokenSetsPrincipal(t *testing.T) {
	ic := authStreamInterceptor(newTestProvider(t))
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer tok-valid"))
	var seen *authn.Principal
	err := ic(nil, &fakeServerStream{ctx: ctx},
		&grpc.StreamServerInfo{FullMethod: "/dpkms.v1.JobService/WatchJob"},
		func(_ any, ss grpc.ServerStream) error {
			seen, _ = authn.FromContext(ss.Context())
			return nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if seen == nil || seen.ID != "ops" {
		t.Fatalf("principal = %+v, want ops", seen)
	}
}
