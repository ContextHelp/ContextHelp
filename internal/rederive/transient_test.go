package rederive

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fetchErr is a fetch from src.test that failed without an answer (a
// timeout, no route); fetchStatus one the source answered with a status.
func fetchErr(text string) error { return fetchStatus("src.test", 0, text) }

func fetchStatus(host string, status int, text string) error {
	return fmt.Errorf("reanalyze: %w", &pipeline.StepError{Step: "url_fetcher",
		Err: &pipeline.FetchError{Host: host, Status: status, Err: errors.New(text)}})
}

// A failure that says nothing about the object (a rate limit, a timeout, a
// server error) is not recorded: the object stays stale for the next start.
func TestRun_TransientFetchFailureIsNotRecorded(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"o1", "o2", "o3"} {
		f.add(id, "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "o1" {
			return fetchErr("unexpected status 429")
		}
		return nil
	}
	res, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 2 || res.Transient != 1 || res.SkippedTotal() != 0 || res.Done() != 3 {
		t.Fatalf("result = %+v", res)
	}
	if got := f.skips(alphaNew); len(got) != 0 {
		t.Fatalf("a transient failure wrote %v", got)
	}
	if f.sigOf("o1") != alphaOld || f.stale() != 1 {
		t.Fatalf("o1 must stay stale: sig %q, stale %d", f.sigOf("o1"), f.stale())
	}
	f.svc.result = nil // the rate limit is over
	res, err = f.run()
	if err != nil || res.Processed != 1 || f.sigOf("o1") != alphaNew {
		t.Fatalf("next run: %+v, %v", res, err)
	}
}

// failureStreak transient failures in a row stop the group; the run
// completes, with nothing recorded, so an outage or a rate limit does
// not walk the whole store.
func TestRun_TransientOutageStopsTheRun(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 8; i++ {
		f.add(fmt.Sprintf("o%d", i), "alpha@v1", alphaOld)
	}
	f.svc.result = func(string, service.ReanalyzeOptions) error { return fetchErr("dial tcp: i/o timeout") }
	prog := &fakeProgress{}
	f.job.Progress = prog
	res, err := f.run()
	if err != nil || !stoppedKind(res, "fetch") || res.Interrupted || res.Transient != failureStreak {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if n := len(f.svc.called()); n != failureStreak {
		t.Errorf("tried %d objects, want %d", n, failureStreak)
	}
	if got := f.skips(alphaNew); len(got) != 0 || f.stale() != 8 || prog.failed != nil || prog.done == nil {
		t.Errorf("skips %v stale %d status failed %v (the status completes: a stopped group is not a failed run)", got, f.stale(), prog.failed)
	}
	if _, herr := f.job.Handle(t.Context(), &storage.Job{ID: "j"}); herr != nil {
		t.Errorf("Handle = %v for an outage; the pool must not retry now", herr)
	}
	f.svc.result = nil
	if res, err = f.run(); err != nil || res.Processed != 8 {
		t.Fatalf("resume: %+v, %v", res, err)
	}
}

// Transient failures that a success separates are not an outage.
func TestRun_ASuccessEndsTheTransientStreak(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 9; i++ {
		f.add(fmt.Sprintf("o%d", i), "alpha@v1", alphaOld)
	}
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "o4" {
			return nil
		}
		return fetchErr("unexpected status 503")
	}
	res, err := f.run()
	if err != nil || res.Transient != 8 || res.Processed != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
}
