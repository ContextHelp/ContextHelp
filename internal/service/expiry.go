package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/expiry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

const expiredRedactionPlaceholder = "[EXPIRED]"

// ExpirySweepResult summarizes one pass of SweepExpired.
type ExpirySweepResult struct {
	Checked           int      `json:"checked"`
	DeletedIDs        []string `json:"deleted_ids"`
	DerivedDeletedIDs []string `json:"derived_deleted_ids,omitempty"`
	RedactedIDs       []string `json:"redacted_ids,omitempty"`
	SkippedHeld       int      `json:"skipped_held"`
	Errors            int      `json:"errors"`
}

// SetExpirySpanLocator installs an optional LLM-backed SpanLocator used for
// free-form natural-language expiry_scope instructions.
func (s *Service) SetExpirySpanLocator(loc expiry.SpanLocator) {
	s.expiryLocator = loc
}

// SetObjectExpiry sets expires_at (UTC) and optional natural-language
// expiry_scope on an existing object. Refuses held objects with ErrObjectHeld.
func (s *Service) SetObjectExpiry(ctx context.Context, id string, at time.Time, scope string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: object id is required", ErrInvalidSubmission)
	}
	if at.IsZero() {
		return fmt.Errorf("%w: expires_at is required", ErrInvalidSubmission)
	}
	return s.Store(ctx).Objects().SetExpiry(ctx, id, at.UTC(), strings.TrimSpace(scope))
}

// ClearObjectExpiry removes expires_at and expiry_scope from an existing
// object. Refuses held objects with ErrObjectHeld.
func (s *Service) ClearObjectExpiry(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: object id is required", ErrInvalidSubmission)
	}
	return s.Store(ctx).Objects().ClearExpiry(ctx, id)
}

// EraseObject erases a single object by ID along with its derived closure
// (derives_from / discovered_by), scrubs or deletes affected entity pages and
// topic_index entries, deletes unreferenced blobs immediately (grace=0), and
// enforces all-or-nothing hold protection across the closure. Unlike
// EraseBySourceKey, it does not write an erasure tombstone.
func (s *Service) EraseObject(ctx context.Context, id, reason string) (*EraseResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: object id is required", ErrInvalidSubmission)
	}
	drv := s.Store(ctx)
	res := &EraseResult{
		MatchedIDs: []string{id},
		DeletedIDs: []string{},
	}

	closureOrder, closureSet, err := collectDerivedClosure(ctx, drv.Edges(), []string{id})
	if err != nil {
		return nil, fmt.Errorf("erase object %s: collect derived: %w", id, err)
	}
	affectedPages, err := findAffectedEntityPages(ctx, drv.Objects(), closureSet)
	if err != nil {
		return nil, fmt.Errorf("erase object %s: find entity pages: %w", id, err)
	}
	if err := checkClosureHolds(ctx, drv.Holds(), closureOrder, affectedPages); err != nil {
		return nil, err
	}

	for i := len(closureOrder) - 1; i >= 0; i-- {
		cid := closureOrder[i]
		dctx, ev := s.announce(ctx, events.TopicObjectDeleted, "service.expiry", cid, events.ObjectDeletedPayload{ObjectID: cid})
		orphans, derr := blob.DeleteObjectWithOptions(dctx, drv, cid, blob.DeleteOptions{Immediate: true})
		if derr != nil {
			if errors.Is(derr, storage.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("erase object %s: delete %s: %w", id, cid, derr)
		}
		logOrphans("expire object", cid, orphans)
		s.publish(ctx, ev)
		res.DeletedIDs = append(res.DeletedIDs, cid)
		if cid != id {
			res.DerivedDeletedIDs = append(res.DerivedDeletedIDs, cid)
		}
	}

	scrubbed, deletedPages, err := s.scrubEntityPagesForErasure(ctx, drv, affectedPages, closureSet)
	if err != nil {
		return nil, fmt.Errorf("erase object %s: scrub entity pages: %w", id, err)
	}
	res.ScrubbedPages = scrubbed
	res.DeletedPages = deletedPages

	if len(closureSet) > 0 {
		scrubbedIdx, err := s.scrubTopicIndexForErasure(ctx, drv, closureSet)
		if err != nil {
			return nil, fmt.Errorf("erase object %s: scrub topic index: %w", id, err)
		}
		res.ScrubbedTopicIndex = scrubbedIdx
	}

	if drv.AuditLog() != nil {
		_ = drv.AuditLog().Append(ctx, &storage.AuditEntry{
			ID:        uuid.New().String(),
			EventType: "object.expired",
			ObjectID:  id,
			Actor:     "system:expiry",
			Payload: map[string]any{
				"reason":            reason,
				"deleted_count":     len(res.DeletedIDs),
				"derived_deleted":   len(res.DerivedDeletedIDs),
				"scrubbed_pages":    res.ScrubbedPages,
				"deleted_pages":     res.DeletedPages,
				"scrubbed_topic_ix": res.ScrubbedTopicIndex,
			},
			CreatedAt: time.Now().UTC(),
		})
	}
	return res, nil
}

