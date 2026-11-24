package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestHoldGuard_BlocksEveryMutationPathAndAllowsReads(t *testing.T) {
	ctx := context.Background()
	bs, err := local.New(t.TempDir())
	require.NoError(t, err)
	drv, err := New(":memory:", bs)
	require.NoError(t, err)
	require.NoError(t, drv.Init(ctx))
	defer func() { _ = drv.Close(ctx) }()

	now := time.Now().UTC().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID: "obj-held-1", Type: "note", RawContent: "immutable legal record", TextContent: "immutable legal record",
		ContentHash: "hash-held-1", Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	other := &storage.KnowledgeObject{
		ID: "obj-free-2", Type: "note", RawContent: "other object", TextContent: "other object",
		ContentHash: "hash-free-2", Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, drv.Objects().Create(ctx, obj))
	require.NoError(t, drv.Objects().Create(ctx, other))

	// Pre-seed edge, attachment, alias before placing hold.
	require.NoError(t, drv.Edges().Create(ctx, &storage.Edge{
		ID: "e-pre", FromType: "object", FromID: obj.ID, ToType: "object", ToID: other.ID, EdgeType: "links_to", CreatedAt: now,
	}))
	attID, err := drv.Attachments().SaveAttachment(ctx, obj.ID, "doc.txt", "text/plain", []byte("hi"))
	require.NoError(t, err)
	require.NoError(t, drv.Aliases().Create(ctx, &storage.Alias{
		Alias: "held-alias", ObjectID: obj.ID, Scope: "global", CreatedAt: now, UpdatedAt: now,
	}))

	// Place active hold.
	require.NoError(t, drv.Holds().Place(ctx, &storage.ObjectHold{
		ObjectID: obj.ID, HoldID: "hold-guard-1", Source: "bos", Reason: "litigation preservation", PlacedBy: "bos", PlacedAt: now,
	}))

	// 1. ObjectStore.Update
	mod := *obj
	mod.RawContent = "tampered"
	require.ErrorIs(t, drv.Objects().Update(ctx, &mod), storage.ErrObjectHeld)

	// 2. ObjectStore.Delete
	require.ErrorIs(t, drv.Objects().Delete(ctx, obj.ID), storage.ErrObjectHeld)

	// 3. ObjectStore.TransitionStatus
	require.ErrorIs(t, drv.Objects().TransitionStatus(ctx, obj.ID, storage.ObjectStatusActive, "discarded", now), storage.ErrObjectHeld)

	// 4. ObjectStore.Reinforce
	_, err = drv.Objects().Reinforce(ctx, "hash-held-1", &storage.KnowledgeObject{Tags: []storage.Tag{{Label: "new"}}})
	require.ErrorIs(t, err, storage.ErrObjectHeld)

	// 5. ObjectStore reminders (SetReminder, ClearReminder, MarkReminded)
	require.ErrorIs(t, drv.Objects().SetReminder(ctx, obj.ID, now.Add(time.Hour)), storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Objects().ClearReminder(ctx, obj.ID), storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Objects().MarkReminded(ctx, obj.ID, now), storage.ErrObjectHeld)

	// 6. EdgeStore mutations touching held object (from or to)
	newEdge := &storage.Edge{ID: "e-new", FromType: "object", FromID: other.ID, ToType: "object", ToID: obj.ID, EdgeType: "links_to", CreatedAt: now}
	require.ErrorIs(t, drv.Edges().Create(ctx, newEdge), storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Edges().CreateAll(ctx, []*storage.Edge{newEdge}), storage.ErrObjectHeld)
	_, err = drv.Edges().CreateMissing(ctx, []*storage.Edge{newEdge})
	require.ErrorIs(t, err, storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Edges().Delete(ctx, "e-pre"), storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Edges().DeleteByObject(ctx, obj.ID), storage.ErrObjectHeld)
	_, err = drv.Edges().DeleteMatching(ctx, []storage.EdgeMatch{{FromType: "object", FromID: obj.ID, ToType: "object", ToID: other.ID, EdgeTypes: []string{"links_to"}}})
	require.ErrorIs(t, err, storage.ErrObjectHeld)

	// 7. AttachmentStore mutations
	_, err = drv.Attachments().SaveAttachment(ctx, obj.ID, "new.txt", "text/plain", []byte("x"))
	require.ErrorIs(t, err, storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Attachments().DeleteAttachment(ctx, attID), storage.ErrObjectHeld)

	// 8. AliasStore mutations
	require.ErrorIs(t, drv.Aliases().Create(ctx, &storage.Alias{Alias: "alias2", ObjectID: obj.ID, Scope: "global", CreatedAt: now, UpdatedAt: now}), storage.ErrObjectHeld)
	require.ErrorIs(t, drv.Aliases().Delete(ctx, "held-alias", "global", ""), storage.ErrObjectHeld)

	// Reads MUST remain fully available while held!
	got, err := drv.Objects().Get(ctx, obj.ID)
	require.NoError(t, err)
	require.Equal(t, "immutable legal record", got.RawContent)
	hits, err := drv.Objects().FTSSearch(ctx, "immutable", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 1)

	// Releasing hold restores mutability.
	released, err := drv.Holds().Release(ctx, obj.ID, "hold-guard-1", "bos", "matter closed", now)
	require.NoError(t, err)
	require.True(t, released)
	require.NoError(t, drv.Objects().Update(ctx, &mod))
	require.NoError(t, drv.Attachments().DeleteAttachment(ctx, attID))
	require.NoError(t, drv.Aliases().Delete(ctx, "held-alias", "global", ""))
	require.NoError(t, drv.Edges().DeleteByObject(ctx, obj.ID))
	require.NoError(t, drv.Objects().Delete(ctx, obj.ID))
}
