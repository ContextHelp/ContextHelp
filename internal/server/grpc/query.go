package grpc

import (
	"context"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
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

	if total > math.MaxInt32 {
		total = math.MaxInt32
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

	if total > math.MaxInt32 {
		total = math.MaxInt32
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

// NodeAwareSearch runs an RSQL query with optional node/edge type filtering.
// Results include graph structure, document projection, and optional node hits.
func (h *queryHandler) NodeAwareSearch(
	ctx context.Context,
	req *pb.NodeAwareSearchRequest,
) (*pb.NodeAwareSearchResponse, error) {
	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	var apiFilter *pluginapi.NodeAwareFilter
	if req.Filter != nil {
		apiFilter = &pluginapi.NodeAwareFilter{
			NodeTypes:      req.Filter.NodeTypes,
			EdgeTypes:      req.Filter.EdgeTypes,
			ReturnNodeHits: req.Filter.ReturnNodeHits,
		}
	}

	profileID := req.ProfileId
	objs, total, err := h.svc.SearchObjectsNodeAware(ctx, req.Query, limit, int(req.Offset), apiFilter, profileID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "node-aware search: %v", err)
	}

	results := make([]*pb.NodeAwareResult, 0, len(objs))
	for _, o := range objs {
		r := &pb.NodeAwareResult{
			Object:       koToProto(o),
			DocumentView: docProjectionToProto(projection.ProjectDocument(o)),
		}
		if apiFilter != nil && apiFilter.ReturnNodeHits && o.Graph != nil {
			r.NodeHits = buildNodeHits(o)
		}
		results = append(results, r)
	}

	if total > math.MaxInt32 {
		total = math.MaxInt32
	}
	return &pb.NodeAwareSearchResponse{Results: results, Total: int32(total)}, nil
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
	if o.Graph != nil {
		p.Graph = objectGraphToProto(o.Graph)
	}
	return p
}

func objectGraphToProto(g *pluginapi.ObjectGraph) *pb.ObjectGraph {
	if g == nil {
		return nil
	}
	pg := &pb.ObjectGraph{
		Nodes: make([]*pb.GraphNode, 0, len(g.Nodes)),
		Edges: make([]*pb.GraphEdge, 0, len(g.Edges)),
	}
	for _, n := range g.Nodes {
		pg.Nodes = append(pg.Nodes, &pb.GraphNode{
			Id:       n.ID,
			NodeType: n.NodeType,
			Label:    n.Label,
			Content:  n.Content,
			Order:    int32(n.Order), // #nosec G115 -- node order is small bounded int
		})
	}
	for _, e := range g.Edges {
		pg.Edges = append(pg.Edges, &pb.GraphEdge{
			Id:       e.ID,
			FromId:   e.FromID,
			ToId:     e.ToID,
			EdgeType: e.EdgeType,
			Weight:   e.Weight,
		})
	}
	return pg
}

func docProjectionToProto(d pluginapi.DocumentProjection) *pb.DocumentProjection {
	dp := &pb.DocumentProjection{
		Title: d.Title,
		Body:  d.Body,
	}
	for _, s := range d.Sections {
		dp.Sections = append(dp.Sections, &pb.Section{
			Title:   s.Title,
			Content: s.Content,
			Order:   int32(s.Order), // #nosec G115 -- section order is small bounded int
		})
	}
	return dp
}

// buildNodeHits constructs NodeHit entries for every node in the object graph.
// Score is 1.0 (presence-based); richer scoring is deferred to T-0180.
func buildNodeHits(o *pluginapi.KnowledgeObject) []*pb.NodeHit {
	if o.Graph == nil {
		return nil
	}
	hits := make([]*pb.NodeHit, 0, len(o.Graph.Nodes))
	for i, n := range o.Graph.Nodes {
		hits = append(hits, &pb.NodeHit{
			ObjectId: o.ID,
			NodeRef:  pluginapi.NodeURI(o.ID, n.NodeType, i),
			NodeType: n.NodeType,
			Snippet:  snippetFromNode(n),
			Score:    1.0,
		})
	}
	return hits
}

func snippetFromNode(n pluginapi.GraphNode) string {
	if n.Content != "" {
		if len(n.Content) > 200 {
			return n.Content[:200]
		}
		return n.Content
	}
	return n.Label
}

func safeTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
