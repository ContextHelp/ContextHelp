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

// inboxStatus is the status that makes an object an inbox item: capture
// sets it, the inbox list and clear select on it, and triage and discard
// act only on an object that has it.
const inboxStatus = "inbox"

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
		Status:      inboxStatus,
		InboxNote:   req.InboxNote,
		Mentions:    req.Mentions,
		// T-0573: caller-asserted hints land directly on Tags (Source:"user")
		// since no pipeline runs at inbox-capture time. Triage will re-enqueue
		// the object via a pipeline job that picks these up via UserHints.
		Tags: userHintsToTags(req.Hints),
		// T-0588: caller-asserted profile partitions the inbox item from
		// the start so list/find queries scoped to a profile see it.
		ProfileID: req.Profile,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Store.Objects().Create(ctx, obj); err != nil {
		return nil, fmt.Errorf("capture to inbox: %w", err)
	}

	if ev, err := events.NewEvent("service.inbox", string(events.TopicInboxCaptured), obj); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return obj, nil
}

// ListInbox returns inbox items matching the filter.
func (s *Service) ListInbox(ctx context.Context, filter InboxFilter) ([]*storage.KnowledgeObject, int, error) {
	f := storage.ObjectFilter{
		Status: inboxStatus,
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

// TriageInbox promotes an inbox item to "active" status and enqueues it
// for pipeline processing. An object that is not an inbox item, or no
// object at all, wraps storage.ErrNotFound and nothing changes.
func (s *Service) TriageInbox(ctx context.Context, id string, req TriageRequest) (string, error) {
	obj, err := s.Store.Objects().Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("triage inbox item %s: %w", id, err)
	}
	now := time.Now().Truncate(time.Second)
	if err := s.Store.Objects().TransitionStatus(ctx, id, inboxStatus, "active", now); err != nil {
		return "", fmt.Errorf("triage inbox item %s: %w", id, err)
	}
	obj.Status = "active"
	obj.UpdatedAt = now

	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.detectPipeline(obj.Source, "", obj.RawContent)
	}

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
		// T-0573: forward caller-asserted hints to the worker so the
		// auto-tagger merge preserves them as Tag{Source:"user"}.
		UserHints: req.Hints,
	}
	if err := s.Queue.Enqueue(ctx, job); err != nil {
		// Compensate: return the item to the inbox.
		_ = s.Store.Objects().TransitionStatus(ctx, id, "active", inboxStatus, time.Now().Truncate(time.Second))
		return "", fmt.Errorf("triage inbox enqueue: %w", err)
	}

	if ev, err := events.NewEvent("service.inbox", string(events.TopicInboxTriaged), obj); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

// DiscardInbox marks an inbox item as discarded. An object that is not
// an inbox item, or no object at all, wraps storage.ErrNotFound and
// nothing changes.
func (s *Service) DiscardInbox(ctx context.Context, id string) error {
	if err := s.Store.Objects().TransitionStatus(ctx, id, inboxStatus, "discarded", time.Now().Truncate(time.Second)); err != nil {
		return fmt.Errorf("discard inbox item %s: %w", id, err)
	}
	return nil
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

// clearInboxPage is how many inbox items ClearInbox reads per page.
var clearInboxPage = 500

// ClearInbox discards every current inbox item, page by page until none
// is left, and returns how many it discarded. Objects in any other
// status are untouched.
func (s *Service) ClearInbox(ctx context.Context) (int, error) {
	now := time.Now().Truncate(time.Second)
	seen := map[string]bool{}
	for {
		items, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{Status: inboxStatus, Limit: clearInboxPage})
		if err != nil {
			return len(seen), fmt.Errorf("clear inbox list: %w", err)
		}
		if len(items) == 0 {
			return len(seen), nil
		}
		for _, obj := range items {
			// A discarded item never lists as inbox again; seeing one
			// twice means the update did not take, and the loop would
			// never end.
			if seen[obj.ID] {
				return len(seen), fmt.Errorf("clear inbox: %s is still in the inbox after discarding it", obj.ID)
			}
			seen[obj.ID] = true
			obj.Status = "discarded"
			obj.UpdatedAt = now
			if err := s.Store.Objects().Update(ctx, obj); err != nil {
				return len(seen) - 1, fmt.Errorf("clear inbox update %s: %w", obj.ID, err)
			}
		}
	}
}
