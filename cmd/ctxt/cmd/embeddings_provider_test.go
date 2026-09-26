package cmd

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// providerDoc mirrors `ctxt embeddings provider --format json`.
type providerDoc struct {
	ModelID   string            `json:"model_id"`
	Backend   string            `json:"backend"`
	Model     string            `json:"model"`
	Endpoint  string            `json:"endpoint"`
	APIKeyEnv string            `json:"api_key_env"`
	Dimension int               `json:"dimension"`
	Sources   map[string]string `json:"sources"`
	Fixed     []string          `json:"fixed"`
}

// embeddingTestDB is setupTestDB plus a providers.embedding block in the
// config file.
func embeddingTestDB(t *testing.T, embeddingYAML string) *testDB {
	t.Helper()
	db := setupTestDB(t)
	cfg, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg = append(cfg, []byte("providers:\n  embedding:\n"+embeddingYAML)...)
	if err := os.WriteFile(db.ConfigPath, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	return db
}

func providerJSON(t *testing.T, db *testDB, args ...string) providerDoc {
	t.Helper()
	out, err := db.exec(append([]string{"embeddings", "provider", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("embeddings provider: %v\n%s", err, out)
	}
	var doc providerDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return doc
}

func TestEmbeddingsProvider_DefaultsWhenUnconfigured(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv(embeddings.EnvEndpoint, "") // TestMain points it at a closed port
	doc := providerJSON(t, db)
	if doc.Backend != embeddings.DefaultBackend || doc.Model != embeddings.DefaultModel || doc.Endpoint != embeddings.DefaultEndpoint {
		t.Fatalf("defaults = %+v", doc)
	}
	for _, f := range []string{"backend", "model", "endpoint", "api_key_env", "dimension"} {
		if doc.Sources[f] != "default" {
			t.Errorf("source of %s = %q, want default", f, doc.Sources[f])
		}
	}
}

// Every layer reachable from the CLI, each winning a different field: the
// config file, kit's -c key=value, env and the per-invocation flag. env must
// beat -c even though kit layers -c after env for other keys.
func TestEmbeddingsProvider_LayersPerFieldThroughTheCLI(t *testing.T) {
	db := embeddingTestDB(t,
		"    backend: ollama\n    model: cfg-model\n    endpoint: http://cfg:11434\n    api_key_env: CFG_KEY\n    dimension: 768\n")
	t.Setenv(embeddings.EnvEndpoint, "http://env:11434")
	t.Setenv(embeddings.EnvModel, "env-model")

	doc := providerJSON(
		t, db,
		"-c", "providers.embedding.endpoint=http://c:11434",
		"-c", "providers.embedding.dimension=1024",
		"--embedding-model", "flag-model",
	)
	want := map[string][2]string{
		"backend":     {"ollama", "config"},
		"model":       {"flag-model", "flag"},
		"endpoint":    {"http://env:11434", "env"},
		"api_key_env": {"CFG_KEY", "config"},
		"dimension":   {"1024", "config-override"},
	}
	got := map[string]string{
		"backend": doc.Backend, "model": doc.Model, "endpoint": doc.Endpoint,
		"api_key_env": doc.APIKeyEnv, "dimension": itoa(doc.Dimension),
	}
	for f, w := range want {
		if got[f] != w[0] || doc.Sources[f] != w[1] {
			t.Errorf("%s = %q from %q, want %q from %q", f, got[f], doc.Sources[f], w[0], w[1])
		}
	}
}

func TestEmbeddingsProvider_TextNeverPrintsTheKey(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv(embeddings.EnvEndpoint, "")
	t.Setenv(embeddings.EnvAPIKeyEnv, "CTXT_TEST_EMBED_KEY")
	t.Setenv("CTXT_TEST_EMBED_KEY", "sk-must-not-print")

	out, err := db.exec("embeddings", "provider")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "sk-must-not-print") {
		t.Fatalf("key value printed:\n%s", out)
	}
	for _, want := range []string{"CTXT_TEST_EMBED_KEY", "env", "backend", "ollama", "default"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

const targetedModelID = "ollama-snowflake-arctic-embed2@2026-09-26"

// registeredModelDB is a test DB with one registered snowflake model.
func registeredModelDB(t *testing.T) *testDB {
	t.Helper()
	db := embeddingTestDB(t, "    model: cfg-model\n")
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatal("driver must be *sqlite.Driver")
	}
	if err := registry.New(d.DB()).Register(context.Background(), registry.Model{
		ModelID: targetedModelID, Provider: "ollama", Dimension: 1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://m3:11434"}`,
	}, false); err != nil {
		t.Fatal(err)
	}
	return db
}

// A targeted model_id resolves through its registry entry.
func TestEmbeddingsProvider_TargetedModelUsesRegistryEntry(t *testing.T) {
	db := registeredModelDB(t)
	const id = targetedModelID

	doc := providerJSON(t, db, id, "--embedding-endpoint", "http://127.0.0.1:11555")
	if strings.Join(doc.Fixed, ",") != "backend,model,dimension" {
		t.Errorf("fixed = %v, want backend, model, dimension", doc.Fixed)
	}
	if doc.ModelID != id || doc.Model != "snowflake-arctic-embed2" || doc.Sources["model"] != "registry" {
		t.Errorf("model = %q from %q (model_id %q), want the registry entry", doc.Model, doc.Sources["model"], doc.ModelID)
	}
	if doc.Dimension != 1024 || doc.Sources["dimension"] != "registry" {
		t.Errorf("dimension = %d from %q", doc.Dimension, doc.Sources["dimension"])
	}
	if doc.Endpoint != "http://127.0.0.1:11555" || doc.Sources["endpoint"] != "flag" {
		t.Errorf("endpoint = %q from %q, want the flag", doc.Endpoint, doc.Sources["endpoint"])
	}
}

func TestEmbeddingsProvider_TargetedModelLabelsFixedFields(t *testing.T) {
	db := registeredModelDB(t)
	out, err := db.exec("embeddings", "provider", targetedModelID)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := strings.Count(out, "fixed by registry"); n != 3 {
		t.Errorf("%d rows labelled fixed by registry, want 3 (backend, model, dimension):\n%s", n, out)
	}
}

func TestEmbeddingsProvider_TargetedModelRefusesIdentityOverride(t *testing.T) {
	db := registeredModelDB(t)
	out, err := db.exec("embeddings", "provider", targetedModelID, "--embedding-model", "nomic-embed-text")
	if err == nil || !strings.Contains(err.Error(), "--embedding-model") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	t.Setenv(embeddings.EnvProvider, "stub")
	out, err = db.exec("embeddings", "provider", targetedModelID)
	if err == nil || !strings.Contains(err.Error(), embeddings.EnvProvider) {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestEmbeddingsProvider_UnsupportedBackendFails(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("embeddings", "provider", "--embedding-provider", "openai")
	if err == nil || !strings.Contains(err.Error()+out, "not supported") {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

// find resolves its provider through the same resolver: a bad endpoint
// from the flag fails the search, naming the flag.
func TestFind_EmbeddingFlagsReachTheResolver(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("find", "anything", "--embedding-endpoint", "127.0.0.1:11555")
	if err == nil || !strings.Contains(err.Error(), "--embedding-endpoint") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if _, err := db.exec("find", "anything", "--embedding-provider", "stub"); err != nil {
		t.Fatalf("find with the stub provider: %v", err)
	}
}

func TestEmbeddingFlags_SignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	applyShapeAnnotations()

	for _, c := range []string{"find", "embeddings provider"} {
		target, _, err := rootCmd.Find(strings.Fields(c))
		if err != nil {
			t.Fatalf("find %q: %v", c, err)
		}
		for _, f := range []string{embeddings.FlagProvider, embeddings.FlagModel, embeddings.FlagEndpoint} {
			if target.LocalFlags().Lookup(f) == nil {
				t.Errorf("%s: missing local --%s", c, f)
			}
		}
	}
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "find") || strings.Contains(v.Path, "embeddings") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
