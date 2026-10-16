package grpc

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type auditSink struct {
	mu      sync.Mutex
	entries []*storage.AccessEntry
}

func (s *auditSink) Append(_ context.Context, e []*storage.AccessEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e...)
	return nil
}

func (s *auditSink) Query(context.Context, storage.AccessFilter) ([]*storage.AccessEntry, error) {
	return nil, nil
}
func (s *auditSink) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

func newSink(t *testing.T) (*auditSink, *sharing.AuditWriter) {
	t.Helper()
	sink := &auditSink{}
	w := sharing.NewAuditWriter(sink, sharing.AuditOptions{Interval: time.Hour, Batch: 1 << 20})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	return sink, w
}

func drain(t *testing.T, sink *auditSink, w *sharing.AuditWriter) []*storage.AccessEntry {
	t.Helper()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]*storage.AccessEntry(nil), sink.entries...)
}

func trail(e *storage.AccessEntry) []string {
	out := make([]string, 0, len(e.Checks))
	for _, c := range e.Checks {
		out = append(out, c.Check+"="+string(c.Outcome)+":"+c.Reason)
	}
	return out
}

func wantTrail(t *testing.T, e *storage.AccessEntry, decision storage.AccessDecision, want ...string) {
	t.Helper()
	if e.Decision != decision {
		t.Errorf("decision = %q, want %q (%v)", e.Decision, decision, trail(e))
	}
	got := trail(e)
	if len(got) != len(want) {
		t.Fatalf("trail = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trail = %v, want %v", got, want)
		}
	}
}

func principalCtx(t *testing.T, scopes ...authn.Scope) context.Context {
	t.Helper()
	p := &authn.Principal{ID: "prn_vis", Provider: authn.MethodAPIKey, Scopes: scopes, Meta: map[string]string{authn.MetaIdentityID: "idn_vis"}}
	return authn.Attach(restrictedCtx(t), p)
}

// An admitted unary call leaves the ordered trail through the
// visibility interceptor; the backstop refusing, and an unclassified
// method, are entries that name the failing check.
func TestReceiptUnaryVisibility(t *testing.T) {
	leak := func(ctx context.Context, _ any) (any, error) {
		l := sharing.LedgerFrom(ctx)
		l.MarkScoped()
		l.AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}, {Id: "hidden"}}}, nil
	}
	clean := func(ctx context.Context, _ any) (any, error) {
		sharing.LedgerFrom(ctx).MarkScoped()
		sharing.LedgerFrom(ctx).AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}}}, nil
	}
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)
	ctx := principalCtx(t, authn.ScopeReadObjects)
	prefix := []string{"authn.api_key=PASS:authenticated"}

	sink, w := newSink(t)
	if _, err := visibilityUnaryInterceptor(visibilityConfig{audit: w, strict: true})(ctx, &pb.ListObjectsRequest{Limit: 5}, info, clean); err != nil {
		t.Fatal(err)
	}
	if _, err := visibilityUnaryInterceptor(visibilityConfig{audit: w, strict: true})(ctx, &pb.ListObjectsRequest{Limit: 5}, info, leak); err == nil {
		t.Fatal("strict leak must be refused")
	}
	if _, err := visibilityUnaryInterceptor(visibilityConfig{audit: w})(ctx, &pb.ListObjectsRequest{Limit: 5}, info, leak); err != nil {
		t.Fatal(err)
	}
	if _, err := visibilityUnaryInterceptor(visibilityConfig{audit: w})(ctx, &pb.ListObjectsRequest{Limit: 5}, unaryInfo("/no.Such/Method"), leak); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unclassified: %v", err)
	}
	es := drain(t, sink, w)
	if len(es) != 4 {
		t.Fatalf("entries = %d, want 4", len(es))
	}
	wantTrail(t, es[0], storage.AccessDecisionAllowed, append(prefix, "route_access=PASS:route_open", "share_policy=PASS:rows_scoped", "deny_class=NOT_APPLICABLE:no_ceiling", "backstop=PASS:clean")...)
	wantTrail(t, es[1], storage.AccessDecisionDenied, append(prefix, "route_access=PASS:route_open", "share_policy=PASS:rows_scoped", "deny_class=NOT_APPLICABLE:no_ceiling", "backstop=FAIL:hit_refused")...)
	wantTrail(t, es[2], storage.AccessDecisionAllowed, append(prefix, "route_access=PASS:route_open", "share_policy=PASS:rows_scoped", "deny_class=NOT_APPLICABLE:no_ceiling", "backstop=FAIL:hit_stripped")...)
	wantTrail(t, es[3], storage.AccessDecisionDenied, append(prefix, "route_access=FAIL:route_unclassified")...)
	if es[0].OperationDigest != es[1].OperationDigest || es[0].OperationDigest == es[3].OperationDigest {
		t.Errorf("digests: same request must match, another method must not: %q %q %q", es[0].OperationDigest, es[1].OperationDigest, es[3].OperationDigest)
	}
}

