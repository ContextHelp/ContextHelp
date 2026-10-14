package service

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetObjectReminder sets a reminder on a knowledge object by ID.
func (s *Service) SetObjectReminder(ctx context.Context, id string, at time.Time) error {
	return s.Store.Objects().SetReminder(ctx, id, at)
}

// ClearObjectReminder removes the reminder from a knowledge object.
func (s *Service) ClearObjectReminder(ctx context.Context, id string) error {
	return s.Store.Objects().ClearReminder(ctx, id)
}

// ListPendingReminders returns all objects that have a scheduled reminder (regardless of due state).
func (s *Service) ListPendingReminders(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	objs, err := s.Store.Objects().ListPendingReminders(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// ListDueReminders returns objects whose reminder is due (remind_at <= now, not yet notified).
func (s *Service) ListDueReminders(ctx context.Context, now time.Time) ([]*storage.KnowledgeObject, error) {
	objs, err := s.Store.Objects().ListDueReminders(ctx, now)
	if err != nil {
		return nil, err
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// MarkReminded marks a reminder as delivered.
func (s *Service) MarkReminded(ctx context.Context, id string, now time.Time) error {
	return s.Store.Objects().MarkReminded(ctx, id, now)
}
