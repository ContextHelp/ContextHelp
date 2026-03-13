package autosuggest_test

import (
	"context"
	"testing"

	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM is a test double for providers.LLMProvider.
type mockLLM struct{ response string }

func (m *mockLLM) Generate(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

func TestSuggestTagsAndMentions_ParsesJSON(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:         "obj_001",
		RawContent: "We decided to adopt Go as the primary language for the project.",
		Summaries:  []string{"Architectural decision about language choice"},
	}
	cfg := autosuggest.DefaultConfig()
	cfg.MaxTags = 3
	cfg.MaxMentions = 2
	llm := &mockLLM{response: `{"tags":["go","architecture","decision"],"mentions":["eng.backend"]}`}

	tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, llm)
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "architecture", "decision"}, tags)
	assert.Equal(t, []string{"eng.backend"}, mentions)
}

func TestSuggestTagsAndMentions_MalformedJSON_ReturnsEmpty(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "obj_002", RawContent: "short"}
	cfg := autosuggest.DefaultConfig()
	llm := &mockLLM{response: "not json at all"}

	tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, llm)
	require.NoError(t, err) // soft failure: returns empty, not an error
	assert.Empty(t, tags)
	assert.Empty(t, mentions)
}

func TestSuggestTagsAndMentions_NilLLM_ReturnsEmpty(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "obj_003", RawContent: "some content"}
	cfg := autosuggest.DefaultConfig()

	tags, mentions, err := autosuggest.SuggestTagsAndMentions(context.Background(), obj, cfg, nil)
	require.NoError(t, err)
	assert.Empty(t, tags)
	assert.Empty(t, mentions)
}
