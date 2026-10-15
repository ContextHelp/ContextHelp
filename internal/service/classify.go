package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrInvalidClass is a class name a classification cannot use.
var ErrInvalidClass = errors.New("invalid class")

// ClassifyObject adds add to and removes remove from the classes of
// object id (ADR-080 §3, `ctxt object classify`) and stores it, bumping
// updated_at. storage.ErrNotFound when the object is missing;
// ErrInvalidClass when a name is malformed or nothing is asked for, in
// which case nothing is written. The stored classes come back sorted and
// unique.
func (s *Service) ClassifyObject(ctx context.Context, id string, add, remove []string) (*storage.KnowledgeObject, error) {
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("%w: nothing to add or remove", ErrInvalidClass)
	}
	for _, c := range slices.Concat(add, remove) {
		if err := storage.ValidateClassName(c); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidClass, err)
		}
	}
	obj, err := s.Store(ctx).Objects().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	classes := slices.Concat(obj.Classes, add)
	classes = slices.DeleteFunc(classes, func(c string) bool { return slices.Contains(remove, c) })
	obj.Classes = storage.NormalizeClasses(classes)
	obj.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateObject(ctx, obj); err != nil {
		return nil, err
	}
	if err := s.resolveContent(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
