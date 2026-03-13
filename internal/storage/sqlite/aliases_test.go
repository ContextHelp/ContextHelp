package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAliasTestObj(t *testing.T, suffix string) *storage.KnowledgeObject {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	return &storage.KnowledgeObject{
		ID:         "alias-obj-" + suffix,
		Type:       "text",
		RawContent: "test content",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestAliasStore_CreateAndResolve(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	obj := newAliasTestObj(t, "create")
	require.NoError(t, db.Objects().Create(ctx, obj))

	now := time.Now().UTC().Truncate(time.Second)
	a := &storage.Alias{
		Alias:     "my-doc",
		ObjectID:  obj.ID,
		Scope:     "global",
		Profile:   "",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, db.Aliases().Create(ctx, a))

	// Resolve: global scope should work regardless of profile.
	id, err := db.Aliases().Resolve(ctx, "my-doc", "any-profile")
	require.NoError(t, err)
	assert.Equal(t, obj.ID, id)
}

func TestAliasStore_ProfileScope_DoesNotLeakToOtherProfile(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	obj := newAliasTestObj(t, "profile")
	require.NoError(t, db.Objects().Create(ctx, obj))

	now := time.Now().UTC().Truncate(time.Second)
	a := &storage.Alias{
		Alias:     "private-doc",
		ObjectID:  obj.ID,
		Scope:     "profile",
		Profile:   "alice",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, db.Aliases().Create(ctx, a))

	// Alice can resolve.
	id, err := db.Aliases().Resolve(ctx, "private-doc", "alice")
	require.NoError(t, err)
	assert.Equal(t, obj.ID, id)

	// Bob cannot resolve.
	_, err = db.Aliases().Resolve(ctx, "private-doc", "bob")
	require.Error(t, err)
}

func TestAliasStore_List(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	obj := newAliasTestObj(t, "list")
	require.NoError(t, db.Objects().Create(ctx, obj))

	now := time.Now().UTC().Truncate(time.Second)
	for _, alias := range []string{"alias-a", "alias-b"} {
		require.NoError(t, db.Aliases().Create(ctx, &storage.Alias{
			Alias: alias, ObjectID: obj.ID, Scope: "global",
			Profile: "", CreatedAt: now, UpdatedAt: now,
		}))
	}

	aliases, err := db.Aliases().List(ctx, storage.AliasFilter{ObjectID: obj.ID})
	require.NoError(t, err)
	assert.Len(t, aliases, 2)
}

func TestAliasStore_Delete(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	obj := newAliasTestObj(t, "delete")
	require.NoError(t, db.Objects().Create(ctx, obj))

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Aliases().Create(ctx, &storage.Alias{
		Alias: "to-delete", ObjectID: obj.ID, Scope: "global",
		Profile: "", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, db.Aliases().Delete(ctx, "to-delete", "global", ""))

	_, err := db.Aliases().Resolve(ctx, "to-delete", "")
	require.Error(t, err, "alias should be gone after delete")
}

func TestMigration010_AliasesTable(t *testing.T) {
	db := newTestDriver(t)
	_, err := db.db.ExecContext(context.Background(),
		`INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
         VALUES ('test-alias', 'fake-id', 'global', '', datetime('now'), datetime('now'))`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FOREIGN KEY constraint failed")
}
