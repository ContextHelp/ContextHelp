package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// derivedEdgeTypes are incoming edge types (from derived -> to source)
// that denote an object derived from or discovered by the erased object.
var derivedEdgeTypes = map[string]bool{
	"derives_from":  true,
	"discovered_by": true,
}

// EraseBySourceOptions configures an erasure by source_key.
type EraseBySourceOptions struct {
	SourceKey    string `json:"source_key"`
	Tombstone    bool   `json:"tombstone"`
	Source       string `json:"source,omitempty"`
	ObligationID string `json:"obligation_id,omitempty"`
	ErasedBy     string `json:"erased_by,omitempty"`
}

// EraseResult summarizes what EraseBySourceKey removed and scrubbed.
type EraseResult struct {
	SourceKey          string   `json:"source_key"`
	MatchedIDs         []string `json:"matched_ids"`
	DeletedIDs         []string `json:"deleted_ids"`
	DerivedDeletedIDs  []string `json:"derived_deleted_ids,omitempty"`
	ScrubbedPages      int      `json:"scrubbed_pages"`
	DeletedPages       int      `json:"deleted_pages"`
	ScrubbedTopicIndex bool     `json:"scrubbed_topic_index"`
	TombstoneWritten   bool     `json:"tombstone_written"`
}

// EraseBySourceKey erases every object matching opts.SourceKey across all
// statuses, plus all transitively derived objects (derives_from,
// discovered_by), scrubs or deletes affected entity pages and topic_index
// entries, deletes unreferenced blobs immediately (grace=0), optionally
// records an erasure tombstone, and writes a content-free audit log entry.
//
// Hold enforcement is all-or-nothing: if any matched object, derived object,
// or affected entity page carries an active hold, nothing is deleted and an
// error wrapping storage.ErrObjectHeld is returned.
func (s *Service) EraseBySourceKey(ctx context.Context, opts EraseBySourceOptions) (*EraseResult, error) {
	sourceKey := strings.TrimSpace(opts.SourceKey)
	if sourceKey == "" {
		return nil, fmt.Errorf("%w: source_key is required", ErrInvalidSubmission)
	}
	drv := s.Store(ctx)
	res := &EraseResult{
		SourceKey:  sourceKey,
		MatchedIDs: []string{},
		DeletedIDs: []string{},
	}

	roots, _, err := drv.Objects().List(ctx, storage.ObjectFilter{
		SourceKey: sourceKey,
		Status:    storage.ObjectStatusAll,
	})
	if err != nil {
		return nil, fmt.Errorf("erase by source %q: list matches: %w", sourceKey, err)
	}

	rootSet := make(map[string]bool, len(roots))
	for _, r := range roots {
		res.MatchedIDs = append(res.MatchedIDs, r.ID)
		rootSet[r.ID] = true
	}

	// Cycle-safe BFS over incoming derives_from / discovered_by edges.
	closureOrder, closureSet, err := collectDerivedClosure(ctx, drv.Edges(), res.MatchedIDs)
	if err != nil {
		return nil, fmt.Errorf("erase by source %q: collect derived: %w", sourceKey, err)
	}

	// Find affected entity pages referencing any object in closureSet.
	affectedPages, err := findAffectedEntityPages(ctx, drv.Objects(), closureSet)
	if err != nil {
		return nil, fmt.Errorf("erase by source %q: find entity pages: %w", sourceKey, err)
	}

	// All-or-nothing hold pre-check across roots, derived objects, and affected pages.
	if err := checkClosureHolds(ctx, drv.Holds(), closureOrder, affectedPages); err != nil {
		return nil, err
	}

	// Delete in reverse BFS order (derived objects first, roots last) with Immediate: true.
	for i := len(closureOrder) - 1; i >= 0; i-- {
		id := closureOrder[i]
		dctx, ev := s.announce(ctx, events.TopicObjectDeleted, "service.erase", id, events.ObjectDeletedPayload{ObjectID: id})
		orphans, derr := blob.DeleteObjectWithOptions(dctx, drv, id, blob.DeleteOptions{Immediate: true})
		if derr != nil {
			if errors.Is(derr, storage.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("erase by source %q: delete %s: %w", sourceKey, id, derr)
		}
		logOrphans("erase object", id, orphans)
		s.publish(ctx, ev)
		res.DeletedIDs = append(res.DeletedIDs, id)
		if !rootSet[id] {
			res.DerivedDeletedIDs = append(res.DerivedDeletedIDs, id)
		}
	}

	// Scrub or delete affected entity pages.
	scrubbed, deletedPages, err := s.scrubEntityPagesForErasure(ctx, drv, affectedPages, closureSet)
	if err != nil {
		return nil, fmt.Errorf("erase by source %q: scrub entity pages: %w", sourceKey, err)
	}
	res.ScrubbedPages = scrubbed
	res.DeletedPages = deletedPages

	// Scrub topic_index if it references any erased ID.
	if len(closureSet) > 0 {
		scrubbedIdx, err := s.scrubTopicIndexForErasure(ctx, drv, closureSet)
		if err != nil {
			return nil, fmt.Errorf("erase by source %q: scrub topic index: %w", sourceKey, err)
		}
		res.ScrubbedTopicIndex = scrubbedIdx
	}

	if opts.Tombstone && drv.Tombstones() != nil {
		src := opts.Source
		if src == "" {
			src = defaultOperatorSource
		}
		erasedBy := opts.ErasedBy
		if erasedBy == "" {
			erasedBy = src
		}
		if err := drv.Tombstones().Put(ctx, &storage.ErasureTombstone{
			SourceKey:    sourceKey,
			Source:       src,
			ObligationID: opts.ObligationID,
			ErasedAt:     time.Now().UTC(),
			ErasedBy:     erasedBy,
		}); err != nil {
			return nil, fmt.Errorf("erase by source %q: write tombstone: %w", sourceKey, err)
		}
		res.TombstoneWritten = true
	}

	s.logErasureAudit(ctx, opts, res)
	return res, nil
}

func collectDerivedClosure(ctx context.Context, edges storage.EdgeStore, seeds []string) ([]string, map[string]bool, error) {
	seen := make(map[string]bool, len(seeds))
	order := make([]string, 0, len(seeds))
	queue := make([]string, 0, len(seeds))
	for _, id := range seeds {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
		queue = append(queue, id)
	}
	if edges == nil {
		return order, seen, nil
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		incoming, err := edges.ListTo(ctx, "object", cur)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range incoming {
			if e.FromType != "object" || !derivedEdgeTypes[e.EdgeType] {
				continue
			}
			if seen[e.FromID] {
				continue
			}
			seen[e.FromID] = true
			order = append(order, e.FromID)
			queue = append(queue, e.FromID)
		}
	}
	return order, seen, nil
}

func findAffectedEntityPages(ctx context.Context, objects storage.ObjectStore, erased map[string]bool) ([]*storage.KnowledgeObject, error) {
	if len(erased) == 0 {
		return nil, nil
	}
	pages, _, err := objects.List(ctx, storage.ObjectFilter{
		Type:   EntityPageType,
		Status: storage.ObjectStatusAll,
	})
	if err != nil {
		return nil, err
	}
	var affected []*storage.KnowledgeObject
	for _, p := range pages {
		if erased[p.ID] {
			continue
		}
		meta, ok := ExtractPageMeta(p)
		if !ok {
			continue
		}
		if pageReferencesAny(meta, erased) {
			affected = append(affected, p)
		}
	}
	return affected, nil
}

func pageReferencesAny(meta EntityPageMeta, ids map[string]bool) bool {
	for _, sid := range meta.SourceIDs {
		if ids[sid] {
			return true
		}
	}
	for _, rev := range meta.RevisionLog {
		if ids[rev.SourceID] {
			return true
		}
	}
	return false
}

func checkClosureHolds(ctx context.Context, holds storage.HoldStore, closure []string, pages []*storage.KnowledgeObject) error {
	if holds == nil {
		return nil
	}
	checkID := func(id string) error {
		active, err := holds.ListActive(ctx, id)
		if err != nil {
			return fmt.Errorf("check holds on %s: %w", id, err)
		}
		if len(active) > 0 {
			return &storage.HoldError{ObjectID: id, Holds: active}
		}
		return nil
	}
	for _, id := range closure {
		if err := checkID(id); err != nil {
			return err
		}
	}
	for _, p := range pages {
		if err := checkID(p.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) scrubEntityPagesForErasure(
	ctx context.Context,
	drv storage.StorageDriver,
	pages []*storage.KnowledgeObject,
	erased map[string]bool,
) (scrubbed, deleted int, err error) {
	for _, page := range pages {
		meta, ok := ExtractPageMeta(page)
		if !ok {
			continue
		}
		keptSources := make([]string, 0, len(meta.SourceIDs))
		for _, sid := range meta.SourceIDs {
			if !erased[sid] {
				keptSources = append(keptSources, sid)
			}
		}
		keptRevs := make([]RevisionEntry, 0, len(meta.RevisionLog))
		for _, rev := range meta.RevisionLog {
			if !erased[rev.SourceID] {
				keptRevs = append(keptRevs, rev)
			}
		}
		if len(keptSources) == 0 {
			if _, derr := blob.DeleteObjectWithOptions(ctx, drv, page.ID, blob.DeleteOptions{Immediate: true}); derr != nil && !errors.Is(derr, storage.ErrNotFound) {
				return scrubbed, deleted, derr
			}
			deleted++
			continue
		}
		meta.SourceIDs = keptSources
		meta.RevisionLog = keptRevs
		meta.RevisionCount++
		snippets := s.collectSourceSnippets(ctx, keptSources)
		page.RawContent = compilePageContent(meta.EntitySlug, snippets)
		page.TextContent = page.RawContent
		if page.Metadata == nil {
			page.Metadata = map[string]any{}
		}
		page.Metadata["entity_page"] = toMap(meta)
		page.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		if uerr := drv.Objects().Update(ctx, page); uerr != nil {
			return scrubbed, deleted, uerr
		}
		scrubbed++
	}
	return scrubbed, deleted, nil
}

func (s *Service) scrubTopicIndexForErasure(ctx context.Context, drv storage.StorageDriver, erased map[string]bool) (bool, error) {
	obj, err := s.getIndexObject(ctx)
	if err != nil || obj == nil {
		return false, err
	}
	var idx TopicIndex
	if err := json.Unmarshal([]byte(obj.RawContent), &idx); err != nil {
		return false, fmt.Errorf("decode topic index: %w", err)
	}
	changed := false
	keptIDs := make([]string, 0, len(idx.ObjectIDs))
	for _, id := range idx.ObjectIDs {
		if erased[id] {
			changed = true
			continue
		}
		keptIDs = append(keptIDs, id)
	}
	keptCats := make([]TopicCategory, 0, len(idx.Categories))
	for _, cat := range idx.Categories {
		entries := make([]TopicEntry, 0, len(cat.Entries))
		for _, e := range cat.Entries {
			if erased[e.ObjectID] {
				changed = true
				continue
			}
			entries = append(entries, e)
		}
		if len(entries) > 0 {
			keptCats = append(keptCats, TopicCategory{Topic: cat.Topic, Entries: entries})
		}
	}
	if !changed {
		return false, nil
	}
	idx.ObjectIDs = keptIDs
	idx.Categories = keptCats
	idx.GeneratedAt = time.Now().UTC().Truncate(time.Second)
	data, err := json.Marshal(&idx)
	if err != nil {
		return false, err
	}
	obj.RawContent = string(data)
	obj.TextContent = obj.RawContent
	obj.UpdatedAt = idx.GeneratedAt
	if err := drv.Objects().Update(ctx, obj); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) logErasureAudit(ctx context.Context, opts EraseBySourceOptions, res *EraseResult) {
	auditStore := s.Store(ctx).AuditLog()
	if auditStore == nil {
		return
	}
	actor := opts.ErasedBy
	if actor == "" {
		actor = opts.Source
	}
	if actor == "" {
		actor = defaultOperatorSource
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: "object.erased",
		ObjectID:  res.SourceKey,
		Actor:     actor,
		Payload: map[string]any{
			"source_key":           res.SourceKey,
			"matched_count":        len(res.MatchedIDs),
			"deleted_count":        len(res.DeletedIDs),
			"derived_deleted":      len(res.DerivedDeletedIDs),
			"scrubbed_pages":       res.ScrubbedPages,
			"deleted_pages":        res.DeletedPages,
			"scrubbed_topic_index": res.ScrubbedTopicIndex,
			"tombstone":            res.TombstoneWritten,
			"obligation_id":        opts.ObligationID,
		},
		CreatedAt: time.Now().UTC(),
	}
	if err := auditStore.Append(ctx, entry); err != nil {
		slog.Warn("erase: audit append (non-fatal)", "source_key", res.SourceKey, "err", err)
	}
}