// The auth interceptor opens the receipt ahead of the scope check: a
// restricted caller refused for a missing scope is an entry that ends
// at scope; an unauthenticated call and the owner's calls are none.
func TestReceiptScopeRefusalThroughAuth(t *testing.T) {
	sink, w := newSink(t)
	chain := chainUnary(authUnaryInterceptor(capChain(), nil, nil, w), visibilityUnaryInterceptor(visibilityConfig{audit: w}))
	handler := func(context.Context, any) (any, error) { return &pb.AnalyzeResponse{}, nil }
	info := unaryInfo(pb.AnalyzeService_Analyze_FullMethodName)

	// reader: no write:objects scope.
	if _, err := chain(bearer(capPersonToken), &pb.AnalyzeRequest{Content: "x"}, info, handler); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("reader Analyze: %v", err)
	}
	// no credential, bad credential, the owner.
	if _, err := chain(context.Background(), &pb.AnalyzeRequest{}, info, handler); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous: %v", err)
	}
	if _, err := chain(bearer("not-a-token"), &pb.AnalyzeRequest{}, info, handler); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := chain(bearer(capOwnerToken), &pb.AnalyzeRequest{}, info, handler); err != nil {
		t.Fatalf("owner: %v", err)
	}
	es := drain(t, sink, w)
	if len(es) != 1 {
		t.Fatalf("entries = %d (%+v), want only the reader's refusal", len(es), es)
	}
	if es[0].PrincipalID != "prn_person" || es[0].Decision != storage.AccessDecisionDenied {
		t.Fatalf("entry = %+v", es[0])
	}
	got := trail(es[0])
	if len(got) != 2 || !strings.HasPrefix(got[0], "authn.") || got[1] != "scope=FAIL:scope_missing" {
		t.Fatalf("trail = %v, want authn then scope=FAIL:scope_missing", got)
	}
}

// A non-owner sending x-ctxt-view-as is an entry that failed view_as;
// the owner's refused preview is not logged; the owner's successful
// preview is logged as the owner.
func TestReceiptViewAs(t *testing.T) {
	sink, w := newSink(t)
	handler := func(ctx context.Context, _ any) (any, error) {
		sharing.LedgerFrom(ctx).AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}}}, nil
	}
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)
	withTarget := func(ctx context.Context) context.Context {
		return metadata.NewIncomingContext(ctx, metadata.Pairs(sharing.MetadataViewAs, "prn_other"))
	}
	ic := visibilityUnaryInterceptor(visibilityConfig{audit: w})

	if _, err := ic(withTarget(principalCtx(t, authn.ScopeReadObjects)), &pb.ListObjectsRequest{}, info, handler); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-owner view-as: %v", err)
	}
	owner := authn.Attach(sharing.WithViewer(context.Background(), sharing.Unrestricted),
		&authn.Principal{ID: storage.OwnerPrincipal, Provider: authn.MethodAPIKey})
	// Resolver is nil: the owner's target resolves to nothing (refused).
	_, _ = ic(withTarget(owner), &pb.ListObjectsRequest{}, info, handler)
	if _, err := ic(owner, &pb.ListObjectsRequest{}, info, handler); err != nil {
		t.Fatal(err)
	}
	es := drain(t, sink, w)
	if len(es) != 1 {
		t.Fatalf("entries = %d (%+v), want only the non-owner's refusal", len(es), es)
	}
	wantTrail(t, es[0], storage.AccessDecisionDenied, "authn.api_key=PASS:authenticated", "view_as=FAIL:owner_only")
}

// A stream is audited like a unary call, its digest covers the method.
func TestReceiptStream(t *testing.T) {
	sink, w := newSink(t)
	ic := visibilityStreamInterceptor(visibilityConfig{audit: w})
	ss := &fakeServerStream{ctx: principalCtx(t, authn.ScopeReadJobs)}
	err := ic(nil, ss, &grpc.StreamServerInfo{FullMethod: pb.JobService_WatchJob_FullMethodName}, func(any, grpc.ServerStream) error { return nil })
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("owner-only stream: %v", err)
	}
	es := drain(t, sink, w)
	if len(es) != 1 {
		t.Fatalf("entries = %d, want 1", len(es))
	}
	wantTrail(t, es[0], storage.AccessDecisionDenied, "authn.api_key=PASS:authenticated", "route_access=FAIL:owner_only")
}

