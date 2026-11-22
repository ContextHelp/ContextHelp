package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stewardRig is a store with a steward and a steward grant, and a
// resolver wired for steward grants.
type stewardRig struct {
	drv     storage.StorageDriver
	steward string
	grant   *storage.StewardGrant
	viewers *sharing.Resolver
	now     time.Time
}

func newStewardRig(t *testing.T) *stewardRig {
	t.Helper()
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	r := &stewardRig{drv: storageutil.NewTestDriver(t), steward: "prn_steward", now: t0.Add(time.Hour)}
	sharingtest.EnsurePrincipal(t, r.drv, r.steward, storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	r.grant = sharingtest.GrantSteward(t, r.drv, r.steward, `{"policies":[{"data":"type==contact"}]}`, t0, t0.Add(48*time.Hour))
	settings := sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour}
	r.viewers = sharing.NewResolver(r.drv.Sharing()).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return settings }))
	return r
}

func (r *stewardRig) chain(meta map[string]string) *authn.Chain {
	p := &authn.Principal{
		ID: r.steward, Kind: storage.PrincipalKindPerson, Provider: authn.MethodAPIKey,
		Roles: []string{authn.RoleReader, authn.RoleStewardRequester}, Meta: meta,
	}
	p.Scopes = authn.ScopesForRoles(p.Roles)
	return authtest.Fixed(p)
}

func anyTokenMD() context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer anything"))
}

// A principal whose Meta names a steward grant gets the elevated
// viewer; without it, its ordinary view.
func TestAuthUnaryResolvesAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)
	run := func(meta map[string]string) sharing.Viewer {
		t.Helper()
		var seen sharing.Viewer
		ic := authUnaryInterceptor(r.chain(meta), nil, r.viewers, nil)
		_, err := ic(anyTokenMD(), nil, unaryInfo(pb.QueryService_ListObjects_FullMethodName),
			func(hctx context.Context, _ any) (any, error) {
				seen, _ = sharing.FromContext(hctx)
				return "ok", nil
			})
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		return seen
	}
	if v := run(nil); v.StewardGrantID() != "" || v.StrictBackstop() {
		t.Errorf("no grant named: viewer elevated to %q", v.StewardGrantID())
	}
	v := run(map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	if v.StewardGrantID() != r.grant.ID || !v.StrictBackstop() {
		t.Errorf("viewer = elevated to %q strict %v, want %s strict", v.StewardGrantID(), v.StrictBackstop(), r.grant.ID)
	}
}

// A grant that fails a check refuses the call and the handler never
// runs: permission denied when the answer is no, unavailable when a
// store could not be read.
func TestAuthUnaryRefusesAFailedElevation(t *testing.T) {
	r := newStewardRig(t)
	named := map[string]string{authn.MetaStewardGrantID: r.grant.ID}
	call := func(viewers *sharing.Resolver) (codes.Code, bool, error) {
		ran := false
		ic := authUnaryInterceptor(r.chain(named), nil, viewers, nil)
		_, err := ic(anyTokenMD(), nil, unaryInfo(pb.QueryService_ListObjects_FullMethodName),
			func(context.Context, any) (any, error) { ran = true; return "ok", nil })
		return status.Code(err), ran, err
	}

	if err := r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now); err != nil {
		t.Fatal(err)
	}
	code, ran, err := call(r.viewers)
	if code != codes.PermissionDenied || ran {
		t.Errorf("revoked: code %s, handler ran %v (%v); want PermissionDenied and no handler", code, ran, err)
	}

	boom := errors.New("dial tcp 10.0.0.7:5432: connection refused")
	down := sharing.NewResolver(failingStewardStore{SharingStore: r.drv.Sharing(), err: boom}).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return sharing.StewardSettings{Enabled: true} }))
	code, ran, err = call(down)
	if code != codes.Unavailable || ran {
		t.Errorf("unreadable store: code %s, handler ran %v (%v); want Unavailable and no handler", code, ran, err)
	}
	if err != nil && strings.Contains(err.Error(), "10.0.0.7") {
		t.Errorf("the cause reached the caller: %v", err)
	}
}

