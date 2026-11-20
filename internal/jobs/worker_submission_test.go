package jobs

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// seenDraft records the draft a step receives.
type seenDraft struct {
	pipeline.BaseContract
	got *storage.KnowledgeObject
}

func (s *seenDraft) Name() string { return "seen-draft" }
func (s *seenDraft) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	cp := *d
	s.got = &cp
	return d, nil
}

type submissionEnv struct {
	driver storage.StorageDriver
	q      *Queue
	pool   *WorkerPool
}

func newSubmissionEnv(t *testing.T, extra ...pipeline.PipelineStep) *submissionEnv {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	if len(extra) > 0 {
		pipes.Register("test.seen", &pipeline.Pipeline{PipelineName: "test.seen", Steps: extra})
	}
	return &submissionEnv{driver: driver, q: q, pool: NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())}
}

func (e *submissionEnv) run(t *testing.T, job *storage.Job) *storage.Job {
	t.Helper()
	if err := e.q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got := runUntilDone(t, e.pool, e.q, job.ID)
	requireCompleted(t, got)
	return got
}

func (e *submissionEnv) object(t *testing.T, id string) *storage.KnowledgeObject {
	t.Helper()
	obj, err := e.driver.Objects().Get(context.Background(), id)
	if err != nil || obj == nil {
		t.Fatalf("get object %s: %v", id, err)
	}
	return obj
}

