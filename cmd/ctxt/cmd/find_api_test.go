package cmd

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// updateFindGolden rewrites testdata/find/*.golden from the current output.
var updateFindGolden = flag.Bool("update-find-golden", false, "rewrite find golden files")

// apiFindQuery is the recorded query of the find-query-path cassettes.
// note-vec holds its own embedding and shares no token with it; note-fts
// holds its words and no vector.
const apiFindQuery = "rotating signing keys without downtime"

// setupFindAPI seeds a deterministic corpus behind an in-process dpkms
// whose query embeddings replay the find-query-path cassettes: note-vec
// (vector only, under the default model arctic-find), note-fts and
// note-task (full-text only, note-task with metadata type task).
func setupFindAPI(t *testing.T, opts ...dpkmstest.Option) *testDB {
	t.Helper()
	client, _ := providertest.OllamaClient(t, findCassettes)
	db := setupTestDB(t, append([]dpkmstest.Option{dpkmstest.WithEmbeddingHTTPClient(client)}, opts...)...)
	ctx := context.Background()

	m := registry.Model{
		ModelID: "arctic-find", Provider: "ollama", Dimension: 1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`,
	}
	if err := registry.New(sqliteDB(t, db).DB()).Register(ctx, m, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Driver.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
		t.Fatal(err)
	}
	p, err := embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: client,
	}).ForModel(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	vec, err := p.Embed(ctx, apiFindQuery)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, o := range []*storage.KnowledgeObject{
		{ID: "note-vec", Type: "note", RawContent: "unrelated wording", CreatedAt: at, UpdatedAt: at},
		{ID: "note-fts", Type: "note", Summaries: []string{"runbook: rotating signing keys without downtime"}, CreatedAt: at, UpdatedAt: at},
		{
			ID: "note-task", Type: "note", Summaries: []string{"task: schedule rotating signing keys"},
			Metadata: map[string]any{"type": "task"}, ProfileID: "ops", CreatedAt: at, UpdatedAt: at,
		},
	} {
		if err := db.Driver.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Driver.Embeddings().Put(ctx, "note-vec", []storage.ObjectVector{{ModelID: m.ModelID, Vector: vec}}); err != nil {
		t.Fatal(err)
	}
	rebuildFTSForTest(t, db)
	return db
}

// assertGolden compares got with testdata/find/<name>.golden. Trailing
// blank lines are not compared: the end-of-file hook trims them.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	got = strings.TrimRight(got, "\n") + "\n"
	path := filepath.Join("testdata", "find", name+".golden")
	if *updateFindGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-find-golden)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from %s\n--- got\n%s\n--- want\n%s", name, path, got, want)
	}
}

// find's output in every mode and rendering matches its golden file:
// the move onto POST /api/v1/find keeps the shapes operators script
// against.
func TestFindOutputGolden(t *testing.T) {
	db := setupFindAPI(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"fts.json", []string{"--fts", "--format", "json"}},
		{"hybrid.json", []string{"--format", "json"}},
		{"vector.json", []string{"--semantic", "--format", "json"}},
		{"explain.json", []string{"--explain", "--format", "json"}},
		{"facets.json", []string{"--facets", "--format", "json"}},
		{"filter.json", []string{"--fts", "--meta-type", "task", "--format", "json"}},
		{"empty-hybrid.json", []string{"--meta-type", "nosuch", "--format", "json"}},
		{"empty-explain.json", []string{"--explain", "--meta-type", "nosuch", "--format", "json"}},
		{"empty-hybrid.txt", []string{"--meta-type", "nosuch"}},
		{"fts.txt", []string{"--fts"}},
		{"hybrid.txt", nil},
		{"explain.txt", []string{"--explain"}},
		{"facets.txt", []string{"--fts", "--facets"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := execFind(t, db, append([]string{"find", apiFindQuery}, tc.args...)...)
			if err != nil {
				t.Fatalf("find %v: %v\n%s", tc.args, err, stderr)
			}
			assertGolden(t, tc.name, stdout)
		})
	}

	t.Run("empty.json", func(t *testing.T) {
		stdout, stderr, err := execFind(t, db, "find", "zzqqxx", "--fts", "--format", "json")
		if err != nil {
			t.Fatalf("find: %v\n%s", err, stderr)
		}
		assertGolden(t, "empty.json", stdout)
	})
}
