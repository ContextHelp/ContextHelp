package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
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
		pipelineName = s.Pipes.Detect(pipeline.DetectInput{
			Source: obj.Source,
			Sniff:  contentSniff(obj.RawContent),
		})
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
		// Compensate: revert status if enqueueing fails.
		obj.Status = "inbox"
		obj.UpdatedAt = time.Now().Truncate(time.Second)
		_ = s.Store.Objects().Update(ctx, obj)
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

// ListInboxQueue returns a combined view of pending/running/failed jobs and raw objects.
// Controlled by InboxQueueFilter; defaults (all false) returns all three categories.
func (s *Service) ListInboxQueue(ctx context.Context, f InboxQueueFilter) ([]*InboxQueueItem, int, error) {
	showAll := !f.Pending && !f.Failed && !f.Raw

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	var items []*InboxQueueItem

	// Pending/running jobs.
	if showAll || f.Pending {
		jobs, _, err := s.Store.Jobs().List(ctx, storage.JobFilter{Status: storage.JobPending, Limit: limit})
		if err != nil {
			return nil, 0, fmt.Errorf("list pending jobs: %w", err)
		}
		for _, j := range jobs {
			items = append(items, jobToQueueItem(j))
		}
		running, _, err := s.Store.Jobs().List(ctx, storage.JobFilter{Status: storage.JobRunning, Limit: limit})
		if err != nil {
			return nil, 0, fmt.Errorf("list running jobs: %w", err)
		}
		for _, j := range running {
			items = append(items, jobToQueueItem(j))
		}
	}

	// Failed jobs.
	if showAll || f.Failed {
		failed, _, err := s.Store.Jobs().List(ctx, storage.JobFilter{Status: storage.JobFailed, Limit: limit})
		if err != nil {
			return nil, 0, fmt.Errorf("list failed jobs: %w", err)
		}
		for _, j := range failed {
			items = append(items, jobToQueueItem(j))
		}
	}

	// Raw objects.
	if showAll || f.Raw {
		objs, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{Status: "raw", Limit: limit})
		if err != nil {
			return nil, 0, fmt.Errorf("list raw objects: %w", err)
		}
		for _, o := range objs {
			items = append(items, objectToQueueItem(o))
		}
	}

	total := len(items)
	// Apply offset/limit to combined result set.
	if f.Offset > 0 {
		if f.Offset >= len(items) {
			return []*InboxQueueItem{}, 0, nil
		}
		items = items[f.Offset:]
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	return items, total, nil
}

func jobToQueueItem(j *storage.Job) *InboxQueueItem {
	// Extract content type from job type ("ingest:text" → "text").
	objType := j.Type
	if idx := len("ingest:"); len(j.Type) > idx && j.Type[:idx] == "ingest:" {
		objType = j.Type[idx:]
	}
	return &InboxQueueItem{
		ID:        j.ID,
		Kind:      "job",
		Status:    string(j.Status),
		Type:      objType,
		Source:    j.Source,
		Pipeline:  j.Pipeline,
		CreatedAt: j.CreatedAt,
		Error:     j.Error,
	}
}

func objectToQueueItem(o *storage.KnowledgeObject) *InboxQueueItem {
	return &InboxQueueItem{
		ID:        o.ID,
		Kind:      "object",
		Status:    o.Status,
		Type:      o.Type,
		Source:    o.Source,
		Pipeline:  o.Pipeline,
		CreatedAt: o.CreatedAt,
	}
}

// ClearInbox discards all current inbox items, returning the count cleared.
func (s *Service) ClearInbox(ctx context.Context) (int, error) {
	items, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{Status: "inbox", Limit: 10000})
	if err != nil {
		return 0, fmt.Errorf("clear inbox list: %w", err)
	}
	now := time.Now().Truncate(time.Second)
	for _, obj := range items {
		obj.Status = "discarded"
		obj.UpdatedAt = now
		if err := s.Store.Objects().Update(ctx, obj); err != nil {
			return 0, fmt.Errorf("clear inbox update %s: %w", obj.ID, err)
		}
	}
	return len(items), nil
}
