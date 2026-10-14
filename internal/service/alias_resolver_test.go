package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAliasStore resolves from a fixed table; resolveErr and listErr
// replace a lookup's result with an error.
type fakeAliasStore struct {
	byAlias    map[string]string // alias -> object id
	resolveErr error
	listErr    error
}

func (f *fakeAliasStore) Create(context.Context, *storage.Alias) error { return nil }

func (f *fakeAliasStore) Resolve(_ context.Context, alias, _ string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	id, ok := f.byAlias[alias]
	if !ok {
		return "", fmt.Errorf("alias %q: %w", alias, storage.ErrNotFound)
	}
	return id, nil
}

func (f *fakeAliasStore) List(_ context.Context, filter storage.AliasFilter) ([]*storage.Alias, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*storage.Alias
	for alias, id := range f.byAlias {
		if id == filter.ObjectID {
			out = append(out, &storage.Alias{Alias: alias, ObjectID: id})
		}
	}
	return out, nil
}

func (f *fakeAliasStore) Delete(context.Context, string, string, string) error { return nil }

// Only a miss means "not an alias": a term the store does not know is
// skipped, a known one expands to its siblings.
func TestStorageAliasResolver_NotFoundSkipsTerm(t *testing.T) {
	r := newStorageAliasResolver(&fakeAliasStore{byAlias: map[string]string{"k8s": "obj-1", "kubernetes": "obj-1"}}, "")
	got, err := r.ResolveAliases(context.Background(), []string{"k8s", "deploy"})
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"k8s": {"kubernetes"}}, got)
}

// Any other store error is a failed lookup and reaches the caller.
func TestStorageAliasResolver_StoreErrorsPropagate(t *testing.T) {
	cause := errors.New("connection refused")
	for name, store := range map[string]*fakeAliasStore{
		"resolve": {resolveErr: cause},
		"list":    {byAlias: map[string]string{"k8s": "obj-1"}, listErr: cause},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := newStorageAliasResolver(store, "").ResolveAliases(context.Background(), []string{"k8s"})
			require.ErrorIs(t, err, cause)
			assert.Nil(t, got)
		})
	}
}

// aliasFailDriver is a store whose alias table cannot be read.
type aliasFailDriver struct {
	storage.StorageDriver
	err error
}

func (d aliasFailDriver) Aliases() storage.AliasStore {
	return &fakeAliasStore{resolveErr: d.err}
}

// A search whose alias lookup fails reports the failure instead of
// returning results for the unexpanded query.
func TestHybridSearch_AliasStoreErrorFailsSearch(t *testing.T) {
	svc := newTestService(t)
	cause := errors.New("aliases table unreadable")
	svc.Store = aliasFailDriver{StorageDriver: svc.Store, err: cause}
	cfg := config.SearchConfig{DefaultMode: "hybrid", FallbackToFTS: true}

	_, err := svc.HybridSearch(context.Background(), "anything", 10, retrieval.SemanticSource{}, cfg)
	require.ErrorIs(t, err, cause)
}
