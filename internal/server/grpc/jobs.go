package grpc

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type jobHandler struct {
	pb.UnimplementedJobServiceServer
	svc *service.Service
}

func newJobHandler(svc *service.Service) *jobHandler {
	return &jobHandler{svc: svc}
}

func (h *jobHandler) GetJob(ctx context.Context, req *pb.GetJobRequest) (*pb.Job, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	j, err := h.svc.GetJob(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get job: %v", err)
	}
	if j == nil {
		return nil, status.Errorf(codes.NotFound, "job %s not found", req.Id)
	}

	return jobToProto(j), nil
}

func (h *jobHandler) ListJobs(ctx context.Context, req *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	filter := storage.JobFilter{
		Status: storage.JobStatus(req.Status),
		Limit:  int(req.Limit),
		Offset: int(req.Offset),
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}

	jobs, total, err := h.svc.ListJobs(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list jobs: %v", err)
	}

	out := make([]*pb.Job, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobToProto(j))
	}

	return &pb.ListJobsResponse{Jobs: out, Total: int32(total)}, nil
}

// WatchJob streams job status updates until the job reaches a terminal state
// or the client disconnects. Uses the event bus for real-time updates plus
// an initial poll to avoid missing state that occurred before subscription.
func (h *jobHandler) WatchJob(req *pb.WatchJobRequest, stream pb.JobService_WatchJobServer) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "id is required")
	}

	ctx := stream.Context()

	// Send current state immediately.
	j, err := h.svc.GetJob(ctx, req.Id)
	if err != nil {
		return status.Errorf(codes.Internal, "get job: %v", err)
	}
	if j == nil {
		return status.Errorf(codes.NotFound, "job %s not found", req.Id)
	}

	if err := stream.Send(jobToStatusUpdate(j)); err != nil {
		return err
	}

	// Already terminal — nothing more to stream.
	if isTerminal(string(j.Status)) {
		return nil
	}

	// Subscribe to job events on the bus.
	updates := make(chan *pb.JobStatusUpdate, 16)

	h.svc.Bus.Subscribe("job.completed", func(_ context.Context, e events.Event) error {
		return forwardIfMatch(req.Id, e, updates)
	})
	h.svc.Bus.Subscribe("job.failed", func(_ context.Context, e events.Event) error {
		return forwardIfMatch(req.Id, e, updates)
	})

	// Poll every 2 s as fallback (bus is best-effort / async).
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case upd := <-updates:
			if err := stream.Send(upd); err != nil {
				return err
			}
			if isTerminal(upd.Status) {
				return nil
			}
		case <-ticker.C:
			j, err := h.svc.GetJob(ctx, req.Id)
			if err != nil {
				return status.Errorf(codes.Internal, "poll job: %v", err)
			}
			if j == nil {
				return nil
			}
			if err := stream.Send(jobToStatusUpdate(j)); err != nil {
				return err
			}
			if isTerminal(string(j.Status)) {
				return nil
			}
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func jobToProto(j *storage.Job) *pb.Job {
	p := &pb.Job{
		Id:         j.ID,
		Type:       j.Type,
		Status:     string(j.Status),
		Payload:    j.Payload,
		Pipeline:   j.Pipeline,
		Source:     j.Source,
		ResultId:   j.ResultID,
		Error:      j.Error,
		RetryCount: int32(j.RetryCount),
		MaxRetries: int32(j.MaxRetries),
		CreatedAt:  timestamppb.New(j.CreatedAt),
		UpdatedAt:  timestamppb.New(j.UpdatedAt),
	}
	if j.StartedAt != nil {
		p.StartedAt = timestamppb.New(*j.StartedAt)
	}
	if j.CompletedAt != nil {
		p.CompletedAt = timestamppb.New(*j.CompletedAt)
	}
	return p
}

func jobToStatusUpdate(j *storage.Job) *pb.JobStatusUpdate {
	u := &pb.JobStatusUpdate{
		JobId:    j.ID,
		Status:   string(j.Status),
		Error:    j.Error,
		ResultId: j.ResultID,
		UpdatedAt: timestamppb.New(j.UpdatedAt),
	}
	return u
}

func isTerminal(status string) bool {
	return status == string(storage.JobCompleted) || status == string(storage.JobFailed)
}

// jobEventPayload is the shape published to the event bus by the worker.
type jobEventPayload struct {
	ID       string `json:"id"`
	ResultID string `json:"result_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

func forwardIfMatch(jobID string, e events.Event, ch chan<- *pb.JobStatusUpdate) error {
	raw, err := json.Marshal(e.Data)
	if err != nil {
		return nil
	}
	var p jobEventPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	if p.ID != jobID {
		return nil
	}
	upd := &pb.JobStatusUpdate{
		JobId:     jobID,
		Status:    eventTypeToStatus(e.Type),
		Error:     p.Error,
		ResultId:  p.ResultID,
		UpdatedAt: timestamppb.New(time.Now()),
	}
	select {
	case ch <- upd:
	default:
	}
	return nil
}

func eventTypeToStatus(eventType string) string {
	switch eventType {
	case "job.completed":
		return string(storage.JobCompleted)
	case "job.failed":
		return string(storage.JobFailed)
	default:
		return string(storage.JobRunning)
	}
}
