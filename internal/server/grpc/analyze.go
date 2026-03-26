package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

type analyzeHandler struct {
	pb.UnimplementedAnalyzeServiceServer
	svc *service.Service
}

func newAnalyzeHandler(svc *service.Service) *analyzeHandler {
	return &analyzeHandler{svc: svc}
}

func (h *analyzeHandler) Analyze(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalyzeResponse, error) {
	if req.Content == "" {
		return nil, status.Error(codes.InvalidArgument, "content is required")
	}

	jobID, err := h.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:     req.Content,
		Type:        req.Type,
		Pipeline:    req.Pipeline,
		Source:      req.Source,
		SourceTitle: req.SourceTitle,
		Raw:         req.Raw,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "analyze: %v", err)
	}

	return &pb.AnalyzeResponse{JobId: jobID}, nil
}
