package cmd

// The embedding-model lifecycle end to end, through the built ctxt and dpkms
// binaries on a fixture instance: register A, ingest, find under A, register
// B, migrate to B, set-default B, find under B, deprecate A, ingest without
// A, purge A. dpkms serve is the real daemon: it ingests, runs the
// migration job and reports progress on /healthz.
//
// Every Ollama call, from either binary, goes through one proxy whose
// transport replays testdata/cassettes/embeddings-journey (xrr). Re-record
// against a real Ollama with snowflake-arctic-embed2 and nomic-embed-text
// pulled; recording also rewrites the embeddings-list and upgrade-status eva
// fixtures from the journey's own output:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestEmbeddingsJourney ./cmd/ctxt/cmd/

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

const journeyCassettes = "testdata/cassettes/embeddings-journey"

const (
	journeyA      = "ollama-snowflake-arctic-embed2@2026-09-26"
	journeyAModel = "snowflake-arctic-embed2"
	journeyADim   = 1024
	journeyB      = "ollama-nomic-embed-text@2026-09-26"
	journeyBModel = "nomic-embed-text"
	journeyBDim   = 768

	// journeyOllamaHost is the host cassettes are keyed and stored under,
	// whatever port the proxy listens on.
	journeyOllamaHost = "http://127.0.0.1:11434"
)

// journeyNotes are short: ingest routes them to text.short.
var journeyNotes = []string{
	"numbat sightings near the dryandra woodland",
	"quokka colonies on rottnest island",
	"bilby burrows in the pilbara",
}

// journeyDoc is a long document, ingested through text.long.
var journeyDoc = strings.Join([]string{
	"Dugongs are large marine mammals that live in warm coastal waters.",
	"In Shark Bay they spend most of the day feeding in shallow meadows,",
	"pulling up whole plants and leaving feeding trails in the sand.",
	"A single animal can eat around forty kilograms of plants every day,",
	"so the health of the meadows decides how many dugongs the bay can support.",
	"Rangers survey the herds from light aircraft each winter,",
	"counting calves and mapping where the animals gather.",
	"Heatwaves that kill the meadows push the herds into deeper water,",
	"where they find less food and face more boat traffic.",
}, " ")

const (
	// journeyQuery shares no word with journeyDoc; only a semantic match
	// ranks the doc first.
	journeyQuery = "where do sea cows graze on seagrass"
	// journeyDualNote is ingested while both models are registered.
	journeyDualNote = "thorny devil lizards drinking dew in the gibson desert"
	// journeyLateNote is ingested after A's deprecation took effect.
	journeyLateNote = "cassowary casques in the daintree rainforest"
)

// --- binaries -------------------------------------------------------------------

var (
	dpkmsBinaryPath string
	dpkmsBinaryErr  error
	dpkmsBinaryOnce sync.Once
)

// e2eDpkmsBinary builds dpkms once per test run, as e2eBinary does ctxt.
func e2eDpkmsBinary(t *testing.T) string {
	t.Helper()
	dpkmsBinaryOnce.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			dpkmsBinaryErr = errors.New("go binary not on PATH")
			return
		}
		dir, err := os.MkdirTemp("", "dpkms-e2e-")
		if err != nil {
			dpkmsBinaryErr = err
			return
		}
		bin := filepath.Join(dir, "dpkms")
		cmd := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/dpkms")
		cmd.Dir = mustRepoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			dpkmsBinaryErr = fmt.Errorf("go build: %w\n%s", err, out)
			return
		}
		dpkmsBinaryPath = bin
	})
	if dpkmsBinaryErr != nil {
		t.Skipf("e2e: %v", dpkmsBinaryErr)
	}
	return dpkmsBinaryPath
}

// --- recorded Ollama --------------------------------------------------------------

// startJourneyOllama serves Ollama's API from the journey cassettes.
func startJourneyOllama(t *testing.T) (string, *providertest.OllamaCalls) {
	t.Helper()
	return startRecordedOllama(t, journeyCassettes)
}

