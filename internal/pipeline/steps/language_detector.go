package steps

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// languageByExt maps file extensions to programming language names.
var languageByExt = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".ts":   "typescript",
	".rs":   "rust",
	".java": "java",
	".rb":   "ruby",
	".cpp":  "cpp",
	".cc":   "cpp",
	".c":    "c",
	".cs":   "csharp",
	".php":  "php",
	".swift": "swift",
	".kt":   "kotlin",
	".scala": "scala",
	".sh":   "bash",
	".bash": "bash",
	".zsh":  "bash",
	".sql":  "sql",
	".html": "html",
	".css":  "css",
	".json": "json",
	".yaml": "yaml",
	".yml":  "yaml",
	".toml": "toml",
	".xml":  "xml",
	".md":   "markdown",
}

// LanguageDetector detects the programming language of a file by its extension.
type LanguageDetector struct {
	pipeline.BaseContract
}

// NewLanguageDetector creates a LanguageDetector.
func NewLanguageDetector() *LanguageDetector {
	return &LanguageDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{},
		}),
	}
}

func (s *LanguageDetector) Name() string { return "language_detector" }

func (s *LanguageDetector) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Try SourceURL first, then metadata["filename"].
	filename := draft.Source
	if fn, ok := draft.Metadata["filename"].(string); ok && fn != "" {
		filename = fn
	}

	ext := strings.ToLower(filepath.Ext(filename))
	lang, known := languageByExt[ext]
	if known {
		draft.Metadata["language"] = lang
	}

	// Emit canonical graph node: one NodeTypeTag for detected language.
	// Skip when draft.ID is empty (pre-ID pipeline drafts) or language unknown.
	if draft.ID != "" && known {
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		nodeID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeTag, 0)
		if draft.Graph.FindNode(nodeID) == nil {
			draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
				ID:       nodeID,
				NodeType: pluginapi.NodeTypeTag,
				Label:    "lang:" + lang,
				Order:    0,
				Metadata: map[string]any{
					"source": "language_detector",
					"lang":   lang,
				},
			})
		}
	}

	return draft, nil
}