type failingStewardStore struct {
	storage.SharingStore
	err error
}

func (f failingStewardStore) StewardGrants() storage.StewardGrantStore {
	return failingStewardGrants{StewardGrantStore: f.SharingStore.StewardGrants(), err: f.err}
}

type failingStewardGrants struct {
	storage.StewardGrantStore
	err error
}

func (f failingStewardGrants) Get(context.Context, string) (*storage.StewardGrant, error) {
	return nil, f.err
}

// An elevated viewer's backstop is strict even on an instance that
// strips.
func TestVisibilityInterceptorIsStrictForAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)
	elevated, err := r.viewers.Resolve(context.Background(), r.steward, storage.PrincipalKindPerson, "", sharing.AsSteward(r.grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	leak := func(ctx context.Context, _ any) (any, error) {
		sharing.LedgerFrom(ctx).AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}, {Id: "hidden", RawContent: "secret"}}, Total: 2}, nil
	}
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)

	resp, err := visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, info, leak)
	if err != nil || len(resp.(*pb.ListObjectsResponse).Objects) != 1 {
		t.Fatalf("an ordinary viewer on a lenient instance has the hit stripped: resp %v err %v", resp, err)
	}
	_, err = visibilityUnaryInterceptor(visibilityConfig{})(sharing.WithViewer(context.Background(), elevated), nil, info, leak)
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Errorf("elevated: err %v, want Internal (strict)", err)
	}
}

// A stream open is resolved like a unary call: a grant that fails a
// check refuses it before the handler runs.
func TestAuthStreamRefusesAFailedElevation(t *testing.T) {
	r := newStewardRig(t)
	if err := r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now); err != nil {
		t.Fatal(err)
	}
	ran := false
	ic := authStreamInterceptor(r.chain(map[string]string{authn.MetaStewardGrantID: r.grant.ID}), nil, r.viewers, nil)
	err := ic(nil, &fakeServerStream{ctx: anyTokenMD()},
		&grpc.StreamServerInfo{FullMethod: pb.JobService_WatchJob_FullMethodName},
		func(any, grpc.ServerStream) error { ran = true; return nil })
	if status.Code(err) != codes.PermissionDenied || ran {
		t.Errorf("revoked grant on a stream: code %s, handler ran %v (%v); want PermissionDenied and no handler", status.Code(err), ran, err)
	}
	if err != nil && !strings.Contains(err.Error(), "STEWARD_GRANT_REVOKED") {
		t.Errorf("error %q does not lead with the check's code", err)
	}
}

// The owner presenting a grant is refused not_steward: an owner key
// never reads under a grant, whoever the grant names.
func TestAuthUnaryRefusesTheOwnerPresentingAGrant(t *testing.T) {
	r := newStewardRig(t)
	owner := &authn.Principal{
		ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Provider: authn.MethodAPIKey,
		Scopes: authn.AllScopes, Meta: map[string]string{authn.MetaStewardGrantID: r.grant.ID},
	}
	ran := false
	ic := authUnaryInterceptor(authtest.Fixed(owner), nil, r.viewers, nil)
	_, err := ic(anyTokenMD(), nil, unaryInfo(pb.QueryService_ListObjects_FullMethodName),
		func(context.Context, any) (any, error) { ran = true; return "ok", nil })
	if status.Code(err) != codes.PermissionDenied || ran {
		t.Fatalf("owner with a grant: code %s, handler ran %v (%v); want PermissionDenied and no handler", status.Code(err), ran, err)
	}
	if !strings.Contains(err.Error(), "STEWARD_NOT_STEWARD") {
		t.Errorf("error %q does not carry STEWARD_NOT_STEWARD", err)
	}
}
