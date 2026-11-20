package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Data and the source key ride the job to the worker.
func TestAnalyzeForwardsDataAndSourceKeyToJob(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	jobID, err := svc.Analyze(ctx, AnalyzeRequest{
		Content:   "https://example.com/v",
		Type:      "url",
		Pipeline:  "text.short",
		Note:      "transcript",
		Data:      map[string]any{"title": "T", "meta": map[string]any{"views": float64(3)}},
		SourceKey: "youtube:abc",
	})
	require.NoError(t, err)

	job, err := svc.store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "youtube:abc", job.UserSourceKey)
	assert.Equal(t, "transcript", job.UserNote)
	assert.Equal(t, map[string]any{"title": "T", "meta": map[string]any{"views": float64(3)}}, job.UserData)
}

// Enqueue forwarded none of the caller fields before; it takes the same
// set as Analyze.
func TestEnqueueForwardsCallerFieldsToJob(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	jobID, err := svc.Enqueue(ctx, AnalyzeRequest{
		Content:   "body",
		Type:      "text",
		Pipeline:  "text.short",
		Note:      "n",
		Data:      map[string]any{"k": "v"},
		SourceKey: "k:1",
		Hints:     []string{"h"},
		Mentions:  []string{"@a.b"},
	})
	require.NoError(t, err)

	job, err := svc.store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, "n", job.UserNote)
	assert.Equal(t, map[string]any{"k": "v"}, job.UserData)
	assert.Equal(t, "k:1", job.UserSourceKey)
	assert.Equal(t, []string{"h"}, job.UserHints)
	assert.Equal(t, []string{"@a.b"}, job.UserMentions)
}

func TestAnalyzeRejectsOversizedNoteAndData(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	_, err := svc.Analyze(ctx, AnalyzeRequest{Content: "x", Type: "text", Note: strings.Repeat("n", MaxAnalyzeNoteBytes+1)})
	assert.True(t, errors.Is(err, ErrInvalidSubmission), "note: %v", err)

	_, err = svc.Analyze(ctx, AnalyzeRequest{Content: "x", Type: "text",
		Data: map[string]any{"blob": strings.Repeat("d", MaxAnalyzeDataBytes)}})
	assert.True(t, errors.Is(err, ErrInvalidSubmission), "data: %v", err)

	_, err = svc.Enqueue(ctx, AnalyzeRequest{Content: "x", Type: "text", Pipeline: "text.short",
		Data: map[string]any{"blob": strings.Repeat("d", MaxAnalyzeDataBytes)}})
	assert.True(t, errors.Is(err, ErrInvalidSubmission), "enqueue data: %v", err)
}

// The silent skip: under duplicates.policy drop, a submission naming a
// stored source key was answered with the stored object and never
// reached the worker, so its note and data were lost.
func TestAnalyzeSourceKeyOfStoredObjectIsNotDropped(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	svc.Cfg.Duplicates = config.DuplicatesConfig{Policy: "drop", CheckExact: true}
	require.NoError(t, svc.store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-key", Type: "text", RawContent: "first", SourceKey: "youtube:abc",
		ContentHash: "h1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	id, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "second capture", Type: "text", SourceKey: "youtube:abc", Note: "better transcript",
	})
	require.NoError(t, err)
	assert.NotEqual(t, "obj-key", id, "a keyed submission is enqueued for the worker, not dropped")
	job, err := svc.store.Jobs().Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, job, "the answer is a job id")
	assert.Equal(t, "better transcript", job.UserNote)
}

// Identical content with a new note under drop: the note must reach the
// worker, which applies it to the stored object.
func TestAnalyzeSameContentWithNoteIsNotDropped(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	svc.Cfg.Duplicates = config.DuplicatesConfig{Policy: "drop", CheckExact: true}
	const content = "same body"
	require.NoError(t, svc.store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-same", Type: "text", RawContent: content,
		ContentHash: storageutil.ContentHash(content, ""), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	id, err := svc.Analyze(ctx, AnalyzeRequest{Content: content, Type: "text", Note: "new note"})
	require.NoError(t, err)
	assert.NotEqual(t, "obj-same", id)

	// Without a note the drop policy still applies.
	id, err = svc.Analyze(ctx, AnalyzeRequest{Content: content, Type: "text"})
	require.NoError(t, err)
	assert.Equal(t, "obj-same", id)
}

func TestAnalyzeRawPersistsDataAndUpsertsBySourceKey(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	id1, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "raw body", Type: "text", Raw: true, SourceKey: "k:1",
		Note: "n1", Data: map[string]any{"v": "1"}, Hints: []string{"a"},
	})
	require.NoError(t, err)
	obj, err := svc.store.Objects().Get(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"v": "1"}, obj.Metadata["data"])

	id2, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "raw body v2", Type: "text", Raw: true, SourceKey: "k:1",
		Note: "n2", Data: map[string]any{"v": "2"}, Hints: []string{"b"},
	})
	require.NoError(t, err, "a second raw submission under the key is an update, not a UNIQUE failure")
	assert.Equal(t, id1, id2)

	obj, err = svc.store.Objects().Get(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, "n2", obj.InboxNote)
	assert.Equal(t, map[string]any{"v": "2"}, obj.Metadata["data"])
	assert.Len(t, obj.Tags, 2)
	assert.Equal(t, "raw body", obj.RawContent)
	_, total, err := svc.store.Objects().List(ctx, storage.ObjectFilter{Status: storage.ObjectStatusAll})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}
