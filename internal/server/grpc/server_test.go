package grpc_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newTestServer creates a gRPC server on a random port, returns the address and
// a cancel func that stops the server.
func newTestServer(t *testing.T) (addr string, conn *grpc.ClientConn) {
	t.Helper()

	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)

	// Pick an OS-assigned port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().String()
	ln.Close() // release so the server can rebind

	srv := grpcserver.New(addr, svc)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("grpc server error: %v", err)
		}
	}()
	t.Cleanup(cancel)

	// Dial.
	conn, err = grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return addr, conn
}

func TestHealthCheck(t *testing.T) {
	_, conn := newTestServer(t)

	hc := grpc_health_v1.NewHealthClient(conn)
	resp, err := hc.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("status: got %s, want SERVING", resp.Status)
	}
}

func TestAnalyze_EmptyContent(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewAnalyzeServiceClient(conn)
	_, err := c.Analyze(context.Background(), &pb.AnalyzeRequest{Content: ""})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestAnalyze_Success(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewAnalyzeServiceClient(conn)
	resp, err := c.Analyze(context.Background(), &pb.AnalyzeRequest{
		Content: "hello world",
		Type:    "text",
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if resp.JobId == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestListJobs_Empty(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewJobServiceClient(conn)
	resp, err := c.ListJobs(context.Background(), &pb.ListJobsRequest{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestGetJob_NotFound(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewJobServiceClient(conn)
	_, err := c.GetJob(context.Background(), &pb.GetJobRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("expected not found error")
	}
}

func TestListEntities_Empty(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewEntityServiceClient(conn)
	resp, err := c.ListEntities(context.Background(), &pb.ListEntitiesRequest{})
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestSearch_MissingQuery(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewQueryServiceClient(conn)
	_, err := c.Search(context.Background(), &pb.SearchRequest{Query: ""})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestListObjects_Empty(t *testing.T) {
	_, conn := newTestServer(t)

	c := pb.NewQueryServiceClient(conn)
	resp, err := c.ListObjects(context.Background(), &pb.ListObjectsRequest{})
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}
