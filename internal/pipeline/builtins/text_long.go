package builtins

import "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"

// text.long routes any text with len >= 500 OR with markdown structure
// (heading or >=5 bullet items) to the heavy NLP pipeline that includes
// sectioner. The markdown-structure OR fixes T-0575: structured bullet
// lists were previously routed to text.short whenever the length test
// was borderline or the routing input was truncated upstream. Choosing
// to OR into text.long (instead of registering a separate
// markdown.structured pipeline) minimises selector-table churn — no new
// entry, just a broader predicate plus a Priority bump so structured
// content wins the tie against text.short when both ContentTests match.
func init() {
	MustRegister("text.long", Def{
		Description: "Long text pipeline (>= 500 chars, or structured markdown)",
		ContentTest: func(content string) bool {
			return len(content) >= 500 || steps.HasMarkdownStructure(content)
		},
		// Lower Priority value = higher priority. Default 0 here, paired
		// with text.short.Priority = 1 below, so text.long wins the tie
		// when both ContentTests match (a short structured markdown doc
		// satisfies len < 500 AND HasMarkdownStructure). Specific-payload
		// selectors like url.github.starred (Priority -1) still run first.
		Priority: 0,
		Steps:    []string{"typedetector", "sectioner", "tagger", "entity_extractor", "entity_resolver", "graph_extractor", "structured_metadata", "c12n_classify", "embedding"},
	})
}
