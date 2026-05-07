package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagger_Deterministic(t *testing.T) {
	ctx := context.Background()
	tagger := NewTagger()
	tagger.maxTags = 3

	// Content where "apple", "banana", "cherry", "date" all appear once.
	// Since maxTags is 3, one will be dropped.
	// Map iteration order in Go is random, so without deterministic tie-break,
	// the set of 3 tags will vary across runs.
	content := "apple banana cherry date"

	results := make(map[string]int)
	iterations := 100

	for i := 0; i < iterations; i++ {
		draft := &storage.KnowledgeObject{
			RawContent: content,
		}
		out, err := tagger.Run(ctx, draft)
		require.NoError(t, err)

		var labels []string
		for _, tag := range out.Tags {
			labels = append(labels, tag.Label)
		}
		key := ""
		for _, l := range labels {
			key += l + ","
		}
		results[key]++
	}

	// If it's deterministic, we should have only 1 unique result set
	assert.Equal(t, 1, len(results), "Expected deterministic tagging, but got multiple variations: %v", results)
}

// T-0573: tagger must merge with pre-existing user-source tags rather than
// overwriting them. User-asserted hints land on draft.Tags before this
// step runs (jobs.worker.parseUserHints / Service.Analyze raw path); they
// must survive the heuristic auto-extraction and win on lowercase-label
// collisions.
func TestTagger_MergesWithUserTags(t *testing.T) {
	ctx := context.Background()
	tagger := NewTagger()

	draft := &storage.KnowledgeObject{
		// "research" appears in both the user tags AND will be auto-extracted
		// from the content; the user tag must win the collision.
		RawContent: "research research research uxr design uxr design",
		Tags: []storage.Tag{
			{Label: "research", Source: "user", Weight: 1.0},
		},
	}

	out, err := tagger.Run(ctx, draft)
	require.NoError(t, err)

	// User-asserted "research" must be preserved with Source:"user" — not
	// overwritten by the auto-tagger's "research" entry.
	require.NotEmpty(t, out.Tags)
	assert.Equal(t, "research", out.Tags[0].Label, "user tag must come first")
	assert.Equal(t, "user", out.Tags[0].Source, "user tag must keep Source:user")

	// Verify "research" appears exactly once (collision dedupe).
	count := 0
	for _, tag := range out.Tags {
		if tag.Label == "research" {
			count++
		}
	}
	assert.Equal(t, 1, count, "research must appear exactly once after merge")

	// Auto-extracted-only labels (ux, design) must still be appended.
	labels := make(map[string]bool)
	for _, tag := range out.Tags {
		labels[tag.Label] = true
	}
	assert.True(t, labels["uxr"], "auto-extracted uxr must be present")
	assert.True(t, labels["design"], "auto-extracted design must be present")
}
