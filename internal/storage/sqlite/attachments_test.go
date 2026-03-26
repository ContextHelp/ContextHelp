package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachmentStore_SaveAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	obj := makeObject("obj-att-1", "text")
	require.NoError(t, d.Objects().Create(ctx, obj))

	data := []byte("hello attachment")
	id, err := d.Attachments().SaveAttachment(ctx, obj.ID, "hello.txt", "text/plain", data)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := d.Attachments().GetAttachment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, obj.ID, got.ObjectID)
	assert.Equal(t, "hello.txt", got.Filename)
	assert.Equal(t, "text/plain", got.MimeType)
	assert.Equal(t, int64(len(data)), got.SizeBytes)
	assert.Equal(t, data, got.Data)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestAttachmentStore_GetNotFound(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.Attachments().GetAttachment(ctx, "att_nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAttachmentStore_ListAttachments(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	obj := makeObject("obj-att-2", "text")
	require.NoError(t, d.Objects().Create(ctx, obj))

	// None initially.
	list, err := d.Attachments().ListAttachments(ctx, obj.ID)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Save two attachments.
	id1, err := d.Attachments().SaveAttachment(ctx, obj.ID, "a.txt", "text/plain", []byte("aaa"))
	require.NoError(t, err)
	id2, err := d.Attachments().SaveAttachment(ctx, obj.ID, "b.png", "image/png", []byte("bbb"))
	require.NoError(t, err)

	list, err = d.Attachments().ListAttachments(ctx, obj.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)

	ids := []string{list[0].ID, list[1].ID}
	assert.Contains(t, ids, id1)
	assert.Contains(t, ids, id2)

	// Data blob should not be populated in list results.
	for _, a := range list {
		assert.Nil(t, a.Data, "ListAttachments should not return data blobs")
	}
}

func TestAttachmentStore_Delete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	obj := makeObject("obj-att-3", "text")
	require.NoError(t, d.Objects().Create(ctx, obj))

	id, err := d.Attachments().SaveAttachment(ctx, obj.ID, "del.txt", "text/plain", []byte("x"))
	require.NoError(t, err)

	require.NoError(t, d.Attachments().DeleteAttachment(ctx, id))

	list, err := d.Attachments().ListAttachments(ctx, obj.ID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestAttachmentStore_AttachmentIDsOnObjectGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	obj := makeObject("obj-att-4", "text")
	require.NoError(t, d.Objects().Create(ctx, obj))

	// No attachments — AttachmentIDs should be empty/nil.
	got, err := d.Objects().Get(ctx, obj.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AttachmentIDs)

	// Add two attachments.
	id1, err := d.Attachments().SaveAttachment(ctx, obj.ID, "f1.txt", "text/plain", []byte("1"))
	require.NoError(t, err)
	id2, err := d.Attachments().SaveAttachment(ctx, obj.ID, "f2.txt", "text/plain", []byte("2"))
	require.NoError(t, err)

	got, err = d.Objects().Get(ctx, obj.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{id1, id2}, got.AttachmentIDs)
}
