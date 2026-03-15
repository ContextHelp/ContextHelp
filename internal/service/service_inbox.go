package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CaptureToInbox stores content as an inbox item without enqueuing a job.
func (s *Service) CaptureToInbox(ctx context.Context, req InboxCaptureRequest) (*storage.KnowledgeObject, error) {
	now := time.Now().Truncate(time.Second)
	id := uuid.New().String()

	h := sha256.Sum256([]byte(req.Content))
	hash := hex.EncodeToString(h[:])

	objType := req.Type
	if objType == "" {
		objType = "text"
	}

	obj := &storage.KnowledgeObject{
		ID:          id,
		Type:        objType,
		RawContent:  req.Content,
		Source:      req.Source,
		ContentHash: hash,
		Status:      "inbox",
		InboxNote:   req.InboxNote,
		Mentions:    req.Mentions,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.Store.Objects().Create(ctx, obj); err != nil {
		return nil, fmt.Errorf("capture to inbox: %w", err)
	}

	if ev, err := events.NewEvent("service.inbox", "inbox.captured", obj); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return obj, nil
}

// ListInbox returns inbox items matching the filter.
func (s *Service) ListInbox(ctx context.Context, filter InboxFilter) ([]*storage.KnowledgeObject, int, error) {
	f := storage.ObjectFilter{
		Status: "inbox",
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}
	if !filter.Before.IsZero() {
		f.Before = &filter.Before
	}
	if !filter.After.IsZero() {
		f.After = &filter.After
	}
	return s.Store.Objects().List(ctx, f)
}

// TriageInbox promotes an inbox item to "active" status and enqueues it for pipeline processing.
func (s *Service) TriageInbox(ctx context.Context, id string, req TriageRequest) (string, error) {
	obj, err := s.Store.Objects().Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("triage inbox: %w", err)
	}

	obj.Status = "active"
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	if err := s.Store.Objects().Update(ctx, obj); err != nil {
		return "", fmt.Errorf("triage inbox update: %w", err)
	}

	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.Pipes.SelectPipeline(obj.RawContent)
	}

	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       "ingest:" + obj.Type,
		Status:     storage.JobPending,
		Payload:    obj.RawContent,
		Pipeline:   pipelineName,
		Source:     obj.Source,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Queue.Enqueue(ctx, job); err != nil {
		return "", fmt.Errorf("triage inbox enqueue: %w", err)
	}

	if ev, err := events.NewEvent("service.inbox", "inbox.triaged", obj); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

// DiscardInbox marks an inbox item as discarded.
func (s *Service) DiscardInbox(ctx context.Context, id string) error {
	obj, err := s.Store.Objects().Get(ctx, id)
	if err != nil {
		return fmt.Errorf("discard inbox: %w", err)
	}
	obj.Status = "discarded"
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	return s.Store.Objects().Update(ctx, obj)
}
