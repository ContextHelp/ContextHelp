package rederive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// llmPlan is a local plan whose steps call the LLM: a success through it
// proves the provider works.
func llmPlan() service.ReanalysisPlan {
	p := localPlan("a", "b")
	p.Locality = pipeline.LocalityLocal
	p.Uses = []pipeline.StepUse{{Step: "tagger", ProviderUse: pipeline.ProviderUse{Resource: pipeline.ResourceLLM, Backend: "ollama", Usable: true}}}
	return p
}

// networkMode makes the fixture's change one that only applies by fetching.
func networkMode(f *fixture) {
	oldEntries, newEntries := []string{"fetch@1", "a@1", "b@1"}, []string{"fetch@2", "a@1", "b@1"}
	f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: alphaNew, Entries: newEntries,
		BaselineSignature: alphaOld, BaselineEntries: oldEntries})
	if err := f.store.UpsertDerivationSignature(f.t.Context(), alphaOld, "alpha", oldEntries); err != nil {
		f.t.Fatal(err)
	}
	f.svc.plans["alpha"] = localPlan("a", "b")
	f.svc.plans["alpha/net"] = localPlan("fetch", "a", "b")
	f.svc.fetched = []string{"src.test"} // a successful re-derive fetched from it
}

func plainErr(string) error { return errors.New("database is locked") }

// A provider that is down at every boot must never turn an object into a
// durable skip: nothing in those runs proved the provider works.
func TestStrikes_ProviderDownAtEveryBootNeverSkips(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	addN(f, 3)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return llmFailure(id) }
	for start := 1; start <= 6; start++ {
		res, err := f.run()
		if err != nil || res.SkippedTotal() != 0 || res.Transient != 3 {
			t.Fatalf("start %d: %+v, %v", start, res, err)
		}
	}
	if len(f.skips(alphaNew)) != 0 || f.stale() != 3 {
		t.Fatalf("skips %v stale %d", f.skips(alphaNew), f.stale())
	}
	f.svc.result = nil
	if res, err := f.run(); err != nil || res.Processed != 3 {
		t.Fatalf("provider up: %+v, %v", res, err)
	}
}

