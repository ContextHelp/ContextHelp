// Package eval provides fixtures and helpers for evaluating pipeline prompt quality.
//
// Usage: run eval tests via TestEvalSuite in eval_test.go.
// Tests skip automatically when LLM API keys are absent.
package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Fixture is a single eval test case loaded from a JSON fixture file.
type Fixture struct {
	// ID uniquely identifies the fixture within its file.
	ID string `json:"id"`
	// Pipeline is the expected pipeline name (e.g. "text.short").
	Pipeline string `json:"pipeline"`
	// Input is the raw content string or file path (when IsFile is true).
	Input string `json:"input"`
	// IsFile indicates Input is a path to a file, not inline content.
	IsFile bool `json:"is_file"`
	// Expect holds per-fixture quality expectations.
	Expect Expectations `json:"expect"`
}

// Expectations defines measurable quality criteria for a fixture.
type Expectations struct {
	// PipelineSelected is the pipeline the selector must choose.
	PipelineSelected string `json:"pipeline_selected,omitempty"`
	// MinTags is the minimum number of tags required.
	MinTags int `json:"min_tags,omitempty"`
	// TagContains lists tag labels that must appear in the output.
	TagContains []string `json:"tag_contains,omitempty"`
	// MinMentions is the minimum number of mentions required.
	MinMentions int `json:"min_mentions,omitempty"`
	// MentionContains lists @namespace.slug strings that must appear.
	MentionContains []string `json:"mention_contains,omitempty"`
	// NoHallucinatedMentions: when true, all extracted mentions must have
	// been literally present in the input (no LLM-hallucinated extras).
	NoHallucinatedMentions bool `json:"no_hallucinated_mentions"`
}

// LoadFixtures reads all fixtures from a JSON file.
func LoadFixtures(path string) ([]Fixture, error) {
	data, err := os.ReadFile(path) // #nosec G304 — test fixture path, not user input
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, err
	}
	return fixtures, nil
}

// FixturesDir returns the absolute path to the embedded fixtures directory.
func FixturesDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "fixtures"
	}
	return filepath.Join(filepath.Dir(file), "fixtures")
}
