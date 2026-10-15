package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

const viewerTestMethod = "/dpkms.v1.QueryService/Search"

func validTokenMD(ctx context.Context) context.Context {
	return metadata.NewIncomingContext(ctx,
		metadata.Pairs("authorization", "Bearer tok-valid-fixture-0123456789abcdef0123456789"))
}

// An authenticated unary call sees the owner's viewer, attached in the
// same step as the principal.
func TestAuthUnaryAttachesUnrestrictedViewer(t *testing.T) {
	ic := authUnaryInterceptor(newTestChain(t), nil, nil)
	var seen sharing.Viewer
	var ok bool
	_, err := ic(validTokenMD(context.Background()), nil, unaryInfo(viewerTestMethod),
		func(hctx context.Context, _ any) (any, error) {
			seen, ok = sharing.FromContext(hctx)
			return "ok", nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if !ok || !seen.IsUnrestricted() {
		t.Fatalf("viewer = (%+v, %v), want (Unrestricted, true)", seen, ok)
	}
}

// A rejected unary call never reaches the handler, so no viewer leaks
// past a failed authentication.
func TestAuthUnaryRejectedAttachesNoViewer(t *testing.T) {
	ic := authUnaryInterceptor(newTestChain(t), nil, nil)
	ran := false
	_, err := ic(context.Background(), nil, unaryInfo(viewerTestMethod),
		func(context.Context, any) (any, error) { ran = true; return "ok", nil })
	if err == nil || ran {
		t.Fatalf("err = %v, handler ran = %v; want Unauthenticated and no handler", err, ran)
	}
}

// Streams get the same viewer as unary calls.
func TestAuthStreamAttachesUnrestrictedViewer(t *testing.T) {
	ic := authStreamInterceptor(newTestChain(t), nil, nil)
	var seen sharing.Viewer
	var ok bool
	err := ic(nil, &fakeServerStream{ctx: validTokenMD(context.Background())},
		&grpc.StreamServerInfo{FullMethod: viewerTestMethod},
		func(_ any, ss grpc.ServerStream) error {
			seen, ok = sharing.FromContext(ss.Context())
			return nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if !ok || !seen.IsUnrestricted() {
		t.Fatalf("viewer = (%+v, %v), want (Unrestricted, true)", seen, ok)
	}
}

// A private instance (no provider) installs the viewer interceptors
// instead of the auth ones; handlers see the owner's viewer and no
// principal.
func TestViewerUnaryInterceptorAttachesViewer(t *testing.T) {
	ic := viewerUnaryInterceptor(sharing.Unrestricted)
	var seen sharing.Viewer
	var ok bool
	resp, err := ic(context.Background(), "req", unaryInfo(viewerTestMethod),
		func(hctx context.Context, req any) (any, error) {
			seen, ok = sharing.FromContext(hctx)
			return req, nil
		})
	if err != nil || resp != "req" {
		t.Fatalf("interceptor: resp=%v err=%v", resp, err)
	}
	if !ok || !seen.IsUnrestricted() {
		t.Fatalf("viewer = (%+v, %v), want (Unrestricted, true)", seen, ok)
	}
}

func TestViewerStreamInterceptorAttachesViewer(t *testing.T) {
	ic := viewerStreamInterceptor(sharing.Unrestricted)
	var seen sharing.Viewer
	var ok bool
	err := ic(nil, &fakeServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: viewerTestMethod},
		func(_ any, ss grpc.ServerStream) error {
			seen, ok = sharing.FromContext(ss.Context())
			return nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if !ok || !seen.IsUnrestricted() {
		t.Fatalf("viewer = (%+v, %v), want (Unrestricted, true)", seen, ok)
	}
}
