package grpc

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// fixedProvider authenticates any presented credential as one fixed
// principal.
type fixedProvider struct{ p *authn.Principal }

func (fixedProvider) Name() string { return "fixed" }

func (f fixedProvider) Authenticate(_ context.Context, cred authn.Credential) (*authn.Principal, error) {
	if cred.Empty() {
		return nil, authn.ErrNoCredential
	}
	out := *f.p
	return &out, nil
}

// registeredMethods lists every method the server registers, reflection
// included, as full method names, with whether each is a stream.
func registeredMethods(t *testing.T) map[string]bool {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	s := New("127.0.0.1:0", svc,
		WithAuth(fixedProvider{p: &authn.Principal{ID: "none"}}),
		WithReflection(true),
	)
	out := map[string]bool{}
	for name, info := range s.grpc.GetServiceInfo() {
		for _, m := range info.Methods {
			out["/"+name+"/"+m.Name] = m.IsClientStream || m.IsServerStream
		}
	}
	if len(out) == 0 {
		t.Fatal("server registers no methods")
	}
	return out
}

// Every registered method except grpc.health.v1 must declare a known
// scope, and the table must not name methods nobody serves.
func TestMethodCoverageEveryMethodDeclaresAScope(t *testing.T) {
	methods := registeredMethods(t)
	for m := range methods {
		if strings.HasPrefix(m, healthMethodPrefix) {
			continue
		}
		scope, ok := methodScopes[m]
		if !ok {
			t.Errorf("%s is registered but missing from methodScopes", m)
			continue
		}
		if !slices.Contains(authn.AllScopes, scope) {
			t.Errorf("%s declares unknown scope %q", m, scope)
		}
	}
	for m := range methodScopes {
		if _, ok := methods[m]; !ok {
			t.Errorf("methodScopes names %s, which the server does not register", m)
		}
	}
}

// Behavioral half: a principal holding no scope is refused by every
// registered method (unary and stream) with PermissionDenied naming the
// scope; the handler never runs.
func TestMethodCoverageScopelessPrincipalDeniedEverywhere(t *testing.T) {
	provider := fixedProvider{p: &authn.Principal{ID: "none"}}
	unary := authUnaryInterceptor(provider, nil)
	stream := authStreamInterceptor(provider, nil)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer any"))

	for m, isStream := range registeredMethods(t) {
		if strings.HasPrefix(m, healthMethodPrefix) {
			continue
		}
		ran := false
		var err error
		if isStream {
			err = stream(nil, &fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: m},
				func(any, grpc.ServerStream) error { ran = true; return nil })
		} else {
			_, err = unary(ctx, nil, unaryInfo(m),
				func(context.Context, any) (any, error) { ran = true; return "ok", nil })
		}
		if ran {
			t.Errorf("%s: handler ran for a scopeless principal", m)
		}
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s: code = %v, want PermissionDenied (err: %v)", m, status.Code(err), err)
			continue
		}
		if !strings.Contains(err.Error(), string(methodScopes[m])) {
			t.Errorf("%s: error %q does not name scope %s", m, err, methodScopes[m])
		}
	}
}

func TestUnknownMethodIsRefused(t *testing.T) {
	ic := authUnaryInterceptor(newTestProvider(t), nil)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer tok-valid"))
	_, err := ic(ctx, nil, unaryInfo("/dpkms.v1.Unlisted/Method"),
		func(context.Context, any) (any, error) { return "ok", nil })
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied for a method with no scope", status.Code(err))
	}
}
