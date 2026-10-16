package grpc

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The access table names exactly the methods of the scope table.
func TestMethodAccessTableCoversEveryMethod(t *testing.T) {
	for m := range methodScopes {
		if _, _, known := MethodAccess(m); !known {
			t.Errorf("%s has no entry in methodAccessTable", m)
		}
	}
	for m := range methodAccessTable {
		if _, ok := methodScopes[m]; !ok {
			t.Errorf("methodAccessTable names %s, which methodScopes does not", m)
		}
	}
}

const (
	capOwnerToken  = "tok-owner-fixture-0123456789abcdef0123456789"
	capPersonToken = "tok-person-fixture-0123456789abcdef012345678"
)

func capChain() *authn.Chain {
	return authtest.TokenChain(
		authtest.Token{Token: capOwnerToken, Principal: storage.OwnerPrincipal, Roles: []string{authn.RoleAdmin}},
		authtest.Token{Token: capPersonToken, Principal: "prn_person", Roles: []string{authn.RoleReader}},
	)
}

func bearer(token string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

// A non-owner principal is refused the owner-only and capability-gated
// methods with PermissionDenied NOT_SHARED before the handler runs,
// reaches the open ones, and the owner reaches everything.
func TestInterceptorsCloseCapabilityGatedMethods(t *testing.T) {
	// The capability check runs in the visibility interceptor, behind
	// auth, so the two are chained as the server chains them.
	unary := chainUnary(authUnaryInterceptor(capChain(), nil, nil), visibilityUnaryInterceptor(visibilityConfig{}))
	stream := chainStream(authStreamInterceptor(capChain(), nil, nil), visibilityStreamInterceptor(visibilityConfig{}))
	call := func(token, method string) (bool, error) {
		ran := false
		_, err := unary(bearer(token), nil, unaryInfo(method), func(context.Context, any) (any, error) { ran = true; return "ok", nil })
		return ran, err
	}
	for _, m := range []string{pb.JobService_GetJob_FullMethodName, pb.EntityService_GetEntity_FullMethodName, pb.QueryService_Search_FullMethodName} {
		ran, err := call(capPersonToken, m)
		st, _ := status.FromError(err)
		if ran || st.Code() != codes.PermissionDenied || !strings.Contains(st.Message(), "NOT_SHARED") {
			t.Errorf("person %s: ran=%v err=%v, want PermissionDenied NOT_SHARED before the handler", m, ran, err)
		}
		if ran, err := call(capOwnerToken, m); !ran || err != nil {
			t.Errorf("owner %s: ran=%v err=%v, want the handler", m, ran, err)
		}
	}
	for _, m := range []string{pb.QueryService_ListObjects_FullMethodName, pb.QueryService_GetObject_FullMethodName} {
		if ran, err := call(capPersonToken, m); !ran || err != nil {
			t.Errorf("person %s: ran=%v err=%v, want the handler (open route)", m, ran, err)
		}
	}

	ran := false
	err := stream(nil, &fakeServerStream{ctx: bearer(capPersonToken)},
		&grpc.StreamServerInfo{FullMethod: pb.JobService_WatchJob_FullMethodName},
		func(any, grpc.ServerStream) error { ran = true; return nil })
	if st, _ := status.FromError(err); ran || st.Code() != codes.PermissionDenied {
		t.Errorf("person WatchJob: ran=%v err=%v, want PermissionDenied", ran, err)
	}
}

// chainUnary runs a, then b, then the handler.
func chainUnary(a, b grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return a(ctx, req, info, func(ctx context.Context, req any) (any, error) { return b(ctx, req, info, handler) })
	}
}

// chainStream runs a, then b, then the handler.
func chainStream(a, b grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return a(srv, ss, info, func(srv any, ss grpc.ServerStream) error { return b(srv, ss, info, handler) })
	}
}
