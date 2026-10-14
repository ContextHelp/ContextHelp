package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// blobLostOnCreateDriver deletes the blobs an object refers to as its
// row lands: a delete or sweep of the same key that counted no
// references before the row was written, deleting the blob the ingest
// put.
type blobLostOnCreateDriver struct {
	storage.StorageDriver
}

func (d blobLostOnCreateDriver) Objects() storage.ObjectStore {
	return blobLostOnCreateObjects{ObjectStore: d.StorageDriver.Objects(), blobs: d.Blobs()}
}

type blobLostOnCreateObjects struct {
	storage.ObjectStore
	blobs storage.BlobStore
}

func (o blobLostOnCreateObjects) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := o.ObjectStore.Create(ctx, obj); err != nil {
		return err
	}
	for _, key := range blob.Keys(obj) {
		if err := o.blobs.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// An ingest whose blob a concurrent delete removes before its row lands
// puts the blob back once the row is written.
func TestProcessPutsBackBlobDeletedBeforeRowLands(t *testing.T) {
	driver := blobLostOnCreateDriver{StorageDriver: storageutil.NewTestDriver(t)}
	q := NewQueue(driver.Jobs())
	pipes := pipeline.NewRegistry()
	pipes.Register("test.externalize", &pipeline.Pipeline{
		PipelineName: "test.externalize",
		Steps:        []pipeline.PipelineStep{steps.NewExternalizer(driver.Blobs(), 16)},
	})
	job := makeJob("job-blob-race")
	job.Pipeline = "test.externalize"
	job.Payload = strings.Repeat("externalized content ", 10)
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got := runUntilDone(t, NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg()), q, job.ID)
	if got.Status != storage.JobCompleted {
		t.Fatalf("job: got %q (err=%q), want completed", got.Status, got.Error)
	}
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	if !blob.IsBlobRef(obj.RawContent) {
		t.Fatalf("raw_content = %.40q, want a blob ref", obj.RawContent)
	}
	content, err := blob.Content(context.Background(), driver.Blobs(), obj.RawContent)
	if err != nil {
		t.Fatalf("the stored row names a deleted blob: %v", err)
	}
	if content != job.Payload {
		t.Errorf("blob content = %.40q, want the ingested content", content)
	}
}
