package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
)

// newRegisterBinaryEnv is a fixture instance for the built ctxt alone: an
// isolated HOME/XDG tree whose config points at a temp SQLite database and
// at the register cassettes through a local proxy. No dpkms runs; ctxt's
// server.url is a closed port.
func newRegisterBinaryEnv(t *testing.T) *journeyEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bin := e2eBinary(t)
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
	ollama, calls := startRecordedOllama(t, registerCassettes)
	e := &journeyEnv{
		t: t, ctxt: bin, dirs: dirs, env: journeyEnviron(dirs), store: sqliteJourneyStore(t),
		ollama: ollama, calls: calls, log: &syncBuffer{},
	}
	e.writeConfig("")
	return e
}

// On the built binary, the kit global --dry-run makes register probe and
// print what it would register, in either flag position and both output
// formats, and write nothing: no registry row, no index. A real register
// afterwards succeeds.
func TestEmbeddingsRegister_DryRunBinary(t *testing.T) {
	e := newRegisterBinaryEnv(t)
	model := []string{"--embedding-model", registerModel}

	for name, args := range map[string][]string{
		"flag after":  append([]string{"embeddings", "register", registerModelID, "--dry-run"}, model...),
		"flag before": append([]string{"--dry-run", "embeddings", "register", registerModelID}, model...),
	} {
		out := e.ok(args...)
		for _, want := range []string{"Dry run: would register " + registerModelID, "Nothing was registered", "1024", "measured"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output missing %q:\n%s", name, want, out)
			}
		}
	}
	var doc registerDoc
	raw := e.ok(append([]string{"embeddings", "register", registerModelID, "--dry-run", "--format", "json"}, model...)...)
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if !doc.DryRun || doc.Dimension != snowflakeDimension || doc.Provider != embeddings.BackendOllama || doc.Index != registerIndexSkipped {
		t.Errorf("json dry run = %+v, want dry_run, dimension %d, ollama, index %q", doc, snowflakeDimension, registerIndexSkipped)
	}
	if got := len(e.calls.URLs()); got != 3 {
		t.Errorf("provider calls = %d, want one probe per dry run", got)
	}

	e.store.connect(t)
	if n := e.store.count(t, `SELECT COUNT(*) FROM embedding_models WHERE model_id = ?`, registerModelID); n != 0 {
		t.Fatalf("dry runs left %d registry rows", n)
	}
	if e.store.indexExists(t, registerModelID) || e.store.signatureExists(t, registerModelID) {
		t.Fatal("dry runs built an index")
	}

	e.ok(append([]string{"embeddings", "register", registerModelID}, model...)...)
	if n := e.store.count(t, `SELECT COUNT(*) FROM embedding_models WHERE model_id = ?`, registerModelID); n != 1 {
		t.Fatalf("real register after dry runs: %d registry rows, want 1", n)
	}
	if !e.store.indexExists(t, registerModelID) {
		t.Fatal("real register after dry runs built no index")
	}
}
