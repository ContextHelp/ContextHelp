package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"hop.top/kit/go/console/output"
)

// Cassettes under testdata/cassettes/embeddings-register were recorded
// against a real local Ollama with snowflake-arctic-embed2 pulled. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestEmbeddingsRegister ./cmd/ctxt/cmd/
const registerCassettes = "testdata/cassettes/embeddings-register"

const (
	registerModelID  = "ollama-snowflake-arctic-embed2@2026-09-26"
	registerModel    = "snowflake-arctic-embed2"
	registerEndpoint = "http://127.0.0.1:11434"
	// snowflakeDimension is what snowflake-arctic-embed2 returns.
	snowflakeDimension = 1024
)

// registerDoc mirrors `ctxt embeddings register --format json`.
type registerDoc struct {
	ModelID    string            `json:"model_id"`
	Provider   string            `json:"provider"`
	Dimension  int               `json:"dimension"`
	IsDefault  bool              `json:"is_default"`
	ConfigJSON json.RawMessage   `json:"config_json"`
	Sources    map[string]string `json:"sources"`
	Index      string            `json:"index"`
	DryRun     bool              `json:"dry_run"`
}

// recordedOllama routes register's provider through the Ollama cassettes
// for the rest of the test.
func recordedOllama(t *testing.T) *providertest.OllamaCalls {
	t.Helper()
	client, calls := providertest.OllamaClient(t, registerCassettes)
	useRegisterHTTPClient(t, client)
	return calls
}

func useRegisterHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	prev := embeddingsRegisterHTTPClient
	embeddingsRegisterHTTPClient = c
	t.Cleanup(func() { embeddingsRegisterHTTPClient = prev })
}

func registryOf(t *testing.T, db *testDB) *registry.Store {
	t.Helper()
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatal("driver must be *sqlite.Driver")
	}
	return registry.New(d.DB())
}

func registerJSON(t *testing.T, db *testDB, args ...string) registerDoc {
	t.Helper()
	out, err := db.exec(append([]string{"embeddings", "register", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("embeddings register: %v\n%s", err, out)
	}
	var doc registerDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return doc
}

// assertNotRegistered fails when id has a registry row.
func assertNotRegistered(t *testing.T, db *testDB, id string) {
	t.Helper()
	m, err := registryOf(t, db).Get(context.Background(), id)
	if !errors.Is(err, registry.ErrModelNotFound) {
		t.Fatalf("unexpected registry row for %s: %+v (err %v)", id, m, err)
	}
}

// assertClass fails unless err is a kit envelope of class code.
func assertClass(t *testing.T, err error, code string) {
	t.Helper()
	var env *output.Error
	if !errors.As(err, &env) || env.Code != code {
		t.Errorf("error class of %v = %v, want %s", err, env, code)
		return
	}
	if env.SuggestedFix == "" && code != output.CodeUsage {
		t.Errorf("%s error carries no suggested fix: %v", code, err)
	}
}

// registeredIDs lists every registry row's model_id.
func registeredIDs(t *testing.T, db *testDB) []string {
	t.Helper()
	models, err := registryOf(t, db).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ModelID)
	}
	return ids
}

