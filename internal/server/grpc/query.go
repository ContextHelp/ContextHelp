package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

type queryHandler struct {
	pb.UnimplementedQueryServiceServer
	svc *service.Service
}

func newQueryHandler(svc *service.Service) *queryHandler {
	return &queryHandler{svc: svc}
}

func (h *queryHandler) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	objs, total, err := h.svc.SearchObjects(ctx, req.Query, limit, int(req.Offset), req.ProfileId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search: %v", err)
	}

	out := make([]*pb.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, koToProto(o))
	}

	return &pb.SearchResponse{Objects: out, Total: int32(total)}, nil
}

func (h *queryHandler) ListObjects(ctx context.Context, req *pb.ListObjectsRequest) (*pb.ListObjectsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	filter := storage.ObjectFilter{
		Type:      req.Type,
		Pipeline:  req.Pipeline,
		ProfileID: req.ProfileId,
		Limit:     limit,
		Offset:    int(req.Offset),
	}

	objs, total, err := h.svc.ListObjects(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list objects: %v", err)
	}

	out := make([]*pb.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, koToProto(o))
	}

	return &pb.ListObjectsResponse{Objects: out, Total: int32(total)}, nil
}

func (h *queryHandler) GetObject(ctx context.Context, req *pb.GetObjectRequest) (*pb.KnowledgeObject, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	o, err := h.svc.GetObject(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get object: %v", err)
	}
	if o == nil {
		return nil, status.Errorf(codes.NotFound, "object %s not found", req.Id)
	}

	return koToProto(o), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func koToProto(o *pluginapi.KnowledgeObject) *pb.KnowledgeObject {
	p := &pb.KnowledgeObject{
		Id:          o.ID,
		Type:        o.Type,
		Source:      o.Source,
		RawContent:  o.RawContent,
		TextContent: o.TextContent,
		Pipeline:    o.Pipeline,
		ProfileId:   o.ProfileID,
		CreatedAt:   safeTimestamp(o.CreatedAt),
		UpdatedAt:   safeTimestamp(o.UpdatedAt),
	}
	return p
}

func safeTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
