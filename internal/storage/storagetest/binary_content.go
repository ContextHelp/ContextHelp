package storagetest

import (
	"bytes"
	"context"
	"encoding/base64"
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
// text) and a job carries it base64-encoded in its text payload, flagged by
// payload_encoding through claim, requeue and retry; both read
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
			ID: "bin-job", Type: "ingest:text", Status: storage.JobPending,
			Payload: payload, PayloadEncoding: storage.PayloadBase64,
			Pipeline: "text.short", Source: "cli", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
		}
		if err := drv.Jobs().Create(ctx, job); err != nil {
			t.Fatalf("Create job: %v", err)
		}
		check := func(step string, got *storage.Job) {
			t.Helper()
			if got.Type != "ingest:text" {
				t.Fatalf("%s: job type = %q, want it unsuffixed", step, got.Type)
			}
			if got.PayloadEncoding != storage.PayloadBase64 {
				t.Fatalf("%s: payload_encoding = %q, want %q", step, got.PayloadEncoding, storage.PayloadBase64)
			}
			b, err := base64.StdEncoding.DecodeString(got.Payload)
			if err != nil {
				t.Fatalf("%s: payload not base64: %v", step, err)
			}
			if !bytes.Equal(b, binaryPayload) {
				t.Fatalf("%s: payload = %x, want %x", step, b, binaryPayload)
			}
		}
		get := func(step string) {
			t.Helper()
			got, err := drv.Jobs().Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("%s: Get job: %v", step, err)
			}
			check(step, got)
		}
		get("create")

		claimed, err := drv.Jobs().AcquireNext(ctx)
		if err != nil || claimed == nil || claimed.ID != job.ID {
			t.Fatalf("AcquireNext = %+v, %v", claimed, err)
		}
		check("acquire", claimed)

		if err := drv.Jobs().Requeue(ctx, job.ID, claimed.Claim); err != nil {
			t.Fatalf("Requeue: %v", err)
		}
		get("requeue")

		if _, err := drv.Jobs().AcquireNext(ctx); err != nil {
			t.Fatalf("AcquireNext again: %v", err)
		}
		if err := drv.Jobs().Fail(ctx, job.ID, "boom"); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		if err := drv.Jobs().Retry(ctx, job.ID); err != nil {
			t.Fatalf("Retry: %v", err)
		}
		get("retry")

		byType, _, err := drv.Jobs().List(ctx, storage.JobFilter{Type: "ingest:text"})
		if err != nil || len(byType) == 0 {
			t.Fatalf("List by type = %d jobs, %v", len(byType), err)
		}
		for _, j := range byType {
			if j.ID == job.ID {
				check("list", j)
			}
		}
	})

	t.Run("text job has no payload encoding", func(t *testing.T) {
		now := fixtureTime()
		job := &storage.Job{
			ID: "text-job", Type: "ingest:text", Status: storage.JobPending,
			Payload: "plain", Pipeline: "text.short", Source: "cli", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
		}
		if err := drv.Jobs().Create(ctx, job); err != nil {
			t.Fatalf("Create job: %v", err)
		}
		got, err := drv.Jobs().Get(ctx, job.ID)
		if err != nil {
			t.Fatalf("Get job: %v", err)
		}
		if got.PayloadEncoding != "" || got.Payload != "plain" {
			t.Fatalf("job = encoding %q payload %q", got.PayloadEncoding, got.Payload)
		}
	})
}