// The dimension is measured from the provider and stored with the resolved
// backend and the resolver's config_json.
func TestEmbeddingsRegister_MeasuresAndStoresDimension(t *testing.T) {
	db := setupTestDB(t)
	calls := recordedOllama(t)

	doc := registerJSON(t, db, registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint)

	if doc.ModelID != registerModelID || doc.Provider != embeddings.BackendOllama || doc.Dimension != snowflakeDimension {
		t.Fatalf("output = %+v, want %s / ollama / %d", doc, registerModelID, snowflakeDimension)
	}
	if doc.IsDefault {
		t.Error("register must not flip the default")
	}
	wantSources := map[string]string{
		"backend": "default", "model": "flag", "endpoint": "flag", "api_key_env": "default", "dimension": "measured",
	}
	for f, w := range wantSources {
		if doc.Sources[f] != w {
			t.Errorf("source of %s = %q, want %q", f, doc.Sources[f], w)
		}
	}
	if got := calls.URLs(); len(got) != 1 || got[0] != registerEndpoint+"/api/embeddings" {
		t.Errorf("probe URLs = %v, want one call to %s/api/embeddings", got, registerEndpoint)
	}

	m, err := registryOf(t, db).Get(context.Background(), registerModelID)
	if err != nil {
		t.Fatalf("registry row: %v", err)
	}
	if m.Provider != embeddings.BackendOllama || m.Dimension != snowflakeDimension || m.IsDefault {
		t.Errorf("stored row = %+v, want provider ollama, dimension %d, not default", m, snowflakeDimension)
	}
	var mc embeddings.ModelConfig
	if err := json.Unmarshal([]byte(m.ConfigJSON), &mc); err != nil {
		t.Fatalf("stored config_json %q: %v", m.ConfigJSON, err)
	}
	want := embeddings.ModelConfig{Backend: "ollama", Model: registerModel, Endpoint: registerEndpoint}
	if mc != want {
		t.Errorf("stored config_json = %+v, want %+v", mc, want)
	}
	if !jsonEqual(t, doc.ConfigJSON, []byte(m.ConfigJSON)) {
		t.Errorf("printed config_json %s differs from stored %s", doc.ConfigJSON, m.ConfigJSON)
	}
}

// Configuration alone is enough: no flag, no model-config file, no $EDITOR.
// A matching providers.embedding.dimension passes its check; the stored
// dimension is still the measured one.
func TestEmbeddingsRegister_FromConfigWithoutFlagsOrEditor(t *testing.T) {
	db := embeddingTestDB(t, "    model: "+registerModel+"\n    endpoint: "+registerEndpoint+"\n    dimension: 1024\n")
	t.Setenv(embeddings.EnvEndpoint, "") // TestMain points it at a closed port
	t.Setenv("EDITOR", "false")
	t.Setenv("VISUAL", "false")
	recordedOllama(t)

	doc := registerJSON(t, db, registerModelID)
	if doc.Dimension != snowflakeDimension || doc.Sources["dimension"] != dimensionSourceMeasured {
		t.Errorf("dimension = %d from %q, want the measured %d", doc.Dimension, doc.Sources["dimension"], snowflakeDimension)
	}
	if doc.Sources["model"] != "config" || doc.Sources["endpoint"] != "config" {
		t.Errorf("sources = %v, want model and endpoint from config", doc.Sources)
	}
}

// providers.embedding.dimension is a check like --dimension, from the
// config file or from -c: a mismatch with the measured dimension is a
// conflict naming both values and the setting, and nothing is registered.
func TestEmbeddingsRegister_ConfigDimensionMismatchFails(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml    string
		args    []string
		setting string
	}{
		"config file": {
			yaml:    "    model: " + registerModel + "\n    endpoint: " + registerEndpoint + "\n    dimension: 768\n",
			setting: "providers.embedding.dimension",
		},
		"-c override": {
			yaml:    "    model: " + registerModel + "\n    endpoint: " + registerEndpoint + "\n",
			args:    []string{"-c", "providers.embedding.dimension=768"},
			setting: "-c providers.embedding.dimension",
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := embeddingTestDB(t, tc.yaml)
			t.Setenv(embeddings.EnvEndpoint, "")
			calls := recordedOllama(t)

			out, err := db.exec(append(tc.args, "embeddings", "register", registerModelID)...)
			if err == nil {
				t.Fatalf("register with a configured dimension of 768 against a 1024-d model succeeded:\n%s", out)
			}
			for _, want := range []string{"768", "1024", tc.setting} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
			}
			assertClass(t, err, output.CodeConflict)
			assertNotRegistered(t, db, registerModelID)
			if len(calls.URLs()) != 1 {
				t.Errorf("provider calls = %v, want the one probe", calls.URLs())
			}
		})
	}
}

