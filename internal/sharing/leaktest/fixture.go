package leaktest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Fixture is an in-process dpkms the cases call: the HTTP API behind
// an httptest server and the gRPC API over an in-memory listener, both
// over one store the caller supplies, guarded by the static token
// provider. It holds the owner and one object of theirs for now; the
// restricted viewers and the corpus they must not see arrive with the
// sharing phases.
type Fixture struct {
	// Server is the HTTP instance: its URL, driver and stack.
	Server *dpkmstest.Server
	// Conn reaches the gRPC API.
	Conn *grpc.ClientConn
	// ObjectID is the seeded object, the owner's.
	ObjectID string
	// JobID is the seeded job, completed, that produced the object.
	JobID string
	// EntitySlug is the seeded entity the object mentions.
	EntitySlug string

	owner Principal
}

// bufSize is the in-memory gRPC listener's buffer.
const bufSize = 1 << 20

// Start builds the fixture over driver and serves it until the test
// ends. The test package must run through testguard (dpkmstest
// requires it).
func Start(t testing.TB, driver storage.StorageDriver) *Fixture {
	t.Helper()
	srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
	f := &Fixture{
		Server: srv,
		owner:  Principal{Name: "owner", Token: srv.Token(dpkmstest.RoleAdmin)},
	}
	f.seed(t)
	f.serveGRPC(t)
	return f
}

// Owner is the instance owner: the admin principal.
func (f *Fixture) Owner() Principal { return f.owner }

// seed stores the owner's object, the job that produced it, the entity
// it mentions and the mention edge, so the routes keyed by ID answer
// about something rather than only their not-found path.
func (f *Fixture) seed(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	entity := &storage.Entity{
		Slug: "leak/owner-entity", Title: "Owner entity", Namespace: "leak",
		CreatedAt: now, UpdatedAt: now,
	}
	obj := &storage.KnowledgeObject{
		ID: "leak-owner-note", Type: "note", Status: storage.ObjectStatusActive,
		Tags:      []storage.Tag{{Label: "leaktest"}},
		CreatedAt: now, UpdatedAt: now,
	}
	job := &storage.Job{
		ID: "leak-owner-job", Type: "analyze", Status: storage.JobCompleted, Pipeline: "text.short",
		ResultID: obj.ID, CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	edge := &storage.Edge{
		ID: "leak-owner-mention", FromType: "object", FromID: obj.ID,
		ToType: storage.EdgeEndpointEntity, ToID: "ctxt://entity/" + entity.Slug,
		EdgeType: "mentions", Weight: 1, CreatedAt: now,
	}
	driver := f.Server.Driver
	if err := driver.Entities().Upsert(ctx, entity); err != nil {
		t.Fatalf("leaktest: seed entity: %v", err)
	}
	if err := driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("leaktest: seed object: %v", err)
	}
	if err := driver.Jobs().Create(ctx, job); err != nil {
		t.Fatalf("leaktest: seed job: %v", err)
	}
	if err := driver.Edges().Create(ctx, edge); err != nil {
		t.Fatalf("leaktest: seed edge: %v", err)
	}
	f.ObjectID, f.JobID, f.EntitySlug = obj.ID, job.ID, entity.Slug
}

// serveGRPC serves the stack's service over gRPC on an in-memory
// listener, behind the same provider, emitter and entitlement gate as
// the HTTP router, reflection on.
func (f *Fixture) serveGRPC(t testing.TB) {
	t.Helper()
	st := f.Server.Stack
	ln := bufconn.Listen(bufSize)
	gs := grpcserver.New(ln.Addr().String(), st.Service,
		grpcserver.WithAuth(st.RouteAuth),
		grpcserver.WithSecurity(st.Security),
		grpcserver.WithEntitlements(st.Entitlements),
		grpcserver.WithReflection(true),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := gs.Serve(ctx, ln); err != nil {
			t.Logf("leaktest: grpc serve: %v", err)
		}
	}()
	t.Cleanup(func() { cancel(); <-done })

	conn, err := grpc.NewClient("passthrough:///"+ln.Addr().String(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return ln.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("leaktest: grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	f.Conn = conn
}

// HTTP calls method path (relative to /api/v1) as p, body JSON-encoded
// when non-nil, and returns what the route answered. A stream is read
// until ctx ends; the frames received by then are the body.
func (f *Fixture) HTTP(ctx context.Context, p Principal, method, path string, body any) (Result, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Result{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.Server.URL+"/api/v1"+path, rd)
	if err != nil {
		return Result{}, err
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	// A body cut by ctx is a stream's answer so far, not a failure.
	if err != nil && ctx.Err() == nil {
		return Result{}, err
	}
	return Result{Status: resp.StatusCode, Header: resp.Header, Body: out}, nil
}

// RPCContext returns ctx carrying p's bearer token as gRPC metadata.
func (f *Fixture) RPCContext(ctx context.Context, p Principal) context.Context {
	if p.Token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+p.Token)
}

// RPCResult folds an RPC's response messages and error into a Result.
func RPCResult(msgs []proto.Message, err error) Result {
	var body bytes.Buffer
	for _, m := range msgs {
		b, merr := protojson.Marshal(m)
		if merr != nil {
			b = []byte("<" + merr.Error() + ">")
		}
		body.Write(b)
		body.WriteByte('\n')
	}
	st, _ := status.FromError(err)
	if err != nil {
		body.WriteString(st.Message())
	}
	return Result{Code: st.Code(), Body: body.Bytes()}
}
