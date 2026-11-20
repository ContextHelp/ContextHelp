package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestGraphQL_PostsQueryWithAccountToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-stars" {
			t.Errorf("Authorization = %q", got)
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req.Query, "viewer") || req.Variables["n"] != float64(5) {
			t.Errorf("body = %s", b)
		}
		w.Write([]byte(`{"data":{"viewer":{"login":"octo"}}}`))
	}))
	defer srv.Close()
	cfg := config.GitHubConfig{Accounts: []config.GitHubAccount{{Name: "stars", Credential: "S", Owners: []string{"octo"}}}}
	get := func(_ context.Context, name string) (string, error) {
		return "tok-" + map[string]string{"S": "stars"}[name], nil
	}
	c := NewClientFromConfig(cfg, get, WithBaseURL(srv.URL))
	var out struct{ Viewer struct{ Login string } }
	if err := c.GraphQL(context.Background(), "octo", "query($n:Int){viewer{login}}", map[string]any{"n": 5}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Viewer.Login != "octo" {
		t.Errorf("login = %q", out.Viewer.Login)
	}
}

func TestGraphQL_ErrorsFailTheCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":null,"errors":[{"type":"INSUFFICIENT_SCOPES","message":"needs user scope"}]}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	err := newTestClient(srv, &slept).GraphQL(context.Background(), "", "{viewer{login}}", nil, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "needs user scope") {
		t.Fatalf("err = %v", err)
	}
}

func TestGraphQL_RateLimitWaitsForReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(10*time.Second).Unix(), 10))
			w.Write([]byte(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`))
			return
		}
		w.Write([]byte(`{"data":{"viewer":{"login":"octo"}}}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	var out struct{ Viewer struct{ Login string } }
	if err := newTestClient(srv, &slept).GraphQL(context.Background(), "", "{viewer{login}}", nil, &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(slept) != 1 || slept[0] != 11*time.Second || out.Viewer.Login != "octo" {
		t.Errorf("calls=%d slept=%v out=%+v", calls.Load(), slept, out)
	}
}

func TestGraphQL_RateLimitBeyondMaxWaitIsTyped(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(time.Hour).Unix(), 10))
		w.Write([]byte(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	err := newTestClient(srv, &slept).GraphQL(context.Background(), "", "{viewer{login}}", nil, &struct{}{})
	var rl *RateLimitError
	if !errors.As(err, &rl) || !errors.Is(err, ErrRateLimited) || len(slept) != 0 {
		t.Fatalf("err = %v slept=%v", err, slept)
	}
}

func TestGraphQLURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.github.com":          "https://api.github.com/graphql",
		"https://ghe.example.com/api/v3":  "https://ghe.example.com/api/graphql",
		"https://ghe.example.com/api/v3/": "https://ghe.example.com/api/graphql",
	} {
		if got := graphqlURL(strings.TrimRight(base, "/")); got != want {
			t.Errorf("graphqlURL(%q) = %q, want %q", base, got, want)
		}
	}
}