// With the provider proved working by another object, the failures of one
// object are strikes, and the failure after three strikes is a skip.
func TestStrikes_ProviderFailureOfOneObjectIsAStrike(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	f.add("bad", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "bad" {
			return llmFailure(id)
		}
		return nil
	}
	for run := 1; run <= 4; run++ {
		f.add(fmt.Sprintf("good%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		skipped := f.skips(alphaNew)["bad"] == storage.SkipProviderFailed
		if skipped != (run == 4) {
			t.Fatalf("after run %d: skipped = %v", run, skipped)
		}
	}
	sk := skipRows(t, f.drv, alphaNew)
	if len(sk) != 1 || !strings.Contains(sk[0].Detail, "after 3 strikes") {
		t.Fatalf("skip = %+v", sk)
	}
}

// A provider kind proved by an object that does not call it proves nothing:
// the LLM failing while only a plan without provider uses succeeds.
func TestStrikes_ASuccessThatDidNotCallTheProviderProvesNothing(t *testing.T) {
	f := newFixture(t) // localPlan: no provider uses
	f.add("bad", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "bad" {
			return llmFailure(id)
		}
		return nil
	}
	for run := 1; run <= 6; run++ {
		f.add(fmt.Sprintf("good%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.skips(alphaNew)) != 0 {
		t.Fatalf("skips = %v", f.skips(alphaNew))
	}
}

// Five failing objects that never succeed, among healthy ones that do: the
// healthy ones go first and are processed; the failing five become skips
// only once something else succeeded in more than three runs.
func TestStrikes_FiveBadObjectsAmongHealthyOnes(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		f.add(fmt.Sprintf("bad%d", i), "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if strings.HasPrefix(id, "bad") {
			return plainErr(id)
		}
		return nil
	}
	// Run 0: nothing else exists, so there is no proof.
	res, err := f.run()
	if err != nil || !stoppedKind(res, "unknown") || res.Transient != 5 || len(f.skips(alphaNew)) != 0 {
		t.Fatalf("run 0 = %+v, %v", res, err)
	}
	for run := 1; run <= 4; run++ {
		f.add(fmt.Sprintf("good%d", run), "alpha@v1", alphaOld)
		f.svc.mu.Lock()
		f.svc.calls = nil
		f.svc.mu.Unlock()
		res, err = f.run()
		if res.Processed != 1 || f.svc.called()[0] != fmt.Sprintf("good%d", run) {
			t.Fatalf("run %d: %+v, %v, order %v: the healthy object must go first", run, res, err, f.svc.called())
		}
		if got := len(f.skips(alphaNew)); (got == 5) != (run == 4) || (got != 0 && got != 5) {
			t.Fatalf("after run %d: %d skips", run, got)
		}
	}
	for id, reason := range f.skips(alphaNew) {
		if reason != storage.SkipFailed {
			t.Errorf("%s skipped as %s", id, reason)
		}
	}
}

// A dead host among healthy pages: the failures are the object's once
// other network fetches succeeded in the same run.
func TestStrikes_ADeadHostAmongHealthyPages(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	f.add("dead", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "dead" {
			return fetchErr("dial tcp: i/o timeout")
		}
		return nil
	}
	for run := 1; run <= 4; run++ {
		f.add(fmt.Sprintf("page%d", run), "alpha@v1", alphaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		if skipped := f.skips(alphaNew)["dead"] == storage.SkipFetchFailed; skipped != (run == 4) {
			t.Fatalf("after run %d: skipped = %v", run, skipped)
		}
	}
}

// The network down for a whole run proves nothing: tries only.
func TestStrikes_NetworkDownForAWholeRunIsTriesOnly(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	addN(f, 3)
	f.svc.result = func(string, service.ReanalyzeOptions) error { return fetchErr("dial tcp: no route to host") }
	for run := 1; run <= 8; run++ {
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.skips(alphaNew)) != 0 || f.stale() != 3 {
		t.Fatalf("skips %v stale %d", f.skips(alphaNew), f.stale())
	}
}

// An error the job does not know is not a durable skip: five in a row stop
// the run with the cause, and nothing is recorded.
func TestUnclassified_EveryObjectFailingStopsTheRunAfterFive(t *testing.T) {
	f := newFixture(t)
	addN(f, 8)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return plainErr(id) }
	res, err := f.run()
	if err != nil || !stoppedKind(res, "unknown") || !strings.Contains(res.Stopped[0].Cause, "database is locked") || res.Transient != failureStreak {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if n := len(f.svc.called()); n != failureStreak {
		t.Fatalf("tried %d objects", n)
	}
	if len(f.skips(alphaNew)) != 0 || f.stale() != 8 {
		t.Fatalf("skips %v stale %d", f.skips(alphaNew), f.stale())
	}
	// An unwrapped step error is unclassified too.
	f2 := newFixture(t)
	f2.add("o1", "alpha@v1", alphaOld)
	f2.svc.result = func(string, service.ReanalyzeOptions) error {
		return fmt.Errorf("reanalyze: %w", &pipeline.StepError{Step: "tagger", Err: errors.New("bad")})
	}
	if res, err := f2.run(); err != nil || res.Transient != 1 || len(f2.skips(alphaNew)) != 0 {
		t.Fatalf("step error: %+v, %v", res, err)
	}
}

func attemptRows(t *testing.T, f *fixture, id string) int {
	t.Helper()
	var n int
	if err := sqliteDB(t, f.drv).QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM rederive_attempts WHERE object_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// An object's tries and strikes are forgotten when it succeeds and when it
// gets a durable skip.
func TestStrikes_CountersAreClearedOnSuccessAndOnSkip(t *testing.T) {
	f := newFixture(t)
	f.add("flaky", "alpha@v1", alphaOld)
	f.add("gone", "alpha@v1", alphaOld)
	fail := true
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		switch {
		case id == "gone":
			return nil
		case fail:
			return plainErr(id)
		}
		return nil
	}
	// gone succeeds, flaky fails: flaky gets a try and a strike.
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if attemptRows(t, f, "flaky") != 1 {
		t.Fatal("no counters recorded for the failing object")
	}
	fail = false
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if attemptRows(t, f, "flaky") != 0 || f.sigOf("flaky") != alphaNew {
		t.Fatalf("counters kept after a success: rows %d", attemptRows(t, f, "flaky"))
	}

	// A durable skip clears them too.
	f.add("refused", "alpha@v1", alphaOld)
	if err := f.store.RecordRederiveTry(t.Context(), "refused", alphaNew); err != nil {
		t.Fatal(err)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "refused" {
			return fmt.Errorf("x: %w", upgrade.ErrCannotReanalyze)
		}
		return nil
	}
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if f.skips(alphaNew)["refused"] != storage.SkipRefused || attemptRows(t, f, "refused") != 0 {
		t.Fatalf("skip %v, counter rows %d", f.skips(alphaNew), attemptRows(t, f, "refused"))
	}
}

// A success that did not fetch (another pipeline, stored content) proves
// nothing about the network.
func TestStrikes_AStoredContentSuccessDoesNotProveTheNetwork(t *testing.T) {
	f := newFixture(t)
	networkMode(f)
	betaOld, betaNew := "beta#0000000000000001", "beta#0000000000000002"
	if err := f.reg.Register("beta", &pipeline.Pipeline{PipelineName: "beta", Signature: betaNew, Entries: []string{"x@2"},
		BaselineSignature: betaOld, BaselineEntries: []string{"x@1"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpsertDerivationSignature(t.Context(), betaOld, "beta", []string{"x@1"}); err != nil {
		t.Fatal(err)
	}
	f.svc.plans["beta"] = localPlan("x")
	// Only the stored-content group succeeds: no fetch, no proof of a host.
	f.svc.fetchedFor = func(string) []string { return nil }
	f.add("dead", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "dead" {
			return fetchErr("dial tcp: i/o timeout")
		}
		return nil
	}
	for run := 1; run <= 6; run++ {
		f.add(fmt.Sprintf("b%d", run), "beta@v1", betaOld)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.skips(alphaNew)) != 0 {
		t.Fatalf("skips = %v", f.skips(alphaNew))
	}
}

// The error that stops a run names what failed and the last cause.
func TestOutage_ErrorNamesTheKindAndTheCause(t *testing.T) {
	f := newFixture(t)
	f.useLLM()
	addN(f, 6)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error { return llmFailure(id) }
	res, err := f.run()
	if err != nil || !stoppedKind(res, "llm") || !strings.Contains(res.Stopped[0].Cause, "connection refused") {
		t.Fatalf("res.Stopped = %+v, err %v", res.Stopped, err)
	}
	f2 := newFixture(t)
	addN(f2, 6)
	f2.svc.result = func(string, service.ReanalyzeOptions) error { return fetchErr("unexpected status 503") }
	if res2, err := f2.run(); err != nil || !stoppedKind(res2, "fetch") || !strings.Contains(res2.Stopped[0].Cause, "503") {
		t.Fatalf("fetch stopped = %+v, err %v", res2.Stopped, err)
	}
}
