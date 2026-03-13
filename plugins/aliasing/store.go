package aliasing

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetAlias creates a new alias record.
func SetAlias(ctx context.Context, store storage.AliasStore, alias, objectID, scope, profile string, now time.Time) error {
	return store.Create(ctx, &storage.Alias{
		Alias:     alias,
		ObjectID:  objectID,
		Scope:     scope,
		Profile:   profile,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// ResolveAlias resolves an alias to an object ID.
// Returns (idOrAlias, nil) unchanged if the alias is not found (soft resolution).
func ResolveAlias(ctx context.Context, store storage.AliasStore, idOrAlias, profile string) (string, error) {
	id, err := store.Resolve(ctx, idOrAlias, profile)
	if err != nil {
		// Not found: return input unchanged.
		return idOrAlias, nil
	}
	return id, nil
}