// A configured dimension describes the configured model. When a higher
// layer picks another model it does not apply; --dimension, a per-call
// check, takes its place.
func TestEmbeddingsRegister_ConfigDimensionScope(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml string
		args []string
	}{
		"flag picks another model": {
			yaml: "    model: nomic-embed-text\n    endpoint: " + registerEndpoint + "\n    dimension: 768\n",
			args: []string{"--embedding-model", registerModel},
		},
		"--dimension replaces the config check": {
			yaml: "    model: " + registerModel + "\n    endpoint: " + registerEndpoint + "\n    dimension: 768\n",
			args: []string{"--dimension", "1024"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := embeddingTestDB(t, tc.yaml)
			t.Setenv(embeddings.EnvEndpoint, "")
			recordedOllama(t)

			doc := registerJSON(t, db, append([]string{registerModelID}, tc.args...)...)
			if doc.Dimension != snowflakeDimension {
				t.Errorf("dimension = %d, want the measured %d", doc.Dimension, snowflakeDimension)
			}
		})
	}
}

// --dimension is a check against the measured value, never its source.
func TestEmbeddingsRegister_DimensionFlagMismatchFails(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)

	out, err := db.exec("embeddings", "register", registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint, "--dimension", "768")
	if err == nil {
		t.Fatalf("register with --dimension 768 against a 1024-d model succeeded:\n%s", out)
	}
	for _, want := range []string{"768", "1024"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	assertClass(t, err, output.CodeConflict)
	assertNotRegistered(t, db, registerModelID)
}

func TestEmbeddingsRegister_DimensionFlagMatchPasses(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)

	doc := registerJSON(t, db, registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint, "--dimension", "1024")
	if doc.Dimension != snowflakeDimension {
		t.Fatalf("dimension = %d, want %d", doc.Dimension, snowflakeDimension)
	}
}

// An unreachable provider fails with the endpoint and a fix, and registers
// nothing. Port 1 (TestMain's CTXT_EMBEDDING_ENDPOINT) refuses connections.
func TestEmbeddingsRegister_UnreachableProviderRegistersNothing(t *testing.T) {
	db := setupTestDB(t)
	useRegisterHTTPClient(t, nil) // the real transport: a genuine refusal

	out, err := db.exec("embeddings", "register", registerModelID, "--embedding-model", registerModel)
	if err == nil {
		t.Fatalf("register against a closed port succeeded:\n%s", out)
	}
	for _, want := range []string{"http://127.0.0.1:1", "--embedding-endpoint", "nothing was registered"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	assertClass(t, err, output.CodePrerequisite)
	assertNotRegistered(t, db, registerModelID)
}

// A reachable provider that cannot embed (model not pulled) also registers
// nothing, and the error says how to fix it.
func TestEmbeddingsRegister_ModelNotPulledRegistersNothing(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)

	out, err := db.exec("embeddings", "register", "ollama-ctxt-missing-embed-model@2026-09-26",
		"--embedding-model", "ctxt-missing-embed-model", "--embedding-endpoint", registerEndpoint)
	if err == nil {
		t.Fatalf("register with an unpulled model succeeded:\n%s", out)
	}
	for _, want := range []string{registerEndpoint, "ollama pull ctxt-missing-embed-model", "not found"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	assertClass(t, err, output.CodePrerequisite)
	assertNotRegistered(t, db, "ollama-ctxt-missing-embed-model@2026-09-26")
}

// The stub backend returns no vector: there is no dimension to measure.
func TestEmbeddingsRegister_EmptyVectorRegistersNothing(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("embeddings", "register", "stub@2026-09-26", "--embedding-provider", "stub")
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	assertClass(t, err, output.CodePrerequisite)
	assertNotRegistered(t, db, "stub@2026-09-26")
}

func TestEmbeddingsRegister_InvalidModelIDRejectedBeforeProbe(t *testing.T) {
	db := setupTestDB(t)
	calls := recordedOllama(t)
	before := registeredIDs(t, db)

	for _, id := range []string{"bad id", "x'); DROP TABLE objects; --", ".leading-dot"} {
		out, err := db.exec("embeddings", "register", id,
			"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint)
		if err == nil || !strings.Contains(err.Error(), "invalid embedding model_id") {
			t.Errorf("register %q: err = %v\n%s", id, err, out)
		}
		assertClass(t, err, output.CodeUsage)
	}
	if got := calls.URLs(); len(got) != 0 {
		t.Errorf("provider probed for an invalid id: %v", got)
	}
	if after := registeredIDs(t, db); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("registry changed: %v -> %v", before, after)
	}
}

func TestEmbeddingsRegister_DuplicateFailsWithoutReprobing(t *testing.T) {
	db := setupTestDB(t)
	calls := recordedOllama(t)
	args := []string{
		"embeddings", "register", registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint,
	}

	if out, err := db.exec(args...); err != nil {
		t.Fatalf("first register: %v\n%s", err, out)
	}
	out, err := db.exec(args...)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("second register: err = %v\n%s", err, out)
	}
	assertClass(t, err, output.CodeConflict)
	if got := calls.URLs(); len(got) != 1 {
		t.Errorf("provider calls = %v, want exactly one (the duplicate must fail before probing)", got)
	}
}

