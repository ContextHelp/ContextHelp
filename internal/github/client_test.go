package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newTestClient returns a client on srv whose sleeps are recorded, not
// taken, and whose clock is fixed.
func newTestClient(srv *httptest.Server, slept *[]time.Duration, opts ...Option) *Client {
	now := time.Unix(1_700_000_000, 0)
	base := []Option{
		WithBaseURL(srv.URL),
		withClock(func() time.Time { return now }),
		withSleep(func(_ context.Context, d time.Duration) error { *slept = append(*slept, d); return nil }),
	}
	return NewClient(append(base, opts...)...)
}

func TestFetchRepo_MapsAPIMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/Hello-World" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()
	var slept []time.Duration
	snap, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "octocat", "Hello-World")
	if err != nil {
		t.Fatal(err)
	}
	if snap.FullName != "octocat/Hello-World" || snap.URL != "https://github.com/octocat/Hello-World" ||
		snap.Description != "My first repository on GitHub!" || snap.Language != "Go" ||
		snap.Stars != 80 || snap.Forks != 9 || snap.OpenIssues != 2 || snap.License != "MIT" ||
		snap.DefaultBranch != "master" || snap.IsArchived || len(snap.Topics) != 3 ||
		snap.PushedAt.Year() != 2024 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestFetchRepo_READMEAndReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r":
			w.Write(fixture(t, "repo.json"))
		case "/repos/o/r/readme":
			if got := r.Header.Get("Accept"); got != "application/vnd.github.raw+json" {
				t.Errorf("readme Accept = %q", got)
			}
			fmt.Fprint(w, "# héllo wörld")
		case "/repos/o/r/releases":
			if r.URL.Query().Get("per_page") != "2" {
				t.Errorf("per_page = %q", r.URL.RawQuery)
			}
			fmt.Fprint(w, `[{"tag_name":"v2","name":"Two","body":"b","published_at":"2024-01-02T00:00:00Z"},{"tag_name":"v1"}]`)
		}
	}))
	defer srv.Close()
	var slept []time.Duration
	snap, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "o", "r", WithREADME(4), WithReleases(2))
	if err != nil {
		t.Fatal(err)
	}
	if snap.README != "# h" { // 4 bytes would split the é: cut back to the rune boundary
		t.Errorf("README = %q", snap.README)
	}
	if len(snap.Releases) != 2 || snap.Releases[0].Tag != "v2" {
		t.Errorf("releases = %+v", snap.Releases)
	}
}

func TestFetchRepo_MissingREADMEIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r" {
			w.Write(fixture(t, "repo.json"))
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	var slept []time.Duration
	snap, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "o", "r", WithREADME(100))
	if err != nil || snap.README != "" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}

func TestFetchIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/7" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"number":7,"title":"T","body":"B","state":"closed","html_url":"https://github.com/o/r/issues/7",
		"labels":[{"name":"bug"}],"user":{"login":"a"},"assignees":[{"login":"b"}],
		"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","closed_at":"2024-01-03T00:00:00Z"}`)
	}))
	defer srv.Close()
	var slept []time.Duration
	i, err := newTestClient(srv, &slept).FetchIssue(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if i.Title != "T" || i.State != "closed" || i.Author != "a" || len(i.Labels) != 1 || i.Assignees[0] != "b" || i.ClosedAt == nil {
		t.Errorf("issue = %+v", i)
	}
}

func TestTypedErrors(t *testing.T) {
	cases := []struct {
		status    int
		headers   map[string]string
		body      string
		want      error
		permanent bool
	}{
		{404, nil, `{"message":"Not Found"}`, ErrNotFound, true},
		{401, nil, `{"message":"Bad credentials"}`, ErrUnauthorized, true},
		{403, nil, `{"message":"Repository access blocked"}`, ErrForbidden, true},
		{500, nil, `{"message":"boom"}`, ErrServer, false},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			var slept []time.Duration
			_, err := newTestClient(srv, &slept, WithMaxRetries(1)).FetchRepo(context.Background(), "o", "r")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var ae *APIError
			if !errors.As(err, &ae) || ae.Status != tc.status {
				t.Errorf("not an *APIError with status %d: %v", tc.status, err)
			}
			if IsPermanent(err) != tc.permanent {
				t.Errorf("IsPermanent = %v, want %v", IsPermanent(err), tc.permanent)
			}
		})
	}
}

func TestServerErrorRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()
	var slept []time.Duration
	if _, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "o", "r"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(slept) != 2 || slept[1] != 2*slept[0] {
		t.Errorf("calls=%d slept=%v, want 3 calls with doubling backoff", calls, slept)
	}
}

func TestPrimaryRateLimit_WaitsForResetThenRetries(t *testing.T) {
	reset := time.Unix(1_700_000_000, 0).Add(10 * time.Second)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
			return
		}
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()
	var slept []time.Duration
	if _, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "o", "r"); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 11*time.Second {
		t.Errorf("slept = %v, want [11s] (reset + 1s skew margin)", slept)
	}
}

func TestSecondaryRateLimit_HonoursRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"message":"You have exceeded a secondary rate limit"}`)
			return
		}
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()
	var slept []time.Duration
	if _, err := newTestClient(srv, &slept).FetchRepo(context.Background(), "o", "r"); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 7*time.Second {
		t.Errorf("slept = %v, want [7s]", slept)
	}
}

