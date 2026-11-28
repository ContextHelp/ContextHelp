package rederive

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func TestRun_DefersWhatWouldNotStayLocal(t *testing.T) {
	for _, loc := range []pipeline.Locality{pipeline.LocalityHosted, pipeline.LocalityUnknown, pipeline.LocalityUnavailable} {
		t.Run(string(loc), func(t *testing.T) {
			f := newFixture(t)
			f.add("o1", "alpha@v1", alphaOld)
			f.add("o2", "alpha@v1", alphaOld)
			plan := localPlan("a", "b")
			plan.Locality = loc
			plan.Uses = []pipeline.StepUse{{Step: "tagger", ProviderUse: pipeline.ProviderUse{Resource: pipeline.ResourceLLM, Backend: "openai", Endpoint: "https://u:p@api.example.com"}}}
			f.svc.plans["alpha"] = plan
			prog := &fakeProgress{}
			f.job.Progress = prog
			bus := &recordingBus{}
			f.job.Bus = bus
			res, err := f.run()
			if err != nil {
				t.Fatal(err)
			}
			if len(f.svc.called()) != 0 {
				t.Fatalf("re-derived %v", f.svc.called())
			}
			if f.stale() != 2 || len(f.skips(alphaNew)) != 0 {
				t.Errorf("stale=%d skips=%v: deferred objects must stay stale and unrecorded", f.stale(), f.skips(alphaNew))
			}
			if res.Total != 0 || len(res.Deferred) != 1 || res.Deferred[0].Pipeline != "alpha" || res.Deferred[0].Objects != 2 {
				t.Fatalf("result = %+v", res)
			}
			r := res.Deferred[0].Reason
			if !strings.Contains(r, string(loc)) || !strings.Contains(r, "tagger") || strings.Contains(r, "api.example.com") {
				t.Errorf("reason %q: want the locality and the step, never the endpoint", r)
			}
			if !slices.Contains(bus.topics, "dpkms.upgrade.rederive.completed") {
				t.Errorf("events = %v", bus.topics)
			}

			// The configuration changes: the next run takes them.
			f.svc.plans["alpha"] = localPlan("a", "b")
			res, err = f.run()
			if err != nil || res.Processed != 2 || len(res.Deferred) != 0 {
				t.Fatalf("after the switch: %+v, %v", res, err)
			}
		})
	}
}

func TestRun_DeferredAndRunnableObjectsInOneRun(t *testing.T) {
	f := newFixture(t)
	if err := f.reg.Register("beta", &pipeline.Pipeline{PipelineName: "beta", Signature: "beta#0000000000000002",
		Entries: []string{"x@2"}, BaselineSignature: "beta#0000000000000001", BaselineEntries: []string{"x@1"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpsertDerivationSignature(context.Background(), "beta#0000000000000001", "beta", []string{"x@1"}); err != nil {
		t.Fatal(err)
	}
	hosted := localPlan("x")
	hosted.Locality = pipeline.LocalityHosted
	f.svc.plans["beta"] = hosted
	f.add("a1", "alpha@v1", alphaOld)
	f.add("b1", "beta@v1", "beta#0000000000000001")
	prog := &fakeProgress{}
	f.job.Progress = prog
	res, err := f.run()
	if err != nil || res.Processed != 1 || res.Total != 1 || len(res.Deferred) != 1 || res.Deferred[0].Pipeline != "beta" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if prog.total != 1 || len(prog.deferred) != 1 {
		t.Errorf("status total=%d deferred=%v: deferred objects are not part of the total but are shown", prog.total, prog.deferred)
	}
	if got := f.svc.called(); !slices.Equal(got, []string{"a1"}) {
		t.Errorf("re-derived %v", got)
	}
}

// --- mode ---

func TestRun_ModeFollowsTheChangedStep(t *testing.T) {
	cases := []struct {
		name        string
		old, new    []string
		storedSteps []string
		storedErr   error
		wantNetwork bool
	}{
		{"a step stored content runs", []string{"a@1", "b@1"}, []string{"a@1", "b@2"}, []string{"a", "b"}, nil, false},
		{"a fetch step", []string{"fetch@1", "b@1"}, []string{"fetch@2", "b@1"}, []string{"b"}, nil, true},
		{"an added fresh-source step", []string{"b@1"}, []string{"clean@1", "b@1"}, []string{"b"}, nil, true},
		{"model only", []string{"b@1", "llm=x/y"}, []string{"b@1", "llm=x/z"}, []string{"b"}, nil, false},
		{"stored content refuses the pipeline", []string{"b@1"}, []string{"b@2"}, nil, upgrade.ErrCannotReanalyze, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: alphaNew, Entries: c.new,
				BaselineSignature: alphaOld, BaselineEntries: c.old})
			if err := f.store.UpsertDerivationSignature(context.Background(), alphaOld, "alpha", c.old); err != nil {
				t.Fatal(err)
			}
			f.svc.plans["alpha"] = localPlan(c.storedSteps...)
			f.svc.plans["alpha/net"] = localPlan("fetch", "clean", "a", "b")
			f.svc.perrs["alpha"] = c.storedErr
			f.add("o1", "alpha@v1", alphaOld)
			if _, err := f.run(); err != nil {
				t.Fatal(err)
			}
			f.svc.mu.Lock()
			defer f.svc.mu.Unlock()
			if len(f.svc.calls) != 1 || f.svc.calls[0].Network != c.wantNetwork || f.svc.calls[0].Guarded != c.wantNetwork {
				t.Fatalf("calls = %+v, want network=%v with the guard only then", f.svc.calls, c.wantNetwork)
			}
		})
	}
}
