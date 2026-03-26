package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// ---------------------------------------------------------------------------
// US-0035 Tests
// ---------------------------------------------------------------------------

// TestUS0035_ObjectCountIdenticalAfterMigration verifies that after populating
// a source SQLite database and copying it via snapshot to a destination SQLite
// database, both databases report identical object counts.
func TestUS0035_ObjectCountIdenticalAfterMigration(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "src.db")
	dstPath := filepath.Join(dstDir, "dst.db")
	snapPath := filepath.Join(dstDir, "snap.db")

	// Populate source driver.
	srcDriver, err := sqlite.New(srcPath)
	require.NoError(t, err)
	require.NoError(t, srcDriver.Init(context.Background()))
	defer srcDriver.Close(context.Background()) //nolint:errcheck

	ctx := context.Background()
	const nObjects = 10
	now := time.Now().Truncate(time.Second)

	for i := 0; i < nObjects; i++ {
		obj := &storage.KnowledgeObject{
			ID:         fmt.Sprintf("migrate-obj-%d", i),
			Type:       "text",
			RawContent: fmt.Sprintf("migration content %d", i),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, srcDriver.Objects().Create(ctx, obj), "seed object %d", i)
	}

	// Snapshot source → snap file (simulates migration export step).
	require.NoError(t, storageutil.SQLiteSnapshot(srcPath, snapPath))

	// Open destination driver using the snapshot as the source DB.
	dstDriver, err := sqlite.New(dstPath)
	require.NoError(t, err)
	require.NoError(t, dstDriver.Init(context.Background()))
	defer dstDriver.Close(context.Background()) //nolint:errcheck

	// Open a fresh driver on the snapshot to verify counts.
	snapDriver, err := sqlite.New(snapPath)
	require.NoError(t, err)
	defer snapDriver.Close(context.Background()) //nolint:errcheck

	srcObjs, srcTotal, err := srcDriver.Objects().List(ctx, storage.ObjectFilter{Limit: 1000, Status: "all"})
	require.NoError(t, err)

	snapObjs, snapTotal, err := snapDriver.Objects().List(ctx, storage.ObjectFilter{Limit: 1000, Status: "all"})
	require.NoError(t, err)

	assert.Equal(t, srcTotal, snapTotal, "snapshot must have same total object count as source")
	assert.Equal(t, len(srcObjs), len(snapObjs), "snapshot must have same object slice length")
}

// TestUS0035_ObjectContentIdenticalAfterMigration verifies that each object's
// raw content is preserved exactly after a snapshot-based migration.
func TestUS0035_ObjectContentIdenticalAfterMigration(t *testing.T) {
	srcDir := t.TempDir()
	snapDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "src.db")
	snapPath := filepath.Join(snapDir, "snap.db")

	srcDriver, err := sqlite.New(srcPath)
	require.NoError(t, err)
	require.NoError(t, srcDriver.Init(context.Background()))
	defer srcDriver.Close(context.Background()) //nolint:errcheck

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	wantContents := map[string]string{
		"mig-content-a": "The quick brown fox",
		"mig-content-b": "Jumps over the lazy dog",
		"mig-content-c": "SQLite migration content test",
	}
	for id, content := range wantContents {
		obj := &storage.KnowledgeObject{
			ID: id, Type: "text", RawContent: content,
			CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, srcDriver.Objects().Create(ctx, obj))
	}

	require.NoError(t, storageutil.SQLiteSnapshot(srcPath, snapPath))

	snapDriver, err := sqlite.New(snapPath)
	require.NoError(t, err)
	defer snapDriver.Close(context.Background()) //nolint:errcheck

	for id, wantContent := range wantContents {
		obj, err := snapDriver.Objects().Get(ctx, id)
		require.NoError(t, err, "object %s must exist in snapshot", id)
		assert.Equal(t, wantContent, obj.RawContent,
			"object %s raw content must be identical after migration", id)
	}
}

// TestUS0035_EdgeCountIdenticalAfterMigration verifies that edges are preserved
// through the snapshot-based migration.
func TestUS0035_EdgeCountIdenticalAfterMigration(t *testing.T) {
	srcDir := t.TempDir()
	snapDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "src.db")
	snapPath := filepath.Join(snapDir, "snap.db")

	srcDriver, err := sqlite.New(srcPath)
	require.NoError(t, err)
	require.NoError(t, srcDriver.Init(context.Background()))
	defer srcDriver.Close(context.Background()) //nolint:errcheck

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed objects and edges.
	for i := 0; i < 3; i++ {
		obj := &storage.KnowledgeObject{
			ID: fmt.Sprintf("mig-edge-obj-%d", i), Type: "text",
			RawContent: fmt.Sprintf("edge content %d", i),
			CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, srcDriver.Objects().Create(ctx, obj))

		edge := &storage.Edge{
			ID: fmt.Sprintf("mig-edge-%d", i),
			FromType: "object", FromID: obj.ID,
			ToType: "entity", ToID: fmt.Sprintf("@test.entity%d", i),
			EdgeType: "mentions", Weight: 1.0, CreatedAt: now,
		}
		require.NoError(t, srcDriver.Edges().Create(ctx, edge))
	}

	require.NoError(t, storageutil.SQLiteSnapshot(srcPath, snapPath))

	snapDriver, err := sqlite.New(snapPath)
	require.NoError(t, err)
	defer snapDriver.Close(context.Background()) //nolint:errcheck

	// Verify each object's edges are present in the snapshot.
	for i := 0; i < 3; i++ {
		objID := fmt.Sprintf("mig-edge-obj-%d", i)
		srcEdges, err := srcDriver.Edges().ListFrom(ctx, "object", objID)
		require.NoError(t, err)
		snapEdges, err := snapDriver.Edges().ListFrom(ctx, "object", objID)
		require.NoError(t, err)
		assert.Equal(t, len(srcEdges), len(snapEdges),
			"edge count for object %s must match in snapshot", objID)
	}
}

// TestUS0035_MigrationWithSecondSQLiteDestination verifies a full copy from
// source to a fresh destination SQLite by re-inserting all objects.
// This uses a test double approach: source driver → enumerate → insert into dst.
func TestUS0035_MigrationWithSecondSQLiteDestination(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "src.db")
	dstPath := filepath.Join(dstDir, "dst.db")

	// Populate source.
	srcDriver, err := sqlite.New(srcPath)
	require.NoError(t, err)
	require.NoError(t, srcDriver.Init(context.Background()))
	defer srcDriver.Close(context.Background()) //nolint:errcheck

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	objects := storageutil.SeedObjects(t, srcDriver, 7)

	// Migrate: open destination, enumerate source, insert into destination.
	dstDriver, err := sqlite.New(dstPath)
	require.NoError(t, err)
	require.NoError(t, dstDriver.Init(context.Background()))
	defer dstDriver.Close(context.Background()) //nolint:errcheck

	for _, obj := range objects {
		migrated := &storage.KnowledgeObject{
			ID:         obj.ID,
			Type:       obj.Type,
			RawContent: obj.RawContent,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, dstDriver.Objects().Create(ctx, migrated),
			"migrate object %s to destination", obj.ID)
	}

	// Verify counts match.
	_, srcTotal, err := srcDriver.Objects().List(ctx, storage.ObjectFilter{Limit: 1000, Status: "all"})
	require.NoError(t, err)

	_, dstTotal, err := dstDriver.Objects().List(ctx, storage.ObjectFilter{Limit: 1000, Status: "all"})
	require.NoError(t, err)

	assert.Equal(t, srcTotal, dstTotal, "destination must have same object count as source after migration")
}
