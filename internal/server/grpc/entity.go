package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type entityHandler struct {
	pb.UnimplementedEntityServiceServer
	svc *service.Service
}

func newEntityHandler(svc *service.Service) *entityHandler {
	return &entityHandler{svc: svc}
}

func (h *entityHandler) ListEntities(ctx context.Context, req *pb.ListEntitiesRequest) (*pb.ListEntitiesResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	entities, err := h.svc.ListEntities(ctx, storage.EntityFilter{
		Namespace: req.Namespace,
		Limit:     limit,
		Offset:    int(req.Offset),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list entities: %v", err)
	}

	out := make([]*pb.Entity, 0, len(entities))
	for _, e := range entities {
		out = append(out, entityToProto(e))
	}

	return &pb.ListEntitiesResponse{Entities: out, Total: int32(len(out))}, nil
}

func (h *entityHandler) GetEntity(ctx context.Context, req *pb.GetEntityRequest) (*pb.Entity, error) {
	if req.Slug == "" {
		return nil, status.Error(codes.InvalidArgument, "slug is required")
	}

	e, err := h.svc.GetEntity(ctx, req.Slug)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get entity: %v", err)
	}
	if e == nil {
		return nil, status.Errorf(codes.NotFound, "entity %s not found", req.Slug)
	}

	return entityToProto(e), nil
}

func (h *entityHandler) GetEntityBacklinks(
	ctx context.Context,
	req *pb.GetEntityBacklinksRequest,
) (*pb.GetEntityBacklinksResponse, error) {
	if req.Slug == "" {
		return nil, status.Error(codes.InvalidArgument, "slug is required")
	}

	objs, err := h.svc.EntityBacklinks(ctx, req.Slug)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "entity backlinks: %v", err)
	}

	out := make([]*pb.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, koToProto(o))
	}

	return &pb.GetEntityBacklinksResponse{Objects: out, Total: int32(len(out))}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func entityToProto(e *storage.Entity) *pb.Entity {
	return &pb.Entity{
		Slug:          e.Slug,
		Title:         e.Title,
		Description:   e.Description,
		Namespace:     e.Namespace,
		Aliases:       e.Aliases,
		ContentStatus: string(e.ContentStatus),
	}
}
