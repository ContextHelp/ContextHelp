package jobs

import (
	"context"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// staleAgainst lists the object IDs whose signature is non-empty and not
// in current.
func staleAgainst(t *testing.T, e *submissionEnv, current ...string) []string {
	t.Helper()
	ds, ok := e.driver.Objects().(storage.DerivationStore)
	if !ok {
		t.Fatalf("%T does not implement storage.DerivationStore", e.driver.Objects())
	}
	ids, err := ds.ListStaleDerivations(context.Background(), current, "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// requireSignedBy fails unless object id carries exactly signature sig.
func requireSignedBy(t *testing.T, e *submissionEnv, id, sig string) {
	t.Helper()
	if !slices.Contains(staleAgainst(t, e, "never#0000000000000000"), id) {
		t.Fatalf("object %s is unsigned, want %q", id, sig)
	}
	if slices.Contains(staleAgainst(t, e, sig), id) {
		t.Fatalf("object %s is signed with something other than %q", id, sig)
	}
}

// A job run through the worker stores an object signed with the
// signature of the pipeline that ran it.
func TestProcessJobStampsPipelineSignature(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	custom, err := e.pool.pipelines.Get("test.seen")
	if err != nil {
		t.Fatal(err)
	}
	custom.Signature = "test.seen#0123456789abcdef"

	job := makeJob("job-derivation-custom")
	job.Pipeline = "test.seen"
	got := e.run(t, job)
	requireSignedBy(t, e, got.ResultID, custom.Signature)

	// A builtin pipeline stamps the signature its build computed.
	builtin := makeJob("job-derivation-builtin")
	builtin.Payload = "a note about the numbat, a termite-eating marsupial of western australia"
	got = e.run(t, builtin)
	p, err := e.pool.pipelines.Get(builtin.Pipeline)
	if err != nil {
		t.Fatal(err)
	}
	if p.Signature == "" {
		t.Fatalf("builtin pipeline %q has no signature", builtin.Pipeline)
	}
	requireSignedBy(t, e, got.ResultID, p.Signature)
}

// A hand-built pipeline has no signature: its objects stay unsigned,
// never stale.
func TestProcessJobUnsignedPipelineLeavesObjectUnsigned(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	job := makeJob("job-derivation-unsigned")
	job.Pipeline = "test.seen"
	got := e.run(t, job)
	if slices.Contains(staleAgainst(t, e, "never#0000000000000000"), got.ResultID) {
		t.Error("object of an unsigned pipeline carries a signature")
	}
}

// rogueExecutor runs the pipeline locally, then claims a signature of its
// own, as a pod with a different registry would.
type rogueExecutor struct{ inner *LocalExecutor }

func (r rogueExecutor) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	d, err := r.inner.Execute(ctx, job)
	if d != nil {
		d.DerivationSig = "rogue#0000000000000000"
	}
	return d, err
}

// The host decides the signature: whatever an executor reports, the object
// is signed with the host registry's signature for the requested pipeline.
func TestProcessJobExecutorCannotChooseSignature(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	custom, err := e.pool.pipelines.Get("test.seen")
	if err != nil {
		t.Fatal(err)
	}
	custom.Signature = "test.seen#0123456789abcdef"
	e.pool.SetExecutor(rogueExecutor{&LocalExecutor{Pipelines: e.pool.pipelines, MaxHops: 5}})

	job := makeJob("job-derivation-pod")
	job.Pipeline = "test.seen"
	got := e.run(t, job)
	requireSignedBy(t, e, got.ResultID, custom.Signature)
}

// A source-key resubmission is a submission-only update: the stored
// content and the pipeline's own output are left as they are, so the row
// keeps the signature of the derivation that produced it. Stamping it with
// the pipeline that just ran would claim a derivation that never happened.
func TestSourceKeyUpdateKeepsTheStoredSignature(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	custom, err := e.pool.pipelines.Get("test.seen")
	if err != nil {
		t.Fatal(err)
	}
	custom.Signature = "test.seen#1111111111111111"

	first := makeJob("job-derivation-key-1")
	first.Pipeline = "test.seen"
	first.UserSourceKey = "derivation:key"
	first.Payload = "first body about the quokka"
	got := e.run(t, first)
	requireSignedBy(t, e, got.ResultID, "test.seen#1111111111111111")

	custom.Signature = "test.seen#2222222222222222"
	second := makeJob("job-derivation-key-2")
	second.Pipeline = "test.seen"
	second.UserSourceKey = "derivation:key"
	second.Payload = "second body about the numbat"
	again := e.run(t, second)
	if again.ResultID != got.ResultID {
		t.Fatalf("resubmission made object %s, want an update of %s", again.ResultID, got.ResultID)
	}
	requireSignedBy(t, e, got.ResultID, "test.seen#1111111111111111")
}
