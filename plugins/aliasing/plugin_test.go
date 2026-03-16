package aliasing_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt-plugin-aliasing"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStorageDriver is a minimal StorageDriver that just provides AliasStore.
type mockStorageDriver struct {
	aliasStore *mockAliasStore
}

func (m *mockStorageDriver) Aliases() pluginapi.AliasStore { return m.aliasStore }

func pluginDepsWithStore(store *mockAliasStore) pluginapi.Deps {
	return pluginapi.Deps{
		Store: &mockStorageDriver{aliasStore: store},
	}
}

// TestPluginE2E verifies the full set/resolve/list/remove cycle.
func TestPluginE2E(t *testing.T) {
	store := &mockAliasStore{}
	p := aliasing.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginDepsWithStore(store)))

	ctx := context.Background()
	objectID := "obj_real_001"

	// Set alias.
	require.NoError(t, p.SetAliasOp(ctx, "project-charter", objectID, "global", ""))

	// Resolve.
	id, err := p.ResolveID(ctx, "project-charter", "")
	require.NoError(t, err)
	assert.Equal(t, objectID, id)

	// Unknown alias passes through.
	id, err = p.ResolveID(ctx, "obj_other", "")
	require.NoError(t, err)
	assert.Equal(t, "obj_other", id)

	// List.
	aliases, err := p.ListAliases(ctx, objectID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "project-charter", aliases[0].Alias)

	// Remove.
	require.NoError(t, p.RemoveAlias(ctx, "project-charter", "global", ""))

	// Verify removed.
	id, err = p.ResolveID(ctx, "project-charter", "")
	require.NoError(t, err)
	assert.Equal(t, "project-charter", id, "after remove, should pass through unchanged")
}
