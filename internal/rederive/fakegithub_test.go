package rederive

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const repoJSON = `{"name":"Hello-World","full_name":"octocat/Hello-World","owner":{"login":"octocat"},
"html_url":"https://github.com/octocat/Hello-World","description":"","language":"Go","topics":[],
"stargazers_count":80,"forks_count":9,"open_issues_count":2,"archived":false,"fork":false,"default_branch":"master",
"created_at":"2011-01-26T19:01:12Z","updated_at":"2024-05-01T10:00:00Z","pushed_at":"2024-04-30T09:00:00Z"}`

// fakeGitHub is a GitHub REST API with one repository whose README and
// answer the test sets. It counts its requests.
type fakeGitHub struct {
	srv    *httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	readme string
	status int // answers every request with it when not 0
	header http.Header
}

func (g *fakeGitHub) set(readme string) {
	g.mu.Lock()
	g.readme, g.status, g.header = readme, 0, nil
	g.mu.Unlock()
}

func (g *fakeGitHub) fail(status int, header http.Header) {
	g.mu.Lock()
	g.status, g.header = status, header
	g.mu.Unlock()
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		g.mu.Lock()
		readme, status, header := g.readme, g.status, g.header
		g.mu.Unlock()
		if status != 0 {
			for k, v := range header {
				w.Header()[k] = v
			}
			http.Error(w, `{"message":"no"}`, status)
			return
		}
		switch strings.ToLower(r.URL.Path) {
		case "/repos/octocat/hello-world":
			_, _ = io.WriteString(w, repoJSON)
		case "/repos/octocat/hello-world/readme":
			_, _ = io.WriteString(w, readme)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}
