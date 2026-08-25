package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type entityHandler struct {
	pb.UnimplementedEntityServiceServer
	svc  *service.Service
	gate *registry.InboundGate
	sec  *security.Emitter
}

func newEntityHandler(svc *service.Service, gate *registry.InboundGate, sec *security.Emitter) *entityHandler {
	return &entityHandler{svc: svc, gate: gate, sec: sec}
}

// ListEntities returns a paginated entity listing. With an inbound gate
// wired (non-private instances), the listing is filtered to the
// namespaces the authenticated principal is entitled to — an index
// browse, so no metering charge. Mirrors the HTTP handler exactly.
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
	if h.gate != nil {
		principal := principalID(ctx)
		entitled := make([]*storage.Entity, 0, len(entities))
		for _, e := range entities {
			if h.gate.Check(ctx, principal, e.Namespace) == nil {
				entitled = append(entitled, e)
			}
		}
		entities = entitled
	}

	out := make([]*pb.Entity, 0, len(entities))
	for _, e := range entities {
		out = append(out, entityToProto(e))
	}

	return &pb.ListEntitiesResponse{Entities: out, Total: int32(len(out))}, nil // #nosec G115 -- total bounded by query limit
}

// GetEntity returns a single entity by slug. A wired inbound gate
// charges the access as a metered entity_resolve against the
// principal's namespace entitlement and quota, exactly like the HTTP
// surface.
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
	if h.gate != nil {
		if gerr := h.gate.Authorize(ctx, principalID(ctx), e.Namespace,
			storage.MeteringEventEntityResolve); gerr != nil {
			return nil, h.gateStatus(ctx, gerr)
		}
	}

	return entityToProto(e), nil
}

// GetEntityBacklinks returns objects that mention the given entity. The
// gate check is unmetered — backlinks ride on the entity's namespace
// entitlement without a quota charge. An entity the store cannot
// resolve fails CLOSED: the gate is never skipped.
func (h *entityHandler) GetEntityBacklinks(
	ctx context.Context,
	req *pb.GetEntityBacklinksRequest,
) (*pb.GetEntityBacklinksResponse, error) {
	if req.Slug == "" {
		return nil, status.Error(codes.InvalidArgument, "slug is required")
	}

	if h.gate != nil {
		e, err := h.svc.GetEntity(ctx, req.Slug)
		if err != nil || e == nil {
			return nil, status.Errorf(codes.NotFound, "entity %s not found", req.Slug)
		}
		if gerr := h.gate.Check(ctx, principalID(ctx), e.Namespace); gerr != nil {
			return nil, h.gateStatus(ctx, gerr)
		}
	}

	objs, err := h.svc.EntityBacklinks(ctx, req.Slug)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "entity backlinks: %v", err)
	}

	out := make([]*pb.KnowledgeObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, koToProto(o))
	}

	return &pb.GetEntityBacklinksResponse{Objects: out, Total: int32(len(out))}, nil // #nosec G115 -- total bounded by query limit
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// principalID returns the authenticated principal ID for inbound
// entitlement checks. Behind the auth interceptor it is always present;
// the empty fallback keeps ungated (private) servers working unchanged.
func principalID(ctx context.Context) string {
	if p, ok := authn.FromContext(ctx); ok {
		return p.ID
	}
	return ""
}

// gateStatus maps InboundGate authorization errors onto the gRPC
// surface: ErrEntitlementRequired → PermissionDenied, ErrQuotaExhausted
// → ResourceExhausted, anything else → Internal (fail closed). Each
// denial is recorded as a security event against the principal, keeping
// parity with the HTTP writeInboundGateError path.
func (h *entityHandler) gateStatus(ctx context.Context, err error) error {
	principal := principalID(ctx)
	switch {
	case registry.IsEntitlementRequired(err):
		if h.sec != nil {
			h.sec.RecordACLDenial(ctx, principal)
		}
		return status.Error(codes.PermissionDenied, "ENTITLEMENT_REQUIRED: "+err.Error())
	case errors.Is(err, registry.ErrQuotaExhausted):
		if h.sec != nil {
			h.sec.RecordQuotaExhausted(ctx, principal, principal)
		}
		return status.Error(codes.ResourceExhausted, "QUOTA_EXHAUSTED: "+err.Error())
	}
	return status.Errorf(codes.Internal, "entitlement check: %v", err)
}

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
