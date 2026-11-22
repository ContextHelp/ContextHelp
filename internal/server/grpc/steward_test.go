package grpc

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stewardRig is a store with a steward and a steward grant, a resolver
// wired for steward grants, the read audit over the store and a
// security emitter whose steward refusals the test can wait on.
type stewardRig struct {
	drv     storage.StorageDriver
	steward string
	grant   *storage.StewardGrant
	viewers *sharing.Resolver
	audit   *sharing.AuditWriter
	sec     *security.Emitter
	refused chan security.Alert
	now     time.Time
}

func newStewardRig(t *testing.T) *stewardRig {
	t.Helper()
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	r := &stewardRig{drv: storageutil.NewTestDriver(t), steward: "prn_steward", now: t0.Add(time.Hour), refused: make(chan security.Alert, 16)}
	sharingtest.EnsurePrincipal(t, r.drv, r.steward, storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	r.grant = sharingtest.GrantSteward(t, r.drv, r.steward, `{"policies":[{"data":"type==contact"}]}`, t0, t0.Add(48*time.Hour))
	settings := sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour}
	r.viewers = sharing.NewResolver(r.drv.Sharing()).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return settings }))
	r.audit = sharing.NewAuditWriter(r.drv.Sharing().AccessLog(), sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = r.audit.Close(context.Background()) })
	r.sec = security.New(security.DefaultConfig(), nil)
	r.sec.AddHandler(func(_ context.Context, a security.Alert) {
		if a.Kind == security.EventStewardElevationRefused {
			r.refused <- a
		}
	})
	t.Cleanup(func() { _ = r.sec.Close(context.Background()) })
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

// entries lists what the audit holds for grant in append order.
func (r *stewardRig) entries(t *testing.T, grant string) []*storage.AccessEntry {
	t.Helper()
	got, err := r.drv.Sharing().AccessLog().Query(context.Background(), storage.AccessFilter{StewardGrantID: grant})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Chain == nil {
			t.Fatalf("steward entry %s is not chained", e.ID)
		}
	}
	slices.SortFunc(got, func(a, b *storage.AccessEntry) int { return cmp.Compare(a.Chain.Seq, b.Chain.Seq) })
	return got
}

func (r *stewardRig) elevatedCtx(t *testing.T) context.Context {
	t.Helper()
	v, err := r.viewers.Resolve(context.Background(), r.steward, storage.PrincipalKindPerson, "", sharing.AsSteward(r.grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	return sharing.WithViewer(context.Background(), v)
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
		ic := authUnaryInterceptor(r.chain(meta), nil, r.viewers, r.audit)
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

// refusedCall runs GetObject through the auth interceptor as the
// steward naming its grant, resolved by viewers and audited on audit.
func (r *stewardRig) refusedCall(viewers *sharing.Resolver, audit *sharing.AuditWriter) (codes.Code, bool, error) {
	ran := false
	ic := authUnaryInterceptor(r.chain(map[string]string{authn.MetaStewardGrantID: r.grant.ID}), r.sec, viewers, audit)
	_, err := ic(anyTokenMD(), &pb.GetObjectRequest{Id: "obj_asked"}, unaryInfo(pb.QueryService_GetObject_FullMethodName),
		func(context.Context, any) (any, error) { ran = true; return "ok", nil })
	return status.Code(err), ran, err
}

// A grant that fails a check refuses the call and the handler never
// runs. The refusal is a durable entry under the method, with the
// steward_grant check failed on its code and the method's operation
// digest, and a security event.
func TestAuthUnaryRefusesAFailedElevation(t *testing.T) {
	r := newStewardRig(t)
	if err := r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now); err != nil {
		t.Fatal(err)
	}
	code, ran, err := r.refusedCall(r.viewers, r.audit)
	if code != codes.PermissionDenied || ran || !strings.Contains(err.Error(), "STEWARD_GRANT_REVOKED") {
		t.Errorf("revoked: code %s, handler ran %v (%v); want PermissionDenied STEWARD_GRANT_REVOKED and no handler", code, ran, err)
	}
	got := r.entries(t, r.grant.ID)
	if len(got) != 1 {
		t.Fatalf("entries after the refusal = %d, want 1 committed before the status", len(got))
	}
	e := got[0]
	if e.Route != pb.QueryService_GetObject_FullMethodName || e.Decision != storage.AccessDecisionDenied ||
		e.Checks[1] != (storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.StewardReasonGrantRevoked}) {
		t.Errorf("refusal entry = %+v %+v", e, e.Checks)
	}
	if want := sharing.OperationDigest("", pb.QueryService_GetObject_FullMethodName, map[string][]string{"req.id": {"obj_asked"}}); e.OperationDigest != want {
		t.Errorf("digest %s, want the method's over its request (%s)", e.OperationDigest, want)
	}
	select {
	case a := <-r.refused:
		if a.Meta["reason"] != sharing.StewardReasonGrantRevoked || a.Meta["grant_id"] != r.grant.ID || a.Meta["deferred"] != "false" {
			t.Errorf("event = %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Error("no security event for the refusal")
	}
}

// A store that cannot be read refuses with Unavailable, the cause not
// shown, and the entry is deferred.
func TestAuthUnaryDefersWhenTheStoreIsUnavailable(t *testing.T) {
	r := newStewardRig(t)
	boom := errors.New("dial tcp 10.0.0.7:5432: connection refused")
	down := sharing.NewResolver(failingStewardStore{SharingStore: r.drv.Sharing(), err: boom}).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return sharing.StewardSettings{Enabled: true} }))
	code, ran, err := r.refusedCall(down, r.audit)
	if code != codes.Unavailable || ran {
		t.Errorf("unreadable store: code %s, handler ran %v (%v); want Unavailable and no handler", code, ran, err)
	}
	if err != nil && strings.Contains(err.Error(), "10.0.0.7") {
		t.Errorf("the cause reached the caller: %v", err)
	}
	if got := r.entries(t, r.grant.ID); len(got) != 1 || got[0].Decision != storage.AccessDecisionDeferred {
		t.Errorf("deferred refusal entry = %+v", got)
	}
}

