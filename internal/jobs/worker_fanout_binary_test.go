package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A job enqueued with binary content carries it base64 too.
func TestEnqueueIngestJobBinaryTravelsBase64(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	bin := string([]byte{0x00, 0xff, 0xfe, 'x'})
	require.NoError(t, q.EnqueueIngestJob(context.Background(), "ingest:text", bin, "text.short", "src", 3))

	jobs, _, err := driver.Jobs().List(context.Background(), storage.JobFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, storage.PayloadBase64, jobs[0].PayloadEncoding)
	got, err := jobContent(jobs[0])
	require.NoError(t, err)
	require.Equal(t, bin, got)
}

// An item that cannot be enqueued is reported, not dropped: the other
// items are still enqueued and the container job fails. Here the URL
// item's pipeline is refused while the text item's is accepted.
func TestFanOutEnqueueFailureFailsContainer(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	q.SetPipelineValidator(func(name string) error {
		if name == "url.generic" {
			return errors.New("no such pipeline")
		}
		return nil
	})
	pool := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job := makeJob("container")
	job.Type = "ingest:batch.jsonl"
	job.Pipeline = "batch.jsonl"
	job.Source = "records.jsonl"
	job.Payload = `{"content":"https://example.com/one"}` + "\n" + `{"content":"a plain text record"}` + "\n"
	require.NoError(t, q.Enqueue(ctx, job))
	go func() {
		for ctx.Err() == nil {
			if got, _ := q.Get(ctx, "container"); got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, err := q.Get(context.Background(), "container")
	require.NoError(t, err)
	require.Equal(t, storage.JobFailed, got.Status, "container must not complete with items lost")
	require.Contains(t, got.Error, "fan-out")
	require.Contains(t, got.Error, "1 of 2")

	// The item that could be enqueued was: the failure is partial.
	jobs, _, err := driver.Jobs().List(context.Background(), storage.JobFilter{Limit: 10})
	require.NoError(t, err)
	var items int
	for _, j := range jobs {
		if j.Type == "ingest:feed_item" {
			items++
			got, err := jobContent(j)
			require.NoError(t, err)
			require.Equal(t, "a plain text record", got)
		}
	}
	require.Equal(t, 1, items, "the item whose pipeline is valid is still enqueued")
}
