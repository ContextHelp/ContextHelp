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
