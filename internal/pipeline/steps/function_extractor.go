package steps

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// goFuncRe matches Go function signatures.
var goFuncRe = regexp.MustCompile(`(?m)^func\s+(\([^)]*\)\s+)?(\w+)\s*\(`)

// FunctionExtractor extracts function signatures from Go source code.
type FunctionExtractor struct {
	pipeline.BaseContract
}

// NewFunctionExtractor creates a FunctionExtractor.
func NewFunctionExtractor() *FunctionExtractor {
	return &FunctionExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *FunctionExtractor) Name() string { return "function_extractor" }

func (s *FunctionExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	lang, _ := draft.Metadata["language"].(string)
	if lang != "go" {
		// Only Go supported for now.
		return draft, nil
	}

	lines := strings.Split(draft.RawContent, "\n")
	var functions []string

	for _, line := range lines {
		if goFuncRe.MatchString(line) {
			sig := strings.TrimSpace(line)
			functions = append(functions, sig)

			draft.Sections = append(draft.Sections, storage.Section{
				Title:   sig,
				Content: sig,
				Order:   len(draft.Sections),
				Metadata: map[string]any{
					"type":     "function",
					"language": "go",
				},
			})
		}
	}

	draft.Metadata["function_count"] = len(functions)
	draft.Metadata["functions"] = functions

	return draft, nil
}
