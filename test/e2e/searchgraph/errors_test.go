//go:build e2e && unix

package searchgraph_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// usageCase is one invalid --graph invocation. flags go after the query;
// each case also runs with them before it.
type usageCase struct {
	name  string
	flags []string
	// msg is a fragment of the error message.
	msg string
}

// Every invalid combination exits 2 with the USAGE error code, in either
// flag position, and fails before searching or writing anything.
func TestUsageErrors(t *testing.T) {
	e := newEnv(t)
	out := func(name string) string { return filepath.Join(e.dir, "out", name) }
	cases := []usageCase{
		// --graph traces the hybrid search: other modes and shapes conflict.
		{"fts", []string{"--graph", "--fts"}, "cannot be combined with --fts"},
		{"semantic", []string{"--graph", "--semantic"}, "cannot be combined with --semantic"},
		{"explain", []string{"--graph", "--explain"}, "cannot be combined with --explain"},
		{"facets", []string{"--graph", "--facets"}, "cannot be combined with --facets"},
		// Tuning flags mean nothing without --graph.
		{"max-nodes without graph", []string{"--graph-max-nodes", "5"}, "--graph-max-nodes has no effect without --graph"},
		{"max-edges without graph", []string{"--graph-max-edges", "5"}, "--graph-max-edges has no effect without --graph"},
		{"similar without graph", []string{"--graph-similar"}, "--graph-similar has no effect without --graph"},
		{"threshold without graph", []string{"--graph-similar-threshold", "0.5"}, "--graph-similar-threshold has no effect without --graph"},
		{"no-browser without graph", []string{"--no-browser"}, "--no-browser has no effect without --graph"},
		{"idle without graph", []string{"--graph-idle-timeout", "1s"}, "--graph-idle-timeout has no effect without --graph"},
		{"threshold without similar", []string{"--graph", "--graph-similar-threshold", "0.5"}, "no effect without --graph-similar"},
		// Out-of-range tuning.
		{"max-nodes zero", []string{"--graph", "--graph-max-nodes", "0"}, "--graph-max-nodes must be at least 1"},
		{"max-edges zero", []string{"--graph", "--graph-max-edges", "0"}, "--graph-max-edges must be at least 1"},
		{"threshold zero", []string{"--graph", "--graph-similar", "--graph-similar-threshold", "0"}, "must be in (0,1]"},
		{"threshold above one", []string{"--graph", "--graph-similar", "--graph-similar-threshold", "1.5"}, "must be in (0,1]"},
		{"idle zero", []string{"--graph", "--graph-idle-timeout", "0s"}, "--graph-idle-timeout must be positive"},
		// Viewer-only flags with a non-viewer destination.
		{"no-browser with json", []string{"--graph", "--no-browser", "--format", "json"}, "--no-browser only applies to the interactive viewer"},
		{"idle with file", []string{"--graph", "--graph-idle-timeout", "1s", "-o", out("g.json")}, "--graph-idle-timeout only applies to the interactive viewer"},
		// Formats without a graph rendering.
		{"csv", []string{"--graph", "--format", "csv"}, "--graph has no csv rendering"},
		{"text", []string{"--graph", "--format", "text"}, "--graph has no text rendering"},
		// -o must name a graph format, agreeing with any --format.
		{"unknown extension", []string{"--graph", "-o", out("g.txt")}, `extension ".txt" names no graph format`},
		{"missing extension", []string{"--graph", "-o", out("graph")}, "no file extension to pick a graph format"},
		{"format contradicts extension", []string{"--graph", "--format", "json", "-o", out("g.graphml")}, "--format json does not match -o"},
		{"yaml contradicts html", []string{"--graph", "--format", "yaml", "-o", out("g.html")}, "--format yaml does not match -o"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, argv := range [][]string{
				append([]string{"find", "deployment"}, tc.flags...),
				append(append([]string{"find"}, tc.flags...), "deployment"),
			} {
				assertUsageError(t, e.run(argv...), tc.msg, argv)
			}
			if _, err := os.Stat(filepath.Join(e.dir, "out")); !os.IsNotExist(err) {
				t.Errorf("a failed run created output: %v", err)
			}
		})
	}
}

// The error envelope carries the stable USAGE code in every rendering.
func TestUsageErrors_Envelope(t *testing.T) {
	e := newEnv(t)
	flags := []string{"--graph", "--fts"}

	r := e.run(append([]string{"find", "deployment", "--format", "json"}, flags...)...)
	var env map[string]any
	if err := json.Unmarshal([]byte(r.stderr), &env); err != nil {
		t.Fatalf("--format json: stderr is no JSON error envelope: %v\n%s", err, r)
	}
	assertEnvelope(t, "json", env, r)

	r = e.run(append([]string{"find", "deployment", "--format", "yaml"}, flags...)...)
	env = nil
	if err := yaml.Unmarshal([]byte(r.stderr), &env); err != nil {
		t.Fatalf("--format yaml: stderr is no YAML error envelope: %v\n%s", err, r)
	}
	assertEnvelope(t, "yaml", env, r)
}

func assertEnvelope(t *testing.T, format string, env map[string]any, r result) {
	t.Helper()
	if r.code != 2 || r.stdout != "" {
		t.Errorf("%s: want exit 2 and empty stdout\n%s", format, r)
	}
	if env["code"] != "USAGE" {
		t.Errorf("%s: envelope code = %v, want USAGE", format, env["code"])
	}
	if fmt.Sprint(env["exit_code"]) != "2" {
		t.Errorf("%s: envelope exit_code = %v, want 2", format, env["exit_code"])
	}
	if !strings.Contains(str(env["message"]), "--fts") {
		t.Errorf("%s: envelope message = %v", format, env["message"])
	}
}

// assertUsageError checks exit 2, nothing on stdout, and the human
// envelope: the USAGE code, then the message.
func assertUsageError(t *testing.T, r result, msg string, argv []string) {
	t.Helper()
	if r.code != 2 {
		t.Errorf("%v: exit %d, want 2\n%s", argv, r.code, r)
	}
	if r.stdout != "" {
		t.Errorf("%v: stdout not empty\n%s", argv, r)
	}
	if !strings.HasPrefix(r.stderr, "USAGE: ") && !strings.Contains(r.stderr, `"code": "USAGE"`) && !strings.HasPrefix(r.stderr, "code: USAGE") {
		t.Errorf("%v: stderr lacks the USAGE code\n%s", argv, r)
	}
	if !strings.Contains(r.stderr, msg) {
		t.Errorf("%v: stderr lacks %q\n%s", argv, msg, r)
	}
}