// assertNoIndex fails when id has a per-model index table.
func assertNoIndex(t *testing.T, db *testDB, id string) {
	t.Helper()
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatal("driver must be *sqlite.Driver")
	}
	var n int
	if err := d.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE ?`,
		"vec_"+storage.EmbeddingIndexName(id)+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%s has %d index objects after a dry run", id, n)
	}
}

// --dry-run resolves the provider and probes the dimension like a real run,
// and writes nothing: no registry row, no index. A real register afterwards
// succeeds.
func TestEmbeddingsRegister_DryRunWritesNothing(t *testing.T) {
	db := setupTestDB(t)
	calls := recordedOllama(t)
	args := []string{registerModelID, "--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint}

	doc := registerJSON(t, db, append(args, "--dry-run")...)
	if !doc.DryRun || doc.ModelID != registerModelID || doc.Provider != embeddings.BackendOllama || doc.Dimension != snowflakeDimension {
		t.Errorf("dry-run output = %+v, want dry_run, %s / ollama / %d", doc, registerModelID, snowflakeDimension)
	}
	if doc.Index != registerIndexSkipped || doc.Sources["dimension"] != dimensionSourceMeasured {
		t.Errorf("dry-run index %q, dimension source %q; want %q, %q",
			doc.Index, doc.Sources["dimension"], registerIndexSkipped, dimensionSourceMeasured)
	}
	assertNotRegistered(t, db, registerModelID)
	assertNoIndex(t, db, registerModelID)

	out, err := db.exec(append([]string{"--dry-run", "embeddings", "register"}, args...)...)
	if err != nil {
		t.Fatalf("text dry run: %v\n%s", err, out)
	}
	for _, want := range []string{"Dry run: would register " + registerModelID, "Nothing was registered", "1024", "measured"} {
		if !strings.Contains(out, want) {
			t.Errorf("text dry run missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Registered ") || strings.Contains(out, "set-default") {
		t.Errorf("text dry run reads like a registration:\n%s", out)
	}
	assertNotRegistered(t, db, registerModelID)
	assertNoIndex(t, db, registerModelID)
	if got := calls.URLs(); len(got) != 2 {
		t.Errorf("provider calls = %v, want one probe per dry run", got)
	}

	real := registerJSON(t, db, args...)
	if real.DryRun || real.Index != registerIndexReady {
		t.Errorf("register after dry runs = %+v, want a real registration with a ready index", real)
	}
}

// A dry run applies every check a real run does: a duplicate or a dimension
// mismatch fails the same way.
func TestEmbeddingsRegister_DryRunKeepsChecks(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)
	args := []string{"embeddings", "register", registerModelID, "--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint}

	out, err := db.exec(append(args, "--dry-run", "--dimension", "768")...)
	if err == nil {
		t.Fatalf("dry run with --dimension 768 against a 1024-d model succeeded:\n%s", out)
	}
	assertClass(t, err, output.CodeConflict)
	assertNotRegistered(t, db, registerModelID)

	if out, err := db.exec(args...); err != nil {
		t.Fatalf("register: %v\n%s", err, out)
	}
	out, err = db.exec(append(args, "--dry-run")...)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("dry run of a registered model: err = %v\n%s", err, out)
	}
	assertClass(t, err, output.CodeConflict)
}

// config_json names the key variable, never its value; neither does the
// output.
func TestEmbeddingsRegister_ConfigJSONCarriesNoSecret(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)
	const keyValue = "sk-register-must-not-store"
	t.Setenv(embeddings.EnvAPIKeyEnv, "CTXT_TEST_EMBED_KEY")
	t.Setenv("CTXT_TEST_EMBED_KEY", keyValue)

	for _, format := range []string{"json", "text"} {
		id := registerModelID + "-" + format
		args := []string{
			"embeddings", "register", id,
			"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint,
		}
		if format == "json" {
			args = append(args, "--format", "json")
		}
		out, err := db.exec(args...)
		if err != nil {
			t.Fatalf("register (%s): %v\n%s", format, err, out)
		}
		if strings.Contains(out, keyValue) {
			t.Errorf("%s output contains the key value:\n%s", format, out)
		}
		m, err := registryOf(t, db).Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(m.ConfigJSON, keyValue) {
			t.Errorf("stored config_json contains the key value: %s", m.ConfigJSON)
		}
		var mc embeddings.ModelConfig
		if err := json.Unmarshal([]byte(m.ConfigJSON), &mc); err != nil {
			t.Fatal(err)
		}
		if mc.APIKeyEnv != "CTXT_TEST_EMBED_KEY" {
			t.Errorf("config_json api_key_env = %q, want the variable name", mc.APIKeyEnv)
		}
		var raw map[string]any
		_ = json.Unmarshal([]byte(m.ConfigJSON), &raw)
		for k := range raw {
			switch k {
			case "backend", "model", "endpoint", "api_key_env":
			default:
				t.Errorf("config_json has unexpected key %q: %s", k, m.ConfigJSON)
			}
		}
	}
}

// The index status the command reports agrees with the storage backend:
// "ready" means the model's index answers a search.
func TestEmbeddingsRegister_IndexStatusMatchesStore(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)

	doc := registerJSON(t, db, registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint)

	q := storage.VectorQuery{ModelID: registerModelID, Vector: make([]float32, snowflakeDimension), TopK: 1}
	q.Vector[0] = 1
	_, searchErr := db.Driver.Embeddings().Search(context.Background(), q)
	switch doc.Index {
	case registerIndexReady:
		if searchErr != nil {
			t.Errorf("index reported %q but Search fails: %v", doc.Index, searchErr)
		}
	case registerIndexUnsupported:
		if !errors.Is(searchErr, errors.ErrUnsupported) {
			t.Errorf("index reported %q but the store supports it (Search err = %v)", doc.Index, searchErr)
		}
	default:
		t.Errorf("index = %q, want %q or %q", doc.Index, registerIndexReady, registerIndexUnsupported)
	}
}

func TestEmbeddingsRegister_TextOutput(t *testing.T) {
	db := setupTestDB(t)
	recordedOllama(t)

	out, err := db.exec("embeddings", "register", registerModelID,
		"--embedding-model", registerModel, "--embedding-endpoint", registerEndpoint)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Registered " + registerModelID, "1024", "measured", registerModel, "flag", "set-default"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// The pre-probe flags are gone: the provider comes from the resolver and
// the default flip from set-default.
func TestEmbeddingsRegister_RemovedFlags(t *testing.T) {
	db := setupTestDB(t)
	for _, flag := range []string{"--model-config=x.json", "--provider=openai", "--make-default"} {
		out, err := db.exec("embeddings", "register", registerModelID, flag)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%s: err = %v\n%s", flag, err, out)
		}
	}
}

func TestEmbeddingsRegister_SignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	applyShapeAnnotations()

	target, _, err := rootCmd.Find([]string{"embeddings", "register"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{embeddings.FlagProvider, embeddings.FlagModel, embeddings.FlagEndpoint, "dimension"} {
		if target.LocalFlags().Lookup(f) == nil {
			t.Errorf("embeddings register: missing local --%s", f)
		}
	}
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "embeddings") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}
