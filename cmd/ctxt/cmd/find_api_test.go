package cmd

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// goldenFloatDigits is how many significant digits of each JSON float
// assertGolden compares. Scores come out of float math (FTS5 bm25, RRF)
// whose last bits differ between platforms: macOS and Linux disagree in
// the 16th digit of the same fts_score. Nine digits keep any real score
// change visible.
const goldenFloatDigits = 9

// jsonNumberToken matches a JSON number token at the start of its input.
var jsonNumberToken = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?`)

// roundJSONFloats rounds every float token outside a string in JSON text
// s to goldenFloatDigits significant digits, leaving every other byte as
// it is. Integers stay exact.
func roundJSONFloats(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '-' || (c >= '0' && c <= '9'):
			if tok := jsonNumberToken.FindString(s[i:]); tok != "" {
				out := tok
				if strings.ContainsAny(tok, ".eE") {
					if f, err := strconv.ParseFloat(tok, 64); err == nil {
						out = strconv.FormatFloat(f, 'g', goldenFloatDigits, 64)
					}
				}
				b.WriteString(out)
				i += len(tok)
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// assertGolden compares got with testdata/find/<name>.golden. Trailing
// blank lines are not compared: the end-of-file hook trims them. In a
// .json golden, floats compare at goldenFloatDigits significant digits.
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
	cmpGot, cmpWant := got, string(want)
	if strings.HasSuffix(name, ".json") {
		cmpGot, cmpWant = roundJSONFloats(cmpGot), roundJSONFloats(cmpWant)
	}
	if cmpWant != cmpGot {
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

// The golden comparison absorbs the last-digit float drift between
// platforms and nothing more: a score off in its third decimal, integers
// and numbers inside strings still compare exactly.
func TestRoundJSONFloats(t *testing.T) {
	const linux, darwin = `{"fts_score": -0.8827586549672106}`, `{"fts_score": -0.8827586549672107}`
	if roundJSONFloats(linux) != roundJSONFloats(darwin) {
		t.Errorf("platform drift not absorbed: %s vs %s", roundJSONFloats(linux), roundJSONFloats(darwin))
	}
	for _, other := range []string{
		`{"fts_score": -0.8837586549672107}`,
		`{"fts_score": -0.8827587549672107}`,
		`{"fts_score": 0.8827586549672107}`,
	} {
		if roundJSONFloats(other) == roundJSONFloats(darwin) {
			t.Errorf("%s compares equal to %s", other, darwin)
		}
	}
	for in, want := range map[string]string{
		`{"n": 42, "f": 0.00819672131147541}`:     `{"n": 42, "f": 0.00819672131}`,
		`{"s": "v1.23456789012", "f": 1e-20}`:     `{"s": "v1.23456789012", "f": 1e-20}`,
		`{"s": "a\"b 1.23456789012", "f": 2.5E3}`: `{"s": "a\"b 1.23456789012", "f": 2500}`,
		`[-1, -0.1234567891234, "\\", 3.0]`:       `[-1, -0.123456789, "\\", 3]`,
	} {
		if got := roundJSONFloats(in); got != want {
			t.Errorf("roundJSONFloats(%s) = %s, want %s", in, got, want)
		}
	}
}
