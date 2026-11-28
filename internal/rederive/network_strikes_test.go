package rederive

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// networkStrikeRuns runs six starts over a network-mode pipeline "alpha"
// whose object "a-odd" always fails with fail, and a stored-content pipeline
// "beta" whose successes call the LLM. With acceptedRefresh, one more alpha
// object succeeds each start, fetching from a host and calling the LLM.
// It returns the start after which "a-odd" was skipped (0: never).
func networkStrikeRuns(t *testing.T, fail func(string) error, acceptedRefresh bool) int {
	t.Helper()
	f := newFixture(t)
	networkMode(f)
	betaOld, _ := betaPipeline(f)
	f.svc.fetchedFor = func(id string) []string {
		if strings.HasPrefix(id, "a-ok") {
			return []string{"src.test"}
		}
		return nil // stored content, or no fetch
	}
	f.svc.countsFor = func(string) map[string]int { return map[string]int{"llm": 1} }
	f.add("a-odd", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "a-odd" {
			return fail(id)
		}
		return nil
	}
	skippedAt := 0
	for run := 1; run <= 6; run++ {
		f.add(fmt.Sprintf("b%d", run), "beta@v1", betaOld)
		if acceptedRefresh {
			f.add(fmt.Sprintf("a-ok%d", run), "alpha@v1", alphaOld)
		}
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		if skippedAt == 0 && len(f.skips(alphaNew)) > 0 {
			skippedAt = run
		}
	}
	return skippedAt
}

// In a group that fetches, nothing strikes without an accepted refresh in
// the run, whatever a stored-content group proves about the model.
func TestNetworkStrikes_UnusableReplyWithoutAnAcceptedRefreshNeverSkips(t *testing.T) {
	if at := networkStrikeRuns(t, unusableErr, false); at != 0 {
		t.Fatalf("skipped after run %d", at)
	}
}

func TestNetworkStrikes_ProviderFailureWithoutAnAcceptedRefreshNeverSkips(t *testing.T) {
	if at := networkStrikeRuns(t, llmFailure, false); at != 0 {
		t.Fatalf("skipped after run %d", at)
	}
}

// With a refresh fetched and accepted in each run, the same failure is the
// object's: skipped on the fourth run.
func TestNetworkStrikes_AnAcceptedRefreshMakesItAStrike(t *testing.T) {
	for name, fail := range map[string]func(string) error{"unusable": unusableErr, "provider": llmFailure} {
		t.Run(name, func(t *testing.T) {
			if at := networkStrikeRuns(t, fail, true); at != 4 {
				t.Fatalf("skipped after run %d, want 4", at)
			}
		})
	}
}

// The streak takes the same evidence as a strike. A stored-content group
// that runs first and shows the model working must not keep a fetching
// group from stopping when every page it fetches gets an unusable reply
// (the model ran on what a portal served): five tries, then the group
// stops, at every start.
func TestNetworkStrikes_UnusableRepliesStopAFetchingGroupWhateverAnotherGroupProved(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	const firstOld, firstNew = "aaa#0000000000000001", "aaa#0000000000000002"
	if err := f.reg.Register("aaa", &pipeline.Pipeline{PipelineName: "aaa", Signature: firstNew, Entries: []string{"x@2"},
		BaselineSignature: firstOld, BaselineEntries: []string{"x@1"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpsertDerivationSignature(t.Context(), firstOld, "aaa", []string{"x@1"}); err != nil {
		t.Fatal(err)
	}
	plan := llmPlan()
	plan.Steps = []string{"x"}
	f.svc.plans["aaa"] = plan
	f.svc.countsFor = func(string) map[string]int { return map[string]int{"llm": 1} }
	f.svc.fetchedFor = func(string) []string { return nil } // no refresh is accepted in this run
	f.add("s1", "aaa@v1", firstOld)
	for i := 0; i < 12; i++ {
		f.add(fmt.Sprintf("p%02d", i), "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if strings.HasPrefix(id, "p") {
			return unusableErr(id)
		}
		return nil
	}
	res, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, id := range f.svc.called() {
		if strings.HasPrefix(id, "p") {
			pages++
		}
	}
	if pages != failureStreak || len(res.Stopped) != 1 || res.Stopped[0].Pipeline != "alpha" {
		t.Fatalf("tried %d pages, stopped %+v: want %d tried and the fetching group stopped", pages, res.Stopped, failureStreak)
	}
	if res.Processed != 1 || len(f.skips(alphaNew)) != 0 {
		t.Fatalf("result %+v, skips %v: the stored-content object is processed, nothing skipped", res, f.skips(alphaNew))
	}
}
