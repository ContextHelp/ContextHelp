package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// ObjectPatch is a partial update of an object's editable metadata
// (PATCH /api/v1/objects/{id}). A nil field is left as it is; a non-nil
// empty list clears tags or mentions.
type ObjectPatch struct {
	Type    *string `json:"type,omitempty"`
	Subtype *string `json:"subtype,omitempty"`
	// Title replaces the object's summaries with this one: the first
	// summary is what list and show print as the title.
	Title *string `json:"title,omitempty"`
	// Summary replaces the first summary and keeps the others.
	Summary *string `json:"summary,omitempty"`
	// Tags replaces the tags with these labels. A label already on the
	// object keeps its source and weight; a new label is marked "manual".
	Tags *[]string `json:"tags,omitempty"`
	// Mentions replaces the mentions, each "@ns.slug" or a
	// ctxt://entity/... URI.
	Mentions *[]string `json:"mentions,omitempty"`
}

var (
	// ErrInvalidPatch is a patch that cannot be applied.
	ErrInvalidPatch = errors.New("invalid object patch")
	// ErrInvalidMention is a mention that does not parse.
	ErrInvalidMention = errors.New("invalid mention")
)

// PatchObject applies p to the object id and stores it, bumping
// updated_at. storage.ErrNotFound when the object is missing;
// ErrInvalidPatch or ErrInvalidMention when p cannot be applied, in which
// case nothing is written.
func (s *Service) PatchObject(ctx context.Context, id string, p ObjectPatch) (*storage.KnowledgeObject, error) {
	if p == (ObjectPatch{}) {
		return nil, fmt.Errorf("%w: no field to update; send type, subtype, title, summary, tags or mentions", ErrInvalidPatch)
	}
	if p.Title != nil && p.Summary != nil {
		return nil, fmt.Errorf("%w: title and summary both set the first summary; send one", ErrInvalidPatch)
	}
	ms, err := parsePatchMentions(p.Mentions)
	if err != nil {
		return nil, err
	}

	// The stored row, refs and all: the patch touches no content.
	obj, err := s.Store(ctx).Objects().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Type != nil {
		obj.Type = *p.Type
	}
	if p.Subtype != nil {
		obj.Subtype = *p.Subtype
	}
	if p.Title != nil {
		obj.Summaries = []string{*p.Title}
	}
	if p.Summary != nil {
		if len(obj.Summaries) > 0 {
			obj.Summaries[0] = *p.Summary
		} else {
			obj.Summaries = []string{*p.Summary}
		}
	}
	if p.Tags != nil {
		obj.Tags = patchTags(obj.Tags, *p.Tags)
	}
	if p.Mentions != nil {
		obj.Mentions = ms
	}
	obj.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateObject(ctx, obj); err != nil {
		return nil, err
	}
	if p.Mentions != nil {
		if err := s.patchMentions(ctx, obj); err != nil {
			return nil, err
		}
	}
	if err := s.resolveContent(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// patchTags replaces stored with labels. A label already stored keeps its
// source and weight; a new label is marked manual; labels are trimmed and
// empty ones dropped.
func patchTags(stored []storage.Tag, labels []string) []storage.Tag {
	existing := make(map[string]storage.Tag, len(stored))
	for _, t := range stored {
		if _, ok := existing[t.Label]; !ok {
			existing[t.Label] = t
		}
	}
	tags := make([]storage.Tag, 0, len(labels))
	for _, label := range labels {
		if label = strings.TrimSpace(label); label == "" {
			continue
		}
		if kept, ok := existing[label]; ok {
			tags = append(tags, kept)
			continue
		}
		tags = append(tags, storage.Tag{Label: label, Source: "manual"})
	}
	return tags
}

// ReprocessObject enqueues a job that re-runs step on the object id
// (see jobs.Reprocessor). storage.ErrNotFound when the object is
// missing; jobs.ErrUnknownReprocessStep for a step outside
// jobs.ReprocessSteps.
func (s *Service) ReprocessObject(ctx context.Context, id, step string) (*storage.Job, error) {
	if !slices.Contains(jobs.ReprocessSteps, step) {
		return nil, fmt.Errorf("step %q: %w", step, jobs.ErrUnknownReprocessStep)
	}
	if _, err := s.Store(ctx).Objects().Get(ctx, id); err != nil {
		return nil, err
	}
	return s.Queue.EnqueueReprocess(ctx, id, step)
}

// parsePatchMentions parses the mentions of a patch; nil when the patch
// sets none, ErrInvalidMention for one that does not parse.
func parsePatchMentions(raw *[]string) ([]uri.URI, error) {
	if raw == nil {
		return nil, nil
	}
	ms := make([]uri.URI, 0, len(*raw))
	for _, m := range *raw {
		u, ok := mentions.Parse(strings.TrimSpace(m))
		if !ok {
			return nil, fmt.Errorf("%w %q: want @namespace.slug or ctxt://entity/<namespace>/<slug>", ErrInvalidMention, m)
		}
		ms = append(ms, u)
	}
	return ms, nil
}

// patchMentions persists the mentions a patch set on obj: thin entity
// rows and missing edges for each, and no edge for a mention the patch
// dropped that the caller's mentions field had made.
func (s *Service) patchMentions(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := s.writeUserMentionEdges(ctx, obj); err != nil {
		return fmt.Errorf("patch object: mentions: %w", err)
	}
	if err := s.dropUserMentionEdges(ctx, obj); err != nil {
		return fmt.Errorf("patch object: mentions: %w", err)
	}
	return nil
}

// dropUserMentionEdges deletes, in both directions, the mention edges of
// obj that name an entity no longer in obj.Mentions and that came from
// the mentions field as the caller asserted it (source_field
// MentionSourceMentions). An edge extracted from the object's text stays:
// the text still holds the mention. The entity rows stay too.
func (s *Service) dropUserMentionEdges(ctx context.Context, obj *storage.KnowledgeObject) error {
	kept := make(map[string]bool, len(obj.Mentions))
	for _, m := range obj.Mentions {
		kept[storage.MentionTarget(m)] = true
	}
	edges, err := s.Store(ctx).Edges().ListFrom(ctx, storage.EdgeEndpointObject, obj.ID)
	if err != nil {
		return fmt.Errorf("list mention edges: %w", err)
	}
	drop := make([]storage.EdgeMatch, 0, 2*len(edges))
	for _, e := range edges {
		if e.EdgeType != storage.EdgeTypeMentions || e.ToType != storage.EdgeEndpointEntity || kept[e.ToID] {
			continue
		}
		if field, _ := e.Metadata[storage.ProvenanceSourceField].(string); field != storage.MentionSourceMentions {
			continue
		}
		drop = append(drop, storage.EdgeMatch{
			FromType: storage.EdgeEndpointObject, FromID: obj.ID,
			ToType: storage.EdgeEndpointEntity, ToID: e.ToID,
			EdgeTypes: []string{storage.EdgeTypeMentions},
		}, storage.EdgeMatch{
			FromType: storage.EdgeEndpointEntity, FromID: e.ToID,
			ToType: storage.EdgeEndpointObject, ToID: obj.ID,
			EdgeTypes: []string{storage.EdgeTypeMentionedIn},
		})
	}
	if len(drop) == 0 {
		return nil
	}
	if _, err := s.Store(ctx).Edges().DeleteMatching(ctx, drop); err != nil {
		return fmt.Errorf("delete dropped mention edges: %w", err)
	}
	return nil
}
