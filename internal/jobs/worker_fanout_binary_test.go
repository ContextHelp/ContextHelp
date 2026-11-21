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

// A fan-out item whose content is binary becomes a child job whose
// payload travels base64, and the worker decodes it back to the bytes.
func TestFanOutBinaryItemTravelsBase64(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pool := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())
	bin := string([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe, 0x80})
	draft := &storage.KnowledgeObject{Metadata: map[string]any{
		"items_to_enqueue": []map[string]any{
			{"content": bin, "source": "bin:1", "pipeline": "text.short"},
			{"content": "plain text", "source": "txt:1", "pipeline": "text.short"},
		},
	}}

	require.NoError(t, pool.fanOutItems(context.Background(), draft))

	jobs, _, err := driver.Jobs().List(context.Background(), storage.JobFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	for _, j := range jobs {
		got, err := jobContent(j)
		require.NoError(t, err)
		switch j.Source {
		case "bin:1":
			require.Equal(t, storage.PayloadBase64, j.PayloadEncoding)
			require.Equal(t, bin, got)
		case "txt:1":
			require.Empty(t, j.PayloadEncoding)
			require.Equal(t, "plain text", got)
		default:
			t.Fatalf("unexpected job source %q", j.Source)
		}
	}
}

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
// items are still enqueued and the container job fails.
func TestFanOutEnqueueFailureFailsContainer(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	q.SetPipelineValidator(func(name string) error {
		if name == "batch.jsonl" {
			return nil
		}
		return errors.New("no such pipeline")
	})
	pool := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job := makeJob("container")
	job.Type = "ingest:batch.jsonl"
	job.Pipeline = "batch.jsonl"
	job.Source = "records.jsonl"
	job.Payload = `{"content":"first record"}` + "\n" + `{"content":"second record"}` + "\n"
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
	require.Contains(t, got.Error, "2 of 2")
}