// An admitted stream's receipt is written when it ends, stamped with its
// start: the backstop checks of the messages come first, as they run as
// each one is sent, then the share policy (NOT_APPLICABLE: the handler
// filters the frames) and the ceiling.
func TestReceiptStreamAdmitted(t *testing.T) {
	const method = "/test.Stream/Watch"
	methodAccessTable[method] = methodAccess{shape: sharing.ShapeObjects}
	t.Cleanup(func() { delete(methodAccessTable, method) })

	sink, w := newSink(t)
	ic := visibilityStreamInterceptor(visibilityConfig{audit: w})
	ss := &sendStream{fakeServerStream: fakeServerStream{ctx: principalCtx(t, authn.ScopeReadObjects)}}
	started := time.Now()
	err := ic(nil, ss, &grpc.StreamServerInfo{FullMethod: method}, func(_ any, s grpc.ServerStream) error {
		l := sharing.LedgerFrom(s.Context())
		l.MarkScoped()
		l.AddObject("a")
		time.Sleep(40 * time.Millisecond)
		return s.SendMsg(&pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	es := drain(t, sink, w)
	if len(es) != 1 {
		t.Fatalf("entries = %d, want 1", len(es))
	}
	wantTrail(t, es[0], storage.AccessDecisionAllowed,
		"authn.api_key=PASS:authenticated", "route_access=PASS:route_open", "backstop=PASS:clean",
		"share_policy=NOT_APPLICABLE:stream_filtered", "deny_class=NOT_APPLICABLE:no_ceiling")
	if d := es[0].At.Sub(started); d < 0 || d > 20*time.Millisecond {
		t.Errorf("At is %v after the start, want the start of the stream", d)
	}
}

type sendStream struct{ fakeServerStream }

func (s *sendStream) SendMsg(any) error { return nil }

// The request's populated fields feed the digest: nested messages,
// lists and values count; defaults do not.
func TestProtoParams(t *testing.T) {
	a := protoParams(&pb.ListObjectsRequest{Type: "contact", Limit: 5})
	b := protoParams(&pb.ListObjectsRequest{Limit: 5, Type: "contact"})
	c := protoParams(&pb.ListObjectsRequest{Type: "contact", Limit: 6})
	if sharing.OperationDigest("", "m", a) != sharing.OperationDigest("", "m", b) {
		t.Error("field order must not matter")
	}
	if sharing.OperationDigest("", "m", a) == sharing.OperationDigest("", "m", c) {
		t.Error("a different value must change the digest")
	}
	if got := protoParams(&pb.ListObjectsRequest{}); len(got) != 0 {
		t.Errorf("defaults = %v, want none", got)
	}
	if protoParams(nil) != nil || protoParams("x") != nil {
		t.Error("a non-message has no params")
	}
}

// The digest leaves out what REST's leaves out: a request's body. Body
// fields (by name), bytes fields and large strings are not in the
// params, so two requests that differ only there share a digest, while
// the fields that choose the operation still count.
func TestProtoParamsExcludeBody(t *testing.T) {
	a := protoParams(&pb.AnalyzeRequest{Content: "secret text", Type: "note", Pipeline: "p"})
	if _, ok := a["req.content"]; ok {
		t.Errorf("params = %v: the body is in the digest", a)
	}
	b := protoParams(&pb.AnalyzeRequest{Content: "other text", Type: "note", Pipeline: "p"})
	if sharing.OperationDigest("", "m", a) != sharing.OperationDigest("", "m", b) {
		t.Error("requests that differ only in the body must share a digest")
	}
	c := protoParams(&pb.AnalyzeRequest{Content: "secret text", Type: "note", Pipeline: "q"})
	if sharing.OperationDigest("", "m", a) == sharing.OperationDigest("", "m", c) {
		t.Error("a different pipeline must change the digest")
	}
	big := protoParams(&pb.AnalyzeRequest{Source: strings.Repeat("x", maxDigestString+1), Type: "note"})
	if _, ok := big["req.source"]; ok {
		t.Error("a string over the bound is body-like and left out")
	}
	if _, ok := big["req.type"]; !ok {
		t.Error("a short field stays in")
	}
}
