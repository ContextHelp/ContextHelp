package jobs

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// refSpy is a text step (not Binary) that records the content it ran on.
type refSpy struct {
	pipeline.BaseContract
	ran *[]string
}

func newRefSpy(ran *[]string) *refSpy {
	return &refSpy{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{Requires: []string{"RawContent"}}),
		ran:          ran,
	}
}

func (s *refSpy) Name() string { return "ref_spy" }
func (s *refSpy) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*s.ran = append(*s.ran, d.RawContent)
	return d, nil
}

// A pipeline that externalizes mid-run: the dedup hash is over the
// content bytes, not the blob ref left in RawContent, and a text step
// after it never runs on the ref of binary content.
func TestWorkerExternalizeMidPipelineBinary(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	var ran []string
	pipes := pipeline.NewRegistry()
	pipes.Register("test.externalize", &pipeline.Pipeline{
		PipelineName: "test.externalize",
		Steps: []pipeline.PipelineStep{
			steps.NewExternalizer(driver.Blobs(), 1<<20),
			newRefSpy(&ran),
		},
	})

	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe, 0x80}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job := makeJob("ext-1")
	job.Pipeline = "test.externalize"
	job.Source = "/tmp/x.png"
	job.PayloadEncoding = storage.PayloadBase64
	job.Payload = base64.StdEncoding.EncodeToString(bin)
	require.NoError(t, q.Enqueue(ctx, job))

	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())
	go func() {
		for ctx.Err() == nil {
			if got, _ := q.Get(ctx, "ext-1"); got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, err := q.Get(context.Background(), "ext-1")
	require.NoError(t, err)
	require.Equal(t, storage.JobCompleted, got.Status, got.Error)
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	require.NoError(t, err)

	require.Equal(t, storageutil.DraftContentHash(string(bin), "/tmp/x.png", nil), obj.ContentHash,
		"hash is over the bytes, as analyze hashes them")
	require.Empty(t, ran, "a text step ran on the blob ref of binary content: %q", ran)
	require.Equal(t, bin, blobBytes(t, driver, obj))
}
