package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Alias scopes. A global alias resolves in every profile; a profile alias
// resolves only in its profile and shadows a global alias of the same name.
const (
	AliasScopeGlobal  = "global"
	AliasScopeProfile = "profile"
)

// ErrAliasExists marks a create for an (alias, scope, profile) key that is
// already taken.
var ErrAliasExists = errors.New("alias already exists")

// ErrInvalidAlias marks an alias request whose scope/profile pair is not
// valid: scope must be global or profile, and a profile name is required
// with scope profile and refused with scope global.
var ErrInvalidAlias = errors.New("invalid alias request")

// aliasStore returns the backend's alias store, or storage.ErrNotImplemented
// when the backend has none.
func (s *Service) aliasStore(ctx context.Context) (storage.AliasStore, error) {
	if s.store == nil {
		return nil, fmt.Errorf("aliases: %w", storage.ErrNotImplemented)
	}
	st := s.Store(ctx).Aliases()
	if st == nil {
		return nil, fmt.Errorf("aliases: %w", storage.ErrNotImplemented)
	}
	return st, nil
}

// NormalizeAliasScope applies the default scope (global) and validates the
// scope/profile pair. It returns the scope to store.
func NormalizeAliasScope(scope, profile string) (string, error) {
	if scope == "" {
		scope = AliasScopeGlobal
	}
	switch scope {
	case AliasScopeGlobal:
		if profile != "" {
			return "", fmt.Errorf("%w: profile is only valid with scope %q", ErrInvalidAlias, AliasScopeProfile)
		}
	case AliasScopeProfile:
		if profile == "" {
			return "", fmt.Errorf("%w: scope %q needs a profile", ErrInvalidAlias, AliasScopeProfile)
		}
	default:
		return "", fmt.Errorf("%w: scope must be %q or %q, got %q", ErrInvalidAlias, AliasScopeGlobal, AliasScopeProfile, scope)
	}
	return scope, nil
}

// CreateAlias stores alias → objectID. The object must exist (a miss wraps
// storage.ErrNotFound) and the (alias, scope, profile) key must be free
// (ErrAliasExists).
func (s *Service) CreateAlias(ctx context.Context, alias, objectID, scope, profile string) (*storage.Alias, error) {
	st, err := s.aliasStore(ctx)
	if err != nil {
		return nil, err
	}
	if alias == "" || objectID == "" {
		return nil, fmt.Errorf("%w: alias and object_id are required", ErrInvalidAlias)
	}
	scope, err = NormalizeAliasScope(scope, profile)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store(ctx).Objects().Get(ctx, objectID); err != nil {
		return nil, fmt.Errorf("alias target %s: %w", objectID, err)
	}
	existing, err := st.List(ctx, storage.AliasFilter{Scope: scope, Profile: profile})
	if err != nil {
		return nil, fmt.Errorf("alias lookup: %w", err)
	}
	for _, a := range existing {
		// The filter skips an empty profile, so match the key exactly here.
		if a.Alias == alias && a.Profile == profile {
			return nil, fmt.Errorf("%w: %q (scope %s) points at %s", ErrAliasExists, alias, scope, a.ObjectID)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	a := &storage.Alias{
		Alias: alias, ObjectID: objectID, Scope: scope, Profile: profile,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// ListAliases lists aliases, all of them or those pointing at objectID.
func (s *Service) ListAliases(ctx context.Context, objectID string) ([]*storage.Alias, error) {
	st, err := s.aliasStore(ctx)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, storage.AliasFilter{ObjectID: objectID})
}

// ResolveAlias returns the object ID an alias names: the profile alias for
// profile first, then the global one. A miss wraps storage.ErrNotFound.
func (s *Service) ResolveAlias(ctx context.Context, alias, profile string) (string, error) {
	st, err := s.aliasStore(ctx)
	if err != nil {
		return "", err
	}
	return st.Resolve(ctx, alias, profile)
}

// DeleteAlias removes the alias with the exact (alias, scope, profile) key.
// A miss wraps storage.ErrNotFound.
func (s *Service) DeleteAlias(ctx context.Context, alias, scope, profile string) error {
	st, err := s.aliasStore(ctx)
	if err != nil {
		return err
	}
	scope, err = NormalizeAliasScope(scope, profile)
	if err != nil {
		return err
	}
	return st.Delete(ctx, alias, scope, profile)
}
