package aliasing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/plugins/aliasing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAliasStore is an in-memory implementation for tests.
type mockAliasStore struct {
	aliases []*storage.Alias
}

func (m *mockAliasStore) Create(_ context.Context, a *storage.Alias) error {
	m.aliases = append(m.aliases, a)
	return nil
}

func (m *mockAliasStore) Resolve(_ context.Context, alias, profile string) (string, error) {
	// Profile-scope first, then global.
	for _, a := range m.aliases {
		if a.Alias == alias && a.Scope == "profile" && a.Profile == profile {
			return a.ObjectID, nil
		}
	}
	for _, a := range m.aliases {
		if a.Alias == alias && a.Scope == "global" {
			return a.ObjectID, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func (m *mockAliasStore) List(_ context.Context, f storage.AliasFilter) ([]*storage.Alias, error) {
	var out []*storage.Alias
	for _, a := range m.aliases {
		if f.ObjectID != "" && a.ObjectID != f.ObjectID {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (m *mockAliasStore) Delete(_ context.Context, alias, scope, profile string) error {
	var filtered []*storage.Alias
	for _, a := range m.aliases {
		if a.Alias != alias || a.Scope != scope || a.Profile != profile {
			filtered = append(filtered, a)
		}
	}
	m.aliases = filtered
	return nil
}

func TestSetAlias_CreatesRecord(t *testing.T) {
	store := &mockAliasStore{}
	now := time.Now()
	err := aliasing.SetAlias(context.Background(), store, "my-doc", "obj_001", "global", "", now)
	require.NoError(t, err)
	aliases, err := store.List(context.Background(), storage.AliasFilter{ObjectID: "obj_001"})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "my-doc", aliases[0].Alias)
}

func TestResolveID_KnownAlias_ReturnsObjectID(t *testing.T) {
	store := &mockAliasStore{}
	now := time.Now()
	_ = aliasing.SetAlias(context.Background(), store, "charter", "obj_abc", "global", "", now)

	id, err := aliasing.ResolveAlias(context.Background(), store, "charter", "")
	require.NoError(t, err)
	assert.Equal(t, "obj_abc", id)
}

func TestResolveID_UnknownAlias_ReturnsSelf(t *testing.T) {
	store := &mockAliasStore{}
	id, err := aliasing.ResolveAlias(context.Background(), store, "obj_abc123", "")
	require.NoError(t, err)
	assert.Equal(t, "obj_abc123", id, "unknown alias passes through unchanged")
}
