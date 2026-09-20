package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// graphExtractorPrompt is the system prompt sent to the LLM.
// The LLM must return a single JSON object with the schema below.
const graphExtractorPrompt = `You are a knowledge extraction assistant. Given content, extract the following
in a single JSON response with these fields:
- summary: string — one-sentence summary of the content (required, non-empty)
- topics: []string — relevant topic/tag labels (0–10 items)
- decisions: []string — explicit decisions stated in the content (0–10 items)
- open_questions: []string — open questions raised but not answered (0–10 items)
- artifacts: []string — referenced artifacts, files, tools, or products (0–10 items)

Return ONLY valid JSON. Example:
{"summary":"...","topics":["go","testing"],"decisions":["Use SQLite"],"open_questions":["Which auth?"],"artifacts":["go.mod","Makefile"]}

Content:
`

// graphExtractResponse is the expected JSON structure from the LLM.
type graphExtractResponse struct {
	Summary       string   `json:"summary"`
	Topics        []string `json:"topics"`
	Decisions     []string `json:"decisions"`
	OpenQuestions []string `json:"open_questions"`
	Artifacts     []string `json:"artifacts"`
}

// GraphExtractor does a single LLM pass to emit typed graph nodes:
// summary, tags (topics), decisions, open questions, and artifacts.
// All nodes are connected to the KO root via EdgeTypeContains edges.
type GraphExtractor struct {
	pipeline.BaseContract
	llm providers.LLMProvider
}

// NewGraphExtractor creates a GraphExtractor without an LLM (no-op mode).
func NewGraphExtractor() *GraphExtractor {
	return &GraphExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent"},
			Produces:     []string{"Graph"},
			Capabilities: []string{"llm"},
		}),
	}
}

// NewGraphExtractorWithLLM creates a GraphExtractor that uses the given LLM provider.
func NewGraphExtractorWithLLM(llm providers.LLMProvider) *GraphExtractor {
	g := NewGraphExtractor()
	g.llm = llm
	return g
}

// Name returns the step identifier.
func (g *GraphExtractor) Name() string { return "graph_extractor" }

// Run performs the single LLM extraction pass and writes nodes/edges into ko.Graph.
// No-ops when: LLM is nil, or RawContent is empty.
func (g *GraphExtractor) Run(ctx context.Context, ko *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if g.llm == nil {
		return ko, nil
	}
	content := ko.RawContent
	if strings.TrimSpace(content) == "" {
		return ko, nil
	}
	// Truncate to keep prompt within reasonable bounds.
	if len(content) > 4000 {
		content = content[:4000]
	}

	raw, err := g.llm.Generate(ctx, graphExtractorPrompt+content)
	if err != nil {
		// Graph extraction is an enrichment pass over an already-valid
		// object. An unavailable or failing LLM must not fail ingestion,
		// so the object flows on without graph annotations.
		return ko, nil //nolint:nilerr // optional enrichment; LLM failure must not fail ingestion
	}

	var resp graphExtractResponse
	// Strip markdown code fences if the LLM wrapped the JSON.
	cleaned := strings.TrimSpace(raw)
	if idx := strings.Index(cleaned, "{"); idx > 0 {
		cleaned = cleaned[idx:]
	}
	if end := strings.LastIndex(cleaned, "}"); end >= 0 && end < len(cleaned)-1 {
		cleaned = cleaned[:end+1]
	}
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		// LLMs do not reliably emit valid JSON; an unparseable response is
		// an expected outcome, not a pipeline fault. Same degradation as
		// an outright generation failure.
		return ko, nil //nolint:nilerr // optional enrichment; unparseable LLM output must not fail ingestion
	}

	if ko.Graph == nil {
		ko.Graph = &pluginapi.ObjectGraph{}
	}

	// Root node ID: use KO ID when set, else a placeholder.
	rootID := ko.ID
	if rootID == "" {
		rootID = "root"
	}

	ordinal := len(ko.Graph.Nodes)

	// Helper to append a node + EdgeTypeContains from root.
	addNode := func(nodeType, label, content string) {
		nodeID := pluginapi.NewNodeID(rootID, nodeType, ordinal)
		if ko.Graph.FindNode(nodeID) != nil {
			return
		}
		ko.Graph.Nodes = append(ko.Graph.Nodes, pluginapi.GraphNode{
			ID:       nodeID,
			NodeType: nodeType,
			Label:    label,
			Content:  content,
			Order:    ordinal,
		})
		ko.Graph.Edges = append(ko.Graph.Edges, pluginapi.GraphEdge{
			ID:       fmt.Sprintf("%s->%s", rootID, nodeID),
			FromID:   rootID,
			ToID:     nodeID,
			EdgeType: pluginapi.EdgeTypeContains,
		})
		ordinal++
	}

	// Summary node.
	if resp.Summary != "" {
		addNode(pluginapi.NodeTypeSummary, resp.Summary, resp.Summary)
	}

	// Tag/topic nodes.
	for _, topic := range resp.Topics {
		t := strings.TrimSpace(topic)
		if t == "" {
			continue
		}
		addNode(pluginapi.NodeTypeTag, t, t)
	}

	// Decision nodes.
	for _, dec := range resp.Decisions {
		d := strings.TrimSpace(dec)
		if d == "" {
			continue
		}
		addNode(pluginapi.NodeTypeDecision, d, d)
	}

	// Open-question nodes.
	for _, q := range resp.OpenQuestions {
		oq := strings.TrimSpace(q)
		if oq == "" {
			continue
		}
		addNode(pluginapi.NodeTypeOpenQuestion, oq, oq)
	}

	// Artifact nodes.
	for _, art := range resp.Artifacts {
		a := strings.TrimSpace(art)
		if a == "" {
			continue
		}
		addNode(pluginapi.NodeTypeArtifact, a, a)
	}

	return ko, nil
}
