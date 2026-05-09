package jit_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// fakeFetcher records every URL it sees and returns canned body/err.
type fakeFetcher struct {
	mu    sync.Mutex
	seen  []string
	body  string
	err   error
	errFn func(urlStr string) error // optional per-URL error override
}

func (f *fakeFetcher) Fetch(_ context.Context, urlStr string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, urlStr)
	if f.errFn != nil {
		if err := f.errFn(urlStr); err != nil {
			return "", err
		}
	}
	if f.err != nil {
		return "", f.err
	}
	return f.body, nil
}

func (f *fakeFetcher) seenURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seen))
	copy(out, f.seen)
	return out
}

func TestExecutor_EmptyInput(t *testing.T) {
	e := jit.NewExecutor(&fakeFetcher{})
	got := e.Execute(context.Background(), "https://acme.io/post/1", nil)
	if got != nil {
		t.Fatalf("Execute(nil) = %v, want nil", got)
	}
}

func TestExecutor_RelativePathsResolveAgainstSource(t *testing.T) {
	f := &fakeFetcher{body: "ok"}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "https://acme.io/post/1", []string{
		"/about",
		"team",
		"../sibling",
	})
	if len(got) != 3 {
		t.Fatalf("Execute returned %d, want 3", len(got))
	}

	wantURLs := []string{
		"https://acme.io/about",
		"https://acme.io/post/team",
		"https://acme.io/sibling",
	}
	for i, want := range wantURLs {
		if got[i].URL != want {
			t.Errorf("got[%d].URL = %q, want %q", i, got[i].URL, want)
		}
		if got[i].Err != nil {
			t.Errorf("got[%d].Err = %v, want nil", i, got[i].Err)
		}
		if got[i].Body != "ok" {
			t.Errorf("got[%d].Body = %q, want %q", i, got[i].Body, "ok")
		}
	}
}

func TestExecutor_SameHostAbsoluteKept(t *testing.T) {
	f := &fakeFetcher{body: "x"}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "https://acme.io/post/1", []string{
		"https://acme.io/team",
	})
	if len(got) != 1 || got[0].URL != "https://acme.io/team" {
		t.Fatalf("Execute = %+v, want one result for https://acme.io/team", got)
	}
}

func TestExecutor_CrossDomainAbsoluteDropped(t *testing.T) {
	f := &fakeFetcher{body: "x"}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "https://acme.io/post/1", []string{
		"https://evil.example/x",
		"http://other.io/y",
		"//cdn.example/z", // protocol-relative cross-domain
		"/keep",           // same-domain relative — must survive
	})
	if len(got) != 1 {
		t.Fatalf("Execute returned %d results, want 1 (only /keep)", len(got))
	}
	if got[0].URL != "https://acme.io/keep" {
		t.Fatalf("got[0].URL = %q, want %q", got[0].URL, "https://acme.io/keep")
	}
	for _, seen := range f.seenURLs() {
		if seen != "https://acme.io/keep" {
			t.Errorf("fetcher saw cross-domain URL %q (must be filtered before fetch)", seen)
		}
	}
}

func TestExecutor_PerPathErrorSurfacedWithoutAborting(t *testing.T) {
	wantErr := errors.New("network down")
	f := &fakeFetcher{
		errFn: func(urlStr string) error {
			if urlStr == "https://acme.io/bad" {
				return wantErr
			}
			return nil
		},
		body: "ok",
	}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "https://acme.io/", []string{
		"/good", "/bad", "/also-good",
	})
	if len(got) != 3 {
		t.Fatalf("Execute returned %d, want 3 (failure must not abort batch)", len(got))
	}
	if got[0].Err != nil || got[2].Err != nil {
		t.Errorf("non-failing paths reported Err: %v, %v", got[0].Err, got[2].Err)
	}
	if !errors.Is(got[1].Err, wantErr) {
		t.Errorf("got[1].Err = %v, want %v", got[1].Err, wantErr)
	}
	if got[1].Body != "" {
		t.Errorf("got[1].Body = %q on error, want empty", got[1].Body)
	}
}

func TestExecutor_ContextCancellationStopsRemaining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeFetcher{
		errFn: func(urlStr string) error {
			// Cancel during the first fetch — the executor must skip remaining.
			cancel()
			return nil
		},
		body: "ok",
	}
	e := jit.NewExecutor(f)

	got := e.Execute(ctx, "https://acme.io/", []string{"/a", "/b", "/c"})
	if len(got) >= 3 {
		t.Fatalf("Execute returned %d results, want < 3 (ctx cancellation must stop)", len(got))
	}
	if len(got) < 1 {
		t.Fatalf("Execute returned 0 results, want at least 1 (the in-flight fetch should be recorded)")
	}
}

func TestExecutor_UnparseableSourceURL(t *testing.T) {
	f := &fakeFetcher{}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "not a url", []string{"/about"})
	if got != nil {
		t.Fatalf("Execute(unparseable source) = %v, want nil", got)
	}
	if len(f.seenURLs()) != 0 {
		t.Fatalf("fetcher invoked %v despite unparseable source", f.seenURLs())
	}
}

func TestExecutor_BlanksAndUnparseablePathsDropped(t *testing.T) {
	f := &fakeFetcher{body: "ok"}
	e := jit.NewExecutor(f)

	got := e.Execute(context.Background(), "https://acme.io/", []string{
		"",
		"   ",
		"://broken",
		"/good",
	})
	if len(got) != 1 || got[0].URL != "https://acme.io/good" {
		t.Fatalf("Execute = %+v, want one result for /good only", got)
	}
}
