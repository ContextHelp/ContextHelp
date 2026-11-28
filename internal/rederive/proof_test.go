package rederive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func betaPipeline(f *fixture) (old, cur string) {
	old, cur = "beta#0000000000000001", "beta#0000000000000002"
	if err := f.reg.Register("beta", &pipeline.Pipeline{PipelineName: "beta", Signature: cur, Entries: []string{"x@2"},
		BaselineSignature: old, BaselineEntries: []string{"x@1"}}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.UpsertDerivationSignature(f.t.Context(), old, "beta", []string{"x@1"}); err != nil {
		f.t.Fatal(err)
	}
	plan := llmPlan()
	plan.Steps = []string{"x"}
	f.svc.plans["beta"] = plan
	return old, cur
}

// Proof is per call: a success that made no call of a kind proves nothing
// about it. An object with nothing to embed succeeds without a single call.
func TestProof_ASuccessWithoutACallProvesNothing(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	f.add("a-empty", "alpha@v1", alphaOld)
	f.svc.countsFor = func(id string) map[string]int {
		if id == "a-empty" {
			return nil // no text: no embed call, no LLM call
		}
		return map[string]int{"embedding": 1}
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "a-empty" || strings.HasPrefix(id, "z-ok") {
			return nil
		}
		// An unreadable embedding registry: never about the object.
		return fmt.Errorf("reanalyze: %q: %w, nothing written: %w", id, upgrade.ErrProviderFailed,
			&providers.CallError{Provider: "embedding registry", Err: errors.New("registry unreadable")})
	}
	f.add("b1", "alpha@v1", alphaOld)
	f.add("b2", "alpha@v1", alphaOld)
	for run := 1; run <= 7; run++ {
		if run >= 2 {
			f.add(fmt.Sprintf("z-ok%d", run), "alpha@v1", alphaOld) // a real embed call succeeds elsewhere
		}
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.skips(alphaNew); len(got) != 0 {
		t.Fatalf("skips = %v: an unreadable registry is never an object's fault", got)
	}
}

// A model that answers nothing, in a pipeline whose LLM steps are all
// length-gated: short objects succeed without any LLM call, long ones get
// unusable replies. The short successes prove nothing about the model.
func TestProof_LengthGatedSuccessesDoNotProveTheModel(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	f.add("long", "alpha@v1", alphaOld)
	f.svc.countsFor = func(string) map[string]int { return nil } // short: no LLM call
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "long" {
			return unusableErr(id)
		}
		return nil
	}
	for run := 1; run <= 6; run++ {
		f.add(fmt.Sprintf("short%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.skips(alphaNew); len(got) != 0 {
		t.Fatalf("skips = %v", got)
	}
}

// A group that does not use the LLM succeeds first; another group gets only
// unusable replies: nothing proved the model, so they stop the run on the
// streak and skip nothing.
func TestProof_AnotherGroupsSuccessDoesNotProveTheModel(t *testing.T) {
	f := newFixture(t)
	betaOld, _ := betaPipeline(f)
	f.svc.countsFor = func(string) map[string]int { return nil }
	f.add("a-fine", "alpha@v1", alphaOld)
	for i := 0; i < 6; i++ {
		f.add(fmt.Sprintf("b%d", i), "beta@v1", betaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if strings.HasPrefix(id, "b") {
			return unusableErr(id)
		}
		return nil
	}
	for run := 1; run <= 5; run++ {
		res, err := f.run()
		if run == 1 && (res.Processed != 1 || err != nil || len(res.Stopped) != 1) {
			t.Fatalf("run 1 = %+v, %v: the other group's success, then a stop on the streak", res, err)
		}
	}
	if got := f.skips("beta#0000000000000002"); len(got) != 0 {
		t.Fatalf("skips = %v", got)
	}
}