// No record, no answer: a refusal whose entry cannot be committed is
// AUDIT_UNAVAILABLE.
func TestAuthUnaryRefusalWithoutItsEntry(t *testing.T) {
	r := newStewardRig(t)
	if err := r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now); err != nil {
		t.Fatal(err)
	}
	broken := sharing.NewAuditWriter(downAccessLog{AccessLogStore: r.drv.Sharing().AccessLog()}, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = broken.Close(context.Background()) })
	code, ran, err := r.refusedCall(r.viewers, broken)
	if code != codes.Unavailable || ran || !strings.Contains(err.Error(), sharing.CodeAuditUnavailable) {
		t.Errorf("refusal without its entry: code %s, ran %v (%v); want Unavailable AUDIT_UNAVAILABLE", code, ran, err)
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

// downAccessLog is an access log whose steward appends fail.
type downAccessLog struct{ storage.AccessLogStore }

func (downAccessLog) AppendSteward(context.Context, *storage.AccessEntry) error {
	return errors.New("access log: database is locked")
}

// An elevated viewer's backstop is strict even on an instance that
// strips.
func TestVisibilityInterceptorIsStrictForAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)
	leak := func(ctx context.Context, _ any) (any, error) {
		sharing.LedgerFrom(ctx).AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}, {Id: "hidden", RawContent: "secret"}}, Total: 2}, nil
	}
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)

	resp, err := visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, info, leak)
	if err != nil || len(resp.(*pb.ListObjectsResponse).Objects) != 1 {
		t.Fatalf("an ordinary viewer on a lenient instance has the hit stripped: resp %v err %v", resp, err)
	}
	_, err = visibilityUnaryInterceptor(visibilityConfig{audit: r.audit})(r.elevatedCtx(t), nil, info, leak)
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Errorf("elevated: err %v, want Internal (strict)", err)
	}
	if got := r.entries(t, r.grant.ID); len(got) != 1 || got[0].Decision != storage.AccessDecisionDenied {
		t.Errorf("strict refusal entry = %+v", got)
	}
}

// releasing is a ListObjects handler that releases obj-1.
func releasing(ctx context.Context, _ any) (any, error) {
	sharing.LedgerFrom(ctx).AddObject("obj-1")
	return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "obj-1", RawContent: "Supplier contract"}}, Total: 1}, nil
}

