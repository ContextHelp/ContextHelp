package storagetest

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// binaryPayload is PNG-like: the signature, a NUL, and bytes that are not
// valid UTF-8. Neither survives a TEXT column (Postgres refuses both).
var binaryPayload = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80, 0xc3, 0x28, 0x00}

// BinaryContentConformance pins how a driver holds binary content: an
// object stores it as a blob (the row keeps the ref, since raw_content is
// text) and a job carries it base64-encoded in its text payload; both read
// back as the exact bytes.
func BinaryContentConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()

	if !blobref.NeedsBlob(string(binaryPayload)) {
		t.Fatal("fixture must be content that needs a blob")
	}

	t.Run("object content in the blob store", func(t *testing.T) {
		key := bk("binary-content")
		if err := drv.Blobs().Put(ctx, key, bytes.NewReader(binaryPayload), storage.BlobMeta{
			ContentType: "image/png", Size: int64(len(binaryPayload)), ContentHash: key,
		}); err != nil {
			t.Fatalf("put blob: %v", err)
		}
		obj := &storage.KnowledgeObject{
			ID: "bin-obj", Type: "text", RawContent: blobref.For(key),
			Metadata:  map[string]any{"blob_key": key, "blob_content_type": "image/png"},
			CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := drv.Objects().Get(ctx, obj.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		content, err := blob.Content(ctx, drv.Blobs(), got.RawContent)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if content != string(binaryPayload) {
			t.Fatalf("content = %x, want %x", content, binaryPayload)
		}
	})

	t.Run("job payload as base64", func(t *testing.T) {
		now := fixtureTime()
		payload := base64.StdEncoding.EncodeToString(binaryPayload)
		job := &storage.Job{
			ID: "bin-job", Type: "ingest:text" + storage.JobTypeBase64, Status: storage.JobPending,
			Payload: payload, Pipeline: "text.short", Source: "cli", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
		}
		if err := drv.Jobs().Create(ctx, job); err != nil {
			t.Fatalf("Create job: %v", err)
		}
		got, err := drv.Jobs().Get(ctx, job.ID)
		if err != nil {
			t.Fatalf("Get job: %v", err)
		}
		if !strings.HasSuffix(got.Type, storage.JobTypeBase64) {
			t.Fatalf("job type = %q, lost the base64 flag", got.Type)
		}
		b, err := base64.StdEncoding.DecodeString(got.Payload)
		if err != nil {
			t.Fatalf("payload not base64: %v", err)
		}
		if !bytes.Equal(b, binaryPayload) {
			t.Fatalf("payload = %x, want %x", b, binaryPayload)
		}
	})
}