func TestSecondaryRateLimit_ByMessageWithoutHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes"}`)
	}))
	defer srv.Close()
	var slept []time.Duration
	_, err := newTestClient(srv, &slept, WithMaxWait(time.Second)).FetchRepo(context.Background(), "o", "r")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.Kind != LimitSecondary || rl.RetryAfter != time.Minute {
		t.Fatalf("err = %#v", err)
	}
	if !errors.Is(err, ErrRateLimited) || IsPermanent(err) {
		t.Errorf("rate limit must be ErrRateLimited and transient: %v", err)
	}
}

func TestRateLimit_BeyondMaxWaitReturnsTypedError(t *testing.T) {
	reset := time.Unix(1_700_000_000, 0).Add(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	var slept []time.Duration
	_, err := newTestClient(srv, &slept, WithMaxWait(30*time.Second)).FetchRepo(context.Background(), "o", "r")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.Kind != LimitPrimary || !rl.Reset.Equal(reset) {
		t.Fatalf("err = %#v", err)
	}
	if len(slept) != 0 {
		t.Errorf("slept %v; a wait beyond MaxWait must not sleep", slept)
	}
}

func TestRateLimit_RetriesExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	var slept []time.Duration
	_, err := newTestClient(srv, &slept, WithMaxRetries(2)).FetchRepo(context.Background(), "o", "r")
	if !errors.Is(err, ErrRateLimited) || calls != 3 {
		t.Errorf("err=%v calls=%d, want ErrRateLimited after 3 calls", err, calls)
	}
}

func TestETag_ConditionalRequestServesCachedBody(t *testing.T) {
	var calls, conditional int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("If-None-Match") == `"abc"` {
			atomic.AddInt32(&conditional, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()
	var slept []time.Duration
	c := newTestClient(srv, &slept)
	for i := 0; i < 2; i++ {
		snap, err := c.FetchRepo(context.Background(), "o", "r")
		if err != nil || snap.Stars != 80 {
			t.Fatalf("call %d: snap=%+v err=%v", i, snap, err)
		}
	}
	if calls != 2 || conditional != 1 {
		t.Errorf("calls=%d conditional=%d, want 2 calls, the second conditional", calls, conditional)
	}
}

func TestAuth_TokenPerOwnerAndAnonymousFallback(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+" auth="+r.Header.Get("Authorization"))
		w.Write(fixture(t, "repo.json"))
	}))
	defer srv.Close()

	secrets := map[string]string{"GITHUB_TOKEN": "tok-default", "GH_WORK": " tok-work\n"} // GH_MISSING absent
	cfg := config.GitHubConfig{
		Credential: "GITHUB_TOKEN",
		Accounts: []config.GitHubAccount{
			{Name: "work", Credential: "GH_WORK", Owners: []string{"Acme"}},
			{Name: "ghost", Credential: "GH_MISSING", Owners: []string{"ghost"}},
		},
	}
	get := func(_ context.Context, name string) (string, error) { return secrets[name], nil }
	c := NewClientFromConfig(cfg, get, WithBaseURL(srv.URL))
	for _, owner := range []string{"acme", "someone", "ghost"} {
		if _, err := c.FetchRepo(context.Background(), owner, "r"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"/repos/acme/r auth=Bearer tok-work",
		"/repos/someone/r auth=Bearer tok-default",
		"/repos/ghost/r auth=", // account secret unset: anonymous, no header
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAuth_SecretErrorFailsTheFetch(t *testing.T) {
	boom := errors.New("keyring locked")
	cfg := config.GitHubConfig{Credential: "GITHUB_TOKEN"}
	c := NewClientFromConfig(cfg, func(context.Context, string) (string, error) { return "", boom })
	if _, err := c.FetchRepo(context.Background(), "o", "r"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrapped %v", err, boom)
	}
}

func TestContextCancelStopsRateLimitWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	c := NewClient(WithBaseURL(srv.URL), withSleep(func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}))
	if _, err := c.FetchRepo(ctx, "o", "r"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
