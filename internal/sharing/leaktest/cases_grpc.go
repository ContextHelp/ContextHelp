package leaktest

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc"
	reflectv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
)

// Server reflection method names, as the gRPC scope table keys them.
const (
	reflectionV1Method      = "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"
	reflectionV1AlphaMethod = "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo"
)

// rpc folds a unary RPC's answer into a Result.
func rpc[M proto.Message](resp M, err error) (Result, error) {
	if err != nil {
		return RPCResult(nil, err), nil
	}
	return RPCResult([]proto.Message{resp}, nil), nil
}

// drain reads a server stream until it ends or ctx cuts it, and folds
// what arrived into a Result.
func drain[M proto.Message](s interface{ Recv() (M, error) }) (Result, error) {
	var msgs []proto.Message
	for {
		m, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return RPCResult(msgs, nil), nil
		}
		if err != nil {
			return RPCResult(msgs, err), nil
		}
		msgs = append(msgs, m)
	}
}

// windowed bounds a stream case's call to StreamWindow.
func windowed(call CallFunc) CallFunc {
	return func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
		ctx, cancel := context.WithTimeout(ctx, StreamWindow)
		defer cancel()
		return call(ctx, f, p)
	}
}

// rpcCases lists the gRPC read RPCs by service.
func rpcCases() []Case {
	return []Case{
		{Route: pb.JobService_GetJob_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewJobServiceClient(f.Conn).GetJob(f.RPCContext(ctx, p), &pb.GetJobRequest{Id: f.JobID}))
		}},
		{Route: pb.JobService_ListJobs_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewJobServiceClient(f.Conn).ListJobs(f.RPCContext(ctx, p), &pb.ListJobsRequest{}))
		}},
		{Route: pb.JobService_WatchJob_FullMethodName, Call: windowed(func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			s, err := pb.NewJobServiceClient(f.Conn).WatchJob(f.RPCContext(ctx, p), &pb.WatchJobRequest{Id: f.JobID})
			if err != nil {
				return RPCResult(nil, err), nil
			}
			return drain(s)
		})},

		{Route: pb.QueryService_Search_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).Search(f.RPCContext(ctx, p), &pb.SearchRequest{Query: rsqlNotes}))
		}},
		{Route: pb.QueryService_ListObjects_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, p), &pb.ListObjectsRequest{}))
		}},
		{Route: pb.QueryService_GetObject_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).GetObject(f.RPCContext(ctx, p), &pb.GetObjectRequest{Id: f.ObjectID}))
		}},
		{Route: pb.QueryService_NodeAwareSearch_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).NodeAwareSearch(f.RPCContext(ctx, p), &pb.NodeAwareSearchRequest{Query: rsqlNotes}))
		}},

		{Route: pb.EntityService_ListEntities_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).ListEntities(f.RPCContext(ctx, p), &pb.ListEntitiesRequest{}))
		}},
		{Route: pb.EntityService_GetEntity_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).GetEntity(f.RPCContext(ctx, p), &pb.GetEntityRequest{Slug: f.EntitySlug}))
		}},
		{Route: pb.EntityService_GetEntityBacklinks_FullMethodName, Call: func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).GetEntityBacklinks(f.RPCContext(ctx, p), &pb.GetEntityBacklinksRequest{Slug: f.EntitySlug}))
		}},

		reflection(reflectionV1Method),
		reflection(reflectionV1AlphaMethod),
	}
}

// reflectionDesc is the ServerReflectionInfo bidi stream. The v1 and
// v1alpha messages share one wire shape, so the v1 types speak both
// methods; the v1alpha package itself is deprecated.
var reflectionDesc = grpc.StreamDesc{StreamName: "ServerReflectionInfo", ServerStreams: true, ClientStreams: true}

// reflection is the case of a server reflection method: it opens the
// stream, asks for the service list and reports the first answer.
func reflection(method string) Case {
	return Case{Route: method, Call: windowed(func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
		s, err := f.Conn.NewStream(f.RPCContext(ctx, p), &reflectionDesc, method)
		if err != nil {
			return RPCResult(nil, err), nil
		}
		req := &reflectv1.ServerReflectionRequest{MessageRequest: &reflectv1.ServerReflectionRequest_ListServices{}}
		if err := s.SendMsg(req); err != nil && !errors.Is(err, io.EOF) {
			return RPCResult(nil, err), nil
		}
		resp := &reflectv1.ServerReflectionResponse{}
		err = s.RecvMsg(resp)
		_ = s.CloseSend()
		return rpc(resp, err)
	})}
}