// startRecordedOllama serves Ollama's API from cassettes (or, when
// recording, from the real Ollama), so binaries that cannot be handed an
// *http.Client still make only recorded calls.
func startRecordedOllama(t *testing.T, cassettes string) (string, *providertest.OllamaCalls) {
	t.Helper()
	client, calls := providertest.OllamaClient(t, cassettes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, journeyOllamaHost+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Header = r.Header.Clone()
		resp, err := client.Do(req)
		if err != nil {
			// A cassette miss lands here: the provider sees a failed call.
			http.Error(w, "journey ollama: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, calls
}

// --- storage inspection -----------------------------------------------------------

// journeyStore is the fixture instance's database as the test inspects it,
// independent of the binaries under test.
type journeyStore struct {
	kind        string
	storageType string
	// storagePath is the SQLite file or the Postgres DSN.
	storagePath string
	dialect     indexsig.Dialect
	open        func() (*sql.DB, func(), error)
	db          *sql.DB
}

func sqliteJourneyStore(t *testing.T) *journeyStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journey.db")
	return &journeyStore{
		kind: "sqlite", storageType: "sqlite", storagePath: path, dialect: indexsig.DialectSQLite,
		open: func() (*sql.DB, func(), error) {
			d, err := sqlite.New(path)
			if err != nil {
				return nil, nil, err
			}
			return d.DB(), func() { _ = d.Close(context.Background()) }, nil
		},
	}
}

// connect opens the inspection handle once dpkms has created the schema.
func (s *journeyStore) connect(t *testing.T) {
	t.Helper()
	db, closeFn, err := s.open()
	if err != nil {
		t.Fatalf("open %s: %v", s.kind, err)
	}
	t.Cleanup(closeFn)
	s.db = db
}

// rebind turns ? placeholders into $n for Postgres.
func (s *journeyStore) rebind(q string) string {
	if s.dialect != indexsig.DialectPostgres {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *journeyStore) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), s.rebind(q), args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (s *journeyStore) rows(t *testing.T, modelID string) int {
	t.Helper()
	return s.count(t, `SELECT COUNT(*) FROM embeddings WHERE model_id = ?`, modelID)
}

func (s *journeyStore) objects(t *testing.T) int {
	t.Helper()
	return s.count(t, `SELECT COUNT(*) FROM objects`)
}

func (s *journeyStore) pipelineObjects(t *testing.T, pipeline string) int {
	t.Helper()
	return s.count(t, `SELECT COUNT(*) FROM objects WHERE pipeline = ? OR pipeline LIKE ?`, pipeline, pipeline+"@%")
}

// indexExists reports whether modelID's ANN index is in the catalog: the
// vec0 table on SQLite, the partial HNSW index on Postgres.
func (s *journeyStore) indexExists(t *testing.T, modelID string) bool {
	t.Helper()
	name := storage.EmbeddingIndexName(modelID)
	if s.dialect == indexsig.DialectPostgres {
		return s.count(t, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?`, "idx_"+name) > 0
	}
	return s.count(t, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, "vec_"+name) > 0
}

func (s *journeyStore) signatureExists(t *testing.T, modelID string) bool {
	t.Helper()
	row, err := indexsig.Load(context.Background(), s.db, s.dialect, indexsig.EmbeddingSignatureID(modelID))
	if err != nil {
		t.Fatalf("load signature of %s: %v", modelID, err)
	}
	return row != nil
}

func (s *journeyStore) jobStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := s.db.QueryRowContext(context.Background(), s.rebind(`SELECT status FROM jobs WHERE id = ?`), id).Scan(&status); err != nil {
		t.Fatalf("job %s: %v", id, err)
	}
	return status
}

// --- the fixture instance ---------------------------------------------------------

// syncBuffer collects a daemon's output while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// journeyEnv is one fixture instance: an isolated HOME/XDG tree whose ctxt
// and dpkms configs point at store, a running dpkms serve, and the recorded
// Ollama.
type journeyEnv struct {
	t      *testing.T
	ctxt   string
	dirs   map[string]string
	env    []string
	store  *journeyStore
	ollama string
	calls  *providertest.OllamaCalls
	server string
	log    *syncBuffer
}

type journeyResult struct {
	stdout, stderr string
	exit           int
}

func newJourneyEnv(t *testing.T, store *journeyStore) *journeyEnv {
	t.Helper()
	ctxtBin, dpkmsBin := e2eBinary(t), e2eDpkmsBinary(t)
	root := t.TempDir()
	dirs := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "run"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dirs["XDG_CONFIG_HOME"], "contexthelp"), 0o700); err != nil {
		t.Fatal(err)
	}
	ollama, calls := startJourneyOllama(t)
	e := &journeyEnv{
		t: t, ctxt: ctxtBin, dirs: dirs, env: journeyEnviron(dirs), store: store,
		ollama: ollama, calls: calls, log: &syncBuffer{},
	}
	e.writeConfig("")
	e.startDpkms(dpkmsBin)
	e.writeConfig("")
	store.connect(t)
	return e
}

// journeyEnviron is the test process env minus everything that could route
// a binary elsewhere (CTXT_*, DPKMS_*, KIT_*, the testguard's closed
// embedding endpoint included), plus the fixture's HOME/XDG tree.
func journeyEnviron(dirs map[string]string) []string {
	drop := []string{"CTXT_", "CH_", "DPKMS_", "KIT_", "XRR_", "BUS_TOKEN=", "HOME=", "XDG_"}
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, p := range drop {
			if strings.HasPrefix(kv, p) {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	for k, v := range dirs {
		env = append(env, k+"="+v)
	}
	return append(env,
		"BUS_TOKEN=embeddings-journey",
		"CTXT_NO_CLIPBOARD=1",
		"CTXT_SECRETS_BACKEND=env",
		"NO_COLOR=1",
	)
}

// writeConfig writes ctxt.yaml and dpkms.yaml; extra is appended to both.
// Until dpkms runs, ctxt's server.url is a closed port.
func (e *journeyEnv) writeConfig(extra string) {
	e.t.Helper()
	common := fmt.Sprintf(`storage:
  type: %s
  path: %q
providers:
  llm:
    backend: stub
  embedding:
    endpoint: %s
jobs:
  poll_interval: 50ms
%s`, e.store.storageType, e.store.storagePath, e.ollama, extra)
	server := e.server
	if server == "" {
		server = "http://127.0.0.1:1"
	}
	dir := filepath.Join(e.dirs["XDG_CONFIG_HOME"], "contexthelp")
	for name, body := range map[string]string{
		"ctxt.yaml":  common + "server:\n  url: " + server + "\n",
		"dpkms.yaml": common,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			e.t.Fatal(err)
		}
	}
}

var dpkmsListening = regexp.MustCompile(`HTTP server listening on (127\.0\.0\.1:\d+)`)

// startDpkms runs dpkms serve on free ports and waits for /healthz.
func (e *journeyEnv) startDpkms(bin string) {
	t := e.t
	t.Helper()
	cmd := exec.Command(bin, "serve", "--port", freePort(t), "--grpc-port", freePort(t), "--workers", "1") // #nosec G204 -- test-built binary
	cmd.Env = e.env
	cmd.Dir = e.dirs["HOME"]
	cmd.Stdout, cmd.Stderr = e.log, e.log
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dpkms: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("dpkms serve output:\n%s", e.log.String())
		}
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			t.Fatalf("dpkms serve exited during startup:\n%s", e.log.String())
		default:
		}
		if m := dpkmsListening.FindStringSubmatch(e.log.String()); m != nil {
			url := "http://" + m[1]
			if resp, err := http.Get(url + "/healthz"); err == nil { // #nosec G107 -- loopback test daemon
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					e.server = url
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dpkms serve not healthy within 60s:\n%s", e.log.String())
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

// serverDefaultingCommands resolve the daemon from --server only; without
// it they use the default local port, never the fixture's server.url.
var serverDefaultingCommands = map[string]bool{"upgrade": true, "status": true, "log": true}

// run executes the built ctxt with args.
func (e *journeyEnv) run(args ...string) journeyResult {
	e.t.Helper()
	if len(args) > 0 && serverDefaultingCommands[args[0]] && !slices.Contains(args, "--server") {
		e.t.Fatalf("ctxt %s without --server would reach the default local port", strings.Join(args, " "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.ctxt, args...) // #nosec G204 -- test-built binary
	cmd.Env = e.env
	cmd.Dir = e.dirs["HOME"]
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	res := journeyResult{stdout: so.String(), stderr: se.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.exit = ee.ExitCode()
	default:
		e.t.Fatalf("ctxt %v: %v", args, err)
	}
	return res
}

// ok runs ctxt and fails the test unless it exits 0.
func (e *journeyEnv) ok(args ...string) string {
	e.t.Helper()
	r := e.run(args...)
	if r.exit != 0 {
		e.t.Fatalf("ctxt %s: exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), r.exit, r.stdout, r.stderr)
	}
	return r.stdout
}

// refused runs ctxt and fails the test unless it exits with code.
func (e *journeyEnv) refused(code int, args ...string) journeyResult {
	e.t.Helper()
	r := e.run(args...)
	if r.exit != code {
		e.t.Fatalf("ctxt %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), r.exit, code, r.stdout, r.stderr)
	}
	return r
}

func journeyDecode[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return v
}

// list runs `ctxt embeddings list --format json`; raw is its stdout.
func (e *journeyEnv) list() (models map[string]embeddingsListItem, raw string) {
	e.t.Helper()
	raw = e.ok("embeddings", "list", "--format", "json")
	doc := journeyDecode[struct {
		Models []embeddingsListItem `json:"models"`
	}](e.t, raw)
	models = map[string]embeddingsListItem{}
	defaults := 0
	for _, m := range doc.Models {
		models[m.ModelID] = m
		if m.IsDefault {
			defaults++
		}
	}
	if defaults > 1 {
		e.t.Fatalf("list shows %d defaults:\n%s", defaults, raw)
	}
	return models, raw
}

// expectModel checks one list row.
func (e *journeyEnv) expectModel(models map[string]embeddingsListItem, id string, dim int, isDefault, deprecated bool, coverage float64) {
	e.t.Helper()
	m, ok := models[id]
	if !ok {
		e.t.Fatalf("%s not listed: %+v", id, models)
	}
	if m.Dimension != dim || m.IsDefault != isDefault || (m.DeprecatedAt != nil) != deprecated ||
		math.Abs(m.Coverage-coverage) > 0.001 || m.Provider != "ollama" || m.RegisteredAt == "" {
		e.t.Fatalf("list row %s = %+v (deprecated_at %v), want dimension %d default %v deprecated %v coverage %.3f",
			id, m, m.DeprecatedAt, dim, isDefault, deprecated, coverage)
	}
}

// status runs `ctxt upgrade status --format json` against the daemon. The
// server is always pinned: without --server the command does not read
// server.url from the config file and falls back to the default local
// port, where a real daemon may listen.
func (e *journeyEnv) status() (upgradeEnvelope, string, int) {
	e.t.Helper()
	r := e.run("upgrade", "status", "--server", e.server, "--format", "json")
	raw := firstJSON(r.stdout)
	return journeyDecode[upgradeEnvelope](e.t, raw), raw, r.exit
}

func (e *journeyEnv) expectIdle(when string) {
	e.t.Helper()
	if st, raw, code := e.status(); code != 0 || st.State != "idle" {
		e.t.Fatalf("upgrade status %s = %s (exit %d), want idle", when, raw, code)
	}
}

// ingest captures text through dpkms and waits for the job.
func (e *journeyEnv) ingest(text string, args ...string) {
	e.t.Helper()
	e.ok(append([]string{"analyze", text, "--wait"}, args...)...)
}

// ingestCorpus ingests the notes and the doc.
func (e *journeyEnv) ingestCorpus() {
	e.t.Helper()
	for _, n := range journeyNotes {
		e.ingest(n)
	}
	e.ingest(journeyDoc, "--pipeline", "text.long")
}

type journeyHit struct {
	ID         string `json:"id"`
	RawContent string `json:"raw_content"`
}

// find runs a semantic `ctxt find` and returns the hits and the model the
// query was embedded with.
func (e *journeyEnv) find(query string) ([]journeyHit, string) {
	e.t.Helper()
	out := journeyDecode[struct {
		Objects     []journeyHit `json:"objects"`
		Diagnostics struct {
			Semantic struct {
				Status  string `json:"status"`
				ModelID string `json:"model_id"`
			} `json:"semantic"`
		} `json:"diagnostics"`
	}](e.t, e.ok("find", query, "--semantic", "--format", "json"))
	if s := out.Diagnostics.Semantic; s.Status != "ok" {
		e.t.Fatalf("find %q: semantic status %q under %q, want ok", query, s.Status, s.ModelID)
	}
	return out.Objects, out.Diagnostics.Semantic.ModelID
}

// expectFind checks a semantic query embeds with modelID and ranks the doc
// first.
func (e *journeyEnv) expectFind(modelID string) {
	e.t.Helper()
	hits, got := e.find(journeyQuery)
	if got != modelID {
		e.t.Fatalf("find embedded the query with %s, want %s", got, modelID)
	}
	if len(hits) == 0 || hits[0].RawContent != journeyDoc {
		e.t.Fatalf("find %q under %s: top hit %+v, want the text.long doc", journeyQuery, modelID, hits)
	}
}

// waitMigration polls `ctxt upgrade status` until jobID is done and the
// daemon is idle again, returning every in-progress report seen and the
// raw output of the most advanced one.
func (e *journeyEnv) waitMigration(jobID string) ([]upgradeEnvelope, string) {
	e.t.Helper()
	var seen []upgradeEnvelope
	var bestRaw string
	bestDone := -1
	deadline := time.Now().Add(2 * time.Minute)
	for {
		st, raw, code := e.status()
		if code != 0 {
			e.t.Fatalf("upgrade status during migration: exit %d\n%s", code, raw)
		}
		if st.State == "in_progress" {
			if st.Done > bestDone {
				bestRaw, bestDone = raw, st.Done
			}
			seen = append(seen, st)
		}
		switch job := e.store.jobStatus(e.t, jobID); {
		case job == string(storage.JobFailed):
			e.t.Fatalf("migration job %s failed; status %s", jobID, raw)
		case job == string(storage.JobCompleted) && st.State == "idle":
			return seen, bestRaw
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("migration job %s did not finish; last status %s", jobID, raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// recordFixture rewrites an eva fixture from real output while recording.
func (e *journeyEnv) recordFixture(name, raw string) {
	e.t.Helper()
	if os.Getenv("XRR_MODE") != "record" || e.store.kind != "sqlite" {
		return
	}
	if !json.Valid([]byte(raw)) {
		e.t.Fatalf("fixture %s is not JSON:\n%s", name, raw)
	}
	path := filepath.Join(mustRepoRoot(e.t), "test", "integration", "testdata", "eva-fixtures", name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(raw)+"\n"), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// expectIntact checks a model's rows, index and signature are all present.
func (e *journeyEnv) expectIntact(modelID string, rows int) {
	e.t.Helper()
	if n := e.store.rows(e.t, modelID); n != rows {
		e.t.Fatalf("%s has %d embedding rows, want %d", modelID, n, rows)
	}
	if !e.store.indexExists(e.t, modelID) || !e.store.signatureExists(e.t, modelID) {
		e.t.Fatalf("%s: index %v signature %v, want both", modelID,
			e.store.indexExists(e.t, modelID), e.store.signatureExists(e.t, modelID))
	}
}

// --- the journey --------------------------------------------------------------------

// runEmbeddingsJourney is the whole lifecycle; each backend runs it as is.
func runEmbeddingsJourney(t *testing.T, e *journeyEnv) {
	const (
		exitConflict     = 4
		exitUnauthorized = 5
	)

	// An empty instance: nothing registered, nothing running.
	models, raw := e.list()
	if len(models) != 0 {
		t.Fatalf("fresh instance lists models:\n%s", raw)
	}
	e.recordFixture("embeddings-list-empty.json", raw)
	e.expectIdle("on a fresh instance")

	// 1. Register A: the dimension is measured through the provider.
	reg := journeyDecode[registerDoc](t, e.ok("embeddings", "register", journeyA,
		"--embedding-model", journeyAModel, "--dimension", fmt.Sprint(journeyADim), "--format", "json"))
	if reg.Dimension != journeyADim || reg.Sources["dimension"] != dimensionSourceMeasured || reg.Index != registerIndexReady || reg.IsDefault {
		t.Fatalf("register %s = %+v", journeyA, reg)
	}
	if !e.store.indexExists(t, journeyA) {
		t.Fatalf("register %s built no index", journeyA)
	}
	// An empty corpus is fully covered, so A can become the default at once.
	sd := journeyDecode[setDefaultDoc](t, e.ok("embeddings", "set-default", journeyA, "--format", "json"))
	if !sd.Changed || sd.PreviousDefault != nil {
		t.Fatalf("set-default %s on an empty corpus = %+v", journeyA, sd)
	}

	// 2. Ingest short notes and a long doc through dpkms.
	e.ingestCorpus()
	corpus := len(journeyNotes) + 1
	if n := e.store.objects(t); n != corpus {
		t.Fatalf("objects after ingest = %d, want %d", n, corpus)
	}
	if s, l := e.store.pipelineObjects(t, "text.short"), e.store.pipelineObjects(t, "text.long"); s != len(journeyNotes) || l != 1 {
		t.Fatalf("pipelines: text.short %d, text.long %d; want %d and 1", s, l, len(journeyNotes))
	}
	e.expectIntact(journeyA, corpus)
	models, _ = e.list()
	e.expectModel(models, journeyA, journeyADim, true, false, 1)

	// 3. Semantic hits under A.
	e.expectFind(journeyA)

	// 4. Register B beside it: not the default, no vectors yet.
	reg = journeyDecode[registerDoc](t, e.ok("embeddings", "register", journeyB,
		"--embedding-model", journeyBModel, "--format", "json"))
	if reg.Dimension != journeyBDim || reg.Index != registerIndexReady {
		t.Fatalf("register %s = %+v", journeyB, reg)
	}
	models, _ = e.list()
	e.expectModel(models, journeyA, journeyADim, true, false, 1)
	e.expectModel(models, journeyB, journeyBDim, false, false, 0)

	// A running dpkms dual-writes new ingests for both models.
	e.ingest(journeyDualNote)
	corpus++
	e.expectIntact(journeyA, corpus)
	e.expectIntact(journeyB, 1)
	partial := 1 / float64(corpus)
	models, raw = e.list()
	e.expectModel(models, journeyB, journeyBDim, false, false, partial)
	e.recordFixture("embeddings-list-migrating.json", raw)

	// 6a. The flip is refused while B's coverage is below the threshold
	// (the refusal comes before the migration, where coverage can be low).
	r := e.refused(exitConflict, "embeddings", "set-default", journeyB, "--format", "json")
	for _, want := range []string{fmt.Sprintf("%.2f", partial), "0.99", "migrate --to " + journeyB} {
		if !strings.Contains(r.stderr+r.stdout, want) {
			t.Errorf("coverage refusal does not mention %q:\n%s%s", want, r.stdout, r.stderr)
		}
	}
	models, _ = e.list()
	e.expectModel(models, journeyA, journeyADim, true, false, 1)
	e.expectModel(models, journeyB, journeyBDim, false, false, partial)

	// 5. Migrate to B in the background; `upgrade status` follows it.
	mig := journeyDecode[migrateDoc](t, e.ok("embeddings", "migrate", "--to", journeyB, "--rate-limit", "2/s", "--format", "json"))
	if mig.Status != migrateQueued || mig.JobID == "" || mig.Missing != corpus-1 {
		t.Fatalf("migrate = %+v, want queued for %d objects", mig, corpus-1)
	}
	seen, midRaw := e.waitMigration(mig.JobID)
	if len(seen) == 0 {
		t.Fatal("upgrade status never reported the migration in progress")
	}
	for _, st := range seen {
		if st.Bucket != "embeddings_migrate" || st.Target != journeyB || st.Total != mig.Missing ||
			st.Done > st.Total || st.Failed != 0 {
			t.Fatalf("in-progress status %+v, want embeddings_migrate to %s over %d objects", st, journeyB, mig.Missing)
		}
	}
	e.recordFixture("upgrade-status-embeddings-migrate-in-progress.json", midRaw)
	e.expectIdle("after the migration")
	e.expectIntact(journeyB, corpus)
	e.expectIntact(journeyA, corpus)
	models, _ = e.list()
	e.expectModel(models, journeyB, journeyBDim, false, false, 1)

	// 6b. At full coverage the flip goes through.
	sd = journeyDecode[setDefaultDoc](t, e.ok("embeddings", "set-default", journeyB, "--format", "json"))
	if !sd.Changed || sd.PreviousDefault == nil || *sd.PreviousDefault != journeyA || sd.Coverage != 1 {
		t.Fatalf("set-default %s = %+v", journeyB, sd)
	}
	models, _ = e.list()
	e.expectModel(models, journeyA, journeyADim, false, false, 1)
	e.expectModel(models, journeyB, journeyBDim, true, false, 1)

	// 7. The very next query embeds with B.
	e.expectFind(journeyB)

	// 8. Deprecate A from today. The typed confirmation comes first.
	today := time.Now().Format("2006-01-02")
	r = e.refused(exitUnauthorized, "embeddings", "deprecate", journeyA, "--on", today, "--format", "json")
	token := tokenFrom(r.stderr)
	if token == "" {
		t.Fatalf("deprecate refusal prints no token:\n%s", r.stderr)
	}
	if models, _ = e.list(); models[journeyA].DeprecatedAt != nil {
		t.Fatal("a refused deprecation stamped the model")
	}
	before := time.Now().Add(-time.Second)
	dep := journeyDecode[deprecateDoc](t, e.ok("embeddings", "deprecate", journeyA, "--on", today, "--confirm-token="+token, "--format", "json"))
	at, err := time.Parse(time.RFC3339, dep.DeprecatedAt)
	if err != nil || !dep.Changed || at.Before(before.Truncate(time.Second)) || at.After(time.Now()) {
		t.Fatalf("deprecate --on %s = %+v, want effective now", today, dep)
	}
	models, _ = e.list()
	e.expectModel(models, journeyA, journeyADim, false, true, 1)

	// 9. New ingests no longer write A.
	e.ingest(journeyLateNote)
	corpus++
	e.expectIntact(journeyA, corpus-1)
	e.expectIntact(journeyB, corpus)
	models, raw = e.list()
	e.expectModel(models, journeyA, journeyADim, false, true, float64(corpus-1)/float64(corpus))
	e.expectModel(models, journeyB, journeyBDim, true, false, 1)
	e.recordFixture("embeddings-list-lifecycle.json", raw)

	// 10. Purge A: typed confirmation, then the grace period refuses it.
	r = e.refused(exitUnauthorized, "embeddings", "purge", journeyA, "--format", "json")
	token = tokenFrom(r.stderr)
	if token == "" {
		t.Fatalf("purge refusal prints no token:\n%s", r.stderr)
	}
	r = e.refused(exitConflict, "embeddings", "purge", journeyA, "--confirm-token="+token, "--format", "json")
	if !strings.Contains(r.stderr+r.stdout, "grace period") {
		t.Errorf("grace refusal does not say why:\n%s%s", r.stdout, r.stderr)
	}
	e.expectIntact(journeyA, corpus-1)

	// A shortened grace period allows it.
	e.writeConfig("embeddings:\n  grace_period: 0s\n")
	pd := journeyDecode[purgeDoc](t, e.ok("embeddings", "purge", journeyA, "--confirm-token="+token, "--format", "json"))
	if pd.Rows != int64(corpus-1) || pd.DryRun {
		t.Fatalf("purge = %+v, want %d rows deleted", pd, corpus-1)
	}

	// 11. A's rows, index and signature are gone; B is intact.
	if n := e.store.rows(t, journeyA); n != 0 {
		t.Errorf("%s kept %d rows after purge", journeyA, n)
	}
	if e.store.indexExists(t, journeyA) {
		t.Errorf("%s kept its index after purge", journeyA)
	}
	if e.store.signatureExists(t, journeyA) {
		t.Errorf("%s kept its index signature after purge", journeyA)
	}
	e.expectIntact(journeyB, corpus)
	models, _ = e.list()
	if len(models) != 1 {
		t.Fatalf("list after purge = %+v, want only %s", models, journeyB)
	}
	e.expectModel(models, journeyB, journeyBDim, true, false, 1)
	e.expectFind(journeyB)
	e.expectIdle("after the purge")
}

func TestEmbeddingsJourney_SQLite(t *testing.T) {
	runEmbeddingsJourney(t, newJourneyEnv(t, sqliteJourneyStore(t)))
}

// --- the operator story -------------------------------------------------------------

const embeddingRegistryStory = "e2e/stories/core-embedding-registry.story.yaml"

// storyExits are the story steps that end in a refusal, by design.
var storyExits = map[string]int{
	"early_flip_refused": 4,
	"purge_preview":      4,
}

// The operator story runs, step by step, against a fixture instance where
// A is the covered default: every invocation is a real command line of the
// built binary, and each exits as the story intends.
func TestEmbeddingsJourney_Story(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(mustRepoRoot(t), embeddingRegistryStory))
	if err != nil {
		t.Fatal(err)
	}
	var story struct {
		Binary string `yaml:"binary"`
		Steps  []struct {
			ID     string   `yaml:"id"`
			Invoke []string `yaml:"invoke"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(raw, &story); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range story.Steps {
		ids[s.ID] = true
	}
	for id := range storyExits {
		if !ids[id] {
			t.Fatalf("story has no step %q", id)
		}
	}

	e := newJourneyEnv(t, sqliteJourneyStore(t))
	e.ok("embeddings", "register", journeyA, "--embedding-model", journeyAModel)
	e.ok("embeddings", "set-default", journeyA)
	e.ingestCorpus()

	for _, s := range story.Steps {
		if len(s.Invoke) < 2 || s.Invoke[0] != story.Binary {
			t.Fatalf("step %s invokes %v, want %s <args>", s.ID, s.Invoke, story.Binary)
		}
		args := s.Invoke[1:]
		if len(args) > 1 && args[0] == "upgrade" && args[1] == "status" {
			// Pinned like journeyEnv.status: never the default local port.
			args = append(append([]string{}, args...), "--server", e.server)
		}
		r := e.run(args...)
		if want := storyExits[s.ID]; r.exit != want {
			t.Fatalf("step %s (%s): exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
				s.ID, strings.Join(s.Invoke, " "), r.exit, want, r.stdout, r.stderr)
		}
		// The migration runs in dpkms; the story's next steps read its result.
		if len(s.Invoke) > 2 && s.Invoke[1] == "embeddings" && s.Invoke[2] == "migrate" {
			if mig := journeyDecode[migrateDoc](t, r.stdout); mig.JobID != "" {
				e.waitMigration(mig.JobID)
			}
		}
	}

	models, _ := e.list()
	e.expectModel(models, journeyB, journeyBDim, true, false, 1)
	e.expectModel(models, journeyA, journeyADim, false, true, 1)
	e.expectIntact(journeyA, len(journeyNotes)+1)
}
