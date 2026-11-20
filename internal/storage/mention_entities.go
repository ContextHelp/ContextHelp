package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	uri "hop.top/cite/scheme"
)

// EnsureMentionEntity makes sure the entity a mention names has a row,
// the entity every mention edge points at. A mention that resolves, by
// slug or alias, to an existing entity of any content status is left as
// it is and that entity returned with created false. An unresolved one
// gets a local placeholder, a ContentStatusThin row titled from its slug
// (ADR-029), returned with created true. A u that is no ctxt://entity/
// URI has no entity: it returns nil.
func EnsureMentionEntity(ctx context.Context, entities EntityStore, u uri.URI, now time.Time) (entity *Entity, created bool, err error) {
	slug := MentionSlug(u)
	if slug == "" {
		return nil, false, nil
	}
	found, rerr := entities.Resolve(ctx, slug)
	switch {
	case rerr == nil && found != nil:
		return found, false, nil
	case rerr != nil && !errors.Is(rerr, ErrNotFound):
		return nil, false, fmt.Errorf("resolve entity %q: %w", slug, rerr)
	}
	entity = &Entity{
		Slug:          slug,
		Title:         MentionTitle(slug),
		Namespace:     strings.SplitN(slug, "/", 2)[0],
		ContentStatus: ContentStatusThin,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := entities.UpsertThin(ctx, entity); err != nil {
		return nil, false, fmt.Errorf("upsert thin entity %q: %w", slug, err)
	}
	return entity, true, nil
}

// EnsureMentionEntities runs EnsureMentionEntity for each target, the
// step every writer of mention edges takes before it writes them so no
// edge names an entity without a row.
func EnsureMentionEntities(ctx context.Context, entities EntityStore, targets []uri.URI, now time.Time) error {
	for _, u := range targets {
		if _, _, err := EnsureMentionEntity(ctx, entities, u, now); err != nil {
			return err
		}
	}
	return nil
}

// MentionTitle derives a display title from an entity slug: its last
// segment, dashes and underscores replaced with spaces.
func MentionTitle(slug string) string {
	parts := strings.Split(slug, "/")
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "-", " ")
	return strings.ReplaceAll(last, "_", " ")
}
