package pipeline

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// StepContract declares what a step requires and produces.
// The canonical definition lives in pkg/pluginapi.
type StepContract = pluginapi.StepContract

// BaseContract provides a default Contract() implementation for embedding in step structs.
type BaseContract struct {
	contract StepContract
}

// Contract returns the step's declared contract.
func (b BaseContract) Contract() StepContract { return b.contract }

// NewBaseContract creates a BaseContract with the given contract.
func NewBaseContract(c StepContract) BaseContract { return BaseContract{contract: c} }

// SeedState is the set of keys guaranteed to exist when a pipeline begins.
var SeedState = []string{"RawContent", "Source", "Pipeline"}

// KeyAliases maps short names to canonical KnowledgeObject field names.
var KeyAliases = map[string]string{
	"text":         "RawContent",
	"content_type": "ContentType",
	"type":         "Type",
	"subtype":      "Subtype",
	"sections":     "Sections",
	"tags":         "Tags",
	"mentions":     "Mentions",
	"metadata":     "Metadata",
}

// Canonicalize resolves a key alias to its canonical KnowledgeObject field name.
// Non-alias keys (including dot-notation like "Metadata.ocr_confidence") pass through unchanged.
func Canonicalize(key string) string {
	// Only alias top-level keys, not dot-notation sub-keys.
	if !strings.Contains(key, ".") {
		if canon, ok := KeyAliases[key]; ok {
			return canon
		}
	}
	return key
}