// SweepExpired processes up to limit objects whose expires_at <= now.
// Objects with an empty ExpiryScope are fully erased via EraseObject.
// Objects with a non-empty natural-language ExpiryScope are selectively
// redacted in place and their ExpiresAt/ExpiryScope cleared.
// Objects under an active hold are skipped cleanly until the hold is released.
func (s *Service) SweepExpired(ctx context.Context, now time.Time, limit int) (*ExpirySweepResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	drv := s.Store(ctx)
	expired, err := drv.Objects().ListExpired(ctx, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("sweep expired: list: %w", err)
	}
	out := &ExpirySweepResult{
		Checked:    len(expired),
		DeletedIDs: []string{},
	}
	for _, obj := range expired {
		if strings.TrimSpace(obj.ExpiryScope) == "" {
			res, eerr := s.EraseObject(ctx, obj.ID, "ttl_expired")
			if eerr != nil {
				if errors.Is(eerr, storage.ErrObjectHeld) {
					out.SkippedHeld++
					slog.Debug("expiry sweep: skipping held object", "object_id", obj.ID)
					continue
				}
				out.Errors++
				slog.Warn("expiry sweep: erase failed", "object_id", obj.ID, "error", eerr)
				continue
			}
			out.DeletedIDs = append(out.DeletedIDs, res.DeletedIDs...)
			out.DerivedDeletedIDs = append(out.DerivedDeletedIDs, res.DerivedDeletedIDs...)
			continue
		}

		if rerr := s.redactExpiredObject(ctx, obj, now.UTC()); rerr != nil {
			if errors.Is(rerr, storage.ErrObjectHeld) {
				out.SkippedHeld++
				slog.Debug("expiry sweep: skipping held object for selective redaction", "object_id", obj.ID)
				continue
			}
			out.Errors++
			slog.Warn("expiry sweep: selective redaction failed", "object_id", obj.ID, "scope", obj.ExpiryScope, "error", rerr)
			continue
		}
		out.RedactedIDs = append(out.RedactedIDs, obj.ID)
	}
	return out, nil
}

