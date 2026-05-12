package jit_test

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// pipelineFetcher is a Fetcher whose response per URL is dictated by maps.
type pipelineFetcher struct {
	bodies map[string]string
	errs   map[string]error
}

func (f *pipelineFetcher) Fetch(_ context.Context, urlStr string) (string, error) {
	if err, ok := f.errs[urlStr]; ok {
		return "", err
	}
	return f.bodies[urlStr], nil
}

func TestPipeline_HappyPath(t *testing.T) {
	prop := &fakeProposer{reply: []string{"/a", "/b"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{
			"https://acme.io/a": "body-a",
			"https://acme.io/b": "body-b",
		},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	cands, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("happy path emitted %d events, want 0", len(got))
	}
}

func TestPipeline_ProposerErrorEmitsScanFailed(t *testing.T) {
	wantErr := errors.New("llm down")
	prop := &fakeProposer{err: wantErr}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	cands, err := p.RunOnce(context.Background(), "obj-99", "https://acme.io/source", "acme.io", "post")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if cands != nil {
		t.Fatalf("candidates = %v, want nil on proposer error", cands)
	}

	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (scan.failed)", len(got))
	}
	if got[0].topic != string(events.ScanFailed) {
		t.Fatalf("topic = %q, want %q", got[0].topic, events.ScanFailed)
	}

	payload, ok := got[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", got[0].payload)
	}
	if payload["object_id"] != "obj-99" {
		t.Errorf("object_id = %v, want obj-99", payload["object_id"])
	}
	q, ok := payload["qualifiers"].(bus.Qualifiers)
	if !ok {
		t.Fatalf("qualifiers type = %T", payload["qualifiers"])
	}
	if q.Mechanism != "jit_proposal" {
		t.Errorf("Mechanism = %q, want jit_proposal", q.Mechanism)
	}
	if q.Reason != wantErr.Error() {
		t.Errorf("Reason = %q, want %q", q.Reason, wantErr.Error())
	}
}

func TestPipeline_EmptyProposalIsNotAFailure(t *testing.T) {
	prop := &fakeProposer{reply: nil} // empty, no error
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	cands, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("err = %v, want nil for empty proposal", err)
	}
	if cands != nil {
		t.Fatalf("candidates = %v, want nil", cands)
	}
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("empty proposal emitted %d events, want 0 (not a failure)", len(got))
	}
}

func TestPipeline_FetchErrorEmitsSubpathFailedPerURL(t *testing.T) {
	prop := &fakeProposer{reply: []string{"/good", "/bad-1", "/bad-2"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)

	netErr := errors.New("connection refused")
	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/good": "ok"},
		errs: map[string]error{
			"https://acme.io/bad-1": netErr,
			"https://acme.io/bad-2": netErr,
		},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	cands, err := p.RunOnce(context.Background(), "obj-7", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("err = %v, want nil (partial fetch failure isn't a scan failure)", err)
	}
	if len(cands) != 1 || cands[0].URL != "https://acme.io/good" {
		t.Fatalf("candidates = %+v, want one entry for /good", cands)
	}

	got := pub.snapshot()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (one subpath.failed per failed URL)", len(got))
	}
	for i, ev := range got {
		if ev.topic != string(events.SubpathFailed) {
			t.Errorf("got[%d].topic = %q, want %q", i, ev.topic, events.SubpathFailed)
		}
	}
}

func TestPipeline_PartialFetchSurvivorsAndFailuresBothSurfaced(t *testing.T) {
	prop := &fakeProposer{reply: []string{"/a", "/b", "/c"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{
			"https://acme.io/a": "ok-a",
			"https://acme.io/c": "ok-c",
		},
		errs: map[string]error{
			"https://acme.io/b": errors.New("net"),
		},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	cands, err := p.RunOnce(context.Background(), "obj-3", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
	if got := pub.snapshot(); len(got) != 1 {
		t.Fatalf("events = %d, want 1 (one subpath.failed for /b)", len(got))
	}
}

func TestPipeline_NilPubIsSafe(t *testing.T) {
	prop := &fakeProposer{err: errors.New("anything")}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{})

	p := jit.NewPipeline(cp, exec, nil, jit.NoOutage())
	_, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err == nil {
		t.Fatal("expected error to propagate even with nil pub")
	}
}

func TestPipeline_ProposerErrorScopesObjectIDAndSubject(t *testing.T) {
	prop := &fakeProposer{err: errors.New("fail")}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	_, _ = p.RunOnce(context.Background(), "obj-id-X", "https://src.example/path", "src.example", "doc")

	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	payload := got[0].payload.(map[string]any)
	if payload["object_id"] != "obj-id-X" {
		t.Errorf("object_id = %v, want obj-id-X", payload["object_id"])
	}
	if payload["subject"] != "https://src.example/path" {
		t.Errorf("subject = %v, want source URL", payload["subject"])
	}
}
