package jobs

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// objectsOf adapts a driver's object store to ReprocessObjects.
type objectsOf struct{ driver storage.StorageDriver }

func (o objectsOf) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return o.driver.Objects().Get(ctx, id)
}

func (o objectsOf) UpdateObject(ctx context.Context, obj *storage.KnowledgeObject) error {
	return o.driver.Objects().Update(ctx, obj)
}

var reprocessSeedTime = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)

func reprocessPool(t *testing.T) (*WorkerPool, *Queue, storage.StorageDriver) {
	t.Helper()
	pool, q, driver := taskPool(t)
	r := &Reprocessor{Objects: objectsOf{driver}, LLM: providers.NewStubLLMProvider()}
	pool.Handle(ReprocessJobType, r.Handle)
	return pool, q, driver
}

func seedReprocessObject(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	err := driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "o-r", Type: "note", Status: "active",
		RawContent: "kubernetes clusters schedule kubernetes pods; kubernetes nodes run pods",
		Tags:       []storage.Tag{{Label: "stale", Source: "auto"}},
		CreatedAt:  reprocessSeedTime, UpdatedAt: reprocessSeedTime,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// TestReprocess_RunsStepAndUpdatesObject: a reprocess job runs its step
// with the pool's providers against the stored object, writes the result
// back, and completes with the object ID as its result.
func TestReprocess_RunsStepAndUpdatesObject(t *testing.T) {
	pool, q, driver := reprocessPool(t)
	seedReprocessObject(t, driver)

	job, err := q.EnqueueReprocess(context.Background(), "o-r", StepTagger)
	if err != nil {
		t.Fatalf("EnqueueReprocess: %v", err)
	}
	stop := startPool(pool)
	defer stop()

	done := waitStatus(t, q, job.ID, storage.JobCompleted)
	if done.ResultID != "o-r" {
		t.Errorf("result_id = %q, want o-r", done.ResultID)
	}
	obj, err := driver.Objects().Get(context.Background(), "o-r")
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, tg := range obj.Tags {
		labels = append(labels, tg.Label)
	}
	if !slices.Contains(labels, "kubernetes") || slices.Contains(labels, "stale") {
		t.Errorf("tags after tagger = %v; want the re-extracted set", labels)
	}
	if !obj.UpdatedAt.After(reprocessSeedTime) {
		t.Errorf("updated_at = %s; want it moved", obj.UpdatedAt)
	}
}

// TestReprocess_FailsWithoutRetry: a job that can never succeed (unknown
// step, unreadable payload, missing object) fails at once instead of
// spending its retries.
func TestReprocess_FailsWithoutRetry(t *testing.T) {
	cases := map[string]string{
		"unknown step":   `{"object_id":"o-r","step":"summarizer"}`,
		"bad payload":    `{`,
		"missing object": `{"object_id":"o-none","step":"tagger"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			pool, q, driver := reprocessPool(t)
			seedReprocessObject(t, driver)
			job, err := q.EnqueueTask(context.Background(), ReprocessJobType, payload, 3)
			if err != nil {
				t.Fatal(err)
			}
			stop := startPool(pool)
			defer stop()
			failed := waitStatus(t, q, job.ID, storage.JobFailed)
			if failed.RetryCount != 0 {
				t.Errorf("retry_count = %d; a permanent failure must not retry", failed.RetryCount)
			}
			if failed.Error == "" {
				t.Error("failed job carries no error")
			}
		})
	}
}

func TestEnqueueReprocess_ValidatesStep(t *testing.T) {
	_, q, _ := taskPool(t)
	for _, step := range []string{"", "summarizer", "structured-metadata"} {
		if _, err := q.EnqueueReprocess(context.Background(), "o-r", step); err == nil {
			t.Errorf("EnqueueReprocess(%q): want an error", step)
		}
	}
	job, err := q.EnqueueReprocess(context.Background(), "o-r", StepStructuredMetadata)
	if err != nil {
		t.Fatal(err)
	}
	var p ReprocessPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		t.Fatal(err)
	}
	if p != (ReprocessPayload{ObjectID: "o-r", Step: StepStructuredMetadata}) || job.Type != ReprocessJobType {
		t.Errorf("job = %+v", job)
	}
	if !strings.Contains(strings.Join(ReprocessSteps, ","), StepEntityExtractor) {
		t.Errorf("ReprocessSteps = %v", ReprocessSteps)
	}
}
