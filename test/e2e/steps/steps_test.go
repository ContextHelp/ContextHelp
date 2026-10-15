//go:build unix

// Package steps_test drives an installed pipeline step through a built
// `dpkms serve`: the example pii-mask step is installed from a local
// directory, inserted before every other step of text.short, and an
// ingested object is stored and indexed masked. A step that fails takes
// its object down with it. Skipped under -short.
package steps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/ideacrafterslabs/ctxt/test/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

func moduleRoot(t *testing.T) string {
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

var (
	buildOnce sync.Once
	binDir    string
	buildErr  error
)

// binaries builds dpkms and the pii-mask example once per test binary.
func binaries(t *testing.T) (dpkms, piiMask string) {
	t.Helper()
	root := moduleRoot(t)
	buildOnce.Do(func() {
		binDir, buildErr = os.MkdirTemp(os.Getenv("XDG_CACHE_HOME"), "steps-e2e-")
		if buildErr != nil {
			return
		}
		for out, pkg := range map[string]string{"dpkms": "./cmd/dpkms", "pii-mask": "./examples/steps/pii-mask"} {
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
	return filepath.Join(binDir, "dpkms"), filepath.Join(binDir, "pii-mask")
}

// piiMaskDir is the example step directory with its executable built in.
func piiMaskDir(t *testing.T, exe string) string {
	t.Helper()
	dir := t.TempDir()
	skill, err := os.ReadFile(filepath.Join(moduleRoot(t), "examples", "steps", "pii-mask", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe) // #nosec G304 -- the binary this test built
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "SKILL.md"), skill, 0o600)
	mustWrite(t, filepath.Join(dir, "pii-mask"), b, 0o700)
	return dir
}

// brokenDir is a step that reports a permanent error.
func brokenDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SKILL.md"), []byte("---\nname: broken\nexecution:\n  type: external\n  executable: run.sh\n---\n"), 0o600)
	mustWrite(t, filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\ncat >/dev/null\nprintf '{\"error\":{\"code\":\"NOPE\",\"message\":\"refusing\"}}'\nexit 1\n"), 0o700)
	return dir
}

func mustWrite(t *testing.T, path string, b []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, b, mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, bin string, env []string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- the dpkms built from this checkout
	cmd.Env = env
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

func getJSON(t *testing.T, u string, v any) {
	t.Helper()
	resp, err := http.Get(u) // #nosec G107 -- the test instance's loopback URL
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d %s", u, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("GET %s: %v: %s", u, err, body)
	}
}

func analyze(t *testing.T, base, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content, "type": "text", "source": "e2e"})
	resp, err := http.Post(base+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusAccepted || out["job_id"] == "" {
		t.Fatalf("analyze: %d %v", resp.StatusCode, out)
	}
	return out["job_id"]
}

type job struct {
	Status   string `json:"status"`
	Error    string `json:"error"`
	ResultID string `json:"result_id"`
}

func waitJob(t *testing.T, base, id string) job {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var j job
		getJSON(t, base+"/api/v1/jobs/"+id, &j)
		if j.Status == "completed" || j.Status == "failed" {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still %s", id, j.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ftsTotal is the full-text hit count for q (POST /api/v1/find, fts).
func ftsTotal(t *testing.T, base, q string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": q, "mode": "fts"})
	resp, err := http.Post(base+"/api/v1/find", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("find %q: %d %s", q, resp.StatusCode, raw)
	}
	var out struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Total
}

func TestPIIMaskStepMasksBeforeStorageAndIndex(t *testing.T) {
	dpkms, piiMask := binaries(t)
	shared := t.TempDir()
	base := map[string]any{
		"storage": map[string]any{"type": "sqlite", "path": filepath.Join(shared, "dpkms.db")},
		"steps":   map[string]any{"path": filepath.Join(shared, "steps")},
	}

	// 1. Install both steps on a running instance, from local directories.
	d1 := testutil.StartDpkms(t, testutil.PrivateDpkms(), testutil.WithDpkmsBinary(dpkms), testutil.WithDpkmsConfig(base))
	for name, dir := range map[string]string{"pii-mask": piiMaskDir(t, piiMask), "broken": brokenDir(t)} {
		if out, code := run(t, dpkms, d1.Env, "pipeline", "step", "install", name, "--path", dir, "--server-url", d1.URL); code != 0 {
			t.Fatalf("install %s: exit %d\n%s", name, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(shared, "steps", "pii-mask", "pii-mask")); err != nil {
		t.Fatalf("installed executable: %v", err)
	}
	if err := d1.Stop(); err != nil {
		t.Fatal(err)
	}

	// 2. Restart with pii-mask inserted ahead of every text.short step,
	// and the broken step in text.long.
	cfg := map[string]any{
		"storage": base["storage"],
		"steps":   base["steps"],
		"pipelines": map[string]any{"overrides": map[string]any{
			"text.short": map[string]any{"insert_steps": []any{map[string]any{
				"step": "pii-mask", "before": "typedetector",
				"config": map[string]any{"email_mask": "[email]", "phone_mask": "[phone]"},
			}}},
			"text.long": map[string]any{"insert_steps": []any{map[string]any{
				"step": "broken", "before": "typedetector",
			}}},
		}},
	}
	d2 := testutil.StartDpkms(t, testutil.PrivateDpkms(), testutil.WithDpkmsBinary(dpkms), testutil.WithDpkmsConfig(cfg))
	if out, code := run(t, dpkms, d2.Env, "config", "validate"); code != 0 {
		t.Fatalf("config validate: exit %d\n%s", code, out)
	}

	// 3. Ingest: stored content and the index carry only the masks.
	j := waitJob(t, d2.URL, analyze(t, d2.URL, "Numbat survey: contact jane.doe@example.com or +1 (415) 555-0123."))
	if j.Status != "completed" {
		t.Fatalf("job %s: %s", j.Status, j.Error)
	}
	var obj struct {
		RawContent string `json:"raw_content"`
	}
	getJSON(t, d2.URL+"/api/v1/objects/"+j.ResultID, &obj)
	if want := "Numbat survey: contact [email] or [phone]."; obj.RawContent != want {
		t.Fatalf("stored raw_content %q, want %q", obj.RawContent, want)
	}
	if n := ftsTotal(t, d2.URL, "numbat"); n != 1 {
		t.Errorf("fts numbat: %d hits, want 1", n)
	}
	for _, q := range []string{"jane", "jane.doe@example.com", "example.com", "555"} {
		if n := ftsTotal(t, d2.URL, q); n != 0 {
			t.Errorf("fts %q: %d hits on masked data, want 0", q, n)
		}
	}

	// 4. A failing step fails its object closed: the job fails, nothing
	// is stored.
	long := strings.Repeat("The quokka census continues across the island. ", 15)
	j = waitJob(t, d2.URL, analyze(t, d2.URL, long))
	if j.Status != "failed" || !strings.Contains(j.Error, "broken: step reported NOPE: refusing") {
		t.Errorf("job %s (%q): want failed by the broken step", j.Status, j.Error)
	}
	if n := ftsTotal(t, d2.URL, "quokka"); n != 0 {
		t.Errorf("fts quokka: %d hits; a failed step's object was stored", n)
	}

	// 5. Uninstall is refused while the config inserts the step.
	if out, code := run(t, dpkms, d2.Env, "pipeline", "step", "uninstall", "pii-mask", "--confirm=yes", "--server-url", d2.URL); code == 0 || !strings.Contains(out, "STEP_IN_USE") && !strings.Contains(out, "in use") {
		t.Errorf("uninstall in-use step: exit %d\n%s", code, out)
	}
}

// An override naming a step that is not installed is a config error:
// config validate reports it and serve refuses to start, without a panic.
func TestUnknownInsertedStepIsAConfigError(t *testing.T) {
	dpkms, _ := binaries(t)
	cfg := map[string]any{
		"pipelines": map[string]any{"overrides": map[string]any{
			"text.short": map[string]any{
				"skip_steps":   []any{"taggr"},
				"insert_steps": []any{map[string]any{"step": "pii-mask", "before": "nope"}},
			},
		}},
	}
	d := testutil.StartDpkms(t, testutil.PrivateDpkms(), testutil.WithDpkmsBinary(dpkms))
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "dpkms.yaml")
	b, err := os.ReadFile(d.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	extra, _ := json.Marshal(cfg)
	// JSON is YAML: append the overrides as a flow mapping key.
	mustWrite(t, cfgPath, append(b, []byte("pipelines: "+string(mustField(t, extra, "pipelines"))+"\n")...), 0o600)
	env := append(append([]string{}, d.Env...), "CTXT_CONFIG="+cfgPath)

	out, code := run(t, dpkms, env, "config", "validate")
	if code == 0 {
		t.Fatalf("config validate passed:\n%s", out)
	}
	for _, want := range []string{
		`pipelines.overrides.text.short.skip_steps[0]: step "taggr" is not in pipeline text.short`,
		`pipelines.overrides.text.short.insert_steps[0].step: unknown step "pii-mask"`,
		`pipelines.overrides.text.short.insert_steps[0].before: step "nope" is not in pipeline text.short`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config validate output lacks %q:\n%s", want, out)
		}
	}

	out, code = run(t, dpkms, env, "serve", "--port", "0", "--grpc-port", "0", "--cookie-bridge-port", "0")
	if code == 0 || !strings.Contains(out, `unknown step "pii-mask"`) || strings.Contains(out, "panic") {
		t.Errorf("serve with a bad override: exit %d\n%s", code, out)
	}
}

func mustField(t *testing.T, raw []byte, key string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}
