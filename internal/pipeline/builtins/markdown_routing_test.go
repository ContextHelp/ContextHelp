package builtins

import (
	"strings"
	"testing"
)

// TestSelectPipelineMarkdownStructure is the T-0575 regression suite.
// Structured markdown — bullet lists or headings — must route to a
// pipeline that includes a sectioner / markdown_parser-style step,
// even when the length test alone is borderline. These tests exercise
// the same Selector(...) pattern as TestSelectPipelineContentFallback
// (builtins_test.go:115/121) so the cause-of-failure path is identical
// to the user-reported bug.
func TestSelectPipelineMarkdownStructure(t *testing.T) {
	r := Registry()

	// Fixture 1: 50 bullet items, no headings. The whole blob is well
	// over 500 chars too, so this is the "obvious" case the original
	// bug report described.
	var bullets strings.Builder
	for i := 0; i < 50; i++ {
		bullets.WriteString("- bullet item with some content here\n")
	}
	bulletDoc := bullets.String()

	// Fixture 2: 1 heading + 4 bullets. Short total length so this
	// hits the "borderline length but structured" path. text.short's
	// ContentTest still matches, but text.long must win because it
	// has Priority 0 (text.short has Priority 1) and HasMarkdownStructure
	// returns true.
	headingDoc := "## Notes\n- one\n- two\n- three\n- four\n"

	cases := []struct {
		desc    string
		content string
	}{
		{"bullet-list markdown (>=5 bullets, no heading)", bulletDoc},
		{"heading + few bullets (short)", headingDoc},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := r.SelectPipeline("", tc.content)
			// Must route to a pipeline whose step list includes
			// sectioner OR markdown_parser. text.long is the
			// expected target post-fix.
			p, err := r.Get(got)
			if err != nil {
				t.Fatalf("registry.Get(%q): %v", got, err)
			}
			hasStructuredStep := false
			for _, s := range p.Steps {
				switch s.Name() {
				case "sectioner", "markdown_parser":
					hasStructuredStep = true
				}
			}
			if !hasStructuredStep {
				stepNames := make([]string, 0, len(p.Steps))
				for _, s := range p.Steps {
					stepNames = append(stepNames, s.Name())
				}
				t.Errorf("SelectPipeline(...) = %q whose steps %v lack sectioner/markdown_parser; want a structured-text pipeline (e.g. text.long)",
					got, stepNames)
			}
		})
	}
}

// TestSelectPipelineShortPlainTextStillRoutesToTextShort guards against
// over-eager promotion: plain prose with <500 chars and no markdown
// structure must keep routing to text.short after the fix.
func TestSelectPipelineShortPlainTextStillRoutesToTextShort(t *testing.T) {
	r := Registry()
	got := r.SelectPipeline("", "just a short note about today's meeting, nothing fancy here.")
	if got != "text.short" {
		t.Errorf("SelectPipeline(plain short text) = %q, want text.short", got)
	}
}
