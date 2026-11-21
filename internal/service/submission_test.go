package service

import (
	"encoding/base64"
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

// Raw path: a source key is per profile. Same profile updates; another
// profile gets its own object and the first is left as it was.
func TestAnalyzeRawSourceKeyIsScopedToProfile(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	raw := func(profile, body, note string, hints ...string) string {
		id, err := svc.Analyze(ctx, AnalyzeRequest{
			Content: body, Type: "text", Raw: true, SourceKey: "k:shared", Profile: profile,
			Note: note, Data: map[string]any{"o": profile}, Hints: hints,
		})
		require.NoError(t, err)
		return id
	}
	a := raw("alice", "alice body", "alice note", "mine")
	assert.Equal(t, a, raw("alice", "alice body 2", "alice note v2"), "same profile updates")
	b := raw("bob", "bob body", "bob note", "theirs")
	require.NotEqual(t, a, b, "another profile must not update alice's object")

	objA, err := svc.store.Objects().Get(ctx, a)
	require.NoError(t, err)
	assert.Equal(t, "alice note v2", objA.InboxNote)
	assert.Equal(t, map[string]any{"o": "alice"}, objA.Metadata["data"])
	require.Len(t, objA.Tags, 1)
	assert.Equal(t, "mine", objA.Tags[0].Label)
	objB, err := svc.store.Objects().Get(ctx, b)
	require.NoError(t, err)
	assert.Equal(t, "bob", objB.ProfileID)
	assert.Equal(t, "bob note", objB.InboxNote)
}

func TestAnalyzeCarriesAmbientFieldsOnJob(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	jobID, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "ambient body", Type: "text", Pipeline: "text.short",
		AmbientSource: "clipboard", Fingerprint: "sha256:f", SessionID: "sess_x",
		Metadata: map[string]any{"app_name": "Terminal"},
	})
	require.NoError(t, err)
	job, err := svc.store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, &storage.AmbientContext{
		Source: "clipboard", Fingerprint: "sha256:f", SessionID: "sess_x",
		Metadata: map[string]any{"app_name": "Terminal"},
	}, job.UserAmbient)

	jobID, err = svc.Enqueue(ctx, AnalyzeRequest{
		Content: "ambient body 2", Type: "text", Pipeline: "text.short", SessionID: "sess_y",
	})
	require.NoError(t, err)
	job, err = svc.store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	require.NotNil(t, job.UserAmbient)
	assert.Equal(t, "sess_y", job.UserAmbient.SessionID)

	jobID, err = svc.Analyze(ctx, AnalyzeRequest{Content: "plain body", Type: "text", Pipeline: "text.short"})
	require.NoError(t, err)
	job, err = svc.store.Jobs().Get(ctx, jobID)
	require.NoError(t, err)
	assert.Nil(t, job.UserAmbient, "no ambient fields -> no ambient context")
}

func TestAnalyzeRawStoresAmbientFields(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	id, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: "raw ambient body about pangolins", Type: "text", Raw: true,
		AmbientSource: "clipboard", Fingerprint: "sha256:r", SessionID: "sess_raw",
		Metadata: map[string]any{"app_name": "Notes", "ambient_source": "spoof"},
	})
	require.NoError(t, err)
	obj, err := svc.store.Objects().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "sess_raw", obj.SessionID)
	assert.Equal(t, "clipboard", obj.Metadata["ambient_source"])
	assert.Equal(t, "sha256:r", obj.Metadata["fingerprint"])
	assert.Equal(t, map[string]any{"app_name": "Notes", "ambient_source": "spoof"}, obj.Metadata["client"])
}

