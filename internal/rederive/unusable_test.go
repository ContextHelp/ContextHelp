package rederive

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func unusableErr(id string) error {
	return fmt.Errorf("reanalyze: %q: %w, nothing written: %w", id, upgrade.ErrProviderFailed,
		&providers.UnusableReplyError{Step: "graph_extractor", Why: "not JSON"})
}

// An unusable reply is the object's problem only once the run has shown the
// provider working: a try, and a strike under the same proof as a failure of
// that provider. One object among successes is skipped on the fourth run.
func TestUnusableReply_OneObjectAmongSuccessesIsSkippedOnTheFourthRun(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	f.add("odd", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "odd" {
			return unusableErr(id)
		}
		return nil
	}
	for run := 1; run <= 4; run++ {
		f.add(fmt.Sprintf("good%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		if skipped := f.skips(alphaNew)["odd"] == storage.SkipUnusableReply; skipped != (run == 4) {
			t.Fatalf("after run %d: skipped = %v", run, skipped)
		}
	}
	sk := skipRows(t, f.drv, alphaNew)
	if len(sk) != 1 || !strings.Contains(sk[0].Detail, "after 3 strikes") || !strings.Contains(sk[0].Detail, "unusable reply") {
		t.Fatalf("skip = %+v", sk)
	}
}

// Five unusable replies first in ID order, then healthy objects: the first
// run, which has shown nothing working, stops after five; the second goes
// to the healthy ones first, then strikes the five without stopping.
func TestUnusableReply_FiveThenHealthyObjects(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	for i := 0; i < 5; i++ {
		f.add(fmt.Sprintf("a-odd%d", i), "alpha@v1", alphaOld)
	}
	for i := 0; i < 3; i++ {
		f.add(fmt.Sprintf("z-good%d", i), "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if strings.HasPrefix(id, "a-odd") {
			return unusableErr(id)
		}
		return nil
	}
	res, err := f.run()
	if err != nil || !stoppedKind(res, "llm unusable reply") || res.Processed != 0 || res.Transient != 5 {
		t.Fatalf("run 1 = %+v, %v", res, err)
	}
	f.svc.mu.Lock()
	f.svc.calls = nil
	f.svc.mu.Unlock()
	res, err = f.run()
	if err != nil || res.Processed != 3 {
		t.Fatalf("run 2 = %+v, %v: the healthy ones, then the five without a stop", res, err)
	}
	if first := f.svc.called()[:3]; !slices.Equal(first, []string{"z-good0", "z-good1", "z-good2"}) {
		t.Fatalf("run 2 began with %v", first)
	}
	// Run 2 struck the five once; three more runs with a success skip them.
	for run := 3; run <= 5; run++ {
		f.add(fmt.Sprintf("z-more%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		if got := len(f.skips(alphaNew)); (got == 5) != (run == 5) {
			t.Fatalf("after run %d: %d skips", run, got)
		}
	}
}

// With no success in the run, an unusable reply is a try, never a strike:
// a model that answers nothing to everyone skips nothing.
func TestUnusableReply_WithoutAnyProofIsOnlyATry(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	f.add("odd", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return unusableErr(id) }
	for run := 1; run <= 6; run++ {
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.skips(alphaNew)) != 0 {
		t.Fatalf("skips = %v", f.skips(alphaNew))
	}
}

// A model answering nothing to every object stops the run after five.
func TestUnusableReply_EveryObjectStopsTheRunAfterFive(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	addN(f, 9)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return unusableErr(id) }
	res, err := f.run()
	if err != nil || !stoppedKind(res, "llm unusable reply") || res.Transient != failureStreak || len(f.skips(alphaNew)) != 0 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if n := len(f.svc.called()); n != failureStreak {
		t.Fatalf("tried %d objects", n)
	}
}

// An unusable reply does not reset a streak of real provider failures.
func TestUnusableReply_ResetsNothing(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	addN(f, 6)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "o2" {
			return unusableErr(id)
		}
		return llmFailure(id)
	}
	if res, err := f.run(); err != nil || len(res.Stopped) != 1 {
		t.Fatalf("stopped = %+v, err %v: the unusable reply must not have reset the streak", res.Stopped, err)
	}
}

// One error holding both a failed call and an unusable reply is a failed
// call: the call failure wins.
func TestUnusableReply_ACallFailureWinsOverIt(t *testing.T) {
	err := fmt.Errorf("reanalyze: %w, nothing written: %w", upgrade.ErrProviderFailed,
		errors.Join(&providers.CallError{Provider: "llm ollama", Err: errors.New("refused")},
			&providers.UnusableReplyError{Step: "tagger", Why: "empty tag list", Provider: "llm"}))
	out := classify(err)
	if out.unusable || out.reason != storage.SkipProviderFailed || out.proof != "provider:llm" {
		t.Fatalf("outcome = %+v", out)
	}
}
