package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/repl"
)

// findRecorder is a proxy in front of a test dpkms that keeps every
// POST /api/v1/find body it forwards.
type findRecorder struct {
	mu     sync.Mutex
	bodies []map[string]any
}

// last returns the last recorded find body.
func (r *findRecorder) last(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		t.Fatal("no POST /api/v1/find reached dpkms")
	}
	return r.bodies[len(r.bodies)-1]
}

func (r *findRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

// recordFind routes db's client config through a recording proxy to
// db.Server, authenticating as role.
func recordFind(t *testing.T, db *testDB, role string) *findRecorder {
	t.Helper()
	target, err := url.Parse(db.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := &findRecorder{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/find" {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read find body: %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("find body is not JSON: %v\n%s", err, raw)
			}
			rec.mu.Lock()
			rec.bodies = append(rec.bodies, body)
			rec.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	body := "server:\n  url: " + srv.URL + "\n"
	if tok := db.Server.Token(role); tok != "" {
		body += "  token: " + tok + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(db.ClientConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db.ClientConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return rec
}

// execFindAs is execFind run under --profile profile.
func execFindAs(t *testing.T, db *testDB, profile string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return execFind(t, db, append([]string{"--profile", profile}, args...)...)
}

// writeFindProfileConfig writes a ctxt config naming the local store and
// a growth profile whose search strategy sets every knob.
func writeFindProfileConfig(t *testing.T, db *testDB, extra string) {
	t.Helper()
	raw, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw) + `profile:
  profiles:
    growth:
      search_strategy:
        mode: fts
        rrf:
          k: 7
          fts_weight: 0.3
          vector_weight: 0.9
        candidate_pool:
          fts: 11
          vector: 13
        min_score: 0.25
` + extra
	if err := os.WriteFile(db.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jsonNumber(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v, want a number", key, m[key])
	}
	return v
}

// The active profile, its search strategy and find's own flags reach
// POST /api/v1/find as resolved values (ADR-077 §6): the profile scopes
// the search, its knobs fill the search block, and a flag outranks the
// profile.
func TestFindSendsResolvedRequest(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	writeFindProfileConfig(t, db, "search:\n  reranker:\n    hop_backlink_boost: 0.07\n")
	rec := recordFind(t, db, dpkmstest.RoleReader)

	if _, stderr, err := execFindAs(t, db, "growth", "find", "deploy", "--limit", "4",
		"--meta-type", "task", "--topic", "k8s", "--person", "alice", "--source-type", "slack",
		"--since", "2026-01-02", "--until", "2026-02-03", "--format", "json"); err != nil {
		t.Fatalf("find: %v\n%s", err, stderr)
	}
	body := rec.last(t)
	if body["query"] != "deploy" || body["mode"] != "fts" || body["profile"] != "growth" {
		t.Errorf("query, mode, profile = %v, %v, %v; want deploy, fts (the profile's mode), growth", body["query"], body["mode"], body["profile"])
	}
	if got := jsonNumber(t, body, "limit"); got != 4 {
		t.Errorf("limit = %v, want 4", got)
	}
	wantFilter := map[string]any{
		"meta_type": "task", "topic": "k8s", "person": "alice", "source_type": "slack",
		"since": "2026-01-02", "until": "2026-02-03",
	}
	filter, _ := body["filter"].(map[string]any)
	for k, v := range wantFilter {
		if filter[k] != v {
			t.Errorf("filter.%s = %v, want %v", k, filter[k], v)
		}
	}
	search, _ := body["search"].(map[string]any)
	for k, want := range map[string]float64{
		"rrf_k": 7, "fts_weight": 0.3, "vector_weight": 0.9, "fts_pool": 11, "vector_pool": 13,
		"min_score": 0.25, "hop_backlink_boost": 0.07,
		// Global defaults travel too: dpkms applies what arrives.
		"mention_boost_per_mention": 0.05, "max_mention_boost": 1, "direct_backlink_boost": 0.08,
	} {
		if got, ok := search[k].(float64); !ok || got != want {
			t.Errorf("search.%s = %#v, want %v", k, search[k], want)
		}
	}
	if search["fallback_to_fts"] != true {
		t.Errorf("search.fallback_to_fts = %#v, want true", search["fallback_to_fts"])
	}
	for _, k := range []string{"explain", "facets"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s sent without its flag: %v", k, body)
		}
	}

	// Flags outrank the profile, mode flags included.
	for _, tc := range []struct {
		flag, mode string
	}{{"--semantic", "vector"}, {"--hybrid", "hybrid"}, {"--fts", "fts"}} {
		if _, stderr, err := execFindAs(t, db, "growth", "find", "deploy", tc.flag, "--rrf-k", "9",
			"--fts-weight", "0.6", "--vector-weight", "0.4", "--fts-pool", "21", "--vector-pool", "23",
			"--min-score", "0", "--format", "json"); err != nil {
			t.Fatalf("find %s: %v\n%s", tc.flag, err, stderr)
		}
		body = rec.last(t)
		if body["mode"] != tc.mode {
			t.Errorf("%s: mode = %v, want %s", tc.flag, body["mode"], tc.mode)
		}
		search, _ = body["search"].(map[string]any)
		for k, want := range map[string]float64{"rrf_k": 9, "fts_weight": 0.6, "vector_weight": 0.4, "fts_pool": 21, "vector_pool": 23, "min_score": 0} {
			if got, ok := search[k].(float64); !ok || got != want {
				t.Errorf("%s: search.%s = %#v, want %v", tc.flag, k, search[k], want)
			}
		}
	}

	// --explain and --facets travel; --explain wins over --facets in
	// hybrid mode, as it always has.
	if _, stderr, err := execFindAs(t, db, "growth", "find", "deploy", "--hybrid", "--explain", "--facets", "--format", "json"); err != nil {
		t.Fatalf("find --explain: %v\n%s", err, stderr)
	}
	if body = rec.last(t); body["explain"] != true || body["facets"] != nil {
		t.Errorf("--explain --facets: explain, facets = %v, %v; want true, absent", body["explain"], body["facets"])
	}
	if _, stderr, err := execFindAs(t, db, "growth", "find", "deploy", "--facets", "--format", "json"); err != nil {
		t.Fatalf("find --facets: %v\n%s", err, stderr)
	}
	if body = rec.last(t); body["facets"] != true || body["explain"] != nil {
		t.Errorf("--facets: explain, facets = %v, %v; want absent, true", body["explain"], body["facets"])
	}
}

// With no active profile the request carries none, so dpkms does not
// scope the search, and the mode is the global default.
func TestFindWithoutProfileSendsNone(t *testing.T) {
	db := setupTestDB(t)
	rec := recordFind(t, db, dpkmstest.RoleAdmin)
	if _, stderr, err := execFind(t, db, "find", "deploy", "--format", "json"); err != nil {
		t.Fatalf("find: %v\n%s", err, stderr)
	}
	body := rec.last(t)
	if _, ok := body["profile"]; ok {
		t.Errorf("profile sent with none active: %v", body["profile"])
	}
	if body["mode"] != "hybrid" {
		t.Errorf("mode = %v, want the global default hybrid", body["mode"])
	}
}

// The active profile scopes results end to end: dpkms answers only
// objects the profile owns.
func TestFindProfileScopesResults(t *testing.T) {
	db := setupFindAPI(t)
	// note-fts and note-task both hold these words; only note-task is ops's.
	stdout, stderr, err := execFindAs(t, db, "ops", "find", "rotating signing keys", "--fts", "--format", "json")
	if err != nil {
		t.Fatalf("find: %v\n%s", err, stderr)
	}
	var out struct {
		Objects []struct {
			ID string `json:"id"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if len(out.Objects) != 1 || out.Objects[0].ID != "note-task" {
		t.Errorf("objects = %+v, want only ops's note-task", out.Objects)
	}
}

// find reads nothing from the local store: with the config's store
// pointing at an empty file it still answers from dpkms.
func TestFindNeverOpensLocalStore(t *testing.T) {
	db := setupFindAPI(t)
	path := detachLocalStore(t, db)
	stdout, stderr, err := execFind(t, db, "find", apiFindQuery, "--fts", "--format", "json")
	if err != nil {
		t.Fatalf("find: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "note-fts") {
		t.Errorf("find did not answer from dpkms:\n%s", stdout)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("find created or opened the local store %s (stat err %v)", path, err)
	}
}

// The --embedding-* flags only configure --graph's local search: the
// query of every other find is embedded on the dpkms host, so they are a
// usage error there, before any request.
func TestFindEmbeddingFlagsNeedGraph(t *testing.T) {
	db := setupTestDB(t)
	rec := recordFind(t, db, dpkmstest.RoleAdmin)
	for _, flag := range [][]string{
		{"--embedding-provider", "stub"},
		{"--embedding-model", "nomic-embed-text"},
		{"--embedding-endpoint", "http://127.0.0.1:11555"},
	} {
		_, _, err := execFind(t, db, append([]string{"find", "deploy"}, flag...)...)
		var env *output.Error
		if !errors.As(err, &env) || env.Code != output.CodeUsage {
			t.Fatalf("find %v: err = %v, want a USAGE error", flag, err)
		}
		if !strings.Contains(env.Message, flag[0]) || !strings.Contains(env.Message, "--graph") {
			t.Errorf("find %v: message %q should name the flag and --graph", flag, env.Message)
		}
	}
	if n := rec.count(); n != 0 {
		t.Errorf("%d find requests sent despite the usage error", n)
	}
}

// ctxt://search/<q> runs find over POST /api/v1/find with the active
// profile, like find itself.
func TestURISearchOverFind(t *testing.T) {
	db := setupTestDB(t)
	writeFindProfileConfig(t, db, "")
	rec := recordFind(t, db, dpkmstest.RoleAdmin)
	if _, stderr, err := execFindAs(t, db, "growth", "ctxt://search/deploy"); err != nil {
		t.Fatalf("ctxt://search: %v\n%s", err, stderr)
	}
	body := rec.last(t)
	if body["query"] != "deploy" || body["profile"] != "growth" || body["mode"] != "fts" {
		t.Errorf("query, profile, mode = %v, %v, %v; want deploy, growth, fts", body["query"], body["profile"], body["mode"])
	}
}

// A REPL line that falls through to find runs the same request: the
// session sends no vector state and the query is embedded on the dpkms
// host.
func TestREPLFindOverFind(t *testing.T) {
	db := setupTestDB(t)
	rec := recordFind(t, db, dpkmstest.RoleAdmin)
	state := &repl.SessionState{}
	ctx := context.WithValue(context.Background(), repl.SessionContextKey{}, state)
	exec := func(args []string) error {
		_, stderr, err := execFind(t, db, args...)
		if err != nil {
			t.Logf("stderr: %s", stderr)
		}
		return err
	}
	rootCmd.SetContext(ctx)
	t.Cleanup(func() { rootCmd.SetContext(context.Background()) })
	for _, line := range []string{"deploy notes", "deploy runbook"} {
		if err := repl.Dispatch(ctx, line, state, exec); err != nil {
			t.Fatalf("REPL %q: %v", line, err)
		}
		if got := rec.last(t)["query"]; got != line {
			t.Errorf("REPL %q sent query %v", line, got)
		}
	}
	if n := rec.count(); n != 2 {
		t.Errorf("%d find requests, want 2", n)
	}
}
