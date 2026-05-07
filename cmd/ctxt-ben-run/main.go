// Command ctxt-ben-run is a minimal, ben-compatible suite runner.
//
// Why this exists:
//   - hop.top/ben at the pinned SHA does not currently build against the
//     restructured hop.top/kit layout (kit moved packages under go/<area>/
//     while ben still imports the flat hop.top/kit/<pkg> paths). This is
//     an upstream code-debt flag, not a defect in this PR.
//   - The ctxt repo needs to enforce the ADR-070/071 recall-floor gate
//     today, on every PR, without waiting for the ben build fix.
//
// What this does:
//   - Reads a ben suite YAML (suites/*.ben.yaml). Same format ben uses;
//     when ben's build is fixed we switch the Makefile to invoke
//     `ben run --suite ...` directly and remove this binary.
//   - Resolves binary plugins by prepending $(BUILD_DIR) to PATH, looking
//     for `ben-adapter-<adapter>` executables (same convention as ben).
//   - Emits a JSON shape compatible with `ben run --format json` so the
//     downstream Makefile / CI / scripts/ben-floor.sh script doesn't need
//     to care which runner produced the file.
//
// What this does NOT do:
//   - No scoring or ranking — suites in this repo all use scorer:raw and
//     post-process via scripts/ben-floor.sh. Adding scoring here would
//     duplicate logic ben already has and is meant to be temporary.
//   - No registry push, no `ben query`, no `ben compare`. Out of scope.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type benSpec struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Version     int            `yaml:"version"`
	Task        benTask        `yaml:"task"`
	Candidates  []benCandidate `yaml:"candidates"`
	Metrics     []string       `yaml:"metrics"`
	Scorer      benScorer      `yaml:"scorer"`
}

type benTask struct {
	Prompt string            `yaml:"prompt"`
	Input  map[string]string `yaml:"input"`
}

type benCandidate struct {
	Name    string `yaml:"name"`
	Adapter string `yaml:"adapter"`
	Cmd     string `yaml:"cmd"`
	Model   string `yaml:"model"`
}

type benScorer struct {
	Strategy string             `yaml:"strategy"`
	Weights  map[string]float64 `yaml:"weights"`
}

// adapterRequest mirrors hop.top/ben/internal/adapter.binaryAdapterRequest
// (v1 of the stdio JSON protocol). Decoupled from ben's internal package
// on purpose so this runner stays buildable when ben drifts.
type adapterRequest struct {
	Action    string         `json:"action"`
	Candidate benCandidate   `json:"candidate"`
	Input     map[string]any `json:"input"`
}

type adapterResponse struct {
	Metrics map[string]float64 `json:"metrics"`
	Output  string             `json:"output"`
}

// runRecord mirrors hop.top/ben/internal/run.Run (the JSON shape ben emits
// with --format json). Field names match exactly so downstream consumers
// can be ben-bound without caring which runner wrote the file.
type runRecord struct {
	RunID        string            `json:"run_id"`
	Suite        string            `json:"suite"`
	SuiteVersion int               `json:"suite_version"`
	Timestamp    time.Time         `json:"timestamp"`
	Scorer       benScorer         `json:"scorer"`
	Candidates   []candidateResult `json:"candidates"`
	Winner       *string           `json:"winner"`
	Metadata     map[string]string `json:"metadata"`
}

type candidateResult struct {
	Name      string             `json:"name"`
	Metrics   map[string]float64 `json:"metrics"`
	Score     *float64           `json:"score"`
	Rank      *int               `json:"rank"`
	RawOutput string             `json:"raw_output"`
	Error     string             `json:"error,omitempty"`
}