func (s *Service) redactExpiredObject(ctx context.Context, obj *storage.KnowledgeObject, now time.Time) error {
	drv := s.Store(ctx)
	if drv.Holds() != nil {
		held, err := drv.Holds().IsHeld(ctx, obj.ID)
		if err != nil {
			return err
		}
		if held {
			return fmt.Errorf("object %s: %w", obj.ID, storage.ErrObjectHeld)
		}
	}

	scope := strings.TrimSpace(obj.ExpiryScope)
	plan := expiry.ClassifyScope(scope)

	// Hydrate blob-backed RawContent before pattern redaction if needed.
	oldBlobKey := ""
	if ref, ok := blobref.Key(obj.RawContent); ok {
		oldBlobKey = ref
		if !plan.RawContent && (len(plan.Patterns) > 0 || plan.NeedsLLM) {
			if err := s.resolveContent(ctx, obj); err != nil {
				return fmt.Errorf("hydrate content for selective expiry: %w", err)
			}
		}
	}

	replacements, err := applyExpiryRedaction(ctx, obj, scope, plan, s.expiryLocator)
	if err != nil {
		return err
	}

	obj.ExpiresAt = nil
	obj.ExpiryScope = ""
	obj.UpdatedAt = now
	if obj.Metadata == nil {
		obj.Metadata = map[string]any{}
	}
	obj.Metadata["expiry_redacted_at"] = now.Format(time.RFC3339)
	obj.Metadata["expiry_redacted_scope"] = scope

	uctx, ev := s.announce(ctx, events.TopicObjectUpdated, "service.expiry", obj.ID, events.ObjectUpdatedPayload{
		ObjectID: obj.ID,
		Type:     obj.Type,
		Fields:   []string{"expiry_redacted"},
	})
	if err := drv.Objects().Update(uctx, obj); err != nil {
		return err
	}
	s.publish(ctx, ev)

	// Immediately remove replaced raw blob if no longer referenced.
	if oldBlobKey != "" && drv.Blobs() != nil {
		if refs, err := drv.Objects().CountBlobRefs(ctx, oldBlobKey); err == nil && refs == 0 {
			_ = drv.Blobs().Delete(ctx, oldBlobKey)
		}
	}

	if drv.AuditLog() != nil {
		_ = drv.AuditLog().Append(ctx, &storage.AuditEntry{
			ID:        uuid.New().String(),
			EventType: "object.expiry_redacted",
			ObjectID:  obj.ID,
			Actor:     "system:expiry",
			Payload: map[string]any{
				"scope":        scope,
				"replacements": replacements,
			},
			CreatedAt: now,
		})
	}
	return nil
}

func applyExpiryRedaction(ctx context.Context, obj *storage.KnowledgeObject, scope string, plan expiry.ScopePlan, loc expiry.SpanLocator) (int, error) {
	replacements := 0
	if plan.RawContent {
		obj.RawContent = ""
		obj.TextContent = ""
		replacements++
	} else {
		n, err := redactObjectTextFields(ctx, obj, scope, plan, loc)
		if err != nil {
			return 0, err
		}
		replacements += n
	}
	if plan.Metadata {
		obj.Metadata = nil
		replacements++
	}
	if plan.InboxNote {
		obj.InboxNote = ""
		replacements++
	} else if !plan.RawContent && obj.InboxNote != "" {
		out, n, err := expiry.RedactText(ctx, obj.InboxNote, scope, expiredRedactionPlaceholder, plan, loc)
		if err != nil {
			return 0, err
		}
		obj.InboxNote = out
		replacements += n
	}
	return replacements, nil
}

func redactObjectTextFields(ctx context.Context, obj *storage.KnowledgeObject, scope string, plan expiry.ScopePlan, loc expiry.SpanLocator) (int, error) {
	total := 0
	var n int
	var err error
	if obj.RawContent, n, err = expiry.RedactText(ctx, obj.RawContent, scope, expiredRedactionPlaceholder, plan, loc); err != nil {
		return 0, err
	}
	total += n
	if obj.TextContent, n, err = expiry.RedactText(ctx, obj.TextContent, scope, expiredRedactionPlaceholder, plan, loc); err != nil {
		return 0, err
	}
	total += n
	for i, sum := range obj.Summaries {
		if obj.Summaries[i], n, err = expiry.RedactText(ctx, sum, scope, expiredRedactionPlaceholder, plan, loc); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// ExpirySweeper runs SweepExpired on a fixed cadence until ctx is cancelled.
type ExpirySweeper struct {
	Sweep    func(ctx context.Context, now time.Time) error
	Interval time.Duration
}

// Run executes an initial sweep and then sweeps on every Interval tick.
func (e *ExpirySweeper) Run(ctx context.Context) error {
	if e == nil || e.Sweep == nil {
		<-ctx.Done()
		return nil
	}
	interval := e.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	if err := e.Sweep(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
		slog.Warn("expiry sweeper: initial sweep failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := e.Sweep(ctx, now.UTC()); err != nil && ctx.Err() == nil {
				slog.Warn("expiry sweeper: sweep failed", "error", err)
			}
		}
	}
}
