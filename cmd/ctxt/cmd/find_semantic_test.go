package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Query embeddings replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2. Re-record with
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestFindSemantic ./cmd/ctxt/cmd/
const findCassettes = "testdata/cassettes/find-query-path"

// execFind runs ctxt with the test config and returns stdout and stderr
// separately, so tests can tell the notice line from the result output.
func execFind(t *testing.T, db *testDB, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled", "instance"} {
		viper.Set(k, "")
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	var errBuf bytes.Buffer
	rootCmd.SetArgs(append([]string{"--config", db.ConfigPath}, args...))
	rootCmd.SetOut(w)
	rootCmd.SetErr(&errBuf)
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	err = rootCmd.Execute()
	w.Close()
	os.Stdout = oldStdout
	rootCmd.SetErr(nil)
	return string(<-done), errBuf.String(), err
}

func sqliteDB(t *testing.T, db *testDB) *sqlite.Driver {
	t.Helper()
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatal("driver must be *sqlite.Driver")
	}
	return d
}

// seedFindCorpus stores one FTS-searchable note and clears any default
// embedding model.
func seedFindCorpus(t *testing.T, db *testDB) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	if err := db.Driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj_sem_1", Type: "text", Summaries: []string{"authentication best practices for web apps"},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	rebuildFTSForTest(t, db)
	if _, err := sqliteDB(t, db).DB().Exec(`UPDATE embedding_models SET is_default = 0`); err != nil {
		t.Fatal(err)
	}
}

const noDefaultNotice = "notice: semantic search unavailable (no_default_model): "

// With no default model, hybrid find shows the FTS results and a notice on
// stderr naming the reason; nothing degrades silently.
func TestFindSemantic_NoDefaultModelNoticeHuman(t *testing.T) {
	db := setupTestDB(t)
	seedFindCorpus(t, db)

	for _, mode := range [][]string{nil, {"--semantic"}} {
		stdout, stderr, err := execFind(t, db, append([]string{"find", "authentication"}, mode...)...)
		if err != nil {
			t.Fatalf("find %v: %v", mode, err)
		}
		if !strings.Contains(stdout, "obj_sem_1") {
			t.Errorf("find %v: FTS result missing from stdout:\n%s", mode, stdout)
		}
		if !strings.Contains(stderr, noDefaultNotice) || !strings.Contains(stderr, "results are full-text only") {
			t.Errorf("find %v: stderr lacks the fallback notice:\n%s", mode, stderr)
		}
		if strings.Contains(stdout, "notice:") {
			t.Errorf("find %v: notice leaked into stdout:\n%s", mode, stdout)
		}
	}
}

// JSON output carries the same report under diagnostics.semantic, with the
// notice on stderr and stdout left parseable.
func TestFindSemantic_NoDefaultModelNoticeJSON(t *testing.T) {
	db := setupTestDB(t)
	seedFindCorpus(t, db)

	stdout, stderr, err := execFind(t, db, "find", "authentication", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Total       int `json:"total"`
		Diagnostics struct {
			Semantic *struct {
				Status string `json:"status"`
				Notice string `json:"notice"`
			} `json:"semantic"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	sem := out.Diagnostics.Semantic
	if sem == nil || sem.Status != "no_default_model" || !strings.HasPrefix(sem.Notice, "semantic search unavailable (no_default_model)") {
		t.Fatalf("diagnostics.semantic = %+v, want status no_default_model with a notice", sem)
	}
	if out.Total != 1 {
		t.Errorf("total = %d, want the 1 FTS result", out.Total)
	}
	if !strings.Contains(stderr, noDefaultNotice) {
		t.Errorf("stderr lacks the notice:\n%s", stderr)
	}

	stdout, _, err = execFind(t, db, "find", "authentication", "--fts", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, `"semantic"`) {
		t.Errorf("--fts must not report a semantic leg:\n%s", stdout)
	}
}

// registerArcticDefault registers a snowflake-arctic-embed2 model as the default.
func registerArcticDefault(t *testing.T, db *testDB, modelID, endpoint string) {
	t.Helper()
	reg := registry.New(sqliteDB(t, db).DB())
	if err := reg.Register(context.Background(), registry.Model{
		ModelID: modelID, Provider: "ollama", Dimension: 1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"` + endpoint + `"}`,
	}, true); err != nil {
		t.Fatal(err)
	}
}

