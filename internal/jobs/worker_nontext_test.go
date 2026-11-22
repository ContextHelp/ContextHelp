package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// nonTextStep lets a text field through that holds NUL or invalid UTF-8.
type nonTextStep struct {
	pipeline.BaseContract
	apply func(*storage.KnowledgeObject)
}

func (s *nonTextStep) Name() string { return "non-text" }
func (s *nonTextStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	s.apply(d)
	return d, nil
}

// A step that lets NUL or invalid UTF-8 into metadata, sections,
// summaries or text content fails the job permanently by field, and
// nothing is stored.
func TestWorkerFailsJobWhenTextFieldHoldsBinary(t *testing.T) {
	cases := map[string]struct {
		apply func(*storage.KnowledgeObject)
		field string
	}{
		"metadata": {func(d *storage.KnowledgeObject) {
			d.Metadata = map[string]any{"email_messages": []map[string]any{{"subject": "x\x00y"}}}
		}, "metadata.email_messages"},
		"section":     {func(d *storage.KnowledgeObject) { d.Sections = []storage.Section{{Title: "t", Content: "a\xffb"}} }, "sections[0]"},
		"summary":     {func(d *storage.KnowledgeObject) { d.Summaries = []string{"ok", "a\x00"} }, "summaries[1]"},
		"textcontent": {func(d *storage.KnowledgeObject) { d.TextContent = "\xff" }, "text_content"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			q := NewQueue(driver.Jobs())
			pipes := pipeline.NewRegistry()
			pipes.Register("test.nontext", &pipeline.Pipeline{
				PipelineName: "test.nontext",
				Steps:        []pipeline.PipelineStep{&nonTextStep{apply: tc.apply}},
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			job := makeJob("job-nontext")
			job.Pipeline = "test.nontext"
			job.MaxRetries = 3
			require.NoError(t, q.Enqueue(ctx, job))
			pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())
			go func() {
				for ctx.Err() == nil {
					if got, _ := q.Get(ctx, "job-nontext"); got != nil && got.Status == storage.JobFailed {
						cancel()
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
			}()
			pool.Start(ctx)

			got, err := q.Get(context.Background(), "job-nontext")
			require.NoError(t, err)
			require.Equal(t, storage.JobFailed, got.Status)
			require.Equal(t, 0, got.RetryCount, "permanent: no retry")
			require.Contains(t, got.Error, tc.field)
			require.Contains(t, got.Error, "invalid UTF-8")
			objs, _, err := driver.Objects().List(context.Background(), storage.ObjectFilter{Limit: 10})
			require.NoError(t, err)
			require.Empty(t, objs)
		})
	}
}