func TestAnalyzeRejectsOversizedAmbientFields(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()

	_, err := svc.Analyze(ctx, AnalyzeRequest{Content: "x", Type: "text",
		Metadata: map[string]any{"blob": strings.Repeat("m", MaxAnalyzeMetadataBytes)}})
	assert.True(t, errors.Is(err, ErrInvalidSubmission), "metadata: %v", err)

	for name, req := range map[string]AnalyzeRequest{
		"ambient_source": {AmbientSource: strings.Repeat("s", MaxAnalyzeAmbientIDBytes+1)},
		"fingerprint":    {Fingerprint: strings.Repeat("f", MaxAnalyzeAmbientIDBytes+1)},
		"session_id":     {SessionID: strings.Repeat("i", MaxAnalyzeAmbientIDBytes+1)},
	} {
		req.Content, req.Type = "x", "text"
		_, err := svc.Analyze(ctx, req)
		assert.True(t, errors.Is(err, ErrInvalidSubmission), "%s: %v", name, err)
		_, err = svc.Enqueue(ctx, req)
		assert.True(t, errors.Is(err, ErrInvalidSubmission), "enqueue %s: %v", name, err)
	}
}

// Dedup hashes the decoded bytes: a base64 submission of bytes already
// stored resolves to the stored object, and the job payload is decoded.
func TestAnalyzeBase64DedupsOnDecodedBytes(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	bin := []byte{0x89, 'P', 'N', 'G', 0xff, 0xfe, 0x00, 0x80}
	const source = "blob-a"
	svc.Cfg.Duplicates = config.DuplicatesConfig{Policy: "drop", CheckExact: true}

	// The object a first capture of these bytes left behind.
	const first = "obj-bin-1"
	require.NoError(t, svc.Store(ctx).Objects().Create(ctx, &storage.KnowledgeObject{
		ID: first, Type: "text", Source: source, RawContent: string(bin),
		ContentHash: storageutil.ContentHash(string(bin), source),
		CreatedAt:   time.Now(), UpdatedAt: time.Now(),
	}))

	again, err := svc.Analyze(ctx, AnalyzeRequest{
		Content: base64.StdEncoding.EncodeToString(bin), ContentEncoding: "base64",
		Type: "text", Source: source,
	})
	require.NoError(t, err)
	require.Equal(t, first, again, "same file twice is one object")
	_, total, err := svc.Store(ctx).Objects().List(ctx, storage.ObjectFilter{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
}

func TestEnqueueBase64PayloadDecoded(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	bin := []byte{0xff, 0xfe, 0x00, 0x80}
	id, err := svc.Enqueue(ctx, AnalyzeRequest{
		Content: base64.StdEncoding.EncodeToString(bin), ContentEncoding: "base64", Type: "text",
	})
	require.NoError(t, err)
	job, err := svc.Store(ctx).Jobs().Get(ctx, id)
	require.NoError(t, err)
	// The payload is a TEXT column: binary travels base64, flagged on the type.
	require.True(t, strings.HasSuffix(job.Type, storage.JobTypeBase64), job.Type)
	got, err := base64.StdEncoding.DecodeString(job.Payload)
	require.NoError(t, err)
	require.Equal(t, bin, got)
}

// Text keeps its payload and job type as they were.
func TestEnqueueTextPayloadUnencoded(t *testing.T) {
	svc := newTestService(t)
	ctx := ownerCtx()
	id, err := svc.Enqueue(ctx, AnalyzeRequest{Content: "héllo", Type: "text"})
	require.NoError(t, err)
	job, err := svc.Store(ctx).Jobs().Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "héllo", job.Payload)
	require.False(t, strings.HasSuffix(job.Type, storage.JobTypeBase64), job.Type)
}

func TestAnalyzeRefusesBadContentEncoding(t *testing.T) {
	svc := newTestService(t)
	for _, req := range []AnalyzeRequest{
		{Content: "***", ContentEncoding: "base64", Type: "text"},
		{Content: "x", ContentEncoding: "hex", Type: "text"},
	} {
		_, err := svc.Analyze(ownerCtx(), req)
		require.ErrorIs(t, err, ErrInvalidContentEncoding)
		_, err = svc.Enqueue(ownerCtx(), req)
		require.ErrorIs(t, err, ErrInvalidContentEncoding)
	}
}
