package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditStore_AppendAndList(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entries := []*storage.AuditEntry{
		{ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{"source": "cli"}, CreatedAt: now},
		{ID: uuid.NewString(), EventType: "object.updated", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{"field": "tags"}, CreatedAt: now.Add(time.Second)},
		{ID: uuid.NewString(), EventType: "object.deleted", ObjectID: "obj_001", Actor: "system", Payload: map[string]any{}, CreatedAt: now.Add(2 * time.Second)},
	}
	for _, e := range entries {
		require.NoError(t, db.AuditLog().Append(ctx, e))
	}

	all, count, err := db.AuditLog().List(ctx, storage.AuditFilter{ObjectID: "obj_001"})
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.Len(t, all, 3)
	assert.Equal(t, "object.created", all[0].EventType)
}

func TestAuditStore_FilterByEventType(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for _, et := range []string{"object.created", "object.updated", "object.created"} {
		require.NoError(t, db.AuditLog().Append(ctx, &storage.AuditEntry{
			ID: uuid.NewString(), EventType: et, ObjectID: "obj_002",
			Actor: "system", Payload: map[string]any{}, CreatedAt: now,
		}))
	}

	results, count, err := db.AuditLog().List(ctx, storage.AuditFilter{EventType: "object.created"})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Len(t, results, 2)
}

func TestAuditStore_GetObjectHistory(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	_ = db.AuditLog().Append(ctx, &storage.AuditEntry{
		ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_hist",
		Actor: "system", Payload: map[string]any{}, CreatedAt: now,
	})
	_ = db.AuditLog().Append(ctx, &storage.AuditEntry{
		ID: uuid.NewString(), EventType: "object.updated", ObjectID: "obj_hist",
		Actor: "user:alice", Payload: map[string]any{}, CreatedAt: now.Add(time.Second),
	})

	history, err := db.AuditLog().GetObjectHistory(ctx, "obj_hist")
	require.NoError(t, err)
	assert.Len(t, history, 2)
	assert.Equal(t, "object.created", history[0].EventType)
	assert.Equal(t, "object.updated", history[1].EventType)
}

func TestAuditStore_Immutable_UpdateRejected(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	now := time.Now().UTC()

	e := &storage.AuditEntry{
		ID: uuid.NewString(), EventType: "object.created", ObjectID: "obj_imm",
		Actor: "system", Payload: map[string]any{}, CreatedAt: now,
	}
	require.NoError(t, db.AuditLog().Append(ctx, e))

	// Direct SQL UPDATE must be rejected by trigger.
	_, err := db.db.ExecContext(ctx,
		`UPDATE audit_log SET actor='tampered' WHERE id=?`, e.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "immutable")
}

func TestMigration011_AuditLogTable(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	// Insert succeeds.
	_, err := db.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, event_type, object_id, actor, payload, created_at)
         VALUES ('test-id-audit', 'object.created', 'obj_001', 'system', '{}', datetime('now'))`)
	require.NoError(t, err)

	// UPDATE is rejected.
	_, err = db.db.ExecContext(ctx,
		`UPDATE audit_log SET actor='hacker' WHERE id='test-id-audit'`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "immutable")

	// DELETE is rejected.
	_, err = db.db.ExecContext(ctx,
		`DELETE FROM audit_log WHERE id='test-id-audit'`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "immutable")
}
