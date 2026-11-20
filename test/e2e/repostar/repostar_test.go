//go:build unix

// Package repostar_test drives the repo-star recipe end to end through
// built binaries: a video capture (note, data, mentions, hints, source
// key) and GitHub repo captures reach a protected `dpkms serve` whose
// url.github.repo pipeline runs an installed classify step, and a star
// import files starred repos under hints and list vocabulary.
//
// Nothing leaves the host: GitHub REST and GraphQL and the video page are
// served by one loopback fixture server, and the classify step is a stub
// speaking the ADR-058 protocol with additive ops (testdata/classify).
package repostar_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/ideacrafterslabs/ctxt/test/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

// ---- binaries ----

var (
	buildOnce sync.Once
	binDir    string
	buildErr  error
)

func moduleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// binaries builds dpkms, ctxt and the classify stub once per test binary.
func binaries(t *testing.T) (dpkms, ctxt, classify string) {
	t.Helper()
	root := moduleRoot(t)
	buildOnce.Do(func() {
		binDir, buildErr = os.MkdirTemp(os.Getenv("XDG_CACHE_HOME"), "repostar-e2e-")
		if buildErr != nil {
			return
		}
		pkgs := map[string]string{
			"dpkms":    "./cmd/dpkms",
			"ctxt":     "./cmd/ctxt",
			"classify": "./test/e2e/repostar/testdata/classify",
		}
		for out, pkg := range pkgs {
			build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", filepath.Join(binDir, out), pkg) // #nosec G204 -- fixed args; fresh temp path
			build.Dir = root
			build.Env = append(os.Environ(), "CGO_ENABLED=1")
			if b, err := build.CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("go build %s: %w\n%s", pkg, err, b)
				return
			}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return filepath.Join(binDir, "dpkms"), filepath.Join(binDir, "ctxt"), filepath.Join(binDir, "classify")
}

// classifyDir is the stub step directory: SKILL.md plus the executable.
func classifyDir(t *testing.T, exe string) string {
	t.Helper()
	dir := t.TempDir()
	skill, err := os.ReadFile(filepath.Join(moduleRoot(t), "test", "e2e", "repostar", "testdata", "classify", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe) // #nosec G304 -- the binary this test built
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"SKILL.md": skill, "classify": b} {
		mode := os.FileMode(0o600)
		if name == "classify" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ---- fixture server: GitHub REST + GraphQL, a video page ----

type fixtureRepo struct {
	FullName    string
	Description string
	Language    string
	Topics      []string
	Stars       int
}

type fixture struct {
	*httptest.Server
	repos        map[string]fixtureRepo // lowercase full name
	videoHits    atomic.Int32
	graphqlToken string
	graphqlSeen  atomic.Int32
}

const (
	repoCandidate  = "octo-org/Agent-Runner"
	repoStarred    = "octo-org/starred-tool"
	repoListedOnly = "octo-org/listed-only"
	viewerLogin    = "star-owner"
	graphqlToken   = "fixture-token"
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{repos: map[string]fixtureRepo{}, graphqlToken: graphqlToken}
	for _, r := range []fixtureRepo{
		{repoCandidate, "Runtime for agent tooling", "Go", []string{"agents", "runtime"}, 120},
		{repoStarred, "A tool already starred", "Go", []string{"cli"}, 40},
		{repoListedOnly, "Filed in a list only", "Rust", []string{"databases"}, 7},
	} {
		f.repos[strings.ToLower(r.FullName)] = r
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{name}", f.repo)
	mux.HandleFunc("GET /repos/{owner}/{name}/readme", f.readme)
	mux.HandleFunc("POST /graphql", f.graphql)
	mux.HandleFunc("GET /watch", f.watch)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fixture) lookup(r *http.Request) (fixtureRepo, bool) {
	rr, ok := f.repos[strings.ToLower(r.PathValue("owner")+"/"+r.PathValue("name"))]
	return rr, ok
}

func (f *fixture) repo(w http.ResponseWriter, r *http.Request) {
	rr, ok := f.lookup(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	owner, name, _ := strings.Cut(rr.FullName, "/")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": name, "full_name": rr.FullName, "owner": map[string]any{"login": owner},
		"html_url":    "https://github.com/" + rr.FullName,
		"description": rr.Description, "language": rr.Language, "topics": rr.Topics,
		"stargazers_count": rr.Stars, "forks_count": 3, "open_issues_count": 1,
		"archived": false, "fork": false, "default_branch": "main",
		"created_at": "2024-01-02T03:04:05Z", "updated_at": "2026-10-01T00:00:00Z", "pushed_at": "2026-10-02T00:00:00Z",
		"license": map[string]any{"spdx_id": "MIT", "name": "MIT License"},
	})
}

func (f *fixture) readme(w http.ResponseWriter, r *http.Request) {
	rr, ok := f.lookup(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = fmt.Fprintf(w, "# %s\n\n%s. Install with the package manager of your choice.\n", rr.FullName, rr.Description)
}

func (f *fixture) graphql(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.graphqlToken {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	f.graphqlSeen.Add(1)
	var req struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	node := func(full string) map[string]any {
		return map[string]any{"nameWithOwner": full, "url": "https://github.com/" + full}
	}
	noMore := map[string]any{"hasNextPage": false, "endCursor": ""}
	var viewer map[string]any
	switch {
	case strings.Contains(req.Query, "starredRepositories"):
		viewer = map[string]any{"login": viewerLogin, "starredRepositories": map[string]any{
			"pageInfo": noMore, "nodes": []any{node(repoStarred)},
		}}
	case strings.Contains(req.Query, "lists("):
		viewer = map[string]any{"lists": map[string]any{
			"pageInfo": noMore,
			"nodes": []any{map[string]any{
				"id": "list-1", "name": "Agent tooling", "slug": "agent-tooling",
				"description": "CLIs and runtimes for agents", "isPrivate": false,
				"items": map[string]any{"pageInfo": noMore, "nodes": []any{node(repoStarred), node(repoListedOnly)}},
			}},
		}}
	default:
		http.Error(w, "unexpected query", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": viewer}})
}

// watch is the video page a capture of a watch URL fetches.
func (f *fixture) watch(w http.ResponseWriter, r *http.Request) {
	f.videoHits.Add(1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, "<html><head><title>Numbat talk %s</title></head><body><h1>Numbat talk</h1>"+
		"<p>A conference talk about the numbat survey tooling and agent runtimes.</p></body></html>", r.URL.Query().Get("v"))
}

// ---- hermetic instance and clients ----

type env struct {
	t      *testing.T
	dpkms  string
	ctxt   string
	fx     *fixture
	d      *testutil.Dpkms
	admin  string
	writer string
}

// startInstance installs the classify stub through the real install path
// on a private instance, then serves the same store, protected, with the
// recipe override inserting the step after tagger.
func startInstance(t *testing.T) *env {
	t.Helper()
	dpkms, ctxt, classify := binaries(t)
	fx := newFixture(t)
	shared := t.TempDir()
	base := map[string]any{
		"storage": map[string]any{"type": "sqlite", "path": filepath.Join(shared, "dpkms.db")},
		"steps":   map[string]any{"path": filepath.Join(shared, "steps")},
		"github":  map[string]any{"api_url": fx.URL},
		"fetch":   map[string]any{"allow_private": true},
		// The harness default (2s) bounds every job's context from server
		// start, not from shutdown: a run longer than that fails its jobs.
		"jobs": map[string]any{"drain_timeout": "10m"},
	}

	d1 := testutil.StartDpkms(t, testutil.PrivateDpkms(), testutil.WithDpkmsBinary(dpkms), testutil.WithDpkmsConfig(base))
	out, code := run(t, dpkms, d1.Env, "", "pipeline", "step", "install", "gh-stars-classify", "--path", classifyDir(t, classify), "--server-url", d1.URL)
	if code != 0 {
		t.Fatalf("install gh-stars-classify: exit %d\n%s", code, out)
	}
	if err := d1.Stop(); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{}
	for k, v := range base {
		cfg[k] = v
	}
	cfg["pipelines"] = recipeOverride(t)
	d := testutil.StartDpkms(t, testutil.WithDpkmsBinary(dpkms), testutil.WithDpkmsConfig(cfg))
	if out, code := run(t, dpkms, d.Env, "", "config", "validate"); code != 0 {
		t.Fatalf("config validate: exit %d\n%s", code, out)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("--- dpkms output ---\n%s", tail(d.Output(), 6000))
		}
	})
	return &env{t: t, dpkms: dpkms, ctxt: ctxt, fx: fx, d: d,
		admin: d.Token(dpkmstest.RoleAdmin), writer: d.Token(dpkmstest.RoleWriter)}
}

// recipeOverride is pipelines.overrides.url.github.repo.insert_steps from
// examples/recipes/github-repo-classify/dpkms.yaml, with the step's config
// swapped for the stub's (the example's config_file belongs to the real
// classifier).
func recipeOverride(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "examples", "recipes", "github-repo-classify", "dpkms.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	pipes, _ := doc["pipelines"].(map[string]any)
	overrides, _ := pipes["overrides"].(map[string]any)
	repo, _ := overrides["url.github.repo"].(map[string]any)
	insert, _ := repo["insert_steps"].([]any)
	if len(insert) != 1 {
		t.Fatalf("example recipe: want one insert_steps entry, got %v", repo)
	}
	step, _ := insert[0].(map[string]any)
	if step["step"] != "gh-stars-classify" || step["after"] != "tagger" {
		t.Fatalf("example recipe step drifted: %v", step)
	}
	step["config"] = map[string]any{"confidence": 0.9}
	return pipes
}

func run(t *testing.T, bin string, env []string, dir string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- binaries built from this checkout
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
		return "", -1
	}
}

// client is a ctxt user on its own hermetic HOME/XDG tree, writing to the
// instance with the writer key.
type client struct {
	e    *env
	root string
	env  []string
}

var scrubbed = map[string]bool{
	"GITHUB_TOKEN": true, "GH_TOKEN": true, "OPENAI_API_KEY": true, "ANTHROPIC_API_KEY": true,
	"GEMINI_API_KEY": true, "GOOGLE_API_KEY": true, "APS_DATA_PATH": true,
}

func (e *env) newClient() *client {
	e.t.Helper()
	root := e.t.TempDir()
	c := &client{e: e, root: root}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "CTXT_") || scrubbed[k] {
			continue
		}
		c.env = append(c.env, kv)
	}
	c.env = append(c.env,
		"HOME="+filepath.Join(root, "home"),
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_DATA_HOME="+filepath.Join(root, "data"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"),
		"XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"CTXT_NO_CLIPBOARD=1",
	)
	path := filepath.Join(root, "config", "contexthelp", "ctxt.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		e.t.Fatal(err)
	}
	cfg := fmt.Sprintf("server:\n  urls:\n    - %s\n  token: %q\n", e.d.URL, e.writer)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (c *client) run(args ...string) string {
	c.e.t.Helper()
	out, code := run(c.e.t, c.e.ctxt, c.env, c.root, args...)
	if code != 0 {
		c.e.t.Fatalf("ctxt %v: exit %d\n%s", args, code, out)
	}
	return out
}

var jobIDRe = regexp.MustCompile(`Job ID: (\S+)`)

// analyze runs `ctxt analyze` as the writer and returns the object the job
// stored or updated, read as the owner (the jobs routes are owner-only, so
// ctxt's own --wait cannot poll with a writer key).
func (c *client) analyze(args ...string) string {
	c.e.t.Helper()
	out := c.run(append([]string{"analyze"}, args...)...)
	m := jobIDRe.FindStringSubmatch(out)
	if m == nil {
		c.e.t.Fatalf("no Job ID in output:\n%s", out)
	}
	var job struct {
		Status   string `json:"status"`
		Error    string `json:"error"`
		ResultID string `json:"result_id"`
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		c.e.get("/api/v1/jobs/"+m[1], &job)
		if job.Status == "completed" || job.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			c.e.t.Fatalf("job %s still %s", m[1], job.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if job.Status != "completed" || job.ResultID == "" {
		c.e.t.Fatalf("job %s: %s %q (result %q)", m[1], job.Status, job.Error, job.ResultID)
	}
	return job.ResultID
}

// get reads path from the instance as the owner.
func (e *env) get(path string, v any) {
	e.t.Helper()
	if err := e.tryGet(path, v); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) tryGet(path string, v any) error {
	req, err := http.NewRequest(http.MethodGet, e.d.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+e.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, v)
}

// ---- stored objects ----

type tag struct {
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
	Source string  `json:"source"`
}

type object struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype"`
	Source    string         `json:"source"`
	SourceKey string         `json:"source_key"`
	Pipeline  string         `json:"pipeline"`
	InboxNote string         `json:"inbox_note"`
	ProfileID string         `json:"profile_id"`
	Metadata  map[string]any `json:"metadata"`
	Tags      []tag          `json:"tags"`
	Mentions  []any          `json:"mentions"`
}

func (e *env) object(id string) object {
	e.t.Helper()
	var o object
	e.get("/api/v1/objects/"+id, &o)
	return o
}

func (e *env) objects() []object {
	e.t.Helper()
	var out struct {
		Data []object `json:"data"`
	}
	e.get("/api/v1/objects?limit=200", &out)
	return out.Data
}

// waitObject polls until an object satisfies pick (async star-import jobs).
func (e *env) waitObject(pick func(object) bool) object {
	e.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		for _, o := range e.objects() {
			if pick(o) {
				return o
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatal("no object matched in time")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func labels(o object) map[string]tag {
	m := map[string]tag{}
	for _, t := range o.Tags {
		m[t.Label] = t
	}
	return m
}

func (o object) data() map[string]any {
	d, _ := o.Metadata["data"].(map[string]any)
	return d
}

func requireTags(t *testing.T, o object, want ...string) {
	t.Helper()
	have := labels(o)
	for _, w := range want {
		if _, ok := have[w]; !ok {
			t.Errorf("object %s: tag %q missing; tags %v", o.ID, w, o.Tags)
		}
	}
}

func forgeCount(os []object, forgeID string) int {
	n := 0
	for _, o := range os {
		if o.Metadata["forge_id"] == forgeID {
			n++
		}
	}
	return n
}

// ---- events ----

type eventLog struct {
	mu     sync.Mutex
	events []events.Event
}

// subscribe opens GET /api/v1/events as the owner and records every event
// until the test ends.
func (e *env) subscribe() *eventLog {
	e.t.Helper()
	log := &eventLog{}
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.d.URL+"/api/v1/events", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("subscribe events: %d %s", resp.StatusCode, b)
	}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			payload, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev events.Event
			if json.Unmarshal([]byte(payload), &ev) == nil {
				log.mu.Lock()
				log.events = append(log.events, ev)
				log.mu.Unlock()
			}
		}
	}()
	time.Sleep(300 * time.Millisecond) // the handler subscribes right after the headers
	return log
}

// ingested returns the object.ingested payloads for objectID.
func (l *eventLog) ingested(objectID string) []events.ObjectIngestedPayload {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []events.ObjectIngestedPayload
	for _, ev := range l.events {
		if ev.Type != string(events.TopicObjectIngested) {
			continue
		}
		var p events.ObjectIngestedPayload
		if json.Unmarshal(ev.Data, &p) == nil && p.ObjectID == objectID {
			out = append(out, p)
		}
	}
	return out
}

func (l *eventLog) waitIngested(t *testing.T, objectID string) []events.ObjectIngestedPayload {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if got := l.ingested(objectID); len(got) > 0 {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("no object.ingested event for %s", objectID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---- the test ----

const (
	videoKey = "youtube:numbat42"
	videoURL = "/watch?v=numbat42"
)

var repoURL = "https://github.com/" + repoCandidate

// flow is the state the steps of the run share: one instance, one writer
// client, one event log, and the objects earlier steps stored.
type flow struct {
	*env
	c       *client
	evlog   *eventLog
	video   string // the fixture video page URL
	videoID string
	repoID  string
}

// TestVideoAndRepoCaptureToStarCandidate runs the steps in order on one
// instance; each depends on what the one before stored.
func TestVideoAndRepoCaptureToStarCandidate(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs dpkms and ctxt")
	}
	e := startInstance(t)
	f := &flow{env: e, evlog: e.subscribe(), c: e.newClient(), video: e.fx.URL + videoURL}
	for _, step := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"video capture stores note, data, mentions, hints, source key", f.videoCapture},
		{"repo capture routes to url.github.repo and classify adds without wiping", f.repoCapture},
		{"one object per repo across URL spellings", f.repoSpellings},
		{"video re-analyze updates the object under the same source key", f.videoUpgrade},
		{"same source key under another profile is another object", f.videoOtherProfile},
		{"star-lists import files repos under hints and list vocabulary", f.starImport},
	} {
		if !t.Run(step.name, step.run) {
			t.FailNow() // later steps read what this one stored
		}
	}
}

func (f *flow) videoCapture(t *testing.T) {
	// The page comes from the fixture; the transcript is the note.
	data := `{"title":"Numbat survey tooling","description":"A talk. Repo: ` + repoURL + `","channel":"Dev Talks","links":["` + repoURL + `"],"transcript":"captions"}`
	f.videoID = f.c.analyze(f.video, "--type", "url", "--source-key", videoKey,
		"--note", "captions transcript v1", "--data", data,
		"--mention", "@channel.dev-talks", "--mention", "@creator.jane-doe",
		"--hint", "talk", "--hint", "agents")

	v := f.object(f.videoID)
	if v.InboxNote != "captions transcript v1" {
		t.Errorf("video note %q", v.InboxNote)
	}
	if v.SourceKey != videoKey {
		t.Errorf("video source key %q, want %q", v.SourceKey, videoKey)
	}
	if got := v.data()["title"]; got != "Numbat survey tooling" {
		t.Errorf("video data.title %v; metadata %v", got, v.Metadata)
	}
	if links, _ := v.data()["links"].([]any); len(links) != 1 || links[0] != repoURL {
		t.Errorf("video data.links %v", v.data()["links"])
	}
	requireTags(t, v, "talk", "agents")
	if have := labels(v); have["talk"].Source != "user" {
		t.Errorf("hint tag source %q, want user", have["talk"].Source)
	}
	if m := fmt.Sprint(v.Mentions); !strings.Contains(m, "dev-talks") || !strings.Contains(m, "jane-doe") {
		t.Errorf("video mentions %v", v.Mentions)
	}
	if f.fx.videoHits.Load() < 1 {
		t.Errorf("video page was not fetched from the fixture")
	}

	// The source-keyed capture announced itself with its key.
	got := f.evlog.waitIngested(t, f.videoID)[0]
	if got.SourceKey != videoKey || got.Source != v.Source || got.Source == "" {
		t.Errorf("video object.ingested source %q key %q; want key %q, source %q", got.Source, got.SourceKey, videoKey, v.Source)
	}
}

func (f *flow) repoCapture(t *testing.T) {
	// No --pipeline: the URL alone routes it.
	f.repoID = f.c.analyze(repoURL)
	r := f.object(f.repoID)
	if r.Pipeline != "url.github.repo" || r.Type != "repo" || r.Subtype != "github" {
		t.Errorf("repo pipeline %q type %q/%q, want url.github.repo repo/github", r.Pipeline, r.Type, r.Subtype)
	}
	for k, want := range map[string]any{
		"forge": "github", "forge_id": repoCandidate, "language": "Go",
		"description": "Runtime for agent tooling", "license": "MIT", "stars": float64(120), "archived": false,
	} {
		if r.Metadata[k] != want {
			t.Errorf("repo metadata[%s] = %v, want %v", k, r.Metadata[k], want)
		}
	}
	requireTags(t, r, "lang:go", "agents", "runtime", "star-candidate")
	if tg := labels(r)["star-candidate"]; tg.Source != "classify" || tg.Weight != 0.9 {
		t.Errorf("star-candidate tag %+v, want source classify weight 0.9 (step config reached the step)", tg)
	}
	star, _ := r.Metadata["star"].(map[string]any)
	if star["confidence"] != 0.9 {
		t.Errorf("repo metadata.star %v, want the classify step's confidence", r.Metadata["star"])
	}
	var tagger bool
	for _, tg := range r.Tags {
		if tg.Source != "github" && tg.Source != "classify" {
			tagger = true
		}
	}
	if !tagger {
		t.Errorf("repo tags %v: none from the tagger survived the classify step", r.Tags)
	}
	got := f.evlog.waitIngested(t, f.repoID)[0]
	if got.Source != repoURL || got.SourceKey != "" || got.Pipeline != "url.github.repo" {
		t.Errorf("repo object.ingested %+v", got)
	}
}

func (f *flow) repoSpellings(t *testing.T) {
	again := f.c.analyze("https://github.com/OCTO-ORG/agent-runner/")
	if again != f.repoID {
		t.Errorf("another spelling of the repo URL made object %s, want %s", again, f.repoID)
	}
	if n := forgeCount(f.objects(), repoCandidate); n != 1 {
		t.Errorf("%d objects for %s, want 1", n, repoCandidate)
	}
}

func (f *flow) videoUpgrade(t *testing.T) {
	data := `{"title":"Numbat survey tooling","channel":"Dev Talks","links":["` + repoURL + `"],"transcript":"whisper"}`
	updated := f.c.analyze(f.video, "--type", "url", "--source-key", videoKey,
		"--note", "whisper transcript v2", "--data", data, "--hint", "transcript:whisper")
	if updated != f.videoID {
		t.Fatalf("re-analyze under the same source key made object %s, want %s", updated, f.videoID)
	}
	v := f.object(f.videoID)
	if v.InboxNote != "whisper transcript v2" {
		t.Errorf("updated note %q", v.InboxNote)
	}
	if got := v.data()["transcript"]; got != "whisper" {
		t.Errorf("updated data.transcript %v", got)
	}
	if _, kept := v.data()["description"]; kept {
		t.Errorf("updated data %v: data replaces, the old description should be gone", v.data())
	}
	requireTags(t, v, "talk", "agents", "transcript:whisper")
	n := 0
	for _, o := range f.objects() {
		if o.SourceKey == videoKey && o.ProfileID == "" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d global objects under %s, want 1", n, videoKey)
	}
}

func (f *flow) videoOtherProfile(t *testing.T) {
	other := f.c.analyze(f.video, "--type", "url", "--source-key", videoKey, "--profile", "research",
		"--note", "research notes", "--data", `{"title":"Numbat survey tooling"}`)
	if other == f.videoID {
		t.Fatalf("same key under profile research updated object %s; want a separate object", f.videoID)
	}
	if o := f.object(other); o.ProfileID != "research" || o.SourceKey != videoKey || o.InboxNote != "research notes" {
		t.Errorf("profile object: profile %q key %q note %q", o.ProfileID, o.SourceKey, o.InboxNote)
	}
	if o := f.object(f.videoID); o.InboxNote != "whisper transcript v2" || o.ProfileID != "" {
		t.Errorf("global video object changed by the profile submission: note %q profile %q", o.InboxNote, o.ProfileID)
	}
}

type importReport struct {
	Account   string `json:"account"`
	Repos     int    `json:"repos"`
	Lists     int    `json:"lists"`
	Submitted int    `json:"submitted"`
	Unchanged int    `json:"unchanged"`
	Failed    int    `json:"failed"`
}

func (f *flow) importStars(t *testing.T) importReport {
	t.Helper()
	out := f.c.run("import", "github", "--star-lists", "--token", graphqlToken, "--github-base-url", f.fx.URL, "--format", "json")
	var rep importReport
	if err := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(jsonPart(out)))).Decode(&rep); err != nil {
		t.Fatalf("import report: %v\n%s", err, out)
	}
	return rep
}

func (f *flow) starImport(t *testing.T) {
	rep := f.importStars(t)
	if rep.Account != viewerLogin || rep.Repos != 2 || rep.Lists != 1 || rep.Submitted != 2 || rep.Failed != 0 {
		t.Fatalf("import report %+v", rep)
	}
	byKey := func(key string) object {
		return f.waitObject(func(o object) bool {
			return o.SourceKey == key && o.ProfileID == "" && o.Type == "repo" && len(o.Tags) > 0
		})
	}
	starred := byKey("github:" + repoStarred)
	listed := byKey("github:" + repoListedOnly)
	for _, o := range []object{starred, listed} {
		requireTags(t, o, "star-list:agent-tooling")
		lists, _ := o.data()["star_lists"].([]any)
		if len(lists) != 1 {
			t.Fatalf("%s data.star_lists %v", o.SourceKey, o.data()["star_lists"])
		}
		l, _ := lists[0].(map[string]any)
		if l["slug"] != "agent-tooling" || l["name"] != "Agent tooling" || l["description"] != "CLIs and runtimes for agents" {
			t.Errorf("%s star list vocabulary %v", o.SourceKey, l)
		}
		if o.Pipeline != "url.github.repo" || o.data()["account"] != viewerLogin {
			t.Errorf("%s pipeline %q account %v", o.SourceKey, o.Pipeline, o.data()["account"])
		}
	}
	f.checkStarred(t, starred)
	f.checkListedOnly(t, listed)
	f.checkRerunAndEvents(t, starred)
}

// checkStarred: a starred repo keeps its hints and gets no star-candidate.
func (f *flow) checkStarred(t *testing.T, starred object) {
	t.Helper()
	requireTags(t, starred, "starred", "lang:go", "cli")
	if tg := labels(starred)["starred"]; tg.Source != "user" {
		t.Errorf("starred hint tag %+v, want source user", tg)
	}
	if starred.data()["starred"] != true {
		t.Errorf("starred repo data.starred %v", starred.data()["starred"])
	}
	if _, bad := labels(starred)["star-candidate"]; bad {
		t.Errorf("starred repo got star-candidate: %v", starred.Tags)
	}
	if _, has := starred.Metadata["star"]; has {
		t.Errorf("starred repo got classify metadata: %v", starred.Metadata["star"])
	}
}

// checkListedOnly: a repo only a list holds is not starred, so it is a
// candidate.
func (f *flow) checkListedOnly(t *testing.T, listed object) {
	t.Helper()
	if tg, bad := labels(listed)["starred"]; (bad && tg.Source == "user") || listed.data()["starred"] != false {
		t.Errorf("list-only repo marked starred: tags %v data %v", listed.Tags, listed.data())
	}
	requireTags(t, listed, "star-candidate", "lang:rust")
}

func (f *flow) checkRerunAndEvents(t *testing.T, starred object) {
	t.Helper()
	// A rerun finds nothing changed.
	if rerun := f.importStars(t); rerun.Submitted != 0 || rerun.Unchanged != 2 {
		t.Errorf("rerun report %+v, want 0 submitted 2 unchanged", rerun)
	}

	// The star-import objects announced their keys.
	if got := f.evlog.waitIngested(t, starred.ID)[0]; got.SourceKey != "github:"+repoStarred {
		t.Errorf("starred repo object.ingested key %q", got.SourceKey)
	}
	if f.fx.graphqlSeen.Load() == 0 {
		t.Error("the star import never reached the fixture GraphQL server")
	}
}

// jsonPart is the JSON document at the end of the command output (the
// CLI may print notices before it).
func jsonPart(out string) []byte {
	i := strings.Index(out, "{")
	if i < 0 {
		return []byte(out)
	}
	return []byte(out[i:])
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
