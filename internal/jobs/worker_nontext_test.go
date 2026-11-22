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
		"graph node label": {func(d *storage.KnowledgeObject) {
			d.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "n", Label: "a\xffb"}}}
		}, "graph.nodes[0]"},
		"graph node metadata": {func(d *storage.KnowledgeObject) {
			d.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "n", Metadata: map[string]any{"k": "x\x00"}}}}
		}, "graph.nodes[0].metadata.k"},
		"inbox note":   {func(d *storage.KnowledgeObject) { d.InboxNote = "n\x00" }, "inbox_note"},
		"subtype":      {func(d *storage.KnowledgeObject) { d.Subtype = "\xff" }, "subtype"},
		"content type": {func(d *storage.KnowledgeObject) { d.ContentType = "\xff" }, "content_type"},
		"profile":      {func(d *storage.KnowledgeObject) { d.ProfileID = "p\x00" }, "profile_id"},
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

// A reinforce writes the count, tags and mentions only: a binary field it
// never writes does not fail the re-submission, and one it writes does.
func TestReinforceChecksOnlyWhatItWrites(t *testing.T) {
	for name, tc := range map[string]struct {
		apply func(*storage.KnowledgeObject)
		want  storage.JobStatus
	}{
		"unwritten summary": {func(d *storage.KnowledgeObject) { d.Summaries = []string{"a\xff"} }, storage.JobCompleted},
		"unwritten graph": {func(d *storage.KnowledgeObject) {
			d.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "n", Label: "\xff"}}}
		}, storage.JobCompleted},
		"tag": {func(d *storage.KnowledgeObject) { d.Tags = []storage.Tag{{Label: "a\x00"}} }, storage.JobFailed},
	} {
		t.Run(name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			q := NewQueue(driver.Jobs())
			runs := 0
			step := &nonTextStep{apply: func(d *storage.KnowledgeObject) {
				runs++
				if runs > 1 {
					tc.apply(d)
				}
			}}
			pipes := pipeline.NewRegistry()
			pipes.Register("test.reinforce", &pipeline.Pipeline{PipelineName: "test.reinforce", Steps: []pipeline.PipelineStep{step}})
			pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())
			for _, id := range []string{"first", "second"} {
				job := makeJob(id)
				job.Pipeline = "test.reinforce"
				job.Payload = "same content"
				require.NoError(t, q.Enqueue(context.Background(), job))
				got := runUntilDone(t, pool, q, id)
				if id == "first" {
					require.Equal(t, storage.JobCompleted, got.Status, got.Error)
					continue
				}
				require.Equal(t, tc.want, got.Status, got.Error)
			}
			objs, _, err := driver.Objects().List(context.Background(), storage.ObjectFilter{Limit: 10})
			require.NoError(t, err)
			require.Len(t, objs, 1)
		})
	}
}

// The caller's note and data are stored on a re-submission, so a binary
// one fails the job on the reinforce path too, with nothing written: the
// stored note, data and count stay as the first capture left them.
func TestReinforceRefusesBinarySubmission(t *testing.T) {
	for name, tc := range map[string]func(*storage.Job){
		"nul note":     func(j *storage.Job) { j.UserNote = "a\x00b" },
		"invalid note": func(j *storage.Job) { j.UserNote = "\xff\xfe" },
		"nul data":     func(j *storage.Job) { j.UserData = map[string]any{"k": "a\x00b"} },
	} {
		t.Run(name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			q := NewQueue(driver.Jobs())
			pipes := pipeline.NewRegistry()
			pipes.Register("test.plain", &pipeline.Pipeline{PipelineName: "test.plain",
				Steps: []pipeline.PipelineStep{&nonTextStep{apply: func(*storage.KnowledgeObject) {}}}})
			pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

			first := makeJob("first")
			first.Pipeline = "test.plain"
			first.Payload = "same content"
			first.UserNote = "first note"
			require.NoError(t, q.Enqueue(context.Background(), first))
			require.Equal(t, storage.JobCompleted, runUntilDone(t, pool, q, "first").Status)

			second := makeJob("second")
			second.Pipeline = "test.plain"
			second.Payload = "same content"
			tc(second)
			require.NoError(t, q.Enqueue(context.Background(), second))
			got := runUntilDone(t, pool, q, "second")
			require.Equal(t, storage.JobFailed, got.Status, got.Error)
			require.Equal(t, 0, got.RetryCount)
			require.Contains(t, got.Error, "invalid UTF-8")

			objs, _, err := driver.Objects().List(context.Background(), storage.ObjectFilter{Limit: 10})
			require.NoError(t, err)
			require.Len(t, objs, 1)
			require.Equal(t, "first note", objs[0].InboxNote)
			require.Empty(t, objs[0].Metadata[storageutil.MetadataDataKey])
			require.Equal(t, 1, objs[0].ReinforcementCount, "failed job reinforced nothing")
		})
	}
}

// An update by source key writes the submission onto the stored object and
// nothing of the draft: a binary draft field it never stores does not stop
// it, a binary note does.
func TestSourceKeyUpdateChecksOnlyWhatItWrites(t *testing.T) {
	for name, tc := range map[string]struct {
		apply func(*storage.KnowledgeObject)
		note  string
		want  storage.JobStatus
	}{
		"unwritten summary": {func(d *storage.KnowledgeObject) { d.Summaries = []string{"a\xff"} }, "new note", storage.JobCompleted},
		"unwritten type":    {func(d *storage.KnowledgeObject) { d.Type = "a\x00" }, "new note", storage.JobCompleted},
		"binary note":       {func(*storage.KnowledgeObject) {}, "a\x00b", storage.JobFailed},
	} {
		t.Run(name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			q := NewQueue(driver.Jobs())
			runs := 0
			step := &nonTextStep{apply: func(d *storage.KnowledgeObject) {
				runs++
				if runs > 1 {
					tc.apply(d)
				}
			}}
			pipes := pipeline.NewRegistry()
			pipes.Register("test.srckey", &pipeline.Pipeline{PipelineName: "test.srckey", Steps: []pipeline.PipelineStep{step}})
			pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

			for i, id := range []string{"first", "second"} {
				job := makeJob(id)
				job.Pipeline = "test.srckey"
				job.Payload = "content " + id
				job.UserSourceKey = "src:1"
				job.UserNote = "first note"
				if i == 1 {
					job.UserNote = tc.note
				}
				require.NoError(t, q.Enqueue(context.Background(), job))
				got := runUntilDone(t, pool, q, id)
				if i == 0 {
					require.Equal(t, storage.JobCompleted, got.Status, got.Error)
					continue
				}
				require.Equal(t, tc.want, got.Status, got.Error)
			}
			objs, _, err := driver.Objects().List(context.Background(), storage.ObjectFilter{Limit: 10})
			require.NoError(t, err)
			require.Len(t, objs, 1)
			if tc.want == storage.JobFailed {
				require.Equal(t, "first note", objs[0].InboxNote)
			} else {
				require.Equal(t, "new note", objs[0].InboxNote)
			}
		})
	}
}
