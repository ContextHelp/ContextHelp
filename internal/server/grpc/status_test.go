package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
)

// An unknown ID is NotFound on every read RPC, as it is 404 on HTTP:
// never Internal, which the leak suite reads as a server failure.
func TestReadRPCsAnswerNotFoundForUnknownID(t *testing.T) {
	_, conn := newTestServer(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"JobService/GetJob": func() error {
			_, err := pb.NewJobServiceClient(conn).GetJob(ctx, &pb.GetJobRequest{Id: "nonexistent"})
			return err
		},
		"JobService/WatchJob": func() error {
			s, err := pb.NewJobServiceClient(conn).WatchJob(ctx, &pb.WatchJobRequest{Id: "nonexistent"})
			if err != nil {
				return err
			}
			_, err = s.Recv()
			return err
		},
		"EntityService/GetEntity": func() error {
			_, err := pb.NewEntityServiceClient(conn).GetEntity(ctx, &pb.GetEntityRequest{Slug: "nonexistent"})
			return err
		},
		"QueryService/GetObject": func() error {
			_, err := pb.NewQueryServiceClient(conn).GetObject(ctx, &pb.GetObjectRequest{Id: "nonexistent"})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if code := status.Code(err); code != codes.NotFound {
				t.Fatalf("code = %v, want NotFound (err: %v)", code, err)
			}
		})
	}
}

// A query that does not parse is InvalidArgument on both search RPCs,
// as it is 400 on HTTP.
func TestSearchRPCsAnswerInvalidArgumentForBadQuery(t *testing.T) {
	_, conn := newTestServer(t)
	ctx := context.Background()
	c := pb.NewQueryServiceClient(conn)
	const bad = "((type==note"

	calls := map[string]func() error{
		"Search": func() error {
			_, err := c.Search(ctx, &pb.SearchRequest{Query: bad})
			return err
		},
		"NodeAwareSearch": func() error {
			_, err := c.NodeAwareSearch(ctx, &pb.NodeAwareSearchRequest{Query: bad})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if code := status.Code(err); code != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (err: %v)", code, err)
			}
		})
	}
}
