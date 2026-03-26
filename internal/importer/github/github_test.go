package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// makeRepos builds a slice of fake apiRepo JSON objects.
func makeRepos(names ...string) []apiRepo {
	repos := make([]apiRepo, len(names))
	for i, n := range names {
		repos[i] = apiRepo{
			FullName:  n,
			HTMLURL:   "https://github.com/" + n,
			Language:  "Go",
			StarCount: i + 1,
		}
	}
	return repos
}

func serveJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TestStarredPagination tests that the importer follows Link-header pagination
// for the starred list endpoint.
func TestStarredPagination(t *testing.T) {
	page1 := makeRepos("user/repo-a", "user/repo-b")
	page2 := makeRepos("user/repo-c")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/testuser/starred":
			if r.URL.Query().Get("page") == "2" {
				serveJSON(w, page2)
				return
			}
			// First page: set Link header pointing to page 2.
			w.Header().Set("Link", fmt.Sprintf(`<%s/users/testuser/starred?per_page=100&page=2>; rel="next"`, "http://"+r.Host))
			serveJSON(w, page1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	imp, err := New(Config{
		Token:    "fake-token",
		Username: "testuser",
		Lists:    []string{ListStarred},
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	repos, err := imp.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}
	for _, r := range repos {
		if r.Source != ListStarred {
			t.Errorf("expected source=%q, got %q", ListStarred, r.Source)
		}
	}
}

// TestDeduplication ensures repos appearing in multiple lists are returned once.
func TestDeduplication(t *testing.T) {
	shared := makeRepos("user/shared-repo")
	starredOnly := makeRepos("user/only-starred")
	watchedOnly := makeRepos("user/only-watched")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/testuser/starred":
			combined := append(shared, starredOnly...)
			serveJSON(w, combined)
		case "/users/testuser/subscriptions":
			combined := append(shared, watchedOnly...)
			serveJSON(w, combined)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	imp, err := New(Config{
		Token:    "fake-token",
		Username: "testuser",
		Lists:    []string{ListStarred, ListWatched},
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	repos, err := imp.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	seen := make(map[string]int)
	for _, r := range repos {
		seen[r.FullName]++
	}

	if seen["user/shared-repo"] != 1 {
		t.Errorf("shared-repo should appear once, got %d", seen["user/shared-repo"])
	}
	if seen["user/only-starred"] != 1 {
		t.Errorf("only-starred should appear once, got %d", seen["user/only-starred"])
	}
	if seen["user/only-watched"] != 1 {
		t.Errorf("only-watched should appear once, got %d", seen["user/only-watched"])
	}
	if len(repos) != 3 {
		t.Errorf("expected 3 total repos, got %d", len(repos))
	}
}

// TestMissingTokenReturnsError checks that New returns an error with no token.
func TestMissingTokenReturnsError(t *testing.T) {
	// Ensure env var is not set.
	orig := os.Getenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_TOKEN")
	defer func() { _ = os.Setenv("GITHUB_TOKEN", orig) }()

	_, err := New(Config{
		Username: "testuser",
	})
	if err == nil {
		t.Fatal("expected error when token is missing, got nil")
	}
}

// TestTokenFallbackFromEnv checks that GITHUB_TOKEN env is used when cfg.Token is empty.
func TestTokenFallbackFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		serveJSON(w, []apiRepo{})
	}))
	defer srv.Close()

	_ = os.Setenv("GITHUB_TOKEN", "env-token")
	defer os.Unsetenv("GITHUB_TOKEN")

	imp, err := New(Config{
		Username: "testuser",
		Lists:    []string{ListStarred},
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	repos, err := imp.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

// TestContributedSearch tests the contributed list via the search endpoint.
func TestContributedSearch(t *testing.T) {
	items := makeRepos("org/project-x", "org/project-y")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/repositories" {
			http.NotFound(w, r)
			return
		}
		serveJSON(w, apiSearchResult{Items: items})
	}))
	defer srv.Close()

	imp, err := New(Config{
		Token:    "fake-token",
		Username: "testuser",
		Lists:    []string{ListContributed},
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	repos, err := imp.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	for _, r := range repos {
		if r.Source != ListContributed {
			t.Errorf("expected source=%q, got %q", ListContributed, r.Source)
		}
	}
}

// TestParseLinkNext verifies Link-header parsing.
func TestParseLinkNext(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{
			header: `<https://api.github.com/repos?page=2>; rel="next", <https://api.github.com/repos?page=5>; rel="last"`,
			want:   "https://api.github.com/repos?page=2",
		},
		{
			header: `<https://api.github.com/repos?page=5>; rel="last"`,
			want:   "",
		},
		{
			header: "",
			want:   "",
		},
	}
	for _, tc := range cases {
		got := parseLinkNext(tc.header)
		if got != tc.want {
			t.Errorf("parseLinkNext(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}