func (e *submissionEnv) count(t *testing.T) int {
	t.Helper()
	_, total, err := e.driver.Objects().List(context.Background(), storage.ObjectFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func dataOf(t *testing.T, obj *storage.KnowledgeObject) map[string]any {
	t.Helper()
	d, _ := obj.Metadata["data"].(map[string]any)
	return d
}

// A note, data and source key ride the job to the persisted object; the
// note sits beside the content, never in place of it.
func TestProcessJobPersistsNoteDataSourceKey(t *testing.T) {
	e := newSubmissionEnv(t)
	job := makeJob("job-sub-1")
	job.Payload = "page body about numbats and the dryandra woodland"
	job.UserNote = "full transcript text"
	job.UserData = map[string]any{"title": "Numbats", "channel": "wild"}
	job.UserSourceKey = "youtube:abc123"

	got := e.run(t, job)

	obj := e.object(t, got.ResultID)
	if obj.InboxNote != "full transcript text" {
		t.Errorf("note: %q", obj.InboxNote)
	}
	if d := dataOf(t, obj); d["title"] != "Numbats" || d["channel"] != "wild" {
		t.Errorf("data: %v", obj.Metadata)
	}
	if obj.SourceKey != "youtube:abc123" {
		t.Errorf("source key: %q", obj.SourceKey)
	}
	if obj.RawContent != job.Payload {
		t.Errorf("content replaced by note: %q", obj.RawContent)
	}
}

// Steps see the note and data on the draft they receive.
func TestProcessJobStepsSeeNoteAndData(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	job := makeJob("job-sub-spy")
	job.Pipeline = "test.seen"
	job.UserNote = "transcript"
	job.UserData = map[string]any{"title": "T"}
	job.UserSourceKey = "youtube:spy"

	e.run(t, job)

	if spy.got == nil {
		t.Fatal("step did not run")
	}
	if spy.got.InboxNote != "transcript" {
		t.Errorf("draft note: %q", spy.got.InboxNote)
	}
	if d, _ := spy.got.Metadata["data"].(map[string]any); d["title"] != "T" {
		t.Errorf("draft metadata: %v", spy.got.Metadata)
	}
	if spy.got.SourceKey != "youtube:spy" {
		t.Errorf("draft source key: %q", spy.got.SourceKey)
	}
}

// Same source key, changed content: no second object; the first one's
// note and data are replaced and its hints and mentions grow.
func TestProcessJobSourceKeyUpdatesInPlace(t *testing.T) {
	e := newSubmissionEnv(t)
	first := makeJob("job-key-1")
	first.Payload = "video page captured with auto captions"
	first.UserNote = "captions transcript"
	first.UserData = map[string]any{"title": "v1"}
	first.UserHints = []string{"video"}
	first.UserMentions = []string{"@channel.wild"}
	first.UserSourceKey = "youtube:upg"
	r1 := e.run(t, first)

	second := makeJob("job-key-2")
	second.Payload = "video page captured again, markup moved around"
	second.UserNote = "whisper transcript"
	second.UserData = map[string]any{"title": "v2"}
	second.UserHints = []string{"video", "tools"}
	second.UserMentions = []string{"@channel.wild", "@guest.ada"}
	second.UserSourceKey = "youtube:upg"
	r2 := e.run(t, second)

	if r2.ResultID != r1.ResultID {
		t.Fatalf("second job answered %s, want the first object %s", r2.ResultID, r1.ResultID)
	}
	if n := e.count(t); n != 1 {
		t.Fatalf("objects: %d, want 1", n)
	}
	obj := e.object(t, r1.ResultID)
	if obj.InboxNote != "whisper transcript" {
		t.Errorf("note not upgraded: %q", obj.InboxNote)
	}
	if dataOf(t, obj)["title"] != "v2" {
		t.Errorf("data not upgraded: %v", obj.Metadata)
	}
	labels := map[string]bool{}
	for _, tag := range obj.Tags {
		labels[tag.Label] = true
	}
	if !labels["video"] || !labels["tools"] {
		t.Errorf("tags: %+v", obj.Tags)
	}
	if len(obj.Mentions) != 2 {
		t.Errorf("mentions: %v", obj.Mentions)
	}
	if obj.ReinforcementCount != 2 {
		t.Errorf("reinforcement_count: %d, want 2", obj.ReinforcementCount)
	}
	if obj.RawContent != first.Payload {
		t.Errorf("stored content must not change on update: %q", obj.RawContent)
	}
}

// No source key, identical content (the content-hash dedup path): the
// existing object takes the new note and data instead of keeping the old.
func TestProcessJobSameContentUpdatesNoteAndData(t *testing.T) {
	e := newSubmissionEnv(t)
	first := makeJob("job-hash-1")
	first.Payload = "same page body about quokkas"
	first.UserNote = "old note"
	first.UserData = map[string]any{"v": "1"}
	r1 := e.run(t, first)

	second := makeJob("job-hash-2")
	second.Payload = first.Payload
	second.UserNote = "new note"
	second.UserData = map[string]any{"v": "2"}
	r2 := e.run(t, second)

	if r2.ResultID != r1.ResultID || e.count(t) != 1 {
		t.Fatalf("want one object, got %d (%s vs %s)", e.count(t), r1.ResultID, r2.ResultID)
	}
	obj := e.object(t, r1.ResultID)
	if obj.InboxNote != "new note" || dataOf(t, obj)["v"] != "2" {
		t.Errorf("note %q data %v", obj.InboxNote, obj.Metadata)
	}
}

// A re-submission that carries no note and no data leaves the stored
// ones alone.
func TestProcessJobResubmitWithoutNoteKeepsStored(t *testing.T) {
	e := newSubmissionEnv(t)
	first := makeJob("job-keep-1")
	first.Payload = "body one"
	first.UserNote = "kept"
	first.UserData = map[string]any{"k": "v"}
	first.UserSourceKey = "k:1"
	r1 := e.run(t, first)

	second := makeJob("job-keep-2")
	second.Payload = "body two"
	second.UserSourceKey = "k:1"
	e.run(t, second)

	obj := e.object(t, r1.ResultID)
	if obj.InboxNote != "kept" || dataOf(t, obj)["k"] != "v" {
		t.Errorf("note %q data %v", obj.InboxNote, obj.Metadata)
	}
}

// The source key is checked before the content hash: content that hashes
// to another stored object still updates the object the key names.
func TestProcessJobSourceKeyWinsOverContentHash(t *testing.T) {
	e := newSubmissionEnv(t)
	other := makeJob("job-win-other")
	other.Payload = "shared page body about bilbies"
	rOther := e.run(t, other)

	keyed := makeJob("job-win-keyed")
	keyed.Payload = "a different body about bilbies"
	keyed.UserSourceKey = "k:win"
	keyed.UserNote = "first"
	rKeyed := e.run(t, keyed)

	again := makeJob("job-win-again")
	again.Payload = other.Payload // hashes to the other object
	again.UserSourceKey = "k:win"
	again.UserNote = "second"
	rAgain := e.run(t, again)

	if rAgain.ResultID != rKeyed.ResultID {
		t.Fatalf("updated %s, want the keyed object %s", rAgain.ResultID, rKeyed.ResultID)
	}
	if e.object(t, rKeyed.ResultID).InboxNote != "second" {
		t.Errorf("keyed object note: %q", e.object(t, rKeyed.ResultID).InboxNote)
	}
	if o := e.object(t, rOther.ResultID); o.ReinforcementCount != 1 || o.InboxNote != "" {
		t.Errorf("other object touched: reinforced %d note %q", o.ReinforcementCount, o.InboxNote)
	}
}

// A source key names a source within one profile: the same key from the
// same profile updates, from another profile it is a separate object and
// the first profile's object is untouched.
func TestProcessJobSourceKeyIsScopedToProfile(t *testing.T) {
	e := newSubmissionEnv(t)
	a := makeJob("job-prof-a")
	a.Payload = "profile a body about dingoes"
	a.UserProfile, a.UserSourceKey = "alice", "k:shared"
	a.UserNote, a.UserData, a.UserHints = "alice note", map[string]any{"o": "alice"}, []string{"mine"}
	rA := e.run(t, a)

	same := makeJob("job-prof-a2")
	same.Payload = "profile a body, later"
	same.UserProfile, same.UserSourceKey = "alice", "k:shared"
	same.UserNote = "alice note v2"
	if r := e.run(t, same); r.ResultID != rA.ResultID {
		t.Fatalf("same profile: updated %s, want %s", r.ResultID, rA.ResultID)
	}

	other := makeJob("job-prof-b")
	other.Payload = "profile b body about dingoes"
	other.UserProfile, other.UserSourceKey = "bob", "k:shared"
	other.UserNote, other.UserData, other.UserHints = "bob note", map[string]any{"o": "bob"}, []string{"theirs"}
	rB := e.run(t, other)

	if rB.ResultID == rA.ResultID {
		t.Fatal("another profile's submission updated alice's object")
	}
	if n := e.count(t); n != 2 {
		t.Fatalf("objects: %d, want 2", n)
	}
	objA := e.object(t, rA.ResultID)
	if objA.InboxNote != "alice note v2" || dataOf(t, objA)["o"] != "alice" {
		t.Errorf("alice's object changed: note %q data %v", objA.InboxNote, objA.Metadata)
	}
	for _, tag := range objA.Tags {
		if tag.Label == "theirs" {
			t.Errorf("bob's hint leaked onto alice's object: %+v", objA.Tags)
		}
	}
	objB := e.object(t, rB.ResultID)
	if objB.ProfileID != "bob" || objB.SourceKey != "k:shared" || objB.InboxNote != "bob note" {
		t.Errorf("bob's object: profile %q key %q note %q", objB.ProfileID, objB.SourceKey, objB.InboxNote)
	}
}
