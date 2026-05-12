package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenizeBulletHyphen(t *testing.T) {
	// Post-T-0565 behaviour: bullet markers are separators, internal hyphens
	// are preserved.
	cases := []struct {
		in          string
		contains    []string
		notContains []string
	}{
		{
			in:       "- auth-flow handles signup",
			contains: []string{"auth-flow", "handles", "signup"},
		},
		{
			in:       "* uri-scheme conflicts with hdl",
			contains: []string{"uri-scheme", "conflicts", "with", "hdl"},
		},
		{
			in:       "Plain prose. No bullets.",
			contains: []string{"plain", "prose", "no", "bullets"},
		},
	}
	for _, tc := range cases {
		got := tokenize(tc.in)
		gotSet := map[string]struct{}{}
		for _, g := range got {
			gotSet[g] = struct{}{}
		}
		for _, want := range tc.contains {
			if _, ok := gotSet[want]; !ok {
				t.Errorf("tokenize(%q): missing %q in %v", tc.in, want, got)
			}
		}
		for _, no := range tc.notContains {
			if _, ok := gotSet[no]; ok {
				t.Errorf("tokenize(%q): unexpectedly contains %q in %v", tc.in, no, got)
			}
		}
	}
}

func TestRunHappyPath(t *testing.T) {
	dir := t.TempDir()

	corpusPath := filepath.Join(dir, "corpus.yaml")
	if err := os.WriteFile(corpusPath, []byte(`
objects:
  - id: doc-auth
    text: "Authentication best practices for OAuth and OIDC"
    tags: [security, authn]
  - id: doc-search
    text: "Search recall regression after FTS tokenizer change"
    tags: [search, recall]
  - id: doc-noise
    text: "Lunch order for tomorrow's planning meeting"
    tags: [misc]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	queriesPath := filepath.Join(dir, "queries.yaml")
	if err := os.WriteFile(queriesPath, []byte(`
k: 2
queries:
  - id: q-auth
    query: "oauth authentication"
    expected_ids: [doc-auth]
  - id: q-recall
    query: "search recall"
    expected_ids: [doc-search]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	req := request{
		Action:    "run",
		Candidate: candidate{Name: "current", Adapter: "ctxt-recall"},
		Input: map[string]any{
			"corpus":  corpusPath,
			"queries": queriesPath,
		},
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(bytes.NewReader(reqData), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v\n%s", err, out.String())
	}
	if got := resp.Metrics["queries_total"]; got != 2 {
		t.Errorf("queries_total = %v, want 2", got)
	}
	if got := resp.Metrics["recall_at_k"]; got < 0.99 {
		t.Errorf("recall_at_k = %v, want ~1.0 on the happy-path fixture", got)
	}
	if !strings.Contains(resp.Output, "mean recall@k") {
		t.Errorf("output missing summary line: %q", resp.Output)
	}
}

func TestRunMissingInputs(t *testing.T) {
	cases := []map[string]any{
		nil,
		{"queries": "x"},
		{"corpus": "x"},
	}
	for i, in := range cases {
		req := request{Action: "run", Input: in}
		data, _ := json.Marshal(req)
		var out bytes.Buffer
		err := run(bytes.NewReader(data), &out)
		if err == nil {
			t.Errorf("case %d: expected error, got nil (out=%s)", i, out.String())
		}
	}
}

func TestRunInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	corpusPath := filepath.Join(dir, "corpus.yaml")
	if err := os.WriteFile(corpusPath, []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	queriesPath := filepath.Join(dir, "queries.yaml")
	if err := os.WriteFile(queriesPath, []byte("queries: []"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := request{Action: "run", Input: map[string]any{
		"corpus": corpusPath, "queries": queriesPath,
	}}
	data, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := run(bytes.NewReader(data), &out); err == nil {
		t.Error("expected error on invalid YAML")
	}
}
