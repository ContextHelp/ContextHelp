package service

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// storageAliasResolver implements search.AliasResolver backed by the alias store.
//
// Strategy: for each query term, resolve it to an objectID, then list all
// aliases for that objectID. The siblings are the concept synonyms that should
// broaden FTS coverage.
type storageAliasResolver struct {
	store   storage.AliasStore
	profile string
}

// newStorageAliasResolver returns an AliasResolver if store is non-nil, else nil.
// Returning nil is safe — ExpandQuery treats a nil resolver as a no-op.
func newStorageAliasResolver(store storage.AliasStore, profile string) *storageAliasResolver {
	if store == nil {
		return nil
	}
	return &storageAliasResolver{store: store, profile: profile}
}

// ResolveAliases resolves each term to its sibling aliases via the alias store.
// Terms that do not resolve to a known object, or whose object has no siblings,
// are silently omitted.
func (r *storageAliasResolver) ResolveAliases(ctx context.Context, terms []string) (map[string][]string, error) {
	out := make(map[string][]string, len(terms))

	for _, term := range terms {
		objectID, err := r.store.Resolve(ctx, term, r.profile)
		if err != nil {
			// Term is not a known alias — skip.
			continue
		}

		siblings, err := r.store.List(ctx, pluginapi.AliasFilter{ObjectID: objectID})
		if err != nil {
			continue
		}

		var aliases []string
		for _, a := range siblings {
			if a.Alias != term {
				aliases = append(aliases, a.Alias)
			}
		}
		if len(aliases) > 0 {
			out[term] = aliases
		}
	}

	return out, nil
}