func main() {
	// Accept ben's CLI shape: `ctxt-ben-run run --suite path --format json`.
	// The leading `run` arg is optional (treated as the only verb we know).
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}

	fs := flag.NewFlagSet("ctxt-ben-run", flag.ExitOnError)
	var suitePath string
	var format string
	fs.StringVar(&suitePath, "suite", "", "path to a ben suite YAML")
	fs.StringVar(&format, "format", "json", "output format (only `json` supported)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	if suitePath == "" {
		fmt.Fprintln(os.Stderr, "usage: ctxt-ben-run --suite <path> [--format json]")
		os.Exit(2)
	}
	if format != "json" {
		fmt.Fprintf(os.Stderr, "ctxt-ben-run: only --format json is supported, got %q\n", format)
		os.Exit(2)
	}

	if err := runSuite(suitePath, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ctxt-ben-run: %v\n", err)
		os.Exit(1)
	}
}

func runSuite(suitePath string, out io.Writer) error {
	specData, err := os.ReadFile(suitePath) //nolint:gosec // operator-supplied suite path
	if err != nil {
		return fmt.Errorf("read suite: %w", err)
	}
	var spec benSpec
	if err := yaml.Unmarshal(specData, &spec); err != nil {
		return fmt.Errorf("parse suite: %w", err)
	}
	if spec.Name == "" {
		return fmt.Errorf("suite missing name")
	}
	if len(spec.Candidates) == 0 {
		return fmt.Errorf("suite has no candidates")
	}

	ctx := context.Background()

	candidates := make([]candidateResult, 0, len(spec.Candidates))
	for _, c := range spec.Candidates {
		// Expand {{input.KEY}} in cmd just like ben does.
		expandedCmd := expandTemplate(c.Cmd, spec.Task.Input)
		c.Cmd = expandedCmd

		inputAny := make(map[string]any, len(spec.Task.Input))
		for k, v := range spec.Task.Input {
			inputAny[k] = v
		}

		req := adapterRequest{
			Action:    "run",
			Candidate: c,
			Input:     inputAny,
		}
		reqData, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal request for candidate %q: %w", c.Name, err)
		}

		binPath, err := resolveAdapterBinary(c.Adapter)
		if err != nil {
			candidates = append(candidates, candidateResult{
				Name:    c.Name,
				Metrics: map[string]float64{},
				Error:   err.Error(),
			})
			continue
		}
		cmd := exec.CommandContext(ctx, binPath) //nolint:gosec // binPath resolved from PATH
		cmd.Stdin = strings.NewReader(string(reqData))
		cmd.Stderr = os.Stderr

		raw, runErr := cmd.Output()
		cr := candidateResult{
			Name:    c.Name,
			Metrics: map[string]float64{},
		}
		if runErr != nil {
			cr.Error = fmt.Sprintf("adapter %q exec failed: %v", c.Adapter, runErr)
		} else {
			var resp adapterResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				cr.Error = fmt.Sprintf("decode response: %v", err)
			} else {
				cr.RawOutput = resp.Output
				for k, v := range resp.Metrics {
					cr.Metrics[k] = v
				}
			}
		}
		candidates = append(candidates, cr)
	}

	hostname, _ := os.Hostname()
	rec := runRecord{
		RunID:        fmt.Sprintf("ctxt-ben-run-%d", time.Now().UnixNano()),
		Suite:        spec.Name,
		SuiteVersion: spec.Version,
		Timestamp:    time.Now().UTC(),
		Scorer:       spec.Scorer,
		Candidates:   candidates,
		Metadata: map[string]string{
			"host":              hostname,
			"runner":            "ctxt-ben-run",
			"runner_compat_for": "hop.top/ben --format json",
		},
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rec)
}

// resolveAdapterBinary searches PATH for `ben-adapter-<name>` (same
// convention hop.top/ben uses). Returns the absolute path or an error if
// nothing executable is found.
func resolveAdapterBinary(adapter string) (string, error) {
	exe := "ben-adapter-" + adapter
	path, err := exec.LookPath(exe)
	if err != nil {
		return "", fmt.Errorf("adapter binary %q not on PATH (looked for %q)", adapter, exe)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func expandTemplate(tmpl string, input map[string]string) string {
	for k, v := range input {
		tmpl = strings.ReplaceAll(tmpl, "{{input."+k+"}}", v)
	}
	return tmpl
}