// A registered model's backend and model are its identity: find's flags
// cannot swap them, and the refusal is a visible provider_error notice.
func TestFindSemantic_IdentityOverrideRefused(t *testing.T) {
	db := setupTestDB(t)
	seedFindCorpus(t, db)
	registerArcticDefault(t, db, "arctic-find", "http://127.0.0.1:11434")

	client, calls := providertest.OllamaClient(t, findCassettes)
	findEmbeddingHTTPClient = client
	t.Cleanup(func() { findEmbeddingHTTPClient = nil })

	for _, flag := range [][]string{{"--embedding-model", "nomic-embed-text"}, {"--embedding-provider", "stub"}} {
		stdout, stderr, err := execFind(t, db, append([]string{"find", "authentication"}, flag...)...)
		if err != nil {
			t.Fatalf("find %v: %v", flag, err)
		}
		if !strings.Contains(stderr, "notice: semantic search unavailable (provider_error): model arctic-find") {
			t.Errorf("find %v: stderr lacks the provider_error notice:\n%s", flag, stderr)
		}
		if !strings.Contains(stdout, "obj_sem_1") {
			t.Errorf("find %v: FTS result missing:\n%s", flag, stdout)
		}
	}
	if urls := calls.URLs(); len(urls) != 0 {
		t.Fatalf("an identity override still embedded: %v", urls)
	}
}

// End to end through the real driver's per-model index: find embeds with the
// default model's provider (endpoint overridable by flag), searches that
// model's index with score = 1 - distance, and follows a default flip made
// through another registry handle. Skips until the driver's EmbeddingStore
// is implemented.
func TestFindSemantic_DefaultModelEndToEnd(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	d := sqliteDB(t, db)
	client, calls := providertest.OllamaClient(t, findCassettes)
	findEmbeddingHTTPClient = client
	t.Cleanup(func() { findEmbeddingHTTPClient = nil })

	// The query's own recorded embedding is each note's vector, under one
	// model only, so a hit names the index that was searched. It is embedded
	// before the gate below so the cassette exists ahead of the per-model
	// index landing.
	query := "rotating signing keys without downtime"
	arctic := func(id, endpoint string) registry.Model {
		return registry.Model{
			ModelID: id, Provider: "ollama", Dimension: 1024,
			ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"` + endpoint + `"}`,
		}
	}
	models := []registry.Model{arctic("arctic-e2e-a", "http://127.0.0.1:11434"), arctic("arctic-e2e-b", "http://127.0.0.1:11556")}
	p, err := embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: client,
	}).ForModel(ctx, models[0])
	if err != nil {
		t.Fatal(err)
	}
	vec, err := p.Embed(ctx, query)
	if err != nil {
		t.Fatal(err)
	}

	reg := registry.New(d.DB())
	for _, m := range models {
		if err := reg.Register(ctx, m, m.ModelID == "arctic-e2e-a"); err != nil {
			t.Fatal(err)
		}
		err := d.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Provider: m.Provider, Dimension: m.Dimension})
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skip("EmbeddingStore not implemented by the sqlite driver yet (per-model index)")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for id, model := range map[string]string{"note-a": "arctic-e2e-a", "note-b": "arctic-e2e-b"} {
		now := time.Now()
		if err := d.Objects().Create(ctx, &storage.KnowledgeObject{ID: id, Type: "note", RawContent: "unrelated wording", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := d.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: model, Vector: vec}}); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) (ids []string, scores []float64, model string) {
		t.Helper()
		stdout, stderr, err := execFind(t, db, append([]string{"find", query, "--semantic", "--format", "json"}, args...)...)
		if err != nil {
			t.Fatalf("find: %v\n%s", err, stderr)
		}
		var out struct {
			Objects []struct {
				ID       string         `json:"id"`
				Metadata map[string]any `json:"metadata"`
			} `json:"objects"`
			Diagnostics struct {
				Semantic struct {
					Status  string `json:"status"`
					ModelID string `json:"model_id"`
				} `json:"semantic"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if out.Diagnostics.Semantic.Status != "ok" {
			t.Fatalf("semantic status = %q, stderr:\n%s", out.Diagnostics.Semantic.Status, stderr)
		}
		for _, o := range out.Objects {
			ids = append(ids, o.ID)
			s, _ := o.Metadata["score"].(float64)
			scores = append(scores, s)
		}
		return ids, scores, out.Diagnostics.Semantic.ModelID
	}

	ids, scores, model := run("--embedding-endpoint", "http://127.0.0.1:11555")
	if model != "arctic-e2e-a" || len(ids) != 1 || ids[0] != "note-a" {
		t.Fatalf("got %v under %s, want [note-a] under arctic-e2e-a", ids, model)
	}
	if scores[0] < 0.999 {
		t.Errorf("identical vector score = %v, want ~1 (1 - distance)", scores[0])
	}
	if urls := calls.URLs(); urls[len(urls)-1] != "http://127.0.0.1:11555/api/embeddings" {
		t.Fatalf("query embedded at %s, want the --embedding-endpoint override", urls[len(urls)-1])
	}

	if _, err := registry.New(d.DB()).SetDefault(ctx, "arctic-e2e-b", 0); err != nil {
		t.Fatal(err)
	}
	ids, _, model = run()
	if model != "arctic-e2e-b" || len(ids) != 1 || ids[0] != "note-b" {
		t.Fatalf("after the flip got %v under %s, want [note-b] under arctic-e2e-b", ids, model)
	}
}
