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
	// Tags replaces the tags with these labels, marked source "manual".
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
	var ms []uri.URI
	if p.Mentions != nil {
		ms = make([]uri.URI, 0, len(*p.Mentions))
		for _, m := range *p.Mentions {
			u, ok := mentions.Parse(strings.TrimSpace(m))
			if !ok {
				return nil, fmt.Errorf("%w %q: want @namespace.slug or ctxt://entity/<namespace>/<slug>", ErrInvalidMention, m)
			}
			ms = append(ms, u)
		}
	}

	obj, err := s.GetObject(ctx, id)
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
		tags := make([]storage.Tag, 0, len(*p.Tags))
		for _, label := range *p.Tags {
			if label = strings.TrimSpace(label); label != "" {
				tags = append(tags, storage.Tag{Label: label, Source: "manual"})
			}
		}
		obj.Tags = tags
	}
	if p.Mentions != nil {
		obj.Mentions = ms
	}
	obj.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateObject(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// ReprocessObject enqueues a job that re-runs step on the object id
// (see jobs.Reprocessor). storage.ErrNotFound when the object is
// missing; jobs.ErrUnknownReprocessStep for a step outside
// jobs.ReprocessSteps.
func (s *Service) ReprocessObject(ctx context.Context, id, step string) (*storage.Job, error) {
	if !slices.Contains(jobs.ReprocessSteps, step) {
		return nil, fmt.Errorf("step %q: %w", step, jobs.ErrUnknownReprocessStep)
	}
	if _, err := s.GetObject(ctx, id); err != nil {
		return nil, err
	}
	return s.Queue.EnqueueReprocess(ctx, id, step)
}
