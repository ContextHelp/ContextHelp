package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSuite_HappyPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("subprocess fixture uses POSIX shebang")
	}
	dir := t.TempDir()

	// Build a fake adapter that just echoes a fixed metrics blob.
	adapterSrc := filepath.Join(dir, "ben-adapter-fakerecall.go")
	if err := os.WriteFile(adapterSrc, []byte(`package main
import (
	"encoding/json"
	"io"
	"os"
)
func main() {
	_, _ = io.ReadAll(os.Stdin)
	resp := map[string]any{
		"metrics": map[string]float64{
			"recall_at_k":     0.91,
			"queries_total":   3,
			"queries_passing": 3,
			"queries_failing": 0,
		},
		"output": "fake recall harness",
	}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	adapterBin := filepath.Join(dir, "ben-adapter-fakerecall")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", adapterBin, adapterSrc)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake adapter: %v\n%s", err, out)
	}

	// Spec referencing the fake adapter (no input templating needed).
	specPath := filepath.Join(dir, "fake.ben.yaml")
	if err := os.WriteFile(specPath, []byte(`
name: fake-suite
version: 1
task:
  prompt: "fake"
  input:
    foo: "bar"
candidates:
  - name: only
    adapter: fakerecall
    cmd: "{{input.foo}}"
metrics:
  - recall_at_k
scorer:
  strategy: raw
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Force PATH to find our fake adapter only.
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	if err := runSuite(specPath, &out); err != nil {
		t.Fatalf("runSuite: %v", err)
	}
	var rec runRecord
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatalf("decode runRecord: %v\n%s", err, out.String())
	}
	if rec.Suite != "fake-suite" {
		t.Errorf("Suite = %q, want %q", rec.Suite, "fake-suite")
	}
	if len(rec.Candidates) != 1 {
		t.Fatalf("len(Candidates) = %d, want 1", len(rec.Candidates))
	}
	got := rec.Candidates[0].Metrics["recall_at_k"]
	if got < 0.9 || got > 0.92 {
		t.Errorf("recall_at_k = %v, want ~0.91", got)
	}
	if !strings.Contains(rec.Candidates[0].RawOutput, "fake recall harness") {
		t.Errorf("RawOutput missing expected text: %q", rec.Candidates[0].RawOutput)
	}
}

func TestRunSuite_AdapterMissing(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "missing.ben.yaml")
	if err := os.WriteFile(specPath, []byte(`
name: missing
version: 1
task:
  prompt: "x"
candidates:
  - name: only
    adapter: doesnotexist-foo-bar
    cmd: "x"
scorer:
  strategy: raw
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Empty PATH so the adapter is unfindable.
	t.Setenv("PATH", "")

	var out bytes.Buffer
	if err := runSuite(specPath, &out); err != nil {
		t.Fatalf("runSuite returned err: %v (expected per-candidate error, not run-level)", err)
	}
	var rec runRecord
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Candidates[0].Error == "" {
		t.Error("expected Candidate[0].Error to mention missing adapter")
	}
}

func TestExpandTemplate(t *testing.T) {
	got := expandTemplate("{{input.a}}/{{input.b}}", map[string]string{"a": "X", "b": "Y"})
	if got != "X/Y" {
		t.Errorf("expandTemplate = %q, want %q", got, "X/Y")
	}
}