// An elevated unary call returns its answer with its entry committed,
// and a call whose entry cannot be committed returns no answer at all:
// Unavailable AUDIT_UNAVAILABLE.
func TestVisibilityInterceptorCommitsElevatedCalls(t *testing.T) {
	r := newStewardRig(t)
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)
	resp, err := visibilityUnaryInterceptor(visibilityConfig{audit: r.audit})(r.elevatedCtx(t), &pb.ListObjectsRequest{}, info, releasing)
	if err != nil || len(resp.(*pb.ListObjectsResponse).Objects) != 1 {
		t.Fatalf("elevated read: resp %v err %v", resp, err)
	}
	if r.audit.Buffered() != 0 {
		t.Error("an elevated entry went to the buffer")
	}
	got := r.entries(t, r.grant.ID)
	if len(got) != 1 || !slices.Equal(got[0].ObjectIDs, []string{"obj-1"}) || got[0].Checks[0].Check != sharing.CheckStewardGrant {
		t.Fatalf("elevated read entry = %+v, want committed with its ids and the steward_grant check", got)
	}

	broken := sharing.NewAuditWriter(downAccessLog{AccessLogStore: r.drv.Sharing().AccessLog()}, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = broken.Close(context.Background()) })
	resp, err = visibilityUnaryInterceptor(visibilityConfig{audit: broken})(r.elevatedCtx(t), &pb.ListObjectsRequest{}, info, releasing)
	if resp != nil || status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), sharing.CodeAuditUnavailable) {
		t.Fatalf("audit down: resp %v err %v, want no answer and Unavailable AUDIT_UNAVAILABLE", resp, err)
	}
	if strings.Contains(err.Error(), "Supplier") || strings.Contains(err.Error(), "obj-1") {
		t.Errorf("the withheld answer leaked into the status: %v", err)
	}

	// An ordinary restricted call keeps the buffered path.
	ordinary := sharing.NewAuditWriter(r.drv.Sharing().AccessLog(), sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = ordinary.Close(context.Background()) })
	if _, err := visibilityUnaryInterceptor(visibilityConfig{audit: ordinary})(restrictedCtx(t), &pb.ListObjectsRequest{}, info, releasing); err != nil {
		t.Fatal(err)
	}
	if ordinary.Buffered() != 1 {
		t.Errorf("ordinary call buffered %d entries, want 1", ordinary.Buffered())
	}
}

// streamMethods are every server-streaming or bidi method the server
// registers: the job watch and the reflection services.
func streamMethods() []string {
	var out []string
	for _, d := range []grpc.ServiceDesc{pb.AnalyzeService_ServiceDesc, pb.JobService_ServiceDesc, pb.QueryService_ServiceDesc, pb.EntityService_ServiceDesc} {
		for _, s := range d.Streams {
			out = append(out, "/"+d.ServiceName+"/"+s.StreamName)
		}
	}
	return append(out, reflectionV1Method, reflectionV1AlphaMethod)
}

// Streams are closed to a steward elevation (ADR-083 §3, §4): every
// stream method refuses an elevated caller before its handler runs,
// and the refusal is a committed entry. The reflection streams, open
// to ordinary restricted viewers, are closed to it too.
func TestStreamsAreClosedToAnElevation(t *testing.T) {
	r := newStewardRig(t)
	methods := streamMethods()
	if !slices.Contains(methods, pb.JobService_WatchJob_FullMethodName) {
		t.Fatalf("stream methods %v miss WatchJob", methods)
	}
	ic := visibilityStreamInterceptor(visibilityConfig{audit: r.audit})
	for _, m := range methods {
		ran := false
		err := ic(nil, &fakeServerStream{ctx: r.elevatedCtx(t)}, &grpc.StreamServerInfo{FullMethod: m, IsServerStream: true},
			func(any, grpc.ServerStream) error { ran = true; return nil })
		if status.Code(err) != codes.PermissionDenied || ran {
			t.Errorf("%s: code %s, handler ran %v; want PermissionDenied before the handler", m, status.Code(err), ran)
		}
	}
	got := r.entries(t, r.grant.ID)
	if len(got) != len(methods) {
		t.Fatalf("%d entries for %d refused streams", len(got), len(methods))
	}
	for _, e := range got {
		last := e.Checks[len(e.Checks)-1]
		if e.Decision != storage.AccessDecisionDenied || last != (storage.AccessCheck{Check: sharing.CheckRouteAccess, Outcome: storage.AccessOutcomeFail, Reason: sharing.ReasonStewardNoStream}) {
			t.Errorf("%s: entry %+v %+v, want denied on steward_no_stream", e.Route, e, e.Checks)
		}
	}
	// An ordinary restricted viewer still reaches reflection.
	ran := false
	if err := ic(nil, &fakeServerStream{ctx: restrictedCtx(t)}, &grpc.StreamServerInfo{FullMethod: reflectionV1Method},
		func(any, grpc.ServerStream) error { ran = true; return nil }); err != nil || !ran {
		t.Errorf("ordinary reflection: err %v ran %v", err, ran)
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
	ic := authStreamInterceptor(r.chain(map[string]string{authn.MetaStewardGrantID: r.grant.ID}), nil, r.viewers, r.audit)
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
	ic := authUnaryInterceptor(authtest.Fixed(owner), nil, r.viewers, r.audit)
	_, err := ic(anyTokenMD(), nil, unaryInfo(pb.QueryService_ListObjects_FullMethodName),
		func(context.Context, any) (any, error) { ran = true; return "ok", nil })
	if status.Code(err) != codes.PermissionDenied || ran {
		t.Fatalf("owner with a grant: code %s, handler ran %v (%v); want PermissionDenied and no handler", status.Code(err), ran, err)
	}
	if !strings.Contains(err.Error(), "STEWARD_NOT_STEWARD") {
		t.Errorf("error %q does not carry STEWARD_NOT_STEWARD", err)
	}
}
