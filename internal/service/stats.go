package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Stats is an at-a-glance summary of an instance, counted in the store.
type Stats struct {
	// KnowledgeObjects counts every object, whatever its status.
	KnowledgeObjects int `json:"knowledge_objects"`
	// ObjectsByType splits KnowledgeObjects by type.
	ObjectsByType map[string]int `json:"objects_by_type"`
	Jobs          JobCounts      `json:"jobs"`
	// Entities counts the entities the caller may list.
	Entities    int `json:"entities"`
	Feeds       int `json:"feeds"`
	ActiveFeeds int `json:"feeds_active"`
	// Reminders counts objects with a reminder set.
	Reminders int `json:"reminders_pending"`
	// Resurfacing counts resurfacing entries neither surfaced nor
	// dismissed, for the requested profile or for every profile. It is
	// nil on a backend without a resurfacing queue (Postgres, for now).
	Resurfacing *int `json:"resurfacing_candidates"`
}

// JobCounts counts jobs by status; Total is their sum.
type JobCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// StatsRequest scopes Stats.
type StatsRequest struct {
	// Profile scopes the resurfacing count; "" counts every profile.
	Profile string
	// EntityVisible, when set, keeps only the entities in namespaces it
	// accepts, so the count never exceeds what the caller may list.
	EntityVisible func(namespace string) bool
}

// Stats counts objects, jobs, entities, feeds, reminders and resurfacing
// candidates. A restricted viewer counts what it may see: objects and
// entities through its scope, its own resurfacing candidates, and
// nothing of the owner-only stores (jobs, feeds, reminders), which are
// counted as none rather than refused (ADR-080 §6: stats are computed
// inside the scoped query).
func (s *Service) Stats(ctx context.Context, req StatsRequest) (*Stats, error) {
	byType, err := s.Store(ctx).Objects().CountByType(ctx)
	if err != nil {
		return nil, err
	}
	st := &Stats{ObjectsByType: byType}
	for _, n := range byType {
		st.KnowledgeObjects += n
	}

	for _, c := range []struct {
		status storage.JobStatus
		into   *int
	}{
		{storage.JobPending, &st.Jobs.Pending},
		{storage.JobRunning, &st.Jobs.Running},
		{storage.JobCompleted, &st.Jobs.Completed},
		{storage.JobFailed, &st.Jobs.Failed},
	} {
		// The total is a COUNT over the status; one row is fetched.
		_, n, err := s.Store(ctx).Jobs().List(ctx, storage.JobFilter{Status: c.status, Limit: 1})
		if errors.Is(err, storage.ErrRestrictedViewer) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("count %s jobs: %w", c.status, err)
		}
		*c.into = n
		st.Jobs.Total += n
	}

	entities, err := s.Store(ctx).Entities().List(ctx, storage.EntityFilter{})
	if err != nil {
		return nil, fmt.Errorf("count entities: %w", err)
	}
	for _, e := range entities {
		if req.EntityVisible == nil || req.EntityVisible(e.Namespace) {
			st.Entities++
		}
	}

	feeds, err := s.Store(ctx).Feeds().List(ctx, storage.FeedFilter{})
	if err != nil && !errors.Is(err, storage.ErrRestrictedViewer) {
		return nil, fmt.Errorf("count feeds: %w", err)
	}
	st.Feeds = len(feeds)
	for _, f := range feeds {
		if f.Status == "active" {
			st.ActiveFeeds++
		}
	}

	reminders, err := s.Store(ctx).Objects().ListPendingReminders(ctx)
	if err != nil && !errors.Is(err, storage.ErrRestrictedViewer) {
		return nil, fmt.Errorf("count reminders: %w", err)
	}
	st.Reminders = len(reminders)

	candidates, err := s.Store(ctx).Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID: req.Profile, UnseenOnly: true,
	})
	switch {
	case errors.Is(err, storage.ErrNotImplemented), errors.Is(err, storage.ErrRestrictedViewer):
		// No queue on this backend, or none the viewer may read: the
		// count is unknown, not zero.
	case err != nil:
		return nil, fmt.Errorf("count resurfacing candidates: %w", err)
	default:
		n := len(candidates)
		st.Resurfacing = &n
	}
	return st, nil
}
